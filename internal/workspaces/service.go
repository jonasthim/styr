// Package workspaces is the service layer for Styr workspaces: per-user
// managed working directories (cloned from git, freshly initialised, or an
// admin-registered shared server path) that sessions run in. It owns
// workspace lifecycle (create, update, delete, retry) and visibility.
package workspaces

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/sessions"
)

// cloneTimeout bounds a `git clone` run for a git-source workspace.
const cloneTimeout = 10 * time.Minute

// maxErrorLen bounds the stored/returned clone failure message.
const maxErrorLen = 300

// defaultProfileID is used when a create request omits default_profile_id.
const defaultProfileID = "interactive"

// Sentinel errors distinguishing the 422 create failure reasons from each
// other; all wrap domain.ErrInvalid so a caller that only checks the
// generic sentinel still gets the right status code.
var (
	ErrInvalidURL  = fmt.Errorf("%w: invalid repository url", domain.ErrInvalid)
	ErrInvalidPath = fmt.Errorf("%w: invalid workspace path", domain.ErrInvalid)
	ErrNameTaken   = fmt.Errorf("%w: workspace name already in use", domain.ErrInvalid)
)

// scpLikeRepo matches an scp-style git remote, e.g. "git@host:org/repo.git".
var scpLikeRepo = regexp.MustCompile(`^[\w.-]+@[\w.-]+:.+$`)

// slugInvalid matches runs of characters not allowed in a managed
// workspace directory slug.
var slugInvalid = regexp.MustCompile(`[^a-z0-9-]+`)

// Service is the workspaces service: it validates and creates workspaces
// (synchronously for "path" and "empty", asynchronously for "git"),
// enforces owner/admin visibility, and manages each managed workspace's
// directory on disk.
type Service struct {
	repo         *db.Workspaces
	sessionsRepo *db.Sessions
	bus          *events.Bus
	usersDir     string
	logger       *slog.Logger

	// runGit runs one git subcommand; overridable in tests.
	runGit func(ctx context.Context, dir, home string, args ...string) (string, error)
}

// New constructs a Service.
func New(repo *db.Workspaces, sessionsRepo *db.Sessions, bus *events.Bus, usersDir string, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Service{repo: repo, sessionsRepo: sessionsRepo, bus: bus, usersDir: usersDir, logger: logger}
	s.runGit = s.execGit
	return s
}

// CreateInput describes a new workspace to create.
type CreateInput struct {
	Name             string
	Source           string // "git" | "path" | "empty"
	RepoURL          string
	Branch           string
	Path             string
	DefaultProfileID string
	Worktrees        bool
}

// UpdateInput describes a patch to an existing workspace; nil fields are
// left unchanged.
type UpdateInput struct {
	DefaultProfileID *string
	Worktrees        *bool
}

// visible reports whether actor may see ws: admins see everything, and
// everyone can see shared (owner-less) workspaces. Duplicated from
// internal/sessions (visible for domain.Session) rather than shared, so
// internal/sessions never has to import internal/workspaces.
func visible(actor sessions.Actor, ws domain.Workspace) bool {
	return actor.IsAdmin || ws.OwnerID == nil || *ws.OwnerID == actor.UserID
}

// ownsOrAdmin reports whether actor may mutate ws (update, delete, retry):
// the workspace's owner, or an admin. A shared (owner-less) workspace can
// only be mutated by an admin.
func ownsOrAdmin(actor sessions.Actor, ws domain.Workspace) bool {
	return actor.IsAdmin || (ws.OwnerID != nil && *ws.OwnerID == actor.UserID)
}

// getVisible loads a workspace and enforces visibility, returning
// domain.ErrNotFound for a workspace actor cannot see.
func (s *Service) getVisible(ctx context.Context, actor sessions.Actor, id string) (domain.Workspace, error) {
	ws, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Workspace{}, err
	}
	if !visible(actor, *ws) {
		return domain.Workspace{}, fmt.Errorf("workspace %s: %w", id, domain.ErrNotFound)
	}
	return *ws, nil
}

// List returns every workspace visible to actor, ordered by name.
func (s *Service) List(ctx context.Context, actor sessions.Actor) ([]domain.Workspace, error) {
	return s.repo.ListVisible(ctx, actor.UserID, actor.IsAdmin)
}

