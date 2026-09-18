package sessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/risk"
)

// approvalCreateFailedMessage is what the child sees denying its permission
// request when styr could not persist the corresponding approval row: the
// request cannot be left unanswered (the child would hang waiting for a
// control_response), and it cannot be safely allowed either, so it is
// denied.
const approvalCreateFailedMessage = "styr could not record the approval"

// pump drains p's event stream for the lifetime of the process, persisting
// and publishing every event and applying the session-state side effects
// each event type carries. It runs as its own goroutine, so it uses a
// background context rather than one tied to the request that started p.
func (s *Service) pump(sess domain.Session, p harness.Process) {
	ctx := context.Background()

	turns := sess.NumTurns
	cost := sess.CostUSD
	tokensIn := sess.TokensIn
	tokensOut := sess.TokensOut

	for ev := range p.Events() {
		_ = s.repos.Sessions.Touch(ctx, sess.ID)

		payload, err := json.Marshal(ev)
		if err != nil {
			payload = []byte(`{}`)
		}

		var seq int64
		if ev.Type != harness.EventPartial {
			seq, _ = s.repos.Events.Append(ctx, sess.ID, string(ev.Type), payload)
		}
		s.bus.Publish(events.Message{Kind: "session.event", SessionID: sess.ID, OwnerID: sess.OwnerID, Seq: seq, Payload: payload})

		switch ev.Type {
		case harness.EventInit:
			if ev.Init != nil {
				// The CLI reports the model it actually resolved (a full name such as
				// "claude-fable-5-1", not the alias Styr asked for) and the commands it
				// would accept as /name turns; both are stored on the session row so the
				// UI can show them without a live process.
				_ = s.repos.Sessions.UpdateModel(ctx, sess.ID, ev.Init.Model)
				_ = s.repos.Sessions.UpdateSlashCommands(ctx, sess.ID, ev.Init.SlashCommands)
			}
		case harness.EventToolUse:
			if ev.ToolUse != nil {
				_ = s.repos.Sessions.UpdateNow(ctx, sess.ID, risk.Summary(ev.ToolUse.Name, ev.ToolUse.Input))
			}
			_ = s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionRunning)
		case harness.EventPermission:
			s.handlePermission(ctx, sess, ev, p)
		case harness.EventResult:
			if ev.Result != nil {
				turns += ev.Result.NumTurns
				cost += ev.Result.CostUSD
				tokensIn += ev.Result.InputTokens
				tokensOut += ev.Result.OutputTokens
			}
			_ = s.repos.Sessions.UpdateStats(ctx, sess.ID, turns, cost, tokensIn, tokensOut)
			_ = s.repos.Sessions.UpdateNow(ctx, sess.ID, "")
			// A worktree session checkpoints the turn's changes and refreshes its diff
			// counters before the stats go out, so session.stats carries both.
			add, del := s.afterResult(ctx, sess, turns)
			s.publishStats(sess, turns, cost, tokensIn, tokensOut, add, del)
			_ = s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionOpen)
		case harness.EventExit:
			s.handleExit(ctx, sess, ev)
		}
	}
}

// handlePermission records a new approval for a permission request and
// moves the session to waiting for a human decision.
//
// If Approvals.Create fails, the child is left blocked on a
// control_request no one will ever answer unless we answer it here: log
// the failure, deny the request (message approvalCreateFailedMessage) so
// the child is unblocked, and fail the session — there is no pending
// approval a human could later decide, so "waiting" would be a dead end.
func (s *Service) handlePermission(ctx context.Context, sess domain.Session, ev harness.Event, p harness.Process) {
	if ev.Permission == nil {
		return
	}
	ap := domain.Approval{
		ID:        s.newApprovalID(),
		SessionID: sess.ID,
		RequestID: ev.Permission.RequestID,
		Tool:      ev.Permission.ToolName,
		Input:     ev.Permission.Input,
		Risk:      risk.Classify(ev.Permission.ToolName, ev.Permission.Input),
		State:     domain.ApprovalPending,
		CreatedAt: time.Now(),
		// An ExitPlanMode request carries the finished plan markdown; it is stored on the
		// approval so the inbox and session view can render the plan card without a live
		// process (see harness.PermissionRequest.Plan).
		Plan: ev.Permission.Plan,
	}
	if err := s.repos.Approvals.Create(ctx, ap); err != nil {
		s.logger.Error("record approval failed", "session_id", sess.ID, "request_id", ev.Permission.RequestID, "error", err)
		if dErr := p.Decide(ctx, harness.Decision{RequestID: ev.Permission.RequestID, Allow: false, Message: approvalCreateFailedMessage}); dErr != nil {
			s.logger.Error("deny after approval create failure: notify process failed", "session_id", sess.ID, "request_id", ev.Permission.RequestID, "error", dErr)
		}
		_ = s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionFailed)
		return
	}
	_ = s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionWaiting)

	if payload, err := json.Marshal(ap); err == nil {
		s.bus.Publish(events.Message{Kind: "approval.created", SessionID: sess.ID, OwnerID: sess.OwnerID, Payload: payload})
	}
}

// publishStats publishes the cumulative turn/cost/token counters and the worktree's diff
// counters after a result event. A session without a worktree reports zero added and removed
// lines.
func (s *Service) publishStats(sess domain.Session, turns int, cost float64, tokensIn, tokensOut, diffAdd, diffDel int) {
	payload, err := json.Marshal(map[string]any{
		"num_turns":  turns,
		"cost_usd":   cost,
		"tokens_in":  tokensIn,
		"tokens_out": tokensOut,
		"diff_add":   diffAdd,
		"diff_del":   diffDel,
	})
	if err != nil {
		return
	}
	s.bus.Publish(events.Message{Kind: "session.stats", SessionID: sess.ID, OwnerID: sess.OwnerID, Payload: payload})
}

// handleExit finalizes a session's process when it exits: closed when the
// exit was requested (Close, Shutdown or eviction) or clean (exit code 0),
// failed otherwise. It always releases the process's scheduler slot.
func (s *Service) handleExit(ctx context.Context, sess domain.Session, ev harness.Event) {
	closing := s.isClosing(sess.ID)
	state := domain.SessionFailed
	if closing || ev.ExitCode == 0 {
		state = domain.SessionClosed
	}
	_ = s.setState(ctx, sess.ID, sess.OwnerID, state)
	s.unregisterProcess(sess.ID)
	s.slots.release()
}
