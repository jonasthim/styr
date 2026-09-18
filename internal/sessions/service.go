// Package sessions is the service layer that drives Claude Code sessions over
// internal/harness: it owns session lifecycle (create, send, approve, close),
// persists the transcript and approvals, publishes live updates on the event
// bus, and schedules concurrent processes within a fixed slot budget.
package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/harness"
)

// startWaitPoll is how often a Send that is waiting on a concurrent Send's
// startProcess (see the starting map on Service) rechecks for the process
// to appear.
const startWaitPoll = 20 * time.Millisecond

// startWaitTimeout bounds how long a Send waits for a concurrent Send's
// startProcess to finish before giving up.
const startWaitTimeout = 10 * time.Second

// Repos bundles the repositories the service reads and writes.
type Repos struct {
	Sessions   *db.Sessions
	Events     *db.Events
	Approvals  *db.Approvals
	Workspaces *db.Workspaces
	Profiles   *db.Profiles
	Tokens     *db.Tokens
	Audit      *db.Audit
	// ReviewComments and Checkpoints back the review surface of a worktree
	// session (see review.go); Users resolves the git author a Commit is
	// attributed to.
	ReviewComments *db.ReviewComments
	Checkpoints    *db.Checkpoints
	Users          *db.Users
}

// Options configures the service: how many processes may run concurrently,
// how long an idle session stays open before it is closed, and where each
// process's HOME directory lives.
type Options struct {
	MaxOpen     int // slots for open processes
	IdleTimeout time.Duration
	UsersDir    string // HOME per user: UsersDir/<userID>
	ServiceHome string // HOME for unattended sessions

	// Logger receives operational errors the service cannot surface any
	// other way (a control-channel write that failed, an approval that
	// could not be recorded, ...). Optional; New defaults to slog.Default().
	Logger *slog.Logger
}

// Actor identifies who is calling the service, for visibility checks and
// audit attribution.
type Actor struct {
	UserID  string
	IsAdmin bool
}

// procEntry is a tracked, live process: the harness.Process itself plus the
// session's owner, cached so the scheduler and Shutdown do not need a DB
// round trip to publish bus messages or decide what to evict.
type procEntry struct {
	proc    harness.Process
	ownerID *string
}

// Service is the sessions service: it owns every live harness.Process and
// mediates all session and approval state changes.
type Service struct {
	repos  Repos
	h      harness.Harness
	bus    *events.Bus
	box    *crypto.Box
	opt    Options
	slots  *slots
	logger *slog.Logger

	// newApprovalID generates a new approval row's id. It is a field
	// (rather than a direct uuid.NewString() call in handlePermission) only
	// so tests can make it deterministic and force a primary-key conflict
	// on Approvals.Create, exercising the create-failure path without
	// needing to break the database itself.
	newApprovalID func() string

	mu sync.Mutex
	// procs tracks every live process by session id.
	procs map[string]*procEntry
	// closing marks a session id whose process is being closed
	// intentionally (Close, Shutdown, eviction), so its pump goroutine
	// knows an EventExit was expected.
	closing map[string]bool
	// starting marks a session id whose startProcess is currently running
	// (called from Send), so a second, concurrent Send for the same id
	// waits for it instead of racing to start a second process. See Send
	// and waitForStartingProcess.
	starting map[string]struct{}
}

// New constructs a Service. The returned Service owns no background
// goroutines beyond one pump per live session process; call RunMaintenance
// periodically (e.g. once a minute) and Shutdown on server stop.
func New(r Repos, h harness.Harness, bus *events.Bus, box *crypto.Box, opt Options) *Service {
	logger := opt.Logger
	if logger == nil {
		logger = slog.Default()
	}
	s := &Service{
		repos:         r,
		h:             h,
		bus:           bus,
		box:           box,
		opt:           opt,
		logger:        logger,
		newApprovalID: uuid.NewString,
		procs:         make(map[string]*procEntry),
		closing:       make(map[string]bool),
		starting:      make(map[string]struct{}),
	}
	s.slots = newSlots(opt.MaxOpen, s.evictOldestIdleUnattended)
	return s
}

