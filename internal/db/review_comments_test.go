package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// seedReviewSession creates the user/workspace/session rows a review
// comment or checkpoint needs as foreign keys, returning the session id.
func seedReviewSession(t *testing.T, d *DB) string {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	users := NewUsers(d)
	if err := users.Create(ctx, domain.User{
		ID: "u-rev", Issuer: "test", Subject: "u-rev", Role: domain.RoleMember, CreatedAt: now, LastLoginAt: now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := NewWorkspaces(d).Create(ctx, domain.Workspace{
		ID: "ws-rev", Name: "rev", Path: t.TempDir(), DefaultProfileID: "interactive",
		Source: domain.WorkspaceSourcePath, State: domain.WorkspaceReady, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	owner := "u-rev"
	if err := NewSessions(d).Create(ctx, domain.Session{
		ID: "s-rev", OwnerID: &owner, Title: "rev", WorkspaceID: "ws-rev", ProfileID: "interactive",
		Harness: "fake", State: domain.SessionOpen, Origin: domain.OriginUI, CreatedAt: now, LastActiveAt: now,
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return "s-rev"
}

func TestReviewComments_CreateListMarkSentDelete(t *testing.T) {
	d := testOpenDB(t)
	ctx := context.Background()
	sessionID := seedReviewSession(t, d)
	repo := NewReviewComments(d)

	now := time.Now()
	first := domain.ReviewComment{
		ID: "rc-1", SessionID: sessionID, Path: "main.go", Line: 12, Side: domain.ReviewSideNew,
		Body: "rename this", AuthorID: "u-rev", CreatedAt: now,
	}
	second := domain.ReviewComment{
		ID: "rc-2", SessionID: sessionID, Path: "README.md", Line: 3, Side: domain.ReviewSideOld,
		Body: "stale", AuthorID: "u-rev", CreatedAt: now.Add(time.Second),
	}
	for _, c := range []domain.ReviewComment{first, second} {
		if err := repo.Create(ctx, c); err != nil {
			t.Fatalf("create %s: %v", c.ID, err)
		}
	}

	got, err := repo.ListBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(got) != 2 || got[0].ID != "rc-1" || got[1].ID != "rc-2" {
		t.Fatalf("comments = %+v, want rc-1 then rc-2", got)
	}
	if got[0].Side != domain.ReviewSideNew || got[0].Line != 12 || got[0].SentAt != nil {
		t.Fatalf("rc-1 = %+v, want new/12/unsent", got[0])
	}

	if err := repo.MarkSent(ctx, []string{"rc-1"}); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	got, err = repo.ListBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListBySession after MarkSent: %v", err)
	}
	if got[0].SentAt == nil {
		t.Fatal("rc-1 sent_at is still null")
	}
	if got[1].SentAt != nil {
		t.Fatalf("rc-2 sent_at = %v, want null", got[1].SentAt)
	}

	// Deleting under the wrong session id must not remove the row.
	if err := repo.Delete(ctx, "rc-2", "other-session"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Delete(wrong session) = %v, want ErrNotFound", err)
	}
	if err := repo.Delete(ctx, "rc-2", sessionID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, err = repo.ListBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListBySession after Delete: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("comments = %d, want 1 after delete", len(got))
	}
}

func TestReviewComments_MarkSentEmptyIsNoOp(t *testing.T) {
	d := testOpenDB(t)
	if err := NewReviewComments(d).MarkSent(context.Background(), nil); err != nil {
		t.Fatalf("MarkSent(nil): %v", err)
	}
}

func TestCheckpoints_CreateListGet(t *testing.T) {
	d := testOpenDB(t)
	ctx := context.Background()
	sessionID := seedReviewSession(t, d)
	repo := NewCheckpoints(d)

	now := time.Now()
	older := domain.Checkpoint{ID: "cp-1", SessionID: sessionID, CommitSHA: "aaa", Turn: 1, Summary: "turn 1", CreatedAt: now}
	newer := domain.Checkpoint{ID: "cp-2", SessionID: sessionID, CommitSHA: "bbb", Turn: 2, Summary: "turn 2", CreatedAt: now.Add(time.Minute)}
	for _, cp := range []domain.Checkpoint{older, newer} {
		if err := repo.Create(ctx, cp); err != nil {
			t.Fatalf("create %s: %v", cp.ID, err)
		}
	}

	list, err := repo.ListBySession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(list) != 2 || list[0].ID != "cp-2" || list[1].ID != "cp-1" {
		t.Fatalf("checkpoints = %+v, want newest first", list)
	}

	got, err := repo.Get(ctx, "cp-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.CommitSHA != "aaa" || got.Turn != 1 || got.Summary != "turn 1" {
		t.Fatalf("checkpoint = %+v", got)
	}

	if _, err := repo.Get(ctx, "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get(missing) = %v, want ErrNotFound", err)
	}
}

// TestSessions_SetWorktreeAndDiffStats covers the two new session column
// writers the review service uses.
func TestSessions_SetWorktreeAndDiffStats(t *testing.T) {
	d := testOpenDB(t)
	ctx := context.Background()
	sessionID := seedReviewSession(t, d)
	repo := NewSessions(d)

	if err := repo.SetWorktree(ctx, sessionID, "/tmp/wt", "styr/abc-title", "deadbeef"); err != nil {
		t.Fatalf("SetWorktree: %v", err)
	}
	if err := repo.UpdateDiffStats(ctx, sessionID, 12, 3); err != nil {
		t.Fatalf("UpdateDiffStats: %v", err)
	}
	sess, err := repo.Get(ctx, sessionID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sess.Worktree != "/tmp/wt" || sess.Branch != "styr/abc-title" || sess.BaseRef != "deadbeef" {
		t.Fatalf("session worktree fields = %+v", sess)
	}
	if sess.DiffAdd != 12 || sess.DiffDel != 3 {
		t.Fatalf("diff stats = %d/%d, want 12/3", sess.DiffAdd, sess.DiffDel)
	}

	if err := repo.SetWorktree(ctx, "nope", "", "", ""); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SetWorktree(missing) = %v, want ErrNotFound", err)
	}
}

// TestWorkspaces_ReviewColumnsRoundTrip covers base_branch and
// auto_checkpoint through Create, Get and Update.
func TestWorkspaces_ReviewColumnsRoundTrip(t *testing.T) {
	d := testOpenDB(t)
	ctx := context.Background()
	repo := NewWorkspaces(d)

	now := time.Now()
	ws := domain.Workspace{
		ID: "ws-review", Name: "review", Path: t.TempDir(), DefaultProfileID: "interactive",
		Worktrees: true, BaseBranch: "main", AutoCheckpoint: true,
		Source: domain.WorkspaceSourcePath, State: domain.WorkspaceReady, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.Create(ctx, ws); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BaseBranch != "main" || !got.AutoCheckpoint || !got.Worktrees {
		t.Fatalf("workspace = %+v", got)
	}

	got.BaseBranch = "develop"
	got.AutoCheckpoint = false
	got.UpdatedAt = now.Add(time.Minute)
	if err := repo.Update(ctx, *got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got2, err := repo.Get(ctx, ws.ID)
	if err != nil {
		t.Fatalf("Get after Update: %v", err)
	}
	if got2.BaseBranch != "develop" || got2.AutoCheckpoint {
		t.Fatalf("workspace after update = %+v", got2)
	}
}
