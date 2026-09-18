package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

// seedScheduleFixtures creates the workspace/profile/template rows a
// schedule's template_id foreign key needs, returning the template id.
func seedScheduleFixtures(t *testing.T, d *DB) string {
	t.Helper()
	seedTemplateFixtures(t, d)
	tpl := newTestTemplate("schedule-template")
	if err := NewTemplates(d).Create(context.Background(), tpl); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	return tpl.ID
}

func newTestSchedule(templateID, name string) domain.Schedule {
	now := time.Now().Truncate(time.Second)
	return domain.Schedule{
		ID:         uuid.NewString(),
		Name:       name,
		TemplateID: templateID,
		Cron:       "*/5 * * * *",
		Enabled:    true,
		Vars:       json.RawMessage(`{"foo":"bar"}`),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

func TestSchedules_CreateGetUpdateDelete(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedScheduleFixtures(t, database)
	schedules := NewSchedules(database)

	sc := newTestSchedule(tplID, "nightly report")
	if err := schedules.Create(ctx, sc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := schedules.Get(ctx, sc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != sc.Name || got.Cron != sc.Cron || string(got.Vars) != string(sc.Vars) || got.OwnerID != nil {
		t.Fatalf("Get = %+v, want matching %+v", got, sc)
	}
	if !got.Enabled {
		t.Fatalf("Get.Enabled = false, want true")
	}
	if got.NextRunAt != nil || got.LastRunAt != nil {
		t.Fatalf("Get next/last run at = %v/%v, want nil/nil", got.NextRunAt, got.LastRunAt)
	}

	sc.Name = "renamed"
	sc.Enabled = false
	sc.Vars = json.RawMessage(`{"foo":"baz"}`)
	sc.UpdatedAt = time.Now()
	if err := schedules.Update(ctx, sc); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err = schedules.Get(ctx, sc.ID)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got.Name != "renamed" || got.Enabled || string(got.Vars) != `{"foo":"baz"}` {
		t.Fatalf("Get after update = %+v", got)
	}

	if err := schedules.Delete(ctx, sc.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := schedules.Get(ctx, sc.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}
}

func TestSchedules_CreateDefaultsEmptyVars(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedScheduleFixtures(t, database)
	schedules := NewSchedules(database)

	sc := newTestSchedule(tplID, "no vars")
	sc.Vars = nil
	if err := schedules.Create(ctx, sc); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := schedules.Get(ctx, sc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Vars) != "{}" {
		t.Fatalf("Vars = %q, want %q", got.Vars, "{}")
	}
}

func TestSchedules_ListVisible(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedScheduleFixtures(t, database)
	seedTestUser(t, database, "u1")
	seedTestUser(t, database, "u2")
	schedules := NewSchedules(database)

	shared := newTestSchedule(tplID, "shared")
	u1sc := newTestSchedule(tplID, "u1-schedule")
	u1sc.OwnerID = strPtr("u1")
	u2sc := newTestSchedule(tplID, "u2-schedule")
	u2sc.OwnerID = strPtr("u2")
	for _, sc := range []domain.Schedule{shared, u1sc, u2sc} {
		if err := schedules.Create(ctx, sc); err != nil {
			t.Fatalf("Create %s: %v", sc.Name, err)
		}
	}

	list, err := schedules.ListVisible(ctx, "u1", false)
	if err != nil {
		t.Fatalf("ListVisible u1: %v", err)
	}
	names := map[string]bool{}
	for _, sc := range list {
		names[sc.Name] = true
	}
	if !names["shared"] || !names["u1-schedule"] || names["u2-schedule"] {
		t.Fatalf("ListVisible u1 = %+v, want shared and u1-schedule only", names)
	}

	adminList, err := schedules.ListVisible(ctx, "", true)
	if err != nil {
		t.Fatalf("ListVisible admin: %v", err)
	}
	if len(adminList) != 3 {
		t.Fatalf("ListVisible admin len = %d, want 3", len(adminList))
	}
}

func TestSchedules_ListDue(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedScheduleFixtures(t, database)
	schedules := NewSchedules(database)

	now := time.Now().Truncate(time.Second)
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)

	due := newTestSchedule(tplID, "due")
	dueDisabled := newTestSchedule(tplID, "due-disabled")
	dueDisabled.Enabled = false
	notDue := newTestSchedule(tplID, "not-due")
	noNext := newTestSchedule(tplID, "no-next")

	for _, sc := range []domain.Schedule{due, dueDisabled, notDue, noNext} {
		if err := schedules.Create(ctx, sc); err != nil {
			t.Fatalf("Create %s: %v", sc.Name, err)
		}
	}
	if err := schedules.SetNextRun(ctx, due.ID, &past); err != nil {
		t.Fatalf("SetNextRun due: %v", err)
	}
	if err := schedules.SetNextRun(ctx, dueDisabled.ID, &past); err != nil {
		t.Fatalf("SetNextRun due-disabled: %v", err)
	}
	if err := schedules.SetNextRun(ctx, notDue.ID, &future); err != nil {
		t.Fatalf("SetNextRun not-due: %v", err)
	}
	// noNext keeps next_run_at NULL.

	list, err := schedules.ListDue(ctx, now)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(list) != 1 || list[0].ID != due.ID {
		t.Fatalf("ListDue = %+v, want only %q", list, due.ID)
	}
}

func TestSchedules_SetNextRunAndRecordRun(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	tplID := seedScheduleFixtures(t, database)
	schedules := NewSchedules(database)

	sc := newTestSchedule(tplID, "record")
	if err := schedules.Create(ctx, sc); err != nil {
		t.Fatalf("Create: %v", err)
	}

	next := time.Now().Add(5 * time.Minute).Truncate(time.Second)
	if err := schedules.SetNextRun(ctx, sc.ID, &next); err != nil {
		t.Fatalf("SetNextRun: %v", err)
	}
	got, err := schedules.Get(ctx, sc.ID)
	if err != nil {
		t.Fatalf("Get after SetNextRun: %v", err)
	}
	if got.NextRunAt == nil || !got.NextRunAt.Equal(next.UTC()) {
		t.Fatalf("NextRunAt = %v, want %v", got.NextRunAt, next)
	}

	if err := schedules.SetNextRun(ctx, sc.ID, nil); err != nil {
		t.Fatalf("SetNextRun nil: %v", err)
	}
	got, err = schedules.Get(ctx, sc.ID)
	if err != nil {
		t.Fatalf("Get after SetNextRun nil: %v", err)
	}
	if got.NextRunAt != nil {
		t.Fatalf("NextRunAt = %v, want nil", got.NextRunAt)
	}

	at := time.Now().Truncate(time.Second)
	if err := schedules.RecordRun(ctx, sc.ID, at, "started"); err != nil {
		t.Fatalf("RecordRun: %v", err)
	}
	got, err = schedules.Get(ctx, sc.ID)
	if err != nil {
		t.Fatalf("Get after RecordRun: %v", err)
	}
	if got.LastRunAt == nil || !got.LastRunAt.Equal(at.UTC()) || got.LastOutcome != "started" {
		t.Fatalf("Get after RecordRun = %+v, want LastRunAt %v LastOutcome started", got, at)
	}

	if err := schedules.SetNextRun(ctx, "no-such-id", &next); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SetNextRun unknown id: err = %v, want ErrNotFound", err)
	}
	if err := schedules.RecordRun(ctx, "no-such-id", at, "started"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("RecordRun unknown id: err = %v, want ErrNotFound", err)
	}
}