// CreateInput describes a new session to start.
type CreateInput struct {
	WorkspaceID string
	ProfileID   string
	Title       string
	Prompt      string
	Origin      domain.Origin
	OriginRef   string
	Owner       *string // nil for an unattended session, run under the service token

	// Model and Effort override the profile's defaults for this session. Empty means "use
	// the profile's", and an empty profile default in turn means "use the CLI's own".
	Model  string
	Effort string

	// JSONSchema and SystemPrompt are passed straight through to
	// harness.StartSpec (--json-schema and --append-system-prompt): an
	// unattended run started from a template constrains its final turn to
	// the template's report schema and appends the template's extra
	// instructions. Both are optional.
	JSONSchema   string
	SystemPrompt string

	// RunID is the internal/runs run this session belongs to. For an
	// unattended origin (webhook, schedule, pipeline) it becomes the
	// session's OriginRef, so a session can be traced back to its run
	// without a second table; OriginRef keeps its existing meaning for
	// every other origin.
	RunID string

	// WorktreePath starts the session in an existing git worktree —
	// another session's, under <workspace>/.styr/worktrees/ — instead of
	// creating a fresh one, so a pipeline's "worktree: shared" step can
	// continue where the previous step left off. Empty (the default) gets
	// a fresh worktree as before. The workspace must have worktrees
	// enabled; otherwise Create returns domain.ErrInvalid.
	WorktreePath string
}

// startOptions carries the per-session harness extras supplied at Create
// time down to startProcess. A resume (Send on a closed session) passes the
// zero value: the schema and the appended system prompt only shape the
// first process, and sessions does not persist them.
type startOptions struct {
	JSONSchema   string
	SystemPrompt string
}

// originRef returns the OriginRef to persist for in: an unattended origin
// records its run id, every other origin keeps the caller's OriginRef.
func (in CreateInput) originRef() string {
	switch in.Origin {
	case domain.OriginWebhook, domain.OriginSchedule, domain.OriginPipeline:
		if in.RunID != "" {
			return in.RunID
		}
	}
	return in.OriginRef
}

// Create persists a new session, starts its harness process and sends the
// initial prompt.
func (s *Service) Create(ctx context.Context, actor Actor, in CreateInput) (domain.Session, error) {
	ws, err := s.repos.Workspaces.Get(ctx, in.WorkspaceID)
	if err != nil {
		return domain.Session{}, err
	}
	if !workspaceVisible(actor, *ws) {
		return domain.Session{}, fmt.Errorf("workspace %s: %w", in.WorkspaceID, domain.ErrNotFound)
	}
	if ws.State != domain.WorkspaceReady {
		return domain.Session{}, fmt.Errorf("%w: workspace is not ready", domain.ErrConflict)
	}
	profile, err := s.repos.Profiles.Get(ctx, in.ProfileID)
	if err != nil {
		return domain.Session{}, err
	}
	// Fail fast, before persisting a session row, when there is no token to run with.
	if _, err := s.resolveToken(ctx, in.Owner); err != nil {
		return domain.Session{}, err
	}

	model, effort := in.Model, in.Effort
	if model == "" {
		model = profile.Model
	}
	if effort == "" {
		effort = profile.Effort
	}
	if !harness.ValidEffort(effort) {
		return domain.Session{}, fmt.Errorf("%w: effort %q is not allowed", domain.ErrInvalid, effort)
	}

	now := time.Now()
	sess := domain.Session{
		ID:           uuid.NewString(),
		OwnerID:      in.Owner,
		Title:        in.Title,
		WorkspaceID:  in.WorkspaceID,
		ProfileID:    in.ProfileID,
		Harness:      string(s.h.Kind()),
		State:        domain.SessionRunning,
		Origin:       in.Origin,
		OriginRef:    in.originRef(),
		CreatedAt:    now,
		LastActiveAt: now,
		Model:        model,
		Effort:       effort,
	}
	if err := s.repos.Sessions.Create(ctx, sess); err != nil {
		return domain.Session{}, err
	}

	// A worktree-enabled workspace gets one git worktree per session, created before the
	// process starts so the CLI's cwd is the worktree from its very first turn — unless the
	// caller names an existing worktree to continue in (a pipeline's "worktree: shared" step).
	switch {
	case in.WorktreePath != "" && !ws.Worktrees:
		_ = s.repos.Sessions.UpdateState(ctx, sess.ID, domain.SessionFailed)
		return domain.Session{}, fmt.Errorf("%w: workspace does not have worktrees enabled", domain.ErrInvalid)
	case in.WorktreePath != "":
		withWorktree, err := s.attachWorktree(ctx, sess, *ws, in.WorktreePath)
		if err != nil {
			_ = s.repos.Sessions.UpdateState(ctx, sess.ID, domain.SessionFailed)
			return domain.Session{}, err
		}
		sess = withWorktree
	case ws.Worktrees:
		withWorktree, err := s.createWorktree(ctx, sess, *ws)
		if err != nil {
			_ = s.repos.Sessions.UpdateState(ctx, sess.ID, domain.SessionFailed)
			return domain.Session{}, err
		}
		sess = withWorktree
	}

	opts := startOptions{JSONSchema: in.JSONSchema, SystemPrompt: in.SystemPrompt}
	if err := s.startProcess(ctx, sess, *ws, *profile, false, in.Prompt, opts); err != nil {
		_ = s.repos.Sessions.UpdateState(ctx, sess.ID, domain.SessionFailed)
		return domain.Session{}, err
	}
	return sess, nil
}

