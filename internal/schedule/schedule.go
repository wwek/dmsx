// Package schedule decides when a recurring backup is due.
//
// The rules follow Databasus (Apache-2.0): hourly, daily, weekly and monthly
// slots, plus a five-field cron expression. A weekly or monthly slot that was
// missed is caught up only inside its own week or month. A cron expression
// that can never fire, such as 31 February, is rejected.
package schedule

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

type Type string

const (
	TypeHourly  Type = "HOURLY"
	TypeDaily   Type = "DAILY"
	TypeWeekly  Type = "WEEKLY"
	TypeMonthly Type = "MONTHLY"
	TypeCron    Type = "CRON"
)

// Schedule is stored in UTC. Weekday uses 0 and 7 for Sunday. DayOfMonth is
// 1–31; a month that is shorter runs on its last day.
type Schedule struct {
	Type           Type    `json:"type"`
	TimeOfDay      *string `json:"timeOfDay,omitempty"`
	Weekday        *int    `json:"weekday,omitempty"`
	DayOfMonth     *int    `json:"dayOfMonth,omitempty"`
	CronExpression *string `json:"cronExpression,omitempty"`
}

const timeLayout = "15:04"

func (s Schedule) Validate() error {
	switch s.Type {
	case TypeHourly, TypeDaily, TypeWeekly, TypeMonthly, TypeCron:
	default:
		return fmt.Errorf("unknown schedule type %q", s.Type)
	}
	if s.Type == TypeDaily || s.Type == TypeWeekly || s.Type == TypeMonthly {
		if s.TimeOfDay == nil {
			return fmt.Errorf("timeOfDay is required")
		}
		if _, err := time.Parse(timeLayout, *s.TimeOfDay); err != nil {
			return fmt.Errorf("timeOfDay must be HH:MM")
		}
	}
	if s.Type == TypeWeekly {
		if s.Weekday == nil || *s.Weekday < 0 || *s.Weekday > 7 {
			return fmt.Errorf("weekday must be 0–7")
		}
	}
	if s.Type == TypeMonthly {
		if s.DayOfMonth == nil || *s.DayOfMonth < 1 || *s.DayOfMonth > 31 {
			return fmt.Errorf("dayOfMonth must be 1–31")
		}
	}
	if s.Type == TypeCron {
		if s.CronExpression == nil || *s.CronExpression == "" {
			return fmt.Errorf("cronExpression is required")
		}
		if _, err := parseCron(*s.CronExpression); err != nil {
			return fmt.Errorf("cronExpression: %w", err)
		}
		if s.cronNeverRuns() {
			return fmt.Errorf("cronExpression has no date it can run on")
		}
	}
	return nil
}

// IsDue reports whether a run is due at now. A schedule that has never run is
// due immediately, except a cron expression that can never fire.
func (s Schedule) IsDue(now time.Time, lastRun *time.Time) bool {
	if s.cronNeverRuns() {
		return false
	}
	if lastRun == nil {
		return true
	}
	switch s.Type {
	case TypeHourly:
		return now.Sub(*lastRun) >= time.Hour
	case TypeDaily:
		return s.dailyDue(now, *lastRun)
	case TypeWeekly:
		return s.weeklyDue(now, *lastRun)
	case TypeMonthly:
		return s.monthlyDue(now, *lastRun)
	case TypeCron:
		return s.cronDue(now, *lastRun)
	default:
		return false
	}
}

func (s Schedule) dailyDue(now, lastRun time.Time) bool {
	if s.TimeOfDay == nil {
		return !sameDay(lastRun, now)
	}
	slot, err := s.clock(now)
	if err != nil {
		return false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), slot.Hour(), slot.Minute(), 0, 0, now.Location())
	lastSlot := today
	if now.Before(today) {
		lastSlot = today.AddDate(0, 0, -1)
	}
	return !now.Before(lastSlot) && lastRun.Before(lastSlot)
}

func (s Schedule) weeklyDue(now, lastRun time.Time) bool {
	if s.Weekday == nil {
		return now.Sub(lastRun) >= 7*24*time.Hour
	}
	daysFromMonday := int(time.Weekday(*s.Weekday)) - 1
	if time.Weekday(*s.Weekday%7) == time.Sunday {
		daysFromMonday = 6
	}
	slot := s.at(startOfWeek(now).AddDate(0, 0, daysFromMonday))
	if now.Before(slot) {
		return false
	}
	return lastRun.Before(slot)
}

func (s Schedule) monthlyDue(now, lastRun time.Time) bool {
	if s.DayOfMonth == nil {
		return lastRun.Before(time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()))
	}
	day := *s.DayOfMonth
	if dim := daysInMonth(now); day > dim {
		day = dim
	}
	slot := s.at(time.Date(now.Year(), now.Month(), day, 0, 0, 0, 0, now.Location()))
	if now.Before(slot) {
		return false
	}
	return lastRun.Before(slot)
}

func (s Schedule) cronDue(now, lastRun time.Time) bool {
	if s.CronExpression == nil {
		return false
	}
	parsed, err := parseCron(*s.CronExpression)
	if err != nil {
		return false
	}
	return !now.Before(parsed.Next(lastRun))
}

func (s Schedule) cronNeverRuns() bool {
	if s.Type != TypeCron || s.CronExpression == nil {
		return false
	}
	parsed, err := parseCron(*s.CronExpression)
	if err != nil {
		return false
	}
	// 2024 is a leap year, so 29 February is inside the parser's search window.
	start := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	return parsed.Next(start).IsZero()
}

func (s Schedule) at(day time.Time) time.Time {
	clock, err := s.clock(day)
	if err != nil {
		return day
	}
	return time.Date(day.Year(), day.Month(), day.Day(), clock.Hour(), clock.Minute(), 0, 0, day.Location())
}

func (s Schedule) clock(time.Time) (time.Time, error) {
	if s.TimeOfDay == nil {
		return time.Time{}, fmt.Errorf("missing time")
	}
	return time.Parse(timeLayout, *s.TimeOfDay)
}

func parseCron(expression string) (cron.Schedule, error) {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	return parser.Parse(expression)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

func startOfWeek(moment time.Time) time.Time {
	sinceSunday := int(moment.Weekday())
	if sinceSunday == 0 {
		sinceSunday = 7
	}
	return time.Date(moment.Year(), moment.Month(), moment.Day()-sinceSunday+1, 0, 0, 0, 0, moment.Location())
}

func daysInMonth(moment time.Time) int {
	return time.Date(moment.Year(), moment.Month()+1, 0, 0, 0, 0, 0, moment.Location()).Day()
}
