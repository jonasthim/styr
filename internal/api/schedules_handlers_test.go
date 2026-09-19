package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/schedules"
)

type scheduleOut struct {
	ID          string          `json:"id"`
	OwnerID     *string         `json:"owner_id"`
	Name        string          `json:"name"`
	TemplateID  string          `json:"template_id"`
	PipelineID  *string         `json:"pipeline_id"`
	Cron        string          `json:"cron"`
	Enabled     bool            `json:"enabled"`
	Vars        json.RawMessage `json:"vars"`
	LastRunAt   *time.Time      `json:"last_run_at"`
	LastOutcome string          `json:"last_outcome"`
	NextRunAt   *time.Time      `json:"next_run_at"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// runIDOut is the {run_id} shape every "start a run" endpoint answers with
// (POST /schedules/{id}/run, POST /templates/{id}/run): shared here since
// both templates_handlers_test.go and this file assert on it.
type runIDOut struct {
	RunID string `json:"run_id"`
}

type scheduleFiringOut struct {
	ID         string    `json:"id"`
	ScheduleID string    `json:"schedule_id"`
	FiredAt    time.Time `json:"fired_at"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason"`
	RunID      *string   `json:"run_id"`
}

func sampleSchedule() domain.Schedule {
	now := time.Now()
	next := now.Add(time.Hour)
	return domain.Schedule{
		ID: "sched-1", Name: "nightly", TemplateID: "tmpl-1", Cron: "0 2 * * *", Enabled: true,
		Vars: json.RawMessage(`{"a":1}`), LastOutcome: "started", NextRunAt: &next,
		CreatedAt: now, UpdatedAt: now,
	}
}

func TestSchedulesCreate_OK(t *testing.T) {
	e := newEnv(t)
	e.schedules.CreateFn = func(_ context.Context, _ api.Actor, in domain.ScheduleInput) (domain.Schedule, error) {
		sc := sampleSchedule()
		sc.Name = in.Name
		sc.Cron = in.Cron
		return sc, nil
	}

	var out scheduleOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/schedules", map[string]any{
		"name": "nightly", "template_id": "tmpl-1", "cron": "0 2 * * *",
	}, &out)
	if status != http.StatusCreated {
		t.Fatalf("POST /schedules = %d, want 201", status)
	}
	if out.ID != "sched-1" || out.Name != "nightly" || out.Cron != "0 2 * * *" {
		t.Fatalf("out = %+v", out)
	}
}

func TestSchedulesCreate_BothTemplateAndPipelineIs422(t *testing.T) {
	e := newEnv(t)
	e.schedules.CreateFn = func(context.Context, api.Actor, domain.ScheduleInput) (domain.Schedule, error) {
		t.Fatal("Create must not be called when both template_id and pipeline_id are set")
		return domain.Schedule{}, nil
	}
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/schedules", map[string]any{
		"name": "nightly", "template_id": "tmpl-1", "pipeline_id": "pipe-1", "cron": "0 2 * * *",
	}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
	if body.Error.Code != "invalid" {
		t.Fatalf("error code = %q, want invalid", body.Error.Code)
	}
}

func TestSchedulesCreate_NeitherTemplateNorPipelineIs422(t *testing.T) {
	e := newEnv(t)
	e.schedules.CreateFn = func(context.Context, api.Actor, domain.ScheduleInput) (domain.Schedule, error) {
		t.Fatal("Create must not be called when neither template_id nor pipeline_id is set")
		return domain.Schedule{}, nil
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/schedules", map[string]any{
		"name": "nightly", "cron": "0 2 * * *",
	}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
}