// Send delivers text to a session's process. It reopens a closed session by
// starting a new process with Resume: true.
//
// A closed session has no tracked process, so two concurrent Sends for the
// same id would otherwise both see procs[id] absent and both call
// startProcess — a TOCTOU that starts two harness processes (and sends two
// Resumes) for one session. The starting map closes that window: whichever
// Send arrives first claims it and starts the process; a second Send for
// the same id waits for that process to appear (or for the first Send to
// fail) instead of racing to start its own.
func (s *Service) Send(ctx context.Context, actor Actor, id, text string) error {
	sess, err := s.getVisible(ctx, actor, id)
	if err != nil {
		return err
	}
	if sess.State == domain.SessionWaiting {
		return fmt.Errorf("%w: answer the pending approval first", domain.ErrConflict)
	}

	s.mu.Lock()
	if entry, ok := s.procs[id]; ok {
		s.mu.Unlock()
		s.recordUserTurn(ctx, sess, text)
		return entry.proc.Send(ctx, harness.UserMessage{Text: text})
	}
	if _, already := s.starting[id]; already {
		s.mu.Unlock()
		entry, err := s.waitForStartingProcess(ctx, id)
		if err != nil {
			return err
		}
		s.recordUserTurn(ctx, sess, text)
		return entry.proc.Send(ctx, harness.UserMessage{Text: text})
	}
	s.starting[id] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.starting, id)
		s.mu.Unlock()
	}()

	ws, err := s.repos.Workspaces.Get(ctx, sess.WorkspaceID)
	if err != nil {
		return err
	}
	profile, err := s.repos.Profiles.Get(ctx, sess.ProfileID)
	if err != nil {
		return err
	}
	if err := s.startProcess(ctx, sess, *ws, *profile, true, text, startOptions{}); err != nil {
		return err
	}
	return nil
}

// waitForStartingProcess polls (every startWaitPoll, up to startWaitTimeout)
// for id's process to be registered by the concurrent Send that is
// currently starting it. It returns domain.ErrConflict if that Send's
// startProcess finishes (the starting marker is cleared) without ever
// registering a process — it failed — or if the wait times out.
func (s *Service) waitForStartingProcess(ctx context.Context, id string) (*procEntry, error) {
	deadline := time.Now().Add(startWaitTimeout)
	for {
		s.mu.Lock()
		entry, ok := s.procs[id]
		_, starting := s.starting[id]
		s.mu.Unlock()
		if ok {
			return entry, nil
		}
		if !starting {
			return nil, fmt.Errorf("%w: session failed to start", domain.ErrConflict)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w: timed out waiting for session to start", domain.ErrConflict)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(startWaitPoll):
		}
	}
}

// Interrupt asks a running session's process to stop the current turn.
func (s *Service) Interrupt(ctx context.Context, actor Actor, id string) error {
	if _, err := s.getVisible(ctx, actor, id); err != nil {
		return err
	}
	s.mu.Lock()
	entry, ok := s.procs[id]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: session has no running process", domain.ErrConflict)
	}
	return entry.proc.Interrupt(ctx)
}

// Close ends a session's process (if any) and marks the session closed.
func (s *Service) Close(ctx context.Context, actor Actor, id string) error {
	sess, err := s.getVisible(ctx, actor, id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	entry, ok := s.procs[id]
	if ok {
		s.closing[id] = true
	}
	s.mu.Unlock()
	if ok {
		return entry.proc.Close(ctx)
	}
	if sess.State == domain.SessionClosed {
		return nil
	}
	return s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionClosed)
}

