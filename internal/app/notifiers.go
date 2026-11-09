package app

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"dmsx/internal/notify"
)

func (a *App) CreateNotifier(ctx context.Context, in Notifier) (Notifier, error) {
	in.Type = strings.ToUpper(in.Type)
	if in.Name == "" {
		return Notifier{}, fmt.Errorf("name is required")
	}
	if _, err := notify.New(in.Type, in.Settings); err != nil {
		return Notifier{}, err
	}
	if len(in.Events) == 0 {
		return Notifier{}, fmt.Errorf("choose at least one event")
	}
	for _, event := range in.Events {
		if !notify.ValidEvent(event) {
			return Notifier{}, fmt.Errorf("unknown event %q", event)
		}
	}
	for _, id := range in.DatabaseIDs {
		if _, err := a.database(ctx, id); err != nil {
			return Notifier{}, err
		}
	}
	sealed, err := a.sealSettings(in.Settings, notify.SecretFields)
	if err != nil {
		return Notifier{}, err
	}
	in.ID = uuid.NewString()
	in.Settings = sealed
	in.LastSentAt = nil
	in.LastError = ""
	in.CreatedAt = time.Now().UTC()
	if in.DatabaseIDs == nil {
		in.DatabaseIDs = []string{}
	}
	if err := a.cat.Put(ctx, kindNotifier, in.ID, in); err != nil {
		return Notifier{}, err
	}
	a.Audit(ctx, "notifier.create", in.Name+" ("+in.Type+")")
	return a.publicNotifier(in)
}

func (a *App) ListNotifiers(ctx context.Context) ([]Notifier, error) {
	var rows []Notifier
	if err := a.cat.List(ctx, kindNotifier, &rows); err != nil {
		return nil, err
	}
	out := make([]Notifier, 0, len(rows))
	for _, row := range rows {
		public, err := a.publicNotifier(row)
		if err != nil {
			return nil, err
		}
		out = append(out, public)
	}
	return out, nil
}

func (a *App) DeleteNotifier(ctx context.Context, id string) error {
	var row Notifier
	if err := a.cat.Get(ctx, kindNotifier, id, &row); err != nil {
		return err
	}
	if err := a.cat.Delete(ctx, kindNotifier, id); err != nil {
		return err
	}
	a.Audit(ctx, "notifier.delete", row.Name)
	return nil
}

// TestNotifier sends a test message now and returns the delivery error.
func (a *App) TestNotifier(ctx context.Context, id string) error {
	var row Notifier
	if err := a.cat.Get(ctx, kindNotifier, id, &row); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return a.deliver(ctx, row, notify.Message{
		Event:   notify.EventTest,
		Heading: "dmsx 测试通知",
		Text:    "这条消息说明 “" + row.Name + "” 可以收到 dmsx 的通知。",
	})
}

func (a *App) publicNotifier(row Notifier) (Notifier, error) {
	opened, err := a.openSettings(row.Settings, notify.SecretFields)
	if err != nil {
		return Notifier{}, err
	}
	row.Settings = maskSettings(opened, notify.SecretFields)
	return row, nil
}

// notify delivers msg in the background to every notifier that wants this
// event for this database. A delivery failure is recorded on the notifier.
func (a *App) notify(databaseID string, msg notify.Message) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var rows []Notifier
		if err := a.cat.List(ctx, kindNotifier, &rows); err != nil {
			log.Printf("notify: %v", err)
			return
		}
		for _, row := range rows {
			if !slices.Contains(row.Events, string(msg.Event)) {
				continue
			}
			if len(row.DatabaseIDs) > 0 && !slices.Contains(row.DatabaseIDs, databaseID) {
				continue
			}
			if err := a.deliver(ctx, row, msg); err != nil {
				log.Printf("notify %s: %v", row.Name, err)
			}
		}
	}()
}

func (a *App) deliver(ctx context.Context, row Notifier, msg notify.Message) error {
	opened, err := a.openSettings(row.Settings, notify.SecretFields)
	if err != nil {
		return err
	}
	sender, err := notify.New(row.Type, opened)
	if err == nil {
		err = sender.Send(ctx, msg)
	}
	now := time.Now().UTC()
	var current Notifier
	if a.cat.Get(context.WithoutCancel(ctx), kindNotifier, row.ID, &current) == nil {
		current.LastSentAt = &now
		current.LastError = ""
		if err != nil {
			current.LastError = err.Error()
		}
		_ = a.cat.Put(context.WithoutCancel(ctx), kindNotifier, current.ID, current)
	}
	return err
}

func (a *App) databaseName(ctx context.Context, id string) string {
	var row Database
	if err := a.cat.Get(ctx, kindDatabase, id, &row); err != nil {
		return id
	}
	return row.Name
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