func TestSchedulesCreate_PipelineIDOnly_OK(t *testing.T) {
	e := newEnv(t)
	e.schedules.CreateFn = func(_ context.Context, _ api.Actor, in domain.ScheduleInput) (domain.Schedule, error) {
		if in.TemplateID != "" {
			t.Fatalf("template_id = %q, want empty", in.TemplateID)
		}
		if in.PipelineID != "pipe-1" {
			t.Fatalf("pipeline_id = %v, want pipe-1", in.PipelineID)
		}
		sc := sampleSchedule()
		sc.TemplateID = ""
		pid := in.PipelineID
		sc.PipelineID = &pid
		return sc, nil
	}
	var out scheduleOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/schedules", map[string]any{
		"name": "nightly", "pipeline_id": "pipe-1", "cron": "0 2 * * *",
	}, &out)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", status)
	}
	if out.PipelineID == nil || *out.PipelineID != "pipe-1" {
		t.Fatalf("pipeline_id = %v, want pipe-1", out.PipelineID)
	}
}

func TestSchedulesList_OK(t *testing.T) {
	e := newEnv(t)
	e.schedules.ListFn = func(context.Context, api.Actor) ([]domain.Schedule, error) {
		return []domain.Schedule{sampleSchedule()}, nil
	}
	var out []scheduleOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/schedules", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /schedules = %d, want 200", status)
	}
	if len(out) != 1 || out[0].ID != "sched-1" {
		t.Fatalf("out = %+v", out)
	}
}

func TestSchedulesGet_NotFound(t *testing.T) {
	e := newEnv(t)
	// GetFn left unset: the fake's default returns domain.ErrNotFound.
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/schedules/missing", nil, &body)
	if status != http.StatusNotFound {
		t.Fatalf("GET missing schedule = %d, want 404", status)
	}
}

func TestSchedulesPatch_ReplacesConfiguration(t *testing.T) {
	e := newEnv(t)
	e.schedules.UpdateFn = func(_ context.Context, _ api.Actor, id string, in domain.ScheduleInput) (domain.Schedule, error) {
		sc := sampleSchedule()
		sc.ID = id
		sc.Enabled = in.Enabled == nil || *in.Enabled
		sc.Cron = in.Cron
		return sc, nil
	}
	var out scheduleOut
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/schedules/sched-1", map[string]any{
		"name": "nightly", "template_id": "tmpl-1", "cron": "*/5 * * * *", "enabled": false,
	}, &out)
	if status != http.StatusOK {
		t.Fatalf("PATCH /schedules/sched-1 = %d, want 200", status)
	}
	if out.Enabled || out.Cron != "*/5 * * * *" {
		t.Fatalf("out = %+v, want enabled=false cron=*/5 * * * *", out)
	}
}

func TestSchedulesDelete_OK(t *testing.T) {
	e := newEnv(t)
	e.schedules.DeleteFn = func(context.Context, api.Actor, string) error { return nil }
	status := e.doJSON(e.adminClient, http.MethodDelete, "/api/v1/schedules/sched-1", nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE /schedules/sched-1 = %d, want 204", status)
	}
}

func TestSchedulesRun_Returns202WithRunID(t *testing.T) {
	e := newEnv(t)
	e.schedules.RunNowFn = func(context.Context, api.Actor, string) (domain.Run, error) {
		return domain.Run{ID: "run-1"}, nil
	}
	var out runIDOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/schedules/sched-1/run", nil, &out)
	if status != http.StatusAccepted {
		t.Fatalf("POST /schedules/sched-1/run = %d, want 202", status)
	}
	if out.RunID != "run-1" {
		t.Fatalf("run_id = %q, want run-1", out.RunID)
	}
}

