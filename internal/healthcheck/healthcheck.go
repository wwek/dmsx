// Package healthcheck decides when a database is due for a liveness probe and
// when a run of probe results changes its status. One success makes a
// database available; it becomes unavailable only after a run of failures.
package healthcheck

import (
	"fmt"
	"time"
)

const (
	StatusUnknown     = ""
	StatusAvailable   = "AVAILABLE"
	StatusUnavailable = "UNAVAILABLE"
)

type Config struct {
	Enabled            bool `json:"enabled"`
	Notify             bool `json:"notify"`
	IntervalMinutes    int  `json:"intervalMinutes"`
	FailuresBeforeDown int  `json:"failuresBeforeDown"`
}

func Default() Config {
	return Config{Enabled: true, Notify: true, IntervalMinutes: 1, FailuresBeforeDown: 3}
}

func (c Config) Validate() error {
	if c.IntervalMinutes < 1 || c.IntervalMinutes > 24*60 {
		return fmt.Errorf("intervalMinutes must be 1–1440")
	}
	if c.FailuresBeforeDown < 1 || c.FailuresBeforeDown > 100 {
		return fmt.Errorf("failuresBeforeDown must be 1–100")
	}
	return nil
}

type Attempt struct {
	At     time.Time `json:"at"`
	Status string    `json:"status"`
	Error  string    `json:"error,omitempty"`
}

// State is what dmsx remembers about one database's probes. Attempts is newest
// first and capped at KeepAttempts.
type State struct {
	DatabaseID string    `json:"databaseId"`
	Status     string    `json:"status"`
	ChangedAt  time.Time `json:"changedAt"`
	CheckedAt  time.Time `json:"checkedAt"`
	Attempts   []Attempt `json:"attempts"`
}

const KeepAttempts = 120

func (c Config) Due(now time.Time, state State) bool {
	if !c.Enabled {
		return false
	}
	if state.CheckedAt.IsZero() {
		return true
	}
	return now.Sub(state.CheckedAt) >= time.Duration(c.IntervalMinutes)*time.Minute
}

// Record adds a probe result and reports the new status when it changed.
func (c Config) Record(state *State, attempt Attempt) (string, bool) {
	state.CheckedAt = attempt.At
	state.Attempts = append([]Attempt{attempt}, state.Attempts...)
	if len(state.Attempts) > KeepAttempts {
		state.Attempts = state.Attempts[:KeepAttempts]
	}
	next, changed := transition(state.Status, attempt.Status, c.FailuresBeforeDown, state.Attempts)
	if changed {
		state.Status = next
		state.ChangedAt = attempt.At
	}
	return next, changed
}

func transition(previous, current string, failuresBeforeDown int, recent []Attempt) (string, bool) {
	if previous == current {
		return previous, false
	}
	if current == StatusAvailable {
		return current, true
	}
	if failuresBeforeDown < 1 {
		failuresBeforeDown = 1
	}
	if len(recent) < failuresBeforeDown {
		return previous, false
	}
	for _, attempt := range recent[:failuresBeforeDown] {
		if attempt.Status != StatusUnavailable {
			return previous, false
		}
	}
	return current, true
}
