// Package retention chooses which completed backups to keep.
// Policies match the three Databasus logical modes: a time window, a count,
// and grandfather-father-son (hourly, daily, weekly, monthly, yearly).
package retention

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Kind string

const (
	KindTime  Kind = "TIME_PERIOD"
	KindCount Kind = "COUNT"
	KindGFS   Kind = "GFS"
)

type Period string

const (
	PeriodDay     Period = "DAY"
	PeriodWeek    Period = "WEEK"
	PeriodMonth   Period = "MONTH"
	Period3Month  Period = "3_MONTH"
	Period6Month  Period = "6_MONTH"
	PeriodYear    Period = "YEAR"
	Period2Year   Period = "2_YEARS"
	Period3Year   Period = "3_YEARS"
	Period4Year   Period = "4_YEARS"
	Period5Year   Period = "5_YEARS"
	PeriodForever Period = "FOREVER"
)

type GFS struct {
	Hours  int `json:"hours"`
	Days   int `json:"days"`
	Weeks  int `json:"weeks"`
	Months int `json:"months"`
	Years  int `json:"years"`
}

type Policy struct {
	Kind   Kind   `json:"kind"`
	Period Period `json:"period"`
	Count  int    `json:"count"`
	GFS    GFS    `json:"gfs"`
}

type Item struct {
	ID        uuid.UUID
	CreatedAt time.Time
	Bytes     int64
}

// OverTotal returns the oldest items that push the stored total past maxBytes.
// The newest item is always kept, even when it alone is larger than the cap.
func OverTotal(maxBytes int64, items []Item) []uuid.UUID {
	if maxBytes <= 0 || len(items) == 0 {
		return nil
	}
	newest := append([]Item(nil), items...)
	sortNewest(newest)
	total := newest[0].Bytes
	var drop []uuid.UUID
	for _, item := range newest[1:] {
		total += item.Bytes
		if total > maxBytes {
			drop = append(drop, item.ID)
		}
	}
	return drop
}

func (p Policy) Validate() error {
	switch p.Kind {
	case KindTime:
		if !p.Period.valid() {
			return fmt.Errorf("unknown retention period %q", p.Period)
		}
	case KindCount:
		if p.Count <= 0 {
			return fmt.Errorf("retention count must be greater than 0")
		}
	case KindGFS:
		if p.GFS.Hours+p.GFS.Days+p.GFS.Weeks+p.GFS.Months+p.GFS.Years <= 0 {
			return fmt.Errorf("GFS retention must keep at least one period")
		}
	default:
		return fmt.Errorf("unknown retention kind %q", p.Kind)
	}
	return nil
}

func (p Period) valid() bool {
	switch p {
	case PeriodDay, PeriodWeek, PeriodMonth, Period3Month, Period6Month,
		PeriodYear, Period2Year, Period3Year, Period4Year, Period5Year, PeriodForever:
		return true
	default:
		return false
	}
}

// Duration is zero for FOREVER, which keeps every backup.
func (p Period) Duration() time.Duration {
	day := 24 * time.Hour
	switch p {
	case PeriodDay:
		return day
	case PeriodWeek:
		return 7 * day
	case PeriodMonth:
		return 30 * day
	case Period3Month:
		return 90 * day
	case Period6Month:
		return 180 * day
	case PeriodYear:
		return 365 * day
	case Period2Year:
		return 2 * 365 * day
	case Period3Year:
		return 3 * 365 * day
	case Period4Year:
		return 4 * 365 * day
	case Period5Year:
		return 5 * 365 * day
	default:
		return 0
	}
}

// Expired returns the items that fall outside the policy. now is the clock
// the caller passes in; the function does not read the wall clock itself.
func (p Policy) Expired(now time.Time, items []Item) []uuid.UUID {
	switch p.Kind {
	case KindCount:
		return expireCount(p.Count, items)
	case KindGFS:
		return expireGFS(p.GFS, items)
	default:
		return expireAge(now, p.Period.Duration(), items)
	}
}

func expireAge(now time.Time, window time.Duration, items []Item) []uuid.UUID {
	if window == 0 {
		return nil
	}
	var drop []uuid.UUID
	for _, item := range items {
		if now.Sub(item.CreatedAt) > window {
			drop = append(drop, item.ID)
		}
	}
	return drop
}

func expireCount(keep int, items []Item) []uuid.UUID {
	newest := append([]Item(nil), items...)
	sortNewest(newest)
	if len(newest) <= keep {
		return nil
	}
	drop := make([]uuid.UUID, 0, len(newest)-keep)
	for _, item := range newest[keep:] {
		drop = append(drop, item.ID)
	}
	return drop
}

type tier struct {
	name  string
	count int
	start func(time.Time) time.Time
	num   func(time.Time) int64
}

func expireGFS(g GFS, items []Item) []uuid.UUID {
	if len(items) == 0 {
		return nil
	}
	newest := append([]Item(nil), items...)
	sortNewest(newest)
	levels := []tier{
		{"hour", g.Hours, func(t time.Time) time.Time { return t.UTC().Truncate(time.Hour) }, func(t time.Time) int64 { return t.UTC().Unix() / 3600 }},
		{"day", g.Days, dayStart, func(t time.Time) int64 { return dayStart(t).Unix() / 86400 }},
		{"week", g.Weeks, weekStart, func(t time.Time) int64 { return weekStart(t).Unix() / 86400 }},
		{"month", g.Months, monthStart, func(t time.Time) int64 { return int64(t.UTC().Year())*12 + int64(t.UTC().Month()) }},
		{"year", g.Years, yearStart, func(t time.Time) int64 { return int64(t.UTC().Year()) }},
	}
	keep := map[uuid.UUID]bool{}
	for _, level := range levels {
		if level.count <= 0 {
			continue
		}
		reference := level.start(newest[0].CreatedAt)
		oldest := level.count
		if level.name == "hour" || level.name == "day" {
			oldest--
		}
		seen := map[time.Time]bool{}
		for _, item := range newest {
			start := level.start(item.CreatedAt)
			if level.num(reference)-level.num(start) > int64(oldest) {
				break
			}
			if seen[start] {
				continue
			}
			seen[start] = true
			keep[item.ID] = true
		}
	}
	var drop []uuid.UUID
	for _, item := range items {
		if !keep[item.ID] {
			drop = append(drop, item.ID)
		}
	}
	return drop
}

func sortNewest(items []Item) {
	for i := 1; i < len(items); i++ {
		j := i
		for j > 0 && items[j].CreatedAt.After(items[j-1].CreatedAt) {
			items[j], items[j-1] = items[j-1], items[j]
			j--
		}
	}
}

func dayStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func weekStart(t time.Time) time.Time {
	d := dayStart(t)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

func monthStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func yearStart(t time.Time) time.Time {
	return time.Date(t.UTC().Year(), time.January, 1, 0, 0, 0, 0, time.UTC)
}
