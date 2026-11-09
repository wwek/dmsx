package healthcheck

import (
	"testing"
	"time"
)

func TestUnavailableNeedsARunOfFailures(t *testing.T) {
	config := Config{Enabled: true, IntervalMinutes: 1, FailuresBeforeDown: 3}
	state := State{Status: StatusAvailable}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if _, changed := config.Record(&state, Attempt{At: start.Add(time.Duration(i) * time.Minute), Status: StatusUnavailable}); changed {
			t.Fatalf("changed after %d failures", i+1)
		}
	}
	next, changed := config.Record(&state, Attempt{At: start.Add(3 * time.Minute), Status: StatusUnavailable})
	if !changed || next != StatusUnavailable || state.Status != StatusUnavailable {
		t.Fatalf("got %q %v", next, changed)
	}
	next, changed = config.Record(&state, Attempt{At: start.Add(4 * time.Minute), Status: StatusAvailable})
	if !changed || next != StatusAvailable {
		t.Fatalf("one success should recover, got %q %v", next, changed)
	}
}

func TestInterleavedSuccessResetsTheRun(t *testing.T) {
	config := Config{Enabled: true, IntervalMinutes: 1, FailuresBeforeDown: 2}
	state := State{Status: StatusAvailable}
	now := time.Now()
	config.Record(&state, Attempt{At: now, Status: StatusUnavailable})
	config.Record(&state, Attempt{At: now, Status: StatusAvailable})
	if _, changed := config.Record(&state, Attempt{At: now, Status: StatusUnavailable}); changed {
		t.Fatal("a success between failures must restart the count")
	}
}

func TestFirstProbeSetsAvailable(t *testing.T) {
	config := Default()
	state := State{}
	next, changed := config.Record(&state, Attempt{At: time.Now(), Status: StatusAvailable})
	if !changed || next != StatusAvailable {
		t.Fatalf("got %q %v", next, changed)
	}
}

func TestAttemptsAreCapped(t *testing.T) {
	config := Default()
	state := State{}
	for i := 0; i < KeepAttempts+10; i++ {
		config.Record(&state, Attempt{At: time.Now(), Status: StatusAvailable})
	}
	if len(state.Attempts) != KeepAttempts {
		t.Fatalf("kept %d", len(state.Attempts))
	}
}

func TestDue(t *testing.T) {
	config := Config{Enabled: true, IntervalMinutes: 5, FailuresBeforeDown: 1}
	now := time.Now()
	if !config.Due(now, State{}) {
		t.Fatal("never probed should be due")
	}
	if config.Due(now, State{CheckedAt: now.Add(-4 * time.Minute)}) {
		t.Fatal("due too early")
	}
	if !config.Due(now, State{CheckedAt: now.Add(-5 * time.Minute)}) {
		t.Fatal("not due after interval")
	}
	config.Enabled = false
	if config.Due(now, State{}) {
		t.Fatal("disabled config is due")
	}
}