// Get returns a workspace, or domain.ErrNotFound when actor cannot see it.
func (s *Service) Get(ctx context.Context, actor sessions.Actor, id string) (domain.Workspace, error) {
	return s.getVisible(ctx, actor, id)
}

// Create validates in and creates a workspace: "path" and "empty" are
// created synchronously; "git" is inserted in state "cloning" and cloned in
// a background goroutine, transitioning to "ready" or "failed".
func (s *Service) Create(ctx context.Context, actor sessions.Actor, in CreateInput) (domain.Workspace, error) {
	if in.Name == "" {
		return domain.Workspace{}, fmt.Errorf("%w: name is required", domain.ErrInvalid)
	}
	if in.DefaultProfileID == "" {
		in.DefaultProfileID = defaultProfileID
	}
	switch domain.WorkspaceSource(in.Source) {
	case domain.WorkspaceSourceGit:
		return s.createGit(ctx, actor, in)
	case domain.WorkspaceSourceEmpty:
		return s.createEmpty(ctx, actor, in)
	case domain.WorkspaceSourcePath:
		return s.createPath(ctx, actor, in)
	default:
		return domain.Workspace{}, fmt.Errorf("%w: source must be one of git, path, empty", domain.ErrInvalid)
	}
}

// createPath registers an admin-owned shared workspace at an existing
// absolute server path.
func (s *Service) createPath(ctx context.Context, actor sessions.Actor, in CreateInput) (domain.Workspace, error) {
	if !actor.IsAdmin {
		return domain.Workspace{}, fmt.Errorf("%w: only an admin may register a shared path workspace", domain.ErrForbidden)
	}
	if in.Path == "" || !filepath.IsAbs(in.Path) {
		return domain.Workspace{}, fmt.Errorf("%w: path must be an absolute path", ErrInvalidPath)
	}
	if !validRepoDir(in.Path) {
		return domain.Workspace{}, fmt.Errorf("%w: %s must be an existing directory containing a git repository", ErrInvalidPath, in.Path)
	}
	now := time.Now()
	ws := domain.Workspace{
		ID:               uuid.NewString(),
		OwnerID:          nil,
		Name:             in.Name,
		Path:             in.Path,
		Source:           domain.WorkspaceSourcePath,
		Managed:          false,
		State:            domain.WorkspaceReady,
		DefaultProfileID: in.DefaultProfileID,
		Worktrees:        in.Worktrees,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := s.repo.Create(ctx, ws); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return domain.Workspace{}, fmt.Errorf("%w: %q", ErrNameTaken, in.Name)
		}
		return domain.Workspace{}, err
	}
	return ws, nil
}

// createEmpty creates a managed, owned workspace directory and runs `git
// init -b main` in it, synchronously (there is no network round trip).
func (s *Service) createEmpty(ctx context.Context, actor sessions.Actor, in CreateInput) (domain.Workspace, error) {
	if actor.UserID == "" {
		return domain.Workspace{}, fmt.Errorf("%w: an owner is required", domain.ErrInvalid)
	}
	dir := s.managedDirPath(actor.UserID, in.Name)
	now := time.Now()
	owner := actor.UserID
	ws := domain.Workspace{
		ID:               uuid.NewString(),
		OwnerID:          &owner,
		Name:             in.Name,
		Path:             dir,
		Source:           domain.WorkspaceSourceEmpty,
		Managed:          true,
		State:            domain.WorkspaceReady,
		DefaultProfileID: in.DefaultProfileID,
		Worktrees:        in.Worktrees,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	// The row is created first so the name-uniqueness check (a real,
	// intended conflict) is distinguished from an orphaned leftover
	// directory at the same slug (an operational hazard, checked next).
	if err := s.repo.Create(ctx, ws); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return domain.Workspace{}, fmt.Errorf("%w: %q", ErrNameTaken, in.Name)
		}
		return domain.Workspace{}, err
	}
	if err := s.prepareEmptyDir(dir); err != nil {
		_ = s.repo.Delete(ctx, ws.ID)
		return domain.Workspace{}, err
	}
	home := s.homeDir(actor.UserID)
	if _, err := s.runGit(ctx, dir, home, "init", "-b", "main"); err != nil {
		_ = os.RemoveAll(dir)
		_ = s.repo.Delete(ctx, ws.ID)
		return domain.Workspace{}, fmt.Errorf("git init: %w", err)
	}
	return ws, nil
}

