package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"dmsx/internal/engine"
	"dmsx/internal/healthcheck"
	"dmsx/internal/notify"
)

const probeSlots = 8

// Watch probes each database on its own interval until ctx ends.
func (a *App) Watch(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	inflight := map[string]bool{}
	done := make(chan string, 64)
	slots := make(chan struct{}, probeSlots)
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-done:
			delete(inflight, id)
		case <-ticker.C:
			var rows []Database
			if err := a.cat.List(ctx, kindDatabase, &rows); err != nil {
				continue
			}
			now := time.Now().UTC()
			for _, db := range rows {
				if inflight[db.ID] || !db.healthConfig().Due(now, a.healthState(ctx, db.ID)) {
					continue
				}
				inflight[db.ID] = true
				go func(id string) {
					slots <- struct{}{}
					a.probe(ctx, id)
					<-slots
					select {
					case done <- id:
					case <-ctx.Done():
					}
				}(db.ID)
			}
		}
	}
}

func (a *App) probe(ctx context.Context, id string) {
	db, err := a.database(ctx, id)
	if err != nil {
		return
	}
	probeCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	attempt := healthcheck.Attempt{Status: healthcheck.StatusAvailable}
	src, release, err := a.source(probeCtx, db, false)
	if err == nil {
		err = engine.Ping(probeCtx, src)
		release()
	}
	if ctx.Err() != nil {
		return
	}
	attempt.At = time.Now().UTC()
	if err != nil {
		attempt.Status = healthcheck.StatusUnavailable
		attempt.Error = clipText(err.Error(), 500)
	}

	a.healthMu.Lock()
	var current Database
	if a.cat.Get(ctx, kindDatabase, id, &current) != nil {
		a.healthMu.Unlock()
		return
	}
	config := current.healthConfig()
	state := a.healthState(ctx, id)
	previous := state.Status
	next, changed := config.Record(&state, attempt)
	_ = a.cat.Put(ctx, kindHealth, id, state)
	a.healthMu.Unlock()

	if !changed || !config.Notify || previous == healthcheck.StatusUnknown && next == healthcheck.StatusAvailable {
		return
	}
	if next == healthcheck.StatusUnavailable {
		a.notify(id, notify.Message{
			Event:   notify.EventHealthDown,
			Heading: "数据库不可用：" + current.Name,
			Text:    "连续 " + itoa(config.FailuresBeforeDown) + " 次探测失败。\n最近一次错误：" + attempt.Error,
		})
		return
	}
	a.notify(id, notify.Message{
		Event:   notify.EventHealthUp,
		Heading: "数据库已恢复：" + current.Name,
		Text:    "探测在 " + attempt.At.Format(time.RFC3339) + " 重新成功。",
	})
}

func (a *App) healthState(ctx context.Context, id string) healthcheck.State {
	var state healthcheck.State
	if a.cat.Get(ctx, kindHealth, id, &state) != nil {
		return healthcheck.State{DatabaseID: id}
	}
	return state
}

// ListHealth returns the probe state of every database with its newest
// attempts.
func (a *App) ListHealth(ctx context.Context) ([]healthcheck.State, error) {
	var rows []Database
	if err := a.cat.List(ctx, kindDatabase, &rows); err != nil {
		return nil, err
	}
	out := make([]healthcheck.State, 0, len(rows))
	for _, db := range rows {
		state := a.healthState(ctx, db.ID)
		if len(state.Attempts) > 30 {
			state.Attempts = state.Attempts[:30]
		}
		if state.Attempts == nil {
			state.Attempts = []healthcheck.Attempt{}
		}
		out = append(out, state)
	}
	return out, nil
}

func (a *App) SetHealthCheck(ctx context.Context, id string, config healthcheck.Config) (Database, error) {
	if err := config.Validate(); err != nil {
		return Database{}, err
	}
	a.healthMu.Lock()
	defer a.healthMu.Unlock()
	var db Database
	if err := a.cat.Get(ctx, kindDatabase, id, &db); err != nil {
		return Database{}, err
	}
	db.HealthCheck = &config
	if err := a.cat.Put(ctx, kindDatabase, id, db); err != nil {
		return Database{}, err
	}
	state := "off"
	if config.Enabled {
		state = "every " + itoa(config.IntervalMinutes) + " min, down after " + itoa(config.FailuresBeforeDown)
	}
	a.Audit(ctx, "database.healthcheck", db.Name+": "+state)
	return db.Public(), nil
}

func clipText(text string, n int) string {
	text = strings.TrimSpace(text)
	if len(text) <= n {
		return text
	}
	return text[:n] + "…"
}

func itoa(n int) string { return strconv.Itoa(n) }