// Get returns a session, or domain.ErrNotFound when actor cannot see it.
func (s *Service) Get(ctx context.Context, actor Actor, id string) (domain.Session, error) {
	return s.getVisible(ctx, actor, id)
}

// List returns every session visible to actor, in attention order.
func (s *Service) List(ctx context.Context, actor Actor) ([]domain.Session, error) {
	return s.repos.Sessions.ListVisible(ctx, actor.UserID, actor.IsAdmin)
}

// Events returns up to limit transcript events for a session after afterSeq.
func (s *Service) Events(ctx context.Context, actor Actor, id string, afterSeq int64, limit int) ([]domain.Event, error) {
	if _, err := s.getVisible(ctx, actor, id); err != nil {
		return nil, err
	}
	return s.repos.Events.ListAfter(ctx, id, afterSeq, limit)
}

// Shutdown closes every live process and marks their sessions closed.
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	entries := make(map[string]*procEntry, len(s.procs))
	for id, e := range s.procs {
		entries[id] = e
		s.closing[id] = true
	}
	s.mu.Unlock()

	var firstErr error
	for id, e := range entries {
		if err := e.proc.Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := s.setState(ctx, id, e.ownerID, domain.SessionClosed); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// visible reports whether actor may see sess: admins see everything, and
// everyone can see sessions with no owner.
func visible(actor Actor, sess domain.Session) bool {
	return actor.IsAdmin || sess.OwnerID == nil || *sess.OwnerID == actor.UserID
}

// workspaceVisible reports whether actor may see ws, by the same rule as
// visible above. Duplicated (rather than shared with internal/workspaces)
// so internal/sessions never has to import internal/workspaces: see
// Service.Create's visibility and readiness check.
func workspaceVisible(actor Actor, ws domain.Workspace) bool {
	return actor.IsAdmin || ws.OwnerID == nil || *ws.OwnerID == actor.UserID
}

// getVisible loads a session and enforces visibility, returning
// domain.ErrNotFound for a session actor cannot see.
func (s *Service) getVisible(ctx context.Context, actor Actor, id string) (domain.Session, error) {
	sess, err := s.repos.Sessions.Get(ctx, id)
	if err != nil {
		return domain.Session{}, err
	}
	if !visible(actor, *sess) {
		return domain.Session{}, fmt.Errorf("session %s: %w", id, domain.ErrNotFound)
	}
	return *sess, nil
}

// resolveToken decrypts the Claude token to use for ownerID (or the service
// token when ownerID is nil). The decrypted value is returned only for
// immediate use in a child process's environment; callers must not persist
// or log it.
func (s *Service) resolveToken(ctx context.Context, ownerID *string) (string, error) {
	get := s.repos.Tokens.GetService
	missing := "add a service token in settings"
	if ownerID != nil {
		id := *ownerID
		get = func(ctx context.Context) ([]byte, []byte, string, *time.Time, error) {
			return s.repos.Tokens.Get(ctx, id)
		}
		missing = "add a Claude token in your profile"
	}
	ciphertext, nonce, _, _, err := get(ctx)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", domain.ErrInvalid, missing)
		}
		return "", err
	}
	plain, err := s.box.Open(ciphertext, nonce)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// homeDir returns (creating if necessary) the HOME directory for a session's
// process: UsersDir/<ownerID> for an owned session, ServiceHome otherwise.
func (s *Service) homeDir(ownerID *string) (string, error) {
	dir := s.opt.ServiceHome
	if ownerID != nil {
		dir = filepath.Join(s.opt.UsersDir, *ownerID)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create home dir: %w", err)
	}
	return dir, nil
}

