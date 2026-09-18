package sessions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/harness"
)

// slots is a counting semaphore over MaxOpen concurrently running processes.
// An interactive acquire that finds every slot busy triggers one eviction
// attempt (closing the oldest idle unattended process) before it waits.
type slots struct {
	sem   chan struct{}
	evict func(ctx context.Context) bool

	// wait is how long acquire waits for a slot before giving up. It is a
	// field, not a constant, only so tests can shrink it; production code
	// always gets 30s from newSlots.
	wait time.Duration
}

// newSlots returns a slots with n slots and a 30s acquire wait.
func newSlots(n int, evict func(ctx context.Context) bool) *slots {
	if n < 1 {
		n = 1
	}
	return &slots{sem: make(chan struct{}, n), evict: evict, wait: 30 * time.Second}
}

// acquire blocks for a free slot, up to s.wait. When interactive and no
// slot is immediately free, it asks evict to close the oldest idle
// unattended process once before waiting.
func (s *slots) acquire(ctx context.Context, interactive bool) error {
	select {
	case s.sem <- struct{}{}:
		return nil
	default:
	}

	if interactive && s.evict != nil {
		s.evict(ctx)
	}

	timer := time.NewTimer(s.wait)
	defer timer.Stop()
	select {
	case s.sem <- struct{}{}:
		return nil
	case <-timer.C:
		return fmt.Errorf("%w: all session slots busy", domain.ErrConflict)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release frees one slot.
func (s *slots) release() {
	select {
	case <-s.sem:
	default:
	}
}

// evictOldestIdleUnattended closes the oldest tracked process whose session
// is idle (state open) and whose profile is unattended, freeing one slot. It
// returns whether it found and closed one.
func (s *Service) evictOldestIdleUnattended(ctx context.Context) bool {
	s.mu.Lock()
	ids := make([]string, 0, len(s.procs))
	for id := range s.procs {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	var (
		oldestID string
		oldestAt time.Time
		found    bool
	)
	for _, id := range ids {
		sess, err := s.repos.Sessions.Get(ctx, id)
		if err != nil || sess.State != domain.SessionOpen {
			continue
		}
		profile, err := s.repos.Profiles.Get(ctx, sess.ProfileID)
		if err != nil || !profile.Unattended {
			continue
		}
		if !found || sess.LastActiveAt.Before(oldestAt) {
			oldestID, oldestAt, found = sess.ID, sess.LastActiveAt, true
		}
	}
	if !found {
		return false
	}
	s.closeTracked(ctx, oldestID)
	return true
}

// closeTracked marks a tracked session's process as closing intentionally
// and closes it, letting the pump goroutine finish the state transition and
// release the slot.
func (s *Service) closeTracked(ctx context.Context, id string) {
	s.mu.Lock()
	entry, ok := s.procs[id]
	if ok {
		s.closing[id] = true
	}
	s.mu.Unlock()
	if ok {
		_ = entry.proc.Close(ctx)
	}
}

// RunMaintenance reaps idle sessions and expires stale pending approvals on
// unattended profiles. Call it periodically (e.g. once a minute) from serve.
func (s *Service) RunMaintenance(ctx context.Context) {
	s.reapIdle(ctx)
	s.expireApprovals(ctx)
}

// reapIdle closes every session in state open whose last activity is older
// than IdleTimeout.
func (s *Service) reapIdle(ctx context.Context) {
	all, err := s.repos.Sessions.ListVisible(ctx, "", true)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-s.opt.IdleTimeout)
	for _, sess := range all {
		if sess.State != domain.SessionOpen {
			continue
		}
		if sess.LastActiveAt.After(cutoff) {
			continue
		}
		s.closeTracked(ctx, sess.ID)
	}
}

// expireApprovals denies every pending approval whose session's profile is
// unattended and whose age exceeds that profile's ApprovalTimeout.
//
// For each expiring approval with a live process, that process is notified
// first. If the notify fails, the failure is logged and this approval is
// left untouched — no database write, no state change — so the pending
// approval is not silently abandoned: the next maintenance tick retries it
// from scratch. Only once the notify succeeds (or there is no live process
// to notify) does the database write happen, via the same guarded
// Approvals.Decide as the human Decide path (internal/sessions/approvals.go):
// its "AND state = 'pending'" means that if a human decided this exact
// approval between ListPendingVisible above and here, this call returns
// domain.ErrConflict and the loop moves on without an audit entry or a
// session state change, since the human decision already accounted for
// both.
func (s *Service) expireApprovals(ctx context.Context) {
	pending, err := s.repos.Approvals.ListPendingVisible(ctx, "", true)
	if err != nil {
		return
	}
	now := time.Now()
	const timeoutMsg = "timed out waiting for a human"

	for _, ap := range pending {
		sess, err := s.repos.Sessions.Get(ctx, ap.SessionID)
		if err != nil {
			continue
		}
		profile, err := s.repos.Profiles.Get(ctx, sess.ProfileID)
		if err != nil || !profile.Unattended {
			continue
		}
		if now.Sub(ap.CreatedAt) < profile.ApprovalTimeout {
			continue
		}

		s.mu.Lock()
		entry, ok := s.procs[sess.ID]
		s.mu.Unlock()
		if ok {
			if err := entry.proc.Decide(ctx, harness.Decision{RequestID: ap.RequestID, Allow: false, Message: timeoutMsg}); err != nil {
				s.logger.Error("expire approval: notify process failed", "approval_id", ap.ID, "session_id", sess.ID, "error", err)
				continue
			}
		}

		if err := s.repos.Approvals.Decide(ctx, ap.ID, domain.ApprovalExpired, "system", nil, timeoutMsg); err != nil {
			if !errors.Is(err, domain.ErrConflict) {
				s.logger.Error("expire approval: db decide failed", "approval_id", ap.ID, "session_id", sess.ID, "error", err)
			}
			continue
		}
		_ = s.repos.Audit.Append(ctx, "system", "approval.expire", ap.ID, nil)
		_ = s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionOpen)
	}
}
