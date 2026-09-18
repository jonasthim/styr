// model.go implements switching a live session to a different model or reasoning effort.
// The CLI takes both as start-up flags only, so a switch means ending the current process
// and resuming the same session id under new flags; the CLI keeps the transcript, and Styr's
// own event log is never interrupted.
package sessions

import (
	"context"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/harness"
)

// switchWaitPoll is how often SwitchModel rechecks whether the process it closed has
// finished exiting (its pump goroutine unregisters it on EventExit).
const switchWaitPoll = 20 * time.Millisecond

// SwitchModel restarts a session's process with a new model and reasoning effort, resuming
// the same session id so the CLI keeps the transcript.
//
// It refuses while the session is waiting on an approval (domain.ErrConflict): the child is
// blocked on a control_request that only the current process can answer, and killing it
// would strand the approval. Otherwise the live process (if any) is closed, the new values
// are persisted, and a resumed process is started with no first message — the operator's
// next turn goes to it. The resulting state change is published as session.state.
//
// An empty model means "the CLI's own default"; an empty effort likewise.
func (s *Service) SwitchModel(ctx context.Context, actor Actor, id, model, effort string) error {
	sess, err := s.getVisible(ctx, actor, id)
	if err != nil {
		return err
	}
	if !harness.ValidEffort(effort) {
		return fmt.Errorf("%w: effort %q is not allowed", domain.ErrInvalid, effort)
	}
	if sess.State == domain.SessionWaiting {
		return fmt.Errorf("%w: answer the pending approval before switching model", domain.ErrConflict)
	}

	ws, err := s.repos.Workspaces.Get(ctx, sess.WorkspaceID)
	if err != nil {
		return err
	}
	profile, err := s.repos.Profiles.Get(ctx, sess.ProfileID)
	if err != nil {
		return err
	}

	if err := s.closeLiveProcess(ctx, sess.ID); err != nil {
		return err
	}

	if err := s.repos.Sessions.UpdateModel(ctx, sess.ID, model); err != nil {
		return err
	}
	if err := s.repos.Sessions.UpdateEffort(ctx, sess.ID, effort); err != nil {
		return err
	}
	sess.Model, sess.Effort = model, effort

	// startProcess publishes the session.state change (running) once the resumed process is
	// tracked. With no first message the process simply waits for the next Send.
	if err := s.startProcess(ctx, sess, *ws, *profile, true, "", startOptions{}); err != nil {
		_ = s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionFailed)
		return err
	}
	return nil
}

// closeLiveProcess closes the session's tracked process, if it has one, and waits for its
// pump goroutine to unregister it, so the caller can immediately start a replacement without
// tripping registerProcess's one-process-per-session rule.
func (s *Service) closeLiveProcess(ctx context.Context, id string) error {
	s.mu.Lock()
	entry, ok := s.procs[id]
	if ok {
		s.closing[id] = true
	}
	s.mu.Unlock()
	if !ok {
		return nil
	}
	if err := entry.proc.Close(ctx); err != nil {
		return err
	}

	deadline := time.Now().Add(startWaitTimeout)
	for {
		s.mu.Lock()
		_, still := s.procs[id]
		s.mu.Unlock()
		if !still {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w: timed out closing the session's process", domain.ErrConflict)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(switchWaitPoll):
		}
	}
}
