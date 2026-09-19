package schedules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/templates"
)

var (
	admin  = Actor{UserID: "u-admin", IsAdmin: true}
	member = Actor{UserID: "u-member"}
	other  = Actor{UserID: "u-other"}
)

// base is the fixed instant tests build their timelines around. Whole
// seconds only: stored timestamps are RFC3339Nano, which trims trailing
// zeros, so sub-second values do not order correctly as strings.
var base = time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC)

// fakeStarter stands in for the run engine: it records what was asked to
// start and can be made to fail or to hand back a run in a chosen outcome.
type fakeStarter struct {
	mu      sync.Mutex
	inputs  []RunInput
	err     error
	nextID  int
	outcome domain.RunOutcome // outcome new runs are created with; default RunRunning
}

func (f *fakeStarter) Start(_ context.Context, in RunInput) (domain.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return domain.Run{}, f.err
	}
	f.nextID++
	outcome := f.outcome
	if outcome == "" {
		outcome = domain.RunRunning
	}
	return domain.Run{ID: fmt.Sprintf("run-%d", f.nextID), Outcome: outcome}, nil
}

func (f *fakeStarter) calls() []RunInput {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]RunInput(nil), f.inputs...)
}

// fakeLookup stands in for the run engine's Get, letting tests control
// whether a run reads back as still running.
type fakeLookup struct {
	mu   sync.Mutex
	runs map[string]domain.RunView
}

func (f *fakeLookup) Get(_ context.Context, id string) (domain.RunView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.runs[id]
	if !ok {
		return domain.RunView{}, domain.ErrNotFound
	}
	return v, nil
}

func (f *fakeLookup) set(id string, outcome domain.RunOutcome) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runs == nil {
		f.runs = map[string]domain.RunView{}
	}
	f.runs[id] = domain.RunView{Run: domain.Run{ID: id, Outcome: outcome}}
}

type fixture struct {
	svc      *Service
	database *db.DB
	repos    Repos
	starter  *fakeStarter
	lookup   *fakeLookup
	tplID    string
	now      time.Time
}

