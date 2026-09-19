package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// Sessions is the repository for the sessions table.
type Sessions struct{ d *DB }

// NewSessions constructs a Sessions repository.
func NewSessions(d *DB) *Sessions { return &Sessions{d: d} }

const sessionColumns = `id, owner_user_id, title, workspace_id, profile_id, harness, harness_ref, state, origin, origin_ref,
	worktree, branch, base_ref, created_at, last_active_at, num_turns, cost_usd, tokens_in, tokens_out, now_line, model, effort,
	slash_commands, diff_add, diff_del, worktree_shared`

// sessionStateOrder is the CASE expression used by ListVisible to sort
// sessions by lifecycle priority before recency.
const sessionStateOrder = `CASE state
	WHEN 'waiting' THEN 0
	WHEN 'running' THEN 1
	WHEN 'open' THEN 2
	WHEN 'closed' THEN 3
	WHEN 'failed' THEN 4
	ELSE 5 END`

// Create inserts a new session row. s.ID must already be set.
func (s *Sessions) Create(ctx context.Context, sess domain.Session) error {
	_, err := s.d.ExecContext(ctx, `
		INSERT INTO sessions (`+sessionColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.OwnerID, sess.Title, sess.WorkspaceID, sess.ProfileID, sess.Harness, sess.HarnessRef,
		string(sess.State), string(sess.Origin), sess.OriginRef, sess.Worktree, sess.Branch, sess.BaseRef,
		nowString(sess.CreatedAt), nowString(sess.LastActiveAt), sess.NumTurns, sess.CostUSD,
		sess.TokensIn, sess.TokensOut, sess.NowLine, sess.Model, sess.Effort,
		marshalToolList(sess.SlashCommands), sess.DiffAdd, sess.DiffDel, boolToInt(sess.WorktreeShared))
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("create session: %w", domain.ErrConflict)
		}
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func scanSession(row interface{ Scan(dest ...any) error }) (*domain.Session, error) {
	var (
		sess                  domain.Session
		ownerID               sql.NullString
		state, origin         string
		createdAt, lastActive string
		slashCommands         string
		worktreeShared        int
	)
	if err := row.Scan(&sess.ID, &ownerID, &sess.Title, &sess.WorkspaceID, &sess.ProfileID, &sess.Harness, &sess.HarnessRef,
		&state, &origin, &sess.OriginRef, &sess.Worktree, &sess.Branch, &sess.BaseRef, &createdAt, &lastActive,
		&sess.NumTurns, &sess.CostUSD, &sess.TokensIn, &sess.TokensOut, &sess.NowLine, &sess.Model, &sess.Effort,
		&slashCommands, &sess.DiffAdd, &sess.DiffDel, &worktreeShared); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(slashCommands), &sess.SlashCommands)
	sess.OwnerID = nullString(ownerID)
	sess.State = domain.SessionState(state)
	sess.Origin = domain.Origin(origin)
	sess.CreatedAt = parseTime(createdAt)
	sess.LastActiveAt = parseTime(lastActive)
	sess.WorktreeShared = worktreeShared != 0
	return &sess, nil
}

