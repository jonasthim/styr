package sessions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/risk"
)

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
				_ = s.repos.Sessions.UpdateModel(ctx, sess.ID, ev.Init.Model)
			}
		case harness.EventToolUse:
			if ev.ToolUse != nil {
				_ = s.repos.Sessions.UpdateNow(ctx, sess.ID, risk.Summary(ev.ToolUse.Name, ev.ToolUse.Input))
			}
			_ = s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionRunning)
		case harness.EventPermission:
			s.handlePermission(ctx, sess, ev)
		case harness.EventResult:
			if ev.Result != nil {
				turns += ev.Result.NumTurns
				cost += ev.Result.CostUSD
				tokensIn += ev.Result.InputTokens
				tokensOut += ev.Result.OutputTokens
			}
			_ = s.repos.Sessions.UpdateStats(ctx, sess.ID, turns, cost, tokensIn, tokensOut)
			_ = s.repos.Sessions.UpdateNow(ctx, sess.ID, "")
			s.publishStats(sess, turns, cost, tokensIn, tokensOut)
			_ = s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionOpen)
		case harness.EventExit:
			s.handleExit(ctx, sess, ev)
		}
	}
}

// handlePermission records a new approval for a permission request and
// moves the session to waiting for a human decision.
func (s *Service) handlePermission(ctx context.Context, sess domain.Session, ev harness.Event) {
	if ev.Permission == nil {
		return
	}
	ap := domain.Approval{
		ID:        uuid.NewString(),
		SessionID: sess.ID,
		RequestID: ev.Permission.RequestID,
		Tool:      ev.Permission.ToolName,
		Input:     ev.Permission.Input,
		Risk:      risk.Classify(ev.Permission.ToolName, ev.Permission.Input),
		State:     domain.ApprovalPending,
		CreatedAt: time.Now(),
	}
	if err := s.repos.Approvals.Create(ctx, ap); err != nil {
		return
	}
	_ = s.setState(ctx, sess.ID, sess.OwnerID, domain.SessionWaiting)

	if payload, err := json.Marshal(ap); err == nil {
		s.bus.Publish(events.Message{Kind: "approval.created", SessionID: sess.ID, OwnerID: sess.OwnerID, Payload: payload})
	}
}

// publishStats publishes the cumulative turn/cost/token counters after a
// result event.
func (s *Service) publishStats(sess domain.Session, turns int, cost float64, tokensIn, tokensOut int) {
	payload, err := json.Marshal(map[string]any{
		"num_turns":  turns,
		"cost_usd":   cost,
		"tokens_in":  tokensIn,
		"tokens_out": tokensOut,
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
