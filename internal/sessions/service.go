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

// Repos bundles the repositories the service reads and writes.
type Repos struct {
	Sessions   *db.Sessions
	Events     *db.Events
	Approvals  *db.Approvals
	Workspaces *db.Workspaces
	Profiles   *db.Profiles
	Tokens     *db.Tokens
	Audit      *db.Audit
}

// Options configures the service: how many processes may run concurrently,
// how long an idle session stays open before it is closed, and where each
// process's HOME directory lives.
type Options struct {
	MaxOpen     int // slots for open processes
	IdleTimeout time.Duration
	UsersDir    string // HOME per user: UsersDir/<userID>
	ServiceHome string // HOME for unattended sessions
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
	repos Repos
	h     harness.Harness
	bus   *events.Bus
	box   *crypto.Box
	opt   Options
	slots *slots

	mu      sync.Mutex
	procs   map[string]*procEntry
	closing map[string]bool
}

// New constructs a Service. The returned Service owns no background
// goroutines beyond one pump per live session process; call RunMaintenance
// periodically (e.g. once a minute) and Shutdown on server stop.
func New(r Repos, h harness.Harness, bus *events.Bus, box *crypto.Box, opt Options) *Service {
	s := &Service{
		repos:   r,
		h:       h,
		bus:     bus,
		box:     box,
		opt:     opt,
		procs:   make(map[string]*procEntry),
		closing: make(map[string]bool),
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
}

// Create persists a new session, starts its harness process and sends the
// initial prompt.
func (s *Service) Create(ctx context.Context, actor Actor, in CreateInput) (domain.Session, error) {
	ws, err := s.repos.Workspaces.Get(ctx, in.WorkspaceID)
	if err != nil {
		return domain.Session{}, err
	}
	profile, err := s.repos.Profiles.Get(ctx, in.ProfileID)
	if err != nil {
		return domain.Session{}, err
	}
	// Fail fast, before persisting a session row, when there is no token to run with.
	if _, err := s.resolveToken(ctx, in.Owner); err != nil {
		return domain.Session{}, err
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
		OriginRef:    in.OriginRef,
		CreatedAt:    now,
		LastActiveAt: now,
	}
	if err := s.repos.Sessions.Create(ctx, sess); err != nil {
		return domain.Session{}, err
	}

	if err := s.startProcess(ctx, sess, *ws, *profile, false, in.Prompt); err != nil {
		_ = s.repos.Sessions.UpdateState(ctx, sess.ID, domain.SessionFailed)
		return domain.Session{}, err
	}
	return sess, nil
}

// Send delivers text to a session's process. It reopens a closed session by
// starting a new process with Resume: true.
func (s *Service) Send(ctx context.Context, actor Actor, id, text string) error {
	sess, err := s.getVisible(ctx, actor, id)
	if err != nil {
		return err
	}
	if sess.State == domain.SessionWaiting {
		return fmt.Errorf("%w: answer the pending approval first", domain.ErrConflict)
	}

	s.mu.Lock()
	entry, ok := s.procs[id]
	s.mu.Unlock()
	if ok {
		return entry.proc.Send(ctx, harness.UserMessage{Text: text})
	}

	ws, err := s.repos.Workspaces.Get(ctx, sess.WorkspaceID)
	if err != nil {
		return err
	}
	profile, err := s.repos.Profiles.Get(ctx, sess.ProfileID)
	if err != nil {
		return err
	}
	if err := s.startProcess(ctx, sess, *ws, *profile, true, text); err != nil {
		return err
	}
	return nil
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
// first message. On any failure before the process starts, the slot is
// released.
func (s *Service) startProcess(ctx context.Context, sess domain.Session, ws domain.Workspace, profile domain.Profile, resume bool, firstMessage string) error {
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
		Cwd:       ws.Path,
		Home:      home,
		Env:       map[string]string{"CLAUDE_CODE_OAUTH_TOKEN": token},
		Profile: harness.Profile{
			Mode:            profile.Mode,
			AllowedTools:    profile.AllowedTools,
			DisallowedTools: profile.DisallowedTools,
			MaxTurns:        profile.MaxTurns,
		},
	}
	p, err := s.h.Start(ctx, spec)
	if err != nil {
		s.slots.release()
		return err
	}
	s.registerProcess(sess.ID, sess.OwnerID, p)
	go s.pump(sess, p)

	if resume {
		if err := s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionRunning); err != nil {
			return err
		}
	}
	return p.Send(ctx, harness.UserMessage{Text: firstMessage})
}

// registerProcess tracks a live process under sessionID.
func (s *Service) registerProcess(sessionID string, ownerID *string, p harness.Process) {
	s.mu.Lock()
	s.procs[sessionID] = &procEntry{proc: p, ownerID: ownerID}
	delete(s.closing, sessionID)
	s.mu.Unlock()
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
