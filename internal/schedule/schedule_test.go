package schedule

import (
	"testing"
	"time"
)

func TestHourly(t *testing.T) {
	s := Schedule{Type: TypeHourly}
	now := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	if !s.IsDue(now, nil) {
		t.Fatal("first run should be due")
	}
	last := now.Add(-59 * time.Minute)
	if s.IsDue(now, &last) {
		t.Fatal("59 minutes is early")
	}
	last = now.Add(-time.Hour)
	if !s.IsDue(now, &last) {
		t.Fatal("exactly one hour is due")
	}
}

func TestDailySlot(t *testing.T) {
	tod := "09:00"
	s := Schedule{Type: TypeDaily, TimeOfDay: &tod}
	now := time.Date(2024, 1, 15, 8, 59, 0, 0, time.UTC)
	last := time.Date(2024, 1, 14, 9, 0, 0, 0, time.UTC)
	if s.IsDue(now, &last) {
		t.Fatal("before the slot")
	}
	now = time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)
	if !s.IsDue(now, &last) {
		t.Fatal("at the slot")
	}
	last = now
	later := time.Date(2024, 1, 15, 15, 0, 0, 0, time.UTC)
	if s.IsDue(later, &last) {
		t.Fatal("already ran this slot")
	}
	evening := "21:00"
	late := Schedule{Type: TypeDaily, TimeOfDay: &evening}
	manual := time.Date(2025, 6, 6, 16, 17, 0, 0, time.UTC)
	slot := time.Date(2025, 6, 6, 21, 0, 0, 0, time.UTC)
	if !late.IsDue(slot, &manual) {
		t.Fatal("an earlier manual run does not consume the evening slot")
	}
}

func TestWeeklyAndMonthly(t *testing.T) {
	tod := "09:00"
	monday := 1
	week := Schedule{Type: TypeWeekly, TimeOfDay: &tod, Weekday: &monday}
	now := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC) // Monday
	last := time.Date(2024, 1, 8, 9, 0, 0, 0, time.UTC)
	if !week.IsDue(now, &last) {
		t.Fatal("weekly slot")
	}
	early := now.Add(-time.Minute)
	if week.IsDue(early, &last) {
		t.Fatal("one minute early")
	}

	day := 31
	month := Schedule{Type: TypeMonthly, TimeOfDay: &tod, DayOfMonth: &day}
	feb28 := time.Date(2024, 2, 28, 9, 0, 0, 0, time.UTC)
	jan := time.Date(2024, 1, 31, 9, 0, 0, 0, time.UTC)
	if month.IsDue(feb28, &jan) {
		t.Fatal("february should wait for the 29th in a leap year")
	}
	feb29 := time.Date(2024, 2, 29, 9, 0, 0, 0, time.UTC)
	if !month.IsDue(feb29, &jan) {
		t.Fatal("short month clamps to the last day")
	}
}

func TestCron(t *testing.T) {
	expr := "0 9 * * *"
	s := Schedule{Type: TypeCron, CronExpression: &expr}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	last := time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)
	if s.IsDue(time.Date(2024, 1, 16, 8, 59, 0, 0, time.UTC), &last) {
		t.Fatal("before the next cron fire")
	}
	if !s.IsDue(time.Date(2024, 1, 16, 9, 0, 0, 0, time.UTC), &last) {
		t.Fatal("at the next cron fire")
	}
	bad := "0 0 31 2 *"
	invalid := Schedule{Type: TypeCron, CronExpression: &bad}
	if err := invalid.Validate(); err == nil {
		t.Fatal("31 February must be rejected")
	}
}