// startProcess acquires a scheduler slot, starts (or resumes) a harness
// process for sess, registers it, launches its pump goroutine and sends the
// first message. An empty firstMessage starts the process without sending
// anything — SwitchModel uses that to reopen a session under new flags
// without inventing a user turn. On any failure before the process starts,
// the slot is released.
//
// The model and effort the process runs with come from sess: Create seeds
// them from CreateInput (falling back to the profile), SwitchModel rewrites
// them, and the CLI's init message later replaces sess.Model with the full
// model name it actually resolved, which is itself a valid --model value on
// the next resume.
func (s *Service) startProcess(ctx context.Context, sess domain.Session, ws domain.Workspace, profile domain.Profile, resume bool, firstMessage string, opts startOptions) error {
	token, err := s.resolveToken(ctx, sess.OwnerID)
	if err != nil {
		return err
	}
	home, err := s.homeDir(sess.OwnerID)
	if err != nil {
		return err
	}

	interactive := !profile.Unattended
	if err := s.slots.acquire(ctx, interactive); err != nil {
		return err
	}

	spec := harness.StartSpec{
		SessionID: sess.ID,
		Resume:    resume,
		Title:     sess.Title,
		Cwd:       sessionCwd(sess, ws),
		Home:      home,
		Env:       map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": token},
		Profile: harness.Profile{
			Mode:            profile.Mode,
			AllowedTools:    profile.AllowedTools,
			DisallowedTools: profile.DisallowedTools,
			MaxTurns:        profile.MaxTurns,
		},
		Model:        sess.Model,
		Effort:       sess.Effort,
		JSONSchema:   opts.JSONSchema,
		SystemPrompt: opts.SystemPrompt,
	}
	p, err := s.h.Start(ctx, spec)
	if err != nil {
		s.slots.release()
		return err
	}
	if err := s.registerProcess(ctx, sess.ID, sess.OwnerID, p); err != nil {
		s.slots.release()
		return err
	}
	go s.pump(sess, p)

	if resume {
		if err := s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionRunning); err != nil {
			return err
		}
	}
	if firstMessage == "" {
		return nil
	}
	s.recordUserTurn(ctx, sess, firstMessage)
	return p.Send(ctx, harness.UserMessage{Text: firstMessage})
}

// sessionCwd is the working directory a session's process runs in: its own git worktree when
// it has one, the workspace checkout otherwise.
func sessionCwd(sess domain.Session, ws domain.Workspace) string {
	if sess.Worktree != "" {
		return sess.Worktree
	}
	return ws.Path
}

// recordUserTurn persists the user's own message as a transcript event of
// type "user" so the transcript shows both sides, and publishes it on the bus.
// The CLI echoes user turns back (--replay-user-messages) but the codec drops
// those echoes, so this is the single source of user turns.
func (s *Service) recordUserTurn(ctx context.Context, sess domain.Session, text string) {
	payload, err := json.Marshal(harness.Event{Type: harness.EventUser, At: time.Now(), Text: text})
	if err != nil {
		return
	}
	seq, err := s.repos.Events.Append(ctx, sess.ID, string(harness.EventUser), payload)
	if err != nil {
		s.logger.Error("record user turn", "session", sess.ID, "err", err)
		return
	}
	s.bus.Publish(events.Message{Kind: "session.event", SessionID: sess.ID, OwnerID: sess.OwnerID, Seq: seq, Payload: payload})
}

// registerProcess tracks a live process under sessionID. If a live process
// is already tracked for sessionID, p is a redundant, duplicate start (the
// starting map in Send exists to prevent exactly this for Send's own
// callers, but registerProcess enforces it unconditionally as the last
// line of defence for every caller): p is closed immediately, without ever
// running its pump goroutine, and domain.ErrConflict is returned so the
// caller does not mistake it for a tracked, live process.
func (s *Service) registerProcess(ctx context.Context, sessionID string, ownerID *string, p harness.Process) error {
	s.mu.Lock()
	if _, exists := s.procs[sessionID]; exists {
		s.mu.Unlock()
		_ = p.Close(ctx)
		return fmt.Errorf("%w: session %s already has a live process", domain.ErrConflict, sessionID)
	}
	s.procs[sessionID] = &procEntry{proc: p, ownerID: ownerID}
	delete(s.closing, sessionID)
	s.mu.Unlock()
	return nil
}

// unregisterProcess stops tracking sessionID's process.
func (s *Service) unregisterProcess(sessionID string) {
	s.mu.Lock()
	delete(s.procs, sessionID)
	delete(s.closing, sessionID)
	s.mu.Unlock()
}

// isClosing reports whether sessionID's process is being closed
// intentionally (via Close, Shutdown or eviction), as opposed to exiting on
// its own.
func (s *Service) isClosing(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closing[sessionID]
}

// setState updates a session's lifecycle state and publishes the change on
// the bus. Every state change goes through this method.
func (s *Service) setState(ctx context.Context, sessionID string, ownerID *string, state domain.SessionState) error {
	if err := s.repos.Sessions.UpdateState(ctx, sessionID, state); err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]string{"state": string(state)})
	s.bus.Publish(events.Message{Kind: "session.state", SessionID: sessionID, OwnerID: ownerID, Payload: payload})
	return nil
}