// prepareEmptyDir refuses to reuse a directory that already exists at dir
// (an orphaned leftover, since the name-uniqueness check that would have
// caught a real duplicate already passed) and creates it.
func (s *Service) prepareEmptyDir(dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%w: a workspace directory already exists at %s", domain.ErrConflict, dir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat workspace dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create workspace dir: %w", err)
	}
	return nil
}

// createGit inserts a "cloning" workspace row and starts the clone in the
// background, returning immediately.
func (s *Service) createGit(ctx context.Context, actor sessions.Actor, in CreateInput) (domain.Workspace, error) {
	if actor.UserID == "" {
		return domain.Workspace{}, fmt.Errorf("%w: an owner is required", domain.ErrInvalid)
	}
	if !validRepoURL(in.RepoURL) {
		return domain.Workspace{}, fmt.Errorf("%w: %q", ErrInvalidURL, in.RepoURL)
	}
	dir := s.managedDirPath(actor.UserID, in.Name)

	now := time.Now()
	owner := actor.UserID
	ws := domain.Workspace{
		ID:               uuid.NewString(),
		OwnerID:          &owner,
		Name:             in.Name,
		Path:             dir,
		Source:           domain.WorkspaceSourceGit,
		RepoURL:          in.RepoURL,
		Branch:           in.Branch,
		Managed:          true,
		State:            domain.WorkspaceCloning,
		DefaultProfileID: in.DefaultProfileID,
		Worktrees:        in.Worktrees,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	// The row is created first so the name-uniqueness check (a real,
	// intended conflict) is distinguished from an orphaned leftover
	// directory at the same slug (an operational hazard, checked next).
	if err := s.repo.Create(ctx, ws); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			return domain.Workspace{}, fmt.Errorf("%w: %q", ErrNameTaken, in.Name)
		}
		return domain.Workspace{}, err
	}
	// `git clone` requires its target directory to not already exist (or
	// be empty); only the parent is created up front.
	if _, err := os.Stat(dir); err == nil {
		_ = s.repo.Delete(ctx, ws.ID)
		return domain.Workspace{}, fmt.Errorf("%w: a workspace directory already exists at %s", domain.ErrConflict, dir)
	} else if !os.IsNotExist(err) {
		_ = s.repo.Delete(ctx, ws.ID)
		return domain.Workspace{}, fmt.Errorf("stat workspace dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		_ = s.repo.Delete(ctx, ws.ID)
		return domain.Workspace{}, fmt.Errorf("create workspace parent dir: %w", err)
	}
	s.publishState(ws.ID, owner, domain.WorkspaceCloning, "")
	go s.runClone(ws)
	return ws, nil
}

// Update patches a workspace's default profile and/or worktrees flag.
// Only the owner or an admin may update a workspace.
func (s *Service) Update(ctx context.Context, actor sessions.Actor, id string, in UpdateInput) (domain.Workspace, error) {
	ws, err := s.getVisible(ctx, actor, id)
	if err != nil {
		return domain.Workspace{}, err
	}
	if !ownsOrAdmin(actor, ws) {
		return domain.Workspace{}, fmt.Errorf("%w: only the owner or an admin may update this workspace", domain.ErrForbidden)
	}
	if in.DefaultProfileID != nil {
		ws.DefaultProfileID = *in.DefaultProfileID
	}
	if in.Worktrees != nil {
		ws.Worktrees = *in.Worktrees
	}
	ws.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, ws); err != nil {
		return domain.Workspace{}, err
	}
	return ws, nil
}

// Delete removes a workspace. It refuses (domain.ErrConflict) while any
// session in state open, running or waiting references it. For a managed
// workspace it also removes the directory (only when it is safely under
// UsersDir, and without following symlinks); a shared path workspace's
// directory is left alone, only its row is deleted.
func (s *Service) Delete(ctx context.Context, actor sessions.Actor, id string) error {
	ws, err := s.getVisible(ctx, actor, id)
	if err != nil {
		return err
	}
	if !ownsOrAdmin(actor, ws) {
		return fmt.Errorf("%w: only the owner or an admin may delete this workspace", domain.ErrForbidden)
	}

	busy, err := s.hasActiveSessions(ctx, id)
	if err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("%w: workspace has open sessions", domain.ErrConflict)
	}

	if ws.Managed {
		if err := s.removeManagedDir(ws.Path); err != nil {
			return err
		}
	}
	return s.repo.Delete(ctx, id)
}

