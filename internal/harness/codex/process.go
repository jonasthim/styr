// process.go owns the process lifecycle of a Codex session. `codex exec` takes exactly one
// prompt per process and exits when the turn ends (testdata/PROTOCOL.md "One process per
// turn"), so a session here is a sequence of short-lived OS processes — the first a fresh
// `codex exec`, every later one a `codex exec resume <thread id>` — behind one long-lived
// events channel that only Close shuts.
package codex

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/jonasthim/styr/internal/harness"
)

// passthroughEnvVars are the parent process environment variables forwarded to the child
// unchanged, matching the Claude runner. Nothing else from the parent's environment is
// inherited; the Codex credentials the child needs come from HOME (the user's own
// `codex login` under their Styr home) or from StartSpec.Env (e.g. OPENAI_API_KEY).
var passthroughEnvVars = []string{"PATH", "TERM", "LANG"}

// process implements harness.Process for the Codex CLI.
type process struct {
	binary string
	spec   harness.StartSpec // JSONSchema rewritten to schemaPath, if any
	events chan harness.Event

	mu          sync.Mutex
	closed      bool
	thread      string        // learned from the first thread.started line
	cur         *exec.Cmd     // the running turn's process, nil when idle
	turnDone    chan struct{} // closed when the running turn's goroutine is finished
	interrupted bool          // the running turn was killed by Interrupt
	resultSent  bool          // the running turn has already emitted its result event
	lastExit    int           // exit code of the most recent turn
	schemaPath  string        // temp file holding StartSpec.JSONSchema, removed by Close
}

// newProcess prepares a session without spawning anything: the first OS process runs when the
// caller sends the first turn. When the spec carries a JSON schema it is written to a temp
// file here, once per session, because `--output-schema` takes a path rather than the schema
// text itself.
func newProcess(binary string, spec harness.StartSpec) (*process, error) {
	p := &process{binary: binary, spec: spec, events: make(chan harness.Event, 256)}
	if spec.JSONSchema != "" {
		f, err := os.CreateTemp("", "styr-codex-schema-*.json")
		if err != nil {
			return nil, fmt.Errorf("codex: write output schema: %w", err)
		}
		if _, err := f.WriteString(spec.JSONSchema); err != nil {
			f.Close()
			os.Remove(f.Name())
			return nil, fmt.Errorf("codex: write output schema: %w", err)
		}
		if err := f.Close(); err != nil {
			os.Remove(f.Name())
			return nil, fmt.Errorf("codex: write output schema: %w", err)
		}
		p.schemaPath = f.Name()
		p.spec.JSONSchema = f.Name()
	}
	return p, nil
}

func (p *process) Events() <-chan harness.Event { return p.events }