// newFixture builds a service over a temp database with a ready workspace
// and a shared template, and a fake starter/lookup driven by a
// caller-controlled clock (f.now).
func newFixture(t *testing.T) *fixture {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ctx := context.Background()
	users := db.NewUsers(database)
	for _, u := range []struct {
		id   string
		role domain.Role
	}{{admin.UserID, domain.RoleAdmin}, {member.UserID, domain.RoleMember}, {other.UserID, domain.RoleMember}} {
		if err := users.Create(ctx, domain.User{
			ID: u.id, Issuer: "test", Subject: u.id, Email: u.id + "@example.com",
			DisplayName: u.id, Role: u.role, CreatedAt: base, LastLoginAt: base,
		}); err != nil {
			t.Fatalf("create user %s: %v", u.id, err)
		}
	}
	if err := db.NewWorkspaces(database).Create(ctx, domain.Workspace{
		ID: "ws-1", Name: "ws", Path: t.TempDir(), DefaultProfileID: "investigate",
		Source: domain.WorkspaceSourcePath, State: domain.WorkspaceReady, CreatedAt: base, UpdatedAt: base,
	}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	tpl := domain.Template{
		ID: uuid.NewString(), Name: "investigate", WorkspaceID: "ws-1", ProfileID: "investigate",
		PromptTemplate: "Investigate.", CreatedAt: base, UpdatedAt: base,
	}
	if err := db.NewTemplates(database).Create(ctx, tpl); err != nil {
		t.Fatalf("create template: %v", err)
	}

	f := &fixture{
		database: database,
		repos: Repos{
			Schedules: db.NewSchedules(database),
			Firings:   db.NewScheduleFirings(database),
		},
		starter: &fakeStarter{},
		lookup:  &fakeLookup{},
		tplID:   tpl.ID,
		now:     base,
	}
	f.svc = New(f.repos, f.starter, f.lookup, func() time.Time { return f.now }, slog.New(slog.DiscardHandler))
	return f
}

func (f *fixture) input(cron string) domain.ScheduleInput {
	return domain.ScheduleInput{Name: "nightly", TemplateID: f.tplID, Cron: cron, Shared: true}
}

func TestService_CreateComputesNextRun(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	sc, err := f.svc.Create(ctx, admin, f.input("*/5 * * * *"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sc.NextRunAt == nil {
		t.Fatalf("NextRunAt = nil, want set")
	}
	want := time.Date(2026, 9, 19, 10, 5, 0, 0, time.UTC)
	if !sc.NextRunAt.Equal(want) {
		t.Fatalf("NextRunAt = %v, want %v", sc.NextRunAt, want)
	}
	if sc.OwnerID != nil {
		t.Fatalf("OwnerID = %v, want nil (shared)", sc.OwnerID)
	}
}

func TestService_CreateValidation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	if _, err := f.svc.Create(ctx, admin, domain.ScheduleInput{TemplateID: f.tplID, Cron: "* * * * *"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create without name: err = %v, want ErrInvalid", err)
	}
	if _, err := f.svc.Create(ctx, admin, domain.ScheduleInput{Name: "x", Cron: "* * * * *"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create without template: err = %v, want ErrInvalid", err)
	}
	if _, err := f.svc.Create(ctx, admin, domain.ScheduleInput{Name: "x", TemplateID: f.tplID, Cron: "nonsense"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create with bad cron: err = %v, want ErrInvalid", err)
	}
	if _, err := f.svc.Create(ctx, admin, domain.ScheduleInput{Name: "x", TemplateID: "no-such-template", Cron: "* * * * *"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Create with unknown template: err = %v, want ErrInvalid", err)
	}
}

func TestService_GetListVisibility(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	shared, err := f.svc.Create(ctx, admin, f.input("* * * * *"))
	if err != nil {
		t.Fatalf("Create shared: %v", err)
	}
	in := f.input("* * * * *")
	in.Shared = false
	mine, err := f.svc.Create(ctx, member, in)
	if err != nil {
		t.Fatalf("Create member: %v", err)
	}

	list, err := f.svc.List(ctx, member)
	if err != nil {
		t.Fatalf("List member: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("List member len = %d, want 2", len(list))
	}

	if _, err := f.svc.Get(ctx, other, mine.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get other's private schedule: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.Get(ctx, other, shared.ID); err != nil {
		t.Fatalf("Get shared schedule: %v", err)
	}
}

func TestService_UpdateRecomputesNextRunOnlyOnCronChange(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	sc, err := f.svc.Create(ctx, admin, f.input("*/5 * * * *"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	firstNext := *sc.NextRunAt

	// Advance the clock and rename without touching the cron: next_run_at
	// must not move.
	f.now = f.now.Add(time.Minute)
	in := f.input("*/5 * * * *")
	in.Name = "renamed"
	updated, err := f.svc.Update(ctx, admin, sc.ID, in)
	if err != nil {
		t.Fatalf("Update (name only): %v", err)
	}
	if updated.Name != "renamed" {
		t.Fatalf("Name = %q, want renamed", updated.Name)
	}
	if !updated.NextRunAt.Equal(firstNext) {
		t.Fatalf("NextRunAt after name-only update = %v, want unchanged %v", updated.NextRunAt, firstNext)
	}

	// Now change the cron: next_run_at must be recomputed from the
	// (advanced) clock.
	in.Cron = "0 * * * *"
	updated, err = f.svc.Update(ctx, admin, sc.ID, in)
	if err != nil {
		t.Fatalf("Update (cron change): %v", err)
	}
	wantNext := time.Date(2026, 9, 19, 11, 0, 0, 0, time.UTC)
	if !updated.NextRunAt.Equal(wantNext) {
		t.Fatalf("NextRunAt after cron change = %v, want %v", updated.NextRunAt, wantNext)
	}
}

func TestService_UpdateDeletePermissions(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	// A private schedule is invisible to a different non-owner, non-admin
	// actor entirely: Update/Delete read as not found, not forbidden.
	in := f.input("* * * * *")
	in.Shared = false
	mine, err := f.svc.Create(ctx, member, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.svc.Update(ctx, other, mine.ID, in); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Update of a private schedule by another user: err = %v, want ErrNotFound", err)
	}
	if err := f.svc.Delete(ctx, other, mine.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete of a private schedule by another user: err = %v, want ErrNotFound", err)
	}
	if err := f.svc.Delete(ctx, member, mine.ID); err != nil {
		t.Fatalf("Delete by owner: %v", err)
	}
	if _, err := f.svc.Get(ctx, member, mine.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: err = %v, want ErrNotFound", err)
	}

	// A shared schedule is visible to everyone but mutable only by an
	// admin: a non-admin sees it, but Update/Delete are forbidden.
	shared, err := f.svc.Create(ctx, admin, f.input("* * * * *"))
	if err != nil {
		t.Fatalf("Create shared: %v", err)
	}
	if _, err := f.svc.Update(ctx, member, shared.ID, f.input("* * * * *")); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Update of a shared schedule by a non-admin: err = %v, want ErrForbidden", err)
	}
	if err := f.svc.Delete(ctx, member, shared.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("Delete of a shared schedule by a non-admin: err = %v, want ErrForbidden", err)
	}
}

func TestService_Tick_FiresOnlyDueSchedules(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	due, err := f.svc.Create(ctx, admin, f.input("*/5 * * * *"))
	if err != nil {
		t.Fatalf("Create due: %v", err)
	}
	notDue, err := f.svc.Create(ctx, admin, f.input("0 * * * *"))
	if err != nil {
		t.Fatalf("Create not-due: %v", err)
	}
	_ = notDue

	// due's next run is 10:05; tick at 10:05 should fire it. notDue's next
	// run is 11:00, so the same tick must leave it alone.
	f.now = time.Date(2026, 9, 19, 10, 5, 0, 0, time.UTC)
	f.svc.Tick(ctx, f.now)

	calls := f.starter.calls()
	if len(calls) != 1 {
		t.Fatalf("starter calls = %d, want 1", len(calls))
	}
	if calls[0].TemplateID != f.tplID || calls[0].Origin != domain.OriginSchedule {
		t.Fatalf("call = %+v, want template %s origin schedule", calls[0], f.tplID)
	}

	firings, err := f.svc.Firings(ctx, admin, due.ID, 10)
	if err != nil {
		t.Fatalf("Firings due: %v", err)
	}
	if len(firings) != 1 || firings[0].Status != domain.FiringStarted || firings[0].RunID == nil {
		t.Fatalf("Firings due = %+v, want one started firing with a run id", firings)
	}

	notDueFirings, err := f.svc.Firings(ctx, admin, notDue.ID, 10)
	if err != nil {
		t.Fatalf("Firings not-due: %v", err)
	}
	if len(notDueFirings) != 0 {
		t.Fatalf("Firings not-due = %+v, want none", notDueFirings)
	}

	// due's next_run_at should have advanced past this tick.
	got, err := f.svc.Get(ctx, admin, due.ID)
	if err != nil {
		t.Fatalf("Get due: %v", err)
	}
	if !got.NextRunAt.After(f.now) {
		t.Fatalf("NextRunAt after tick = %v, want after %v", got.NextRunAt, f.now)
	}
	if got.LastOutcome != "started" {
		t.Fatalf("LastOutcome = %q, want started", got.LastOutcome)
	}
}

func TestService_Tick_SkipsOverlap(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	sc, err := f.svc.Create(ctx, admin, f.input("*/5 * * * *"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	f.now = time.Date(2026, 9, 19, 10, 5, 0, 0, time.UTC)
	f.svc.Tick(ctx, f.now)
	first := f.starter.calls()
	if len(first) != 1 {
		t.Fatalf("first tick calls = %d, want 1", len(first))
	}
	// The started run is still running.
	firings, err := f.svc.Firings(ctx, admin, sc.ID, 1)
	if err != nil || len(firings) != 1 || firings[0].RunID == nil {
		t.Fatalf("Firings after first tick = %+v, %v", firings, err)
	}
	f.lookup.set(*firings[0].RunID, domain.RunRunning)

	f.now = time.Date(2026, 9, 19, 10, 10, 0, 0, time.UTC)
	f.svc.Tick(ctx, f.now)
	if len(f.starter.calls()) != 1 {
		t.Fatalf("starter calls after overlapping tick = %d, want still 1", len(f.starter.calls()))
	}

	got, err := f.svc.Firings(ctx, admin, sc.ID, 10)
	if err != nil {
		t.Fatalf("Firings: %v", err)
	}
	if len(got) != 2 || got[0].Status != domain.FiringSkippedOverlap {
		t.Fatalf("Firings = %+v, want newest skipped_overlap", got)
	}

	// Once the run finishes, the next tick fires again.
	f.lookup.set(*firings[0].RunID, domain.RunSuccess)
	f.now = time.Date(2026, 9, 19, 10, 15, 0, 0, time.UTC)
	f.svc.Tick(ctx, f.now)
	if len(f.starter.calls()) != 2 {
		t.Fatalf("starter calls after run finished = %d, want 2", len(f.starter.calls()))
	}
}

func TestService_Tick_RecordsFailure(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.starter.err = errors.New("boom")

	sc, err := f.svc.Create(ctx, admin, f.input("*/5 * * * *"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	f.now = time.Date(2026, 9, 19, 10, 5, 0, 0, time.UTC)
	f.svc.Tick(ctx, f.now)

	firings, err := f.svc.Firings(ctx, admin, sc.ID, 10)
	if err != nil {
		t.Fatalf("Firings: %v", err)
	}
	if len(firings) != 1 || firings[0].Status != domain.FiringFailed || firings[0].Reason == "" {
		t.Fatalf("Firings = %+v, want one failed firing with a reason", firings)
	}

	got, err := f.svc.Get(ctx, admin, sc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastOutcome != "failed" {
		t.Fatalf("LastOutcome = %q, want failed", got.LastOutcome)
	}
	// The tick still recomputed next_run_at, so a schedule with a transient
	// failure keeps trying on its cadence rather than stalling.
	if !got.NextRunAt.After(f.now) {
		t.Fatalf("NextRunAt after failed tick = %v, want after %v", got.NextRunAt, f.now)
	}
}

func TestService_RunNow_IgnoresCron(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	// A schedule whose next run is far in the future.
	sc, err := f.svc.Create(ctx, admin, f.input("0 0 1 1 *"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	farFuture := *sc.NextRunAt

	run, err := f.svc.RunNow(ctx, admin, sc.ID)
	if err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if run.ID == "" {
		t.Fatalf("RunNow run.ID empty")
	}
	if len(f.starter.calls()) != 1 {
		t.Fatalf("starter calls = %d, want 1", len(f.starter.calls()))
	}

	// next_run_at is untouched by RunNow: the cron schedule is unaffected.
	got, err := f.svc.Get(ctx, admin, sc.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.NextRunAt.Equal(farFuture) {
		t.Fatalf("NextRunAt after RunNow = %v, want unchanged %v", got.NextRunAt, farFuture)
	}
	if got.LastOutcome != "started" {
		t.Fatalf("LastOutcome = %q, want started", got.LastOutcome)
	}

	firings, err := f.svc.Firings(ctx, admin, sc.ID, 10)
	if err != nil {
		t.Fatalf("Firings: %v", err)
	}
	if len(firings) != 1 || firings[0].Status != domain.FiringStarted {
		t.Fatalf("Firings = %+v, want one started firing", firings)
	}
}

func TestService_VarsMergedWithScheduleKey(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)

	in := f.input("* * * * *")
	in.Vars = json.RawMessage(`{"env":"prod"}`)
	sc, err := f.svc.Create(ctx, admin, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	f.now = time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	if _, err := f.svc.RunNow(ctx, admin, sc.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}

	calls := f.starter.calls()
	if len(calls) != 1 {
		t.Fatalf("starter calls = %d, want 1", len(calls))
	}
	if calls[0].Vars["env"] != "prod" {
		t.Fatalf("Vars[env] = %v, want prod", calls[0].Vars["env"])
	}
	sched, ok := calls[0].Vars["schedule"].(map[string]any)
	if !ok {
		t.Fatalf("Vars[schedule] = %v (%T), want a map", calls[0].Vars["schedule"], calls[0].Vars["schedule"])
	}
	if sched["name"] != sc.Name {
		t.Fatalf("Vars[schedule][name] = %v, want %v", sched["name"], sc.Name)
	}
	if sched["fired_at"] != f.now.UTC().Format(time.RFC3339) {
		t.Fatalf("Vars[schedule][fired_at] = %v, want %v", sched["fired_at"], f.now.UTC().Format(time.RFC3339))
	}
}

// stubPipelineStarter stands in for the pipeline executor.
type stubPipelineStarter struct {
	mu    sync.Mutex
	calls []pipelineStart
	err   error
}

type pipelineStart struct {
	actor      Actor
	pipelineID string
	input      templates.Vars
	origin     domain.Origin
	originRef  string
}

func (s *stubPipelineStarter) Start(_ context.Context, actor Actor, pipelineID string, input templates.Vars,
	origin domain.Origin, originRef string,
) (domain.PipelineRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, pipelineStart{actor: actor, pipelineID: pipelineID, input: input, origin: origin, originRef: originRef})
	if s.err != nil {
		return domain.PipelineRun{}, s.err
	}
	return domain.PipelineRun{ID: fmt.Sprintf("prun-%d", len(s.calls)), State: domain.PipelineRunRunning, StartedAt: base}, nil
}

func (s *stubPipelineStarter) snapshot() []pipelineStart {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]pipelineStart(nil), s.calls...)
}

// A schedule may name a pipeline instead of a template; a tick then starts
// a pipeline run and the firing records it behind the "pr:" prefix.
func TestService_TickStartsAPipelineWhenTheScheduleNamesOne(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	starter := &stubPipelineStarter{}
	f.svc.WithPipelines(starter)

	pl := domain.Pipeline{ID: "pl-1", Name: "nightly-sweep", WorkspaceID: "ws-1",
		YAML: "name: nightly-sweep\nsteps: []\n", CreatedAt: base, UpdatedAt: base}
	if err := db.NewPipelines(f.database).Create(ctx, pl); err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	sc, err := f.svc.Create(ctx, admin, domain.ScheduleInput{
		Name: "nightly", PipelineID: pl.ID, Cron: "*/5 * * * *", Shared: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sc.TemplateID != "" || sc.PipelineID == nil || *sc.PipelineID != pl.ID {
		t.Fatalf("schedule = %+v, want a pipeline target only", sc)
	}

	f.now = base.Add(5 * time.Minute)
	f.svc.Tick(ctx, f.now)

	calls := starter.snapshot()
	if len(calls) != 1 {
		t.Fatalf("pipeline starts = %d, want 1", len(calls))
	}
	if calls[0].pipelineID != pl.ID || calls[0].origin != domain.OriginSchedule || calls[0].originRef != sc.ID {
		t.Fatalf("pipeline start = %+v", calls[0])
	}
	if sched, _ := calls[0].input["schedule"].(map[string]any); sched["name"] != "nightly" {
		t.Fatalf("pipeline input = %+v, want the schedule vars", calls[0].input)
	}
	if got := len(f.starter.calls()); got != 0 {
		t.Fatalf("started %d template runs, want none", got)
	}

	firings, err := f.repos.Firings.ListBySchedule(ctx, sc.ID, 10)
	if err != nil {
		t.Fatalf("list firings: %v", err)
	}
	if len(firings) != 1 || firings[0].Status != domain.FiringStarted {
		t.Fatalf("firings = %+v", firings)
	}
	if firings[0].RunID == nil || *firings[0].RunID != PipelineRunRefPrefix+"prun-1" {
		t.Fatalf("firing run_id = %v, want the prefixed pipeline run id", firings[0].RunID)
	}

	// A "pr:" reference is not a run row, and this service was wired
	// without a pipeline reader: the overlap check leaves the schedule
	// firing rather than skipping forever.
	f.now = base.Add(10 * time.Minute)
	f.svc.Tick(ctx, f.now)
	if got := len(starter.snapshot()); got != 2 {
		t.Fatalf("pipeline starts after the second tick = %d, want 2", got)
	}
}

// stubPipelineRuns stands in for the executor's IsRunning, letting a test
// control whether the pipeline run a schedule last started reads back as
// still going.
type stubPipelineRuns struct {
	mu      sync.Mutex
	running map[string]bool
	err     error
	asked   []string
}

func (s *stubPipelineRuns) IsRunning(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, id)
	if s.err != nil {
		return false, s.err
	}
	return s.running[id], nil
}

func (s *stubPipelineRuns) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked...)
}

// With a pipeline reader attached, a pipeline schedule skips a tick while
// its previous pipeline run is still going — exactly like a template one —
// and fires again once that run has finished.
func TestService_TickSkipsOverlappingPipelineRun(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	starter := &stubPipelineStarter{}
	runsLookup := &stubPipelineRuns{running: map[string]bool{"prun-1": true}}
	f.svc.WithPipelines(starter).WithPipelineRuns(runsLookup)

	pl := domain.Pipeline{ID: "pl-overlap", Name: "sweep", WorkspaceID: "ws-1",
		YAML: "name: sweep\nsteps: []\n", CreatedAt: base, UpdatedAt: base}
	if err := db.NewPipelines(f.database).Create(ctx, pl); err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	sc, err := f.svc.Create(ctx, admin, domain.ScheduleInput{
		Name: "sweep", PipelineID: pl.ID, Cron: "*/5 * * * *", Shared: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	f.now = base.Add(5 * time.Minute)
	f.svc.Tick(ctx, f.now)
	if got := len(starter.snapshot()); got != 1 {
		t.Fatalf("pipeline starts after the first tick = %d, want 1", got)
	}

	// prun-1 is still running, so the second tick is recorded as skipped
	// rather than starting a second pipeline run.
	f.now = base.Add(10 * time.Minute)
	f.svc.Tick(ctx, f.now)
	if got := len(starter.snapshot()); got != 1 {
		t.Fatalf("pipeline starts after the overlapping tick = %d, want still 1", got)
	}
	if asked := runsLookup.snapshot(); len(asked) == 0 || asked[len(asked)-1] != "prun-1" {
		t.Fatalf("IsRunning asked for %v, want the unprefixed pipeline run id", asked)
	}
	firings, err := f.repos.Firings.ListBySchedule(ctx, sc.ID, 10)
	if err != nil {
		t.Fatalf("list firings: %v", err)
	}
	if len(firings) != 2 || firings[0].Status != domain.FiringSkippedOverlap {
		t.Fatalf("firings = %+v, want newest skipped_overlap", firings)
	}

	// Once it has finished, the schedule fires again.
	runsLookup.mu.Lock()
	runsLookup.running["prun-1"] = false
	runsLookup.mu.Unlock()
	f.now = base.Add(15 * time.Minute)
	f.svc.Tick(ctx, f.now)
	if got := len(starter.snapshot()); got != 2 {
		t.Fatalf("pipeline starts after the run finished = %d, want 2", got)
	}
}

// A reader that cannot answer must never wedge a schedule: an error reads
// as "not overlapping", the same rule the run-row lookup follows.
func TestService_TickFiresWhenThePipelineLookupFails(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	starter := &stubPipelineStarter{}
	runsLookup := &stubPipelineRuns{err: errors.New("boom")}
	f.svc.WithPipelines(starter).WithPipelineRuns(runsLookup)

	pl := domain.Pipeline{ID: "pl-err", Name: "sweep-err", WorkspaceID: "ws-1",
		YAML: "name: sweep-err\nsteps: []\n", CreatedAt: base, UpdatedAt: base}
	if err := db.NewPipelines(f.database).Create(ctx, pl); err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	if _, err := f.svc.Create(ctx, admin, domain.ScheduleInput{
		Name: "sweep-err", PipelineID: pl.ID, Cron: "*/5 * * * *", Shared: true,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	f.now = base.Add(5 * time.Minute)
	f.svc.Tick(ctx, f.now)
	f.now = base.Add(10 * time.Minute)
	f.svc.Tick(ctx, f.now)
	if got := len(starter.snapshot()); got != 2 {
		t.Fatalf("pipeline starts = %d, want 2 (a failed lookup never skips)", got)
	}
}

// A schedule input names exactly one of template_id and pipeline_id.
func TestService_CreateRequiresExactlyOneTarget(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	pl := domain.Pipeline{ID: "pl-2", Name: "either-or", WorkspaceID: "ws-1",
		YAML: "name: either-or\nsteps: []\n", CreatedAt: base, UpdatedAt: base}
	if err := db.NewPipelines(f.database).Create(ctx, pl); err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	if _, err := f.svc.Create(ctx, admin, domain.ScheduleInput{Name: "neither", Cron: "*/5 * * * *"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("no target error = %v, want ErrInvalid", err)
	}
	_, err := f.svc.Create(ctx, admin, domain.ScheduleInput{
		Name: "both", TemplateID: f.tplID, PipelineID: pl.ID, Cron: "*/5 * * * *",
	})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("two targets error = %v, want ErrInvalid", err)
	}
}