// Get loads a session by id.
func (s *Sessions) Get(ctx context.Context, id string) (*domain.Session, error) {
	row := s.d.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id = ?`, id)
	sess, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get session: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get session: %w", err)
	}
	return sess, nil
}

// ListVisible returns sessions visible to userID: those they own, those
// with no owner, or every session when isAdmin. Ordered by lifecycle state
// priority (waiting, running, open, closed, failed) then by last_active_at
// descending.
func (s *Sessions) ListVisible(ctx context.Context, userID string, isAdmin bool) ([]domain.Session, error) {
	query := `SELECT ` + sessionColumns + ` FROM sessions`
	args := []any{}
	if !isAdmin {
		query += ` WHERE owner_user_id = ? OR owner_user_id IS NULL`
		args = append(args, userID)
	}
	query += ` ORDER BY ` + sessionStateOrder + `, last_active_at DESC`

	rows, err := s.d.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list visible sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

// UpdateState sets a session's lifecycle state.
func (s *Sessions) UpdateState(ctx context.Context, id string, state domain.SessionState) error {
	return s.exec1(ctx, `UPDATE sessions SET state = ? WHERE id = ?`, string(state), id)
}

// UpdateStats sets turn count, cost and token counters.
func (s *Sessions) UpdateStats(ctx context.Context, id string, turns int, cost float64, in, out int) error {
	return s.exec1(ctx, `UPDATE sessions SET num_turns = ?, cost_usd = ?, tokens_in = ?, tokens_out = ? WHERE id = ?`,
		turns, cost, in, out, id)
}

// UpdateNow sets the latest "what it is doing" summary line.
func (s *Sessions) UpdateNow(ctx context.Context, id string, nowLine string) error {
	return s.exec1(ctx, `UPDATE sessions SET now_line = ? WHERE id = ?`, nowLine, id)
}

// UpdateHarness records the harness the session's process actually reported
// itself as on its init message. It is normally the same value Create stored,
// but the CLI is the authority: a session row only asks for a harness, the
// init message is what answered.
func (s *Sessions) UpdateHarness(ctx context.Context, id string, kind string) error {
	return s.exec1(ctx, `UPDATE sessions SET harness = ? WHERE id = ?`, kind, id)
}

// UpdateHarnessRef records the harness-native conversation id the session's
// process reported on its init message (harness.Init.HarnessRef): the Codex
// CLI's thread id, so a later process can be started with
// harness.StartSpec.ResumeRef and continue the same CLI-side conversation.
// A harness with no id of its own to resume by never reports one and leaves
// the column empty.
func (s *Sessions) UpdateHarnessRef(ctx context.Context, id string, ref string) error {
	return s.exec1(ctx, `UPDATE sessions SET harness_ref = ? WHERE id = ?`, ref, id)
}

// UpdateModel sets the model in use.
func (s *Sessions) UpdateModel(ctx context.Context, id string, model string) error {
	return s.exec1(ctx, `UPDATE sessions SET model = ? WHERE id = ?`, model, id)
}

// UpdateEffort sets the reasoning effort the session's process runs with.
func (s *Sessions) UpdateEffort(ctx context.Context, id string, effort string) error {
	return s.exec1(ctx, `UPDATE sessions SET effort = ? WHERE id = ?`, effort, id)
}

// SetWorktree records the git worktree a session runs in: its absolute
// path, the branch it is checked out on and the commit it started from.
// All three are cleared together (empty strings) when the worktree is
// discarded.
func (s *Sessions) SetWorktree(ctx context.Context, id, path, branch, baseRef string) error {
	return s.exec1(ctx, `UPDATE sessions SET worktree = ?, branch = ?, base_ref = ? WHERE id = ?`,
		path, branch, baseRef, id)
}

// SetWorktreeShared updates worktree_shared: true records that the session
// was created on a worktree an earlier session already owned
// (sessions.CreateInput.WorktreePath), rather than getting a fresh one of
// its own. It is separate from SetWorktree so a plain worktree creation
// (SetWorktree alone) leaves the column at its default, false.
func (s *Sessions) SetWorktreeShared(ctx context.Context, id string, shared bool) error {
	return s.exec1(ctx, `UPDATE sessions SET worktree_shared = ? WHERE id = ?`, boolToInt(shared), id)
}

// GetByWorktree returns the session that first created the worktree at
// path — the one whose base_ref every session later reusing that path
// (CreateInput.WorktreePath) inherits, since `git worktree list` itself
// carries no record of the commit a worktree started from. ErrNotFound
// when no session has ever run on that path.
func (s *Sessions) GetByWorktree(ctx context.Context, path string) (*domain.Session, error) {
	row := s.d.QueryRowContext(ctx,
		`SELECT `+sessionColumns+` FROM sessions WHERE worktree = ? ORDER BY created_at ASC LIMIT 1`, path)
	sess, err := scanSession(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get session by worktree: %w", domain.ErrNotFound)
		}
		return nil, fmt.Errorf("get session by worktree: %w", err)
	}
	return sess, nil
}

// CountByWorktree counts sessions, other than excludeSessionID, whose
// worktree is path and whose state is one of states. Discard uses it to
// refuse removing a worktree another session is still using.
func (s *Sessions) CountByWorktree(ctx context.Context, path, excludeSessionID string, states []domain.SessionState) (int, error) {
	if len(states) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(states))
	args := make([]any, 0, len(states)+2)
	args = append(args, path, excludeSessionID)
	for i, st := range states {
		placeholders[i] = "?"
		args = append(args, string(st))
	}
	query := `SELECT COUNT(*) FROM sessions WHERE worktree = ? AND id != ? AND state IN (` +
		strings.Join(placeholders, ",") + `)`
	var n int
	if err := s.d.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count sessions by worktree: %w", err)
	}
	return n, nil
}

// UpdateDiffStats sets the worktree's added/removed line counts against
// base_ref, as of the last turn.
func (s *Sessions) UpdateDiffStats(ctx context.Context, id string, add, del int) error {
	return s.exec1(ctx, `UPDATE sessions SET diff_add = ?, diff_del = ? WHERE id = ?`, add, del, id)
}

// UpdateSlashCommands stores the command list the CLI reported on its init message.
func (s *Sessions) UpdateSlashCommands(ctx context.Context, id string, commands []string) error {
	return s.exec1(ctx, `UPDATE sessions SET slash_commands = ? WHERE id = ?`, marshalToolList(commands), id)
}

// Touch bumps last_active_at to the current time.
func (s *Sessions) Touch(ctx context.Context, id string) error {
	return s.exec1(ctx, `UPDATE sessions SET last_active_at = ? WHERE id = ?`, nowString(time.Now()), id)
}

// exec1 runs an UPDATE ... WHERE id = ? statement, where the last argument
// before id is already appended by the caller (id is the final arg).
func (s *Sessions) exec1(ctx context.Context, query string, args ...any) error {
	res, err := s.d.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update session: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("update session: %w", domain.ErrNotFound)
	}
	return nil
}