// Send starts one turn: a `codex exec` process for the first turn of the session, a
// `codex exec resume <thread>` process for every later one. It returns as soon as the child is
// running; the turn's events arrive on Events() and end with an EventResult. Sending while a
// turn is still running is an error — the CLI has no way to accept a second prompt.
func (p *process) Send(ctx context.Context, m harness.UserMessage) error {
	// A turn's result event is emitted as soon as the CLI reports it, a moment before the
	// child is reaped and the turn's bookkeeping is cleared. A caller that sends the next turn
	// the instant it sees the result is therefore not sending into a running turn but into a
	// winding-down one, and waits for it rather than being refused.
wait:
	for {
		p.mu.Lock()
		switch {
		case p.closed:
			p.mu.Unlock()
			return errors.New("codex: process closed")
		case p.turnDone == nil:
			break wait // idle, and holding the lock
		case !p.resultSent:
			p.mu.Unlock()
			return errors.New("codex: a turn is already running")
		default:
			done := p.turnDone
			p.mu.Unlock()
			select {
			case <-done:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	args := BuildArgs(p.spec, m.Text, p.thread)

	// exec.Command, not CommandContext: a turn must outlive the context used to start it
	// (e.g. an HTTP request context), and is instead torn down via Interrupt or Close.
	cmd := exec.Command(p.binary, args...)
	cmd.Dir = p.spec.Cwd
	cmd.Env = p.childEnv()
	cmd.WaitDelay = 5 * time.Second
	// Stdin stays at /dev/null: `codex exec` folds piped stdin into the *first* prompt as a
	// <stdin> block, so it is not a channel for later turns and must not carry anything.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		p.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		p.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		p.mu.Unlock()
		return fmt.Errorf("codex: start turn: %w", err)
	}
	done := make(chan struct{})
	p.cur, p.turnDone = cmd, done
	p.interrupted, p.resultSent = false, false
	p.mu.Unlock()

	go p.runTurn(cmd, stdout, stderr, done)
	return nil
}

// childEnv builds the child's environment: an isolated HOME, the passthrough allowlist, and
// whatever the spec adds (OPENAI_API_KEY among them). Sorted so the argv/env of a turn is
// reproducible for tests and logs.
func (p *process) childEnv() []string {
	env := []string{"HOME=" + p.spec.Home}
	for _, k := range passthroughEnvVars {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	keys := make([]string, 0, len(p.spec.Env))
	for k := range p.spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+p.spec.Env[k])
	}
	return env
}

// runTurn pumps one turn's stdout onto the events channel and closes done when the child has
// exited and every event of the turn has been emitted. Close waits for done before closing the
// channel, so nothing is ever sent on a closed channel.
func (p *process) runTurn(cmd *exec.Cmd, stdout, stderr io.Reader, done chan struct{}) {
	defer close(done)
	// Registered second, so it runs *before* close(done) but *after* the last event has been
	// sent: Close decides whether to wait on the turn by reading turnDone, and clearing it any
	// earlier would let Close close the events channel out from under a pending send.
	defer func() {
		p.mu.Lock()
		p.cur, p.turnDone = nil, nil
		p.mu.Unlock()
	}()

	var lastErr string
	var errMu sync.Mutex
	errDone := make(chan struct{})
	go func() { // stderr: keep the last line, the only place a local failure is reported
		defer close(errDone)
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			if line := sc.Text(); line != "" {
				errMu.Lock()
				lastErr = line
				errMu.Unlock()
			}
		}
	}()

	dec := NewDecoder(p.spec.Model, p.spec.JSONSchema != "")
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		for _, ev := range dec.Line(sc.Bytes(), time.Now()) {
			switch {
			case ev.Type == harness.EventInit && ev.Init != nil && ev.Init.SessionID != "":
				p.mu.Lock()
				p.thread = ev.Init.SessionID
				p.mu.Unlock()
			case ev.Type == harness.EventResult:
				p.mu.Lock()
				p.resultSent = true
				p.mu.Unlock()
			}
			p.events <- ev
		}
	}
	<-errDone

	code := 0
	if err := cmd.Wait(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	errMu.Lock()
	stderrText := lastErr
	errMu.Unlock()

	p.mu.Lock()
	p.lastExit = code
	interrupted := p.interrupted
	p.mu.Unlock()

	if dec.SawResult() {
		return
	}
	// The turn produced no terminal event: either Interrupt killed it, or the CLI failed
	// before reaching the API and said so only on stderr (testdata/PROTOCOL.md, fixture 05).
	r := &harness.Result{Subtype: "error", IsError: true, NumTurns: 1, Text: stderrText}
	if interrupted {
		r.Subtype, r.Text = "interrupted", "turn interrupted"
	}
	p.mu.Lock()
	p.resultSent = true
	p.mu.Unlock()
	p.events <- harness.Event{Type: harness.EventResult, At: time.Now(), Result: r}
}

// Decide always fails with harness.ErrUnsupported: `codex exec` has no host-side approval
// channel at all, so there is never a pending permission request to answer. Styr expresses a
// Codex session's permissions as the sandbox policy instead (docs/DECISIONS.md ADR-018).
func (p *process) Decide(ctx context.Context, d harness.Decision) error {
	return harness.ErrUnsupported
}

// Interrupt stops the running turn by killing its process; the turn then ends with an
// EventResult of subtype "interrupted". Interrupting an idle session does nothing.
func (p *process) Interrupt(ctx context.Context) error {
	p.mu.Lock()
	cmd := p.cur
	if cmd != nil {
		p.interrupted = true
	}
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

// Close ends the session: it kills a running turn, waits for that turn's events to be
// delivered, emits a final EventExit carrying the last turn's exit code, and closes the events
// channel. Safe to call more than once; only the first call has any effect.
func (p *process) Close(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	cmd, done, schema := p.cur, p.turnDone, p.schemaPath
	p.schemaPath = ""
	p.mu.Unlock()

	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	var err error
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			// The child has been killed, so its goroutine is about to finish either way; wait
			// for it so the events channel is always closed exactly once, and report the
			// deadline to the caller.
			err = ctx.Err()
			<-done
		}
	}
	if schema != "" {
		os.Remove(schema)
	}

	p.mu.Lock()
	code := p.lastExit
	p.mu.Unlock()
	p.events <- harness.Event{Type: harness.EventExit, At: time.Now(), ExitCode: code}
	close(p.events)
	return err
}
