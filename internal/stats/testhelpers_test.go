package stats

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
)

// testOpenDB opens a fresh migrated database in a per-test temp directory.
func testOpenDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "styr.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// seedWorkspace inserts a workspace using the builtin "interactive" profile,
// satisfying the sessions.workspace_id and templates.workspace_id FKs.
func seedWorkspace(t *testing.T, ctx context.Context, d *db.DB, id string) domain.Workspace {
	t.Helper()
	now := time.Now()
	ws := domain.Workspace{
		ID: id, Name: id, Path: "/srv/" + id, Source: domain.WorkspaceSourcePath,
		DefaultProfileID: "interactive", State: domain.WorkspaceReady, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.NewWorkspaces(d).Create(ctx, ws); err != nil {
		t.Fatalf("seed workspace %q: %v", id, err)
	}
	return ws
}

// seedUser inserts a user with a specific id and display name, satisfying
// sessions.owner_user_id and templates.owner_user_id FKs.
func seedUser(t *testing.T, ctx context.Context, d *db.DB, id, displayName string) domain.User {
	t.Helper()
	now := time.Now()
	u := domain.User{
		ID: id, Issuer: "https://issuer", Subject: id, Email: id + "@example.com",
		DisplayName: displayName, Role: domain.RoleMember, CreatedAt: now, LastLoginAt: now,
	}
	if err := db.NewUsers(d).Create(ctx, u); err != nil {
		t.Fatalf("seed user %q: %v", id, err)
	}
	return u
}

// seedSession is a fully-specified session fixture: every stats-relevant
// column (owner, origin, state, cost, timestamps) is a caller-supplied
// argument rather than a default, since Gantt and Costs branch on all of
// them.
type seedSessionArgs struct {
	id           string
	workspaceID  string
	ownerID      *string
	title        string
	state        domain.SessionState
	origin       domain.Origin
	costUSD      float64
	createdAt    time.Time
	lastActiveAt time.Time
}

func seedSession(t *testing.T, ctx context.Context, d *db.DB, a seedSessionArgs) domain.Session {
	t.Helper()
	sess := domain.Session{
		ID: a.id, OwnerID: a.ownerID, Title: a.title, WorkspaceID: a.workspaceID, ProfileID: "interactive",
		Harness: "claude", State: a.state, Origin: a.origin, CostUSD: a.costUSD,
		CreatedAt: a.createdAt, LastActiveAt: a.lastActiveAt,
	}
	if err := db.NewSessions(d).Create(ctx, sess); err != nil {
		t.Fatalf("seed session %q: %v", a.id, err)
	}
	return sess
}

// seedEvent inserts a raw events row with an explicit `at`, bypassing
// db.Events.Append (which always stamps time.Now()) so segment boundary
// tests get deterministic timestamps.
func seedEvent(t *testing.T, ctx context.Context, d *db.DB, sessionID string, seq int64, at time.Time, typ string) {
	t.Helper()
	if _, err := d.ExecContext(ctx, `INSERT INTO events (session_id, seq, at, type, payload) VALUES (?, ?, ?, ?, ?)`,
		sessionID, seq, formatTime(at), typ, `{}`); err != nil {
		t.Fatalf("seed event %s#%d: %v", sessionID, seq, err)
	}
}

// seedTemplate inserts a template, satisfying runs.template_id's FK.
func seedTemplate(t *testing.T, ctx context.Context, d *db.DB, id, workspaceID, name string) domain.Template {
	t.Helper()
	now := time.Now()
	tpl := domain.Template{
		ID: id, Name: name, WorkspaceID: workspaceID, ProfileID: "investigate",
		PromptTemplate: "do the thing", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.NewTemplates(d).Create(ctx, tpl); err != nil {
		t.Fatalf("seed template %q: %v", id, err)
	}
	return tpl
}

// seedRun inserts a run tied to sessionID and templateID with a cost, for
// the top-templates aggregation in Costs.
func seedRun(t *testing.T, ctx context.Context, d *db.DB, sessionID, templateID string, costUSD float64, startedAt time.Time) domain.Run {
	t.Helper()
	tid := templateID
	r := domain.Run{
		ID: uuid.NewString(), SessionID: sessionID, TemplateID: &tid, Origin: "webhook",
		StartedAt: startedAt, Outcome: domain.RunSuccess, CostUSD: costUSD,
	}
	if err := db.NewRuns(d).Create(ctx, r); err != nil {
		t.Fatalf("seed run: %v", err)
	}
	return r
}

func newService(t *testing.T, d *db.DB) *Service {
	t.Helper()
	return New(d, db.NewSessions(d), db.NewUsers(d), nil)
}