func TestSchedulesFirings_DefaultAndExplicitLimit(t *testing.T) {
	e := newEnv(t)
	e.schedules.FiringsFn = func(context.Context, api.Actor, string, int) ([]domain.ScheduleFiring, error) {
		runID := "run-1"
		return []domain.ScheduleFiring{{
			ID: "fire-1", ScheduleID: "sched-1", FiredAt: time.Now(), Status: domain.FiringStarted, RunID: &runID,
		}}, nil
	}

	var out []scheduleFiringOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/schedules/sched-1/firings", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET firings = %d, want 200", status)
	}
	if len(out) != 1 || out[0].Status != "started" || out[0].RunID == nil || *out[0].RunID != "run-1" {
		t.Fatalf("out = %+v", out)
	}
	call, ok := e.schedules.lastCall()
	if !ok || call.method != "Firings" || call.args[1] != 0 {
		t.Fatalf("last call = %+v, ok=%v, want Firings with limit 0 (service default)", call, ok)
	}

	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/schedules/sched-1/firings?limit=5", nil, nil); status != http.StatusOK {
		t.Fatalf("GET firings?limit=5 = %d, want 200", status)
	}
	call, _ = e.schedules.lastCall()
	if call.args[1] != 5 {
		t.Fatalf("limit passed through = %v, want 5", call.args[1])
	}
}

func TestSchedulesPreview_OK(t *testing.T) {
	e := newEnv(t)
	e.schedules.PreviewFn = func(cronExpr string) (schedules.Preview, error) {
		if cronExpr != "*/5 * * * *" {
			t.Fatalf("cron passed to Preview = %q", cronExpr)
		}
		base := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
		next := make([]time.Time, 5)
		for i := range next {
			next[i] = base.Add(time.Duration(i+1) * 5 * time.Minute)
		}
		return schedules.Preview{Next: next, Description: "every 5 minutes"}, nil
	}

	var out struct {
		Next        []string `json:"next"`
		Description string   `json:"description"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/schedules/preview", map[string]any{"cron": "*/5 * * * *"}, &out)
	if status != http.StatusOK {
		t.Fatalf("POST /schedules/preview = %d, want 200", status)
	}
	if len(out.Next) != 5 {
		t.Fatalf("next = %v, want 5 timestamps", out.Next)
	}
	for _, ts := range out.Next {
		if _, err := time.Parse(time.RFC3339, ts); err != nil {
			t.Fatalf("next entry %q is not RFC3339: %v", ts, err)
		}
	}
	if out.Description != "every 5 minutes" {
		t.Fatalf("description = %q", out.Description)
	}
}

func TestSchedulesPreview_InvalidCronIs422(t *testing.T) {
	e := newEnv(t)
	e.schedules.PreviewFn = func(cronExpr string) (schedules.Preview, error) {
		return schedules.Preview{}, domain.ErrInvalid
	}

	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/schedules/preview", map[string]any{"cron": "not a cron"}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("POST /schedules/preview (bad cron) = %d, want 422", status)
	}
	if body.Error.Code != "invalid_cron" {
		t.Fatalf("error code = %q, want invalid_cron", body.Error.Code)
	}
}

// TestSchedulesGet_JSONNeverExposesMoreThanDocumentedFields guards the
// contract's field list (id, owner_id, name, template_id, cron, enabled,
// vars, last_run_at, last_outcome, next_run_at, created_at, updated_at):
// nothing else should ever appear in a schedule's JSON, now or in a future
// change to domain.Schedule.
func TestSchedulesGet_JSONNeverExposesMoreThanDocumentedFields(t *testing.T) {
	e := newEnv(t)
	e.schedules.GetFn = func(context.Context, api.Actor, string) (domain.Schedule, error) {
		return sampleSchedule(), nil
	}

	_, raw := e.doJSONHeaders(e.adminClient, http.MethodGet, "/api/v1/schedules/sched-1", nil, nil, true)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("unmarshal: %v, body=%s", err, raw)
	}
	want := map[string]bool{
		"id": true, "owner_id": true, "name": true, "template_id": true, "pipeline_id": true, "cron": true, "enabled": true,
		"vars": true, "last_run_at": true, "last_outcome": true, "next_run_at": true,
		"created_at": true, "updated_at": true,
	}
	if len(fields) != len(want) {
		t.Fatalf("got %d fields %v, want exactly %v", len(fields), keysOf(fields), want)
	}
	for k := range fields {
		if !want[k] {
			t.Fatalf("unexpected field %q in schedule JSON: %s", k, raw)
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
