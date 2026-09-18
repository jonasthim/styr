package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

func newTestScheduleFiring(scheduleID string, status domain.ScheduleFiringStatus, at time.Time) domain.ScheduleFiring {
	return domain.ScheduleFiring{
		ID:         uuid.NewString(),
		ScheduleID: scheduleID,
		FiredAt:    at,
		Status:     status,
	}
}

func TestScheduleFirings_CreateAndListBySchedule(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedScheduleFixtures(t, database)
	sc := newTestSchedule(tplID, "firing-schedule")
	if err := NewSchedules(database).Create(ctx, sc); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	firings := NewScheduleFirings(database)

	base := time.Now().Truncate(time.Second)
	f1 := newTestScheduleFiring(sc.ID, domain.FiringSkippedOverlap, base)
	f1.Reason = "previous run still running"
	f2 := newTestScheduleFiring(sc.ID, domain.FiringStarted, base.Add(time.Minute))
	runID := "run-1"
	f2.RunID = &runID
	f3 := newTestScheduleFiring(sc.ID, domain.FiringFailed, base.Add(2*time.Minute))
	f3.Reason = "boom"

	for _, f := range []domain.ScheduleFiring{f1, f2, f3} {
		if err := firings.Create(ctx, f); err != nil {
			t.Fatalf("Create %s: %v", f.ID, err)
		}
	}

	list, err := firings.ListBySchedule(ctx, sc.ID, 10)
	if err != nil {
		t.Fatalf("ListBySchedule: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ListBySchedule len = %d, want 3", len(list))
	}
	// Newest first.
	if list[0].ID != f3.ID || list[1].ID != f2.ID || list[2].ID != f1.ID {
		t.Fatalf("ListBySchedule order = %+v, want f3, f2, f1", list)
	}
	if list[0].Status != domain.FiringFailed || list[0].Reason != "boom" {
		t.Fatalf("list[0] = %+v, want failed/boom", list[0])
	}
	if list[1].RunID == nil || *list[1].RunID != "run-1" {
		t.Fatalf("list[1].RunID = %v, want run-1", list[1].RunID)
	}
	if list[2].Status != domain.FiringSkippedOverlap {
		t.Fatalf("list[2].Status = %v, want skipped_overlap", list[2].Status)
	}

	limited, err := firings.ListBySchedule(ctx, sc.ID, 2)
	if err != nil {
		t.Fatalf("ListBySchedule limited: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("ListBySchedule limited len = %d, want 2", len(limited))
	}
}

func TestScheduleFirings_LastStartedRunID(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedScheduleFixtures(t, database)
	sc := newTestSchedule(tplID, "overlap-schedule")
	if err := NewSchedules(database).Create(ctx, sc); err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
	firings := NewScheduleFirings(database)

	if _, err := firings.LastStartedRunID(ctx, sc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("LastStartedRunID before any firing: err = %v, want ErrNotFound", err)
	}

	base := time.Now().Truncate(time.Second)
	skipped := newTestScheduleFiring(sc.ID, domain.FiringSkippedOverlap, base)
	if err := firings.Create(ctx, skipped); err != nil {
		t.Fatalf("Create skipped: %v", err)
	}
	if _, err := firings.LastStartedRunID(ctx, sc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("LastStartedRunID with only a skip: err = %v, want ErrNotFound", err)
	}

	older := newTestScheduleFiring(sc.ID, domain.FiringStarted, base.Add(time.Minute))
	olderRun := "run-old"
	older.RunID = &olderRun
	if err := firings.Create(ctx, older); err != nil {
		t.Fatalf("Create older started: %v", err)
	}
	newer := newTestScheduleFiring(sc.ID, domain.FiringStarted, base.Add(2*time.Minute))
	newerRun := "run-new"
	newer.RunID = &newerRun
	if err := firings.Create(ctx, newer); err != nil {
		t.Fatalf("Create newer started: %v", err)
	}

	got, err := firings.LastStartedRunID(ctx, sc.ID)
	if err != nil {
		t.Fatalf("LastStartedRunID: %v", err)
	}
	if got != "run-new" {
		t.Fatalf("LastStartedRunID = %q, want run-new", got)
	}
}
