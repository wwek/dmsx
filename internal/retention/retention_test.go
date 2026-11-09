package retention

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCountAndAge(t *testing.T) {
	a, b, c := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	items := []Item{
		{ID: a, CreatedAt: now.Add(-time.Hour)},
		{ID: b, CreatedAt: now.Add(-48 * time.Hour)},
		{ID: c, CreatedAt: now.Add(-10 * 24 * time.Hour)},
	}
	drop := Policy{Kind: KindCount, Count: 2}.Expired(now, items)
	if len(drop) != 1 || drop[0] != c {
		t.Fatalf("count drop = %v", drop)
	}
	drop = Policy{Kind: KindTime, Period: PeriodWeek}.Expired(now, items)
	if len(drop) != 1 || drop[0] != c {
		t.Fatalf("age drop = %v", drop)
	}
	if got := (Policy{Kind: KindTime, Period: PeriodForever}).Expired(now, items); len(got) != 0 {
		t.Fatalf("forever dropped %v", got)
	}
}

func TestOverTotalDropsOldestPastTheCap(t *testing.T) {
	now := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	newest, middle, oldest := uuid.New(), uuid.New(), uuid.New()
	items := []Item{
		{ID: oldest, CreatedAt: now.Add(-3 * time.Hour), Bytes: 10},
		{ID: newest, CreatedAt: now, Bytes: 60},
		{ID: middle, CreatedAt: now.Add(-time.Hour), Bytes: 30},
	}
	drop := OverTotal(95, items)
	if len(drop) != 1 || drop[0] != oldest {
		t.Fatalf("drop = %v", drop)
	}
	drop = OverTotal(10, items)
	if len(drop) != 2 {
		t.Fatalf("newest must stay even when larger than the cap, drop = %v", drop)
	}
	if got := OverTotal(0, items); got != nil {
		t.Fatalf("no cap dropped %v", got)
	}
}

func TestGFSKeepsOnePerDay(t *testing.T) {
	now := time.Date(2024, 6, 3, 18, 0, 0, 0, time.UTC)
	keepToday := uuid.New()
	olderToday := uuid.New()
	yesterday := uuid.New()
	ancient := uuid.New()
	items := []Item{
		{ID: keepToday, CreatedAt: now},
		{ID: olderToday, CreatedAt: now.Add(-2 * time.Hour)},
		{ID: yesterday, CreatedAt: now.Add(-24 * time.Hour)},
		{ID: ancient, CreatedAt: now.Add(-10 * 24 * time.Hour)},
	}
	drop := Policy{Kind: KindGFS, GFS: GFS{Days: 2}}.Expired(now, items)
	gone := map[uuid.UUID]bool{}
	for _, id := range drop {
		gone[id] = true
	}
	if !gone[olderToday] || !gone[ancient] {
		t.Fatalf("drop = %v", drop)
	}
	if gone[keepToday] || gone[yesterday] {
		t.Fatalf("kept runs were dropped: %v", drop)
	}
}