// Retry re-runs a failed git clone. Only valid for a workspace in state
// failed with source git.
func (s *Service) Retry(ctx context.Context, actor sessions.Actor, id string) (domain.Workspace, error) {
	ws, err := s.getVisible(ctx, actor, id)
	if err != nil {
		return domain.Workspace{}, err
	}
	if !ownsOrAdmin(actor, ws) {
		return domain.Workspace{}, fmt.Errorf("%w: only the owner or an admin may retry this workspace", domain.ErrForbidden)
	}
	if ws.Source != domain.WorkspaceSourceGit {
		return domain.Workspace{}, fmt.Errorf("%w: only a git workspace can be retried", domain.ErrConflict)
	}
	if ws.State != domain.WorkspaceFailed {
		return domain.Workspace{}, fmt.Errorf("%w: workspace is not in a failed state", domain.ErrConflict)
	}

	_ = os.RemoveAll(ws.Path) // clear any partial leftover from the failed attempt
	ws.State = domain.WorkspaceCloning
	ws.Error = ""
	ws.UpdatedAt = time.Now()
	if err := s.repo.Update(ctx, ws); err != nil {
		return domain.Workspace{}, err
	}
	s.publishState(ws.ID, *ws.OwnerID, domain.WorkspaceCloning, "")
	go s.runClone(ws)
	return ws, nil
}

// hasActiveSessions reports whether any session anywhere references
// workspaceID and is open, running or waiting. It loads every session (via
// Sessions.ListVisible as an admin, which applies no owner filter) rather
// than adding a new db.Sessions query method, keeping this card's DB
// surface to internal/db/workspaces.go alone.
func (s *Service) hasActiveSessions(ctx context.Context, workspaceID string) (bool, error) {
	all, err := s.sessionsRepo.ListVisible(ctx, "", true)
	if err != nil {
		return false, fmt.Errorf("check active sessions: %w", err)
	}
	for _, sess := range all {
		if sess.WorkspaceID != workspaceID {
			continue
		}
		switch sess.State {
		case domain.SessionOpen, domain.SessionRunning, domain.SessionWaiting:
			return true, nil
		}
	}
	return false, nil
}

// managedDirPath returns the managed directory a new owned workspace named
// name would live at: UsersDir/ownerID/workspaces/<slug>. Existence is
// checked separately (see prepareEmptyDir, createGit) after the
// name-uniqueness check has already run.
func (s *Service) managedDirPath(ownerID, name string) string {
	return filepath.Join(s.usersDir, ownerID, "workspaces", slugify(name))
}

// homeDir returns the HOME directory used for git commands run on behalf
// of ownerID.
func (s *Service) homeDir(ownerID string) string {
	return filepath.Join(s.usersDir, ownerID)
}

// removeManagedDir deletes a managed workspace's directory, refusing to
// touch anything outside UsersDir or any path whose top component is a
// symlink; os.RemoveAll itself never follows symlinks it encounters while
// walking a directory tree, it just unlinks them.
func (s *Service) removeManagedDir(path string) error {
	absUsersDir, err := filepath.Abs(s.usersDir)
	if err != nil {
		return fmt.Errorf("resolve users dir: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve workspace dir: %w", err)
	}
	rel, err := filepath.Rel(absUsersDir, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refuse to remove workspace dir outside users dir: %s", path)
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat workspace dir: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refuse to remove workspace dir: %s is a symlink", path)
	}
	if err := os.RemoveAll(absPath); err != nil {
		return fmt.Errorf("remove workspace dir: %w", err)
	}
	return nil
}

