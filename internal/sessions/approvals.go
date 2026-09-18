package sessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/harness"
)

// Decide answers a pending approval: it records the approval, forwards the
// decision to the session's process, moves the session back to running, and
// writes an audit entry.
//
// The database write happens before the child is ever touched.
// Approvals.Decide's UPDATE only affects a row still in state 'pending', so
// it is the single point that decides who wins a race to answer this
// approval (a second human clicking decide, or the scheduler's expiry
// sweep in expireApprovals racing the same approval): the loser gets
// domain.ErrConflict back and returns immediately, never writing to the
// child, so the child never sees two control_responses for the same
// request.
func (s *Service) Decide(ctx context.Context, actor Actor, approvalID string, allow bool, updated json.RawMessage, msg string) error {
	ap, err := s.repos.Approvals.Get(ctx, approvalID)
	if err != nil {
		return err
	}
	sess, err := s.getVisible(ctx, actor, ap.SessionID)
	if err != nil {
		return err
	}

	state := domain.ApprovalDenied
	action := "approval.deny"
	if allow {
		state = domain.ApprovalAllowed
		action = "approval.allow"
	}
	if err := s.repos.Approvals.Decide(ctx, approvalID, state, actor.UserID, updated, msg); err != nil {
		return err
	}

	s.mu.Lock()
	entry, ok := s.procs[sess.ID]
	s.mu.Unlock()
	if ok {
		if err := entry.proc.Decide(ctx, harness.Decision{
			RequestID:    ap.RequestID,
			Allow:        allow,
			UpdatedInput: updated,
			Message:      msg,
		}); err != nil {
			return err
		}
	}

	if err := s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionRunning); err != nil {
		return err
	}
	if err := s.repos.Audit.Append(ctx, actor.UserID, action, approvalID, nil); err != nil {
		return err
	}

	if updatedAp, err := s.repos.Approvals.Get(ctx, approvalID); err == nil {
		if payload, err := json.Marshal(updatedAp); err == nil {
			s.bus.Publish(events.Message{Kind: "approval.decided", SessionID: sess.ID, OwnerID: sess.OwnerID, Payload: payload})
		}
	}
	return nil
}

// PendingApprovals returns every pending approval visible to actor.
func (s *Service) PendingApprovals(ctx context.Context, actor Actor) ([]domain.Approval, error) {
	return s.repos.Approvals.ListPendingVisible(ctx, actor.UserID, actor.IsAdmin)
}

// Snooze defers a pending approval until the given time.
func (s *Service) Snooze(ctx context.Context, actor Actor, approvalID string, until time.Time) error {
	ap, err := s.repos.Approvals.Get(ctx, approvalID)
	if err != nil {
		return err
	}
	if _, err := s.getVisible(ctx, actor, ap.SessionID); err != nil {
		return err
	}
	return s.repos.Approvals.Snooze(ctx, approvalID, until)
}
