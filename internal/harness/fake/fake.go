// Package fake provides an in-memory harness.Harness for tests and local development: it
// never execs a process. Each session replays a scripted sequence of Steps as events, and can
// optionally pause a step after its first event (a permission request) until the caller
// answers with Decide.
package fake

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jonasthim/styr/internal/harness"
)

// Step is one scripted turn: the events a Send should emit, optionally pausing after the
// first event (a permission request) until Decide is called.
type Step struct {
	Events          []harness.Event // emitted in order after a Send
	WaitForDecision bool            // if true, the step emits Events[0] then blocks until Decide
}

// Harness is a scripted, in-memory harness.Harness. Every Start gets its own fresh copy of
// Steps, so the same Harness can be reused to start several sessions with identical scripts.
type Harness struct {
	Steps []Step
	mu    sync.Mutex
	Procs []*Process // every Process ever started, for test inspection
}

// New returns a Harness that replays steps, in order, one per Send, for every session it starts.
func New(steps ...Step) *Harness { return &Harness{Steps: steps} }

// Kind identifies this Harness as the fake, in-memory implementation.
func (h *Harness) Kind() harness.Kind { return "fake" }

// Start validates spec and starts a new scripted Process.
func (h *Harness) Start(ctx context.Context, spec harness.StartSpec) (harness.Process, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	p := &Process{
		steps:   append([]Step(nil), h.Steps...),
		events:  make(chan harness.Event, 256),
		decided: make(chan harness.Decision, 8),
		Spec:    spec,
	}
	h.mu.Lock()
	h.Procs = append(h.Procs, p)
	h.mu.Unlock()
	return p, nil
}

// Process is a scripted, in-memory harness.Process. Its exported fields record everything the
// caller did, for tests to assert against.
type Process struct {
	Spec       harness.StartSpec
	Sent       []harness.UserMessage
	Decisions  []harness.Decision
	Interrupts int
	Closed     bool

	mu      sync.Mutex
	steps   []Step
	events  chan harness.Event
	decided chan harness.Decision
}

// Events returns the channel of scripted events.
func (p *Process) Events() <-chan harness.Event { return p.events }

// Send records m and, if a step remains, replays its events asynchronously; a permission step
// emits only its first event before pausing for Decide. With no steps left, Send synthesizes a
// trivial successful result so callers do not need to script every turn.
func (p *Process) Send(ctx context.Context, m harness.UserMessage) error {
	p.mu.Lock()
	if p.Closed {
		p.mu.Unlock()
		return errors.New("fake: closed")
	}
	p.Sent = append(p.Sent, m)
	if len(p.steps) == 0 {
		p.mu.Unlock()
		p.events <- harness.Event{Type: harness.EventResult, At: time.Now(), Result: &harness.Result{Subtype: "success", NumTurns: 1}}
		return nil
	}
	step := p.steps[0]
	p.steps = p.steps[1:]
	p.mu.Unlock()

	go func() {
		for i, ev := range step.Events {
			ev.At = time.Now()
			p.events <- ev
			if i == 0 && step.WaitForDecision {
				<-p.decided
			}
		}
	}()
	return nil
}

// Decide records d and, if a step is currently paused for it, unblocks it.
func (p *Process) Decide(ctx context.Context, d harness.Decision) error {
	p.mu.Lock()
	p.Decisions = append(p.Decisions, d)
	p.mu.Unlock()
	select {
	case p.decided <- d:
	default:
	}
	return nil
}

// Interrupt records the interrupt and emits a synthetic "interrupted" result, mirroring the
// real CLI's behaviour of resuming with a result after an interrupt (see
// internal/harness/claude/testdata/PROTOCOL.md, fixture 05).
func (p *Process) Interrupt(ctx context.Context) error {
	p.mu.Lock()
	p.Interrupts++
	p.mu.Unlock()
	p.events <- harness.Event{Type: harness.EventResult, At: time.Now(), Result: &harness.Result{Subtype: "interrupted", NumTurns: 1}}
	return nil
}

// Close marks the process closed, emits an exit event, and closes the events channel. Safe to
// call more than once; only the first call has any effect.
func (p *Process) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Closed {
		return nil
	}
	p.Closed = true
	p.events <- harness.Event{Type: harness.EventExit, At: time.Now()}
	close(p.events)
	return nil
}