// runClone performs the actual `git clone` for a "cloning" workspace and
// transitions it to ready or failed. It runs in its own goroutine with its
// own timeout-bound context, independent of the HTTP request that started
// it (see createGit, Retry).
func (s *Service) runClone(ws domain.Workspace) {
	ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
	defer cancel()

	home := s.homeDir(*ws.OwnerID)
	args := []string{"clone"}
	if ws.Branch != "" {
		args = append(args, "--branch", ws.Branch)
	}
	args = append(args, "--single-branch", "--no-tags", ws.RepoURL, ws.Path)

	out, err := s.runGit(ctx, "", home, args...)
	if err != nil {
		_ = os.RemoveAll(ws.Path)
		msg := cloneFailureMessage(out, ws.RepoURL)
		s.setState(ws.ID, *ws.OwnerID, domain.WorkspaceFailed, msg)
		return
	}
	s.setState(ws.ID, *ws.OwnerID, domain.WorkspaceReady, "")
}

// setState persists and publishes a workspace state transition; failures
// to persist are logged (there is no request left to return them to).
func (s *Service) setState(id, ownerID string, state domain.WorkspaceState, errMsg string) {
	if err := s.repo.SetState(context.Background(), id, state, errMsg, time.Now()); err != nil {
		s.logger.Error("update workspace state", "workspace", id, "state", state, "err", err)
		return
	}
	s.publishState(id, ownerID, state, errMsg)
}

// publishState publishes a "workspace.state" bus message, owner-scoped so
// the SSE handler's visibility filter (which keys on OwnerID) applies.
func (s *Service) publishState(id, ownerID string, state domain.WorkspaceState, errMsg string) {
	payload, err := json.Marshal(map[string]string{"id": id, "state": string(state), "error": errMsg})
	if err != nil {
		return
	}
	owner := ownerID
	s.bus.Publish(events.Message{Kind: "workspace.state", OwnerID: &owner, Payload: payload})
}

// execGit runs one git subcommand with a locked-down environment: no
// terminal prompt, non-interactive SSH with new host keys auto-accepted
// (never blocking on a prompt Styr cannot answer), and HOME pointed at the
// owning user's home directory so per-user git config and credential
// helpers apply. It returns combined stdout+stderr for error reporting.
func (s *Service) execGit(ctx context.Context, dir, home string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new",
	)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// cloneFailureMessage extracts the last non-blank line of git's combined
// output (its most specific error line), redacts any embedded basic-auth
// credentials from repoURL, and bounds the result to maxErrorLen.
func cloneFailureMessage(out, repoURL string) string {
	msg := lastNonBlankLine(out)
	if msg == "" {
		msg = "git clone failed"
	}
	msg = redactCredentials(msg, repoURL)
	if len(msg) > maxErrorLen {
		msg = msg[:maxErrorLen]
	}
	return msg
}

func lastNonBlankLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}

// redactCredentials replaces a repo URL's userinfo (and any occurrence of
// the raw URL) in msg with a redacted form, e.g.
// "https://***:***@host/repo.git".
func redactCredentials(msg, repoURL string) string {
	u, err := url.Parse(repoURL)
	if err != nil || u.User == nil {
		return msg
	}
	redacted := *u
	redacted.User = url.UserPassword("***", "***")
	msg = strings.ReplaceAll(msg, repoURL, redacted.String())
	if raw := u.User.String(); raw != "" {
		msg = strings.ReplaceAll(msg, raw+"@", "***:***@")
	}
	return msg
}

// validRepoURL accepts https://, ssh://, file:// (for tests and local
// mirrors) and scp-style (user@host:path) git remotes; anything else is
// rejected up front rather than handed to git.
func validRepoURL(raw string) bool {
	switch {
	case strings.HasPrefix(raw, "https://"), strings.HasPrefix(raw, "ssh://"):
		u, err := url.Parse(raw)
		return err == nil && u.Host != ""
	case strings.HasPrefix(raw, "file://"):
		u, err := url.Parse(raw)
		return err == nil && u.Path != ""
	case scpLikeRepo.MatchString(raw):
		return true
	default:
		return false
	}
}

// validRepoDir reports whether path exists, is a directory, and contains a
// .git entry (directory or file, as in a git worktree checkout).
func validRepoDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// slugify turns name into a managed-directory slug: lowercased, with runs
// of characters outside [a-z0-9-] collapsed to a single "-", trimmed of
// leading/trailing "-", and capped at 40 characters.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = slugInvalid.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = strings.TrimRight(s[:40], "-")
	}
	if s == "" {
		s = "workspace"
	}
	return s
}
