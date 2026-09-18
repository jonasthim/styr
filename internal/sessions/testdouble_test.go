package sessions

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jonasthim/styr/internal/harness"
)

// controlledProcess is a minimal harness.Process test double, independent
// of internal/harness/fake's scripted Harness, for tests that need to
// control exactly what an individual call does — in particular, forcing
// Decide to fail. internal/harness/fake's Process always succeeds its
// Decide call, so it cannot exercise the error paths these regression
// tests target.
//
// Tests using controlledProcess register it directly into a Service's
// tracked process map (via registerProcess) rather than going through
// Create/Send, since they only need to exercise the approval-decide and
// expiry code paths, not full process startup.
type controlledProcess struct {
	events chan harness.Event

	mu        sync.Mutex
	decideErr error
	decisions []harness.Decision
	sent      []harness.UserMessage
	closed    bool
}

func newControlledProcess() *controlledProcess {
	return &controlledProcess{events: make(chan harness.Event, 16)}
}

func (p *controlledProcess) Events() <-chan harness.Event { return p.events }

func (p *controlledProcess) Send(ctx context.Context, m harness.UserMessage) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, m)
	return nil
}

// Decide returns decideErr (set via setDecideErr) instead of recording the
// decision when one is configured; otherwise it records d like
// internal/harness/fake's Process does.
func (p *controlledProcess) Decide(ctx context.Context, d harness.Decision) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.decideErr != nil {
		return p.decideErr
	}
	p.decisions = append(p.decisions, d)
	return nil
}

func (p *controlledProcess) Interrupt(ctx context.Context) error { return nil }

func (p *controlledProcess) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.events)
	}
	return nil
}

func (p *controlledProcess) setDecideErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.decideErr = err
}

func (p *controlledProcess) Decisions() []harness.Decision {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]harness.Decision(nil), p.decisions...)
}

// recordingHandler is a slog.Handler that keeps every record it is asked to
// handle, for tests asserting that a failure was logged rather than
// silently discarded.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *recordingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *recordingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}
