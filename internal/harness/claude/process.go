// process.go owns the exec.Cmd lifecycle for a running Claude Code CLI process: spawning it,
// pumping decoded stdout lines onto the Events channel, and writing encoded stdin lines for
// Send/Decide/Interrupt. See harness.go for argv construction and codec.go for the wire format.
package claude

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/harness"
)

// process implements harness.Process by driving a real (or fake) Claude Code CLI subprocess.
type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan harness.Event
	sid    string
	mu     sync.Mutex // guards stdin writes and closed
	closed bool
	done   chan struct{}
}

// passthroughEnvVars are the parent process environment variables forwarded to the child
// unchanged. Nothing else from the parent's environment is inherited: in particular
// XDG_CONFIG_HOME is deliberately left unset for the child even when set for Styr itself, so
// the CLI never picks up host-side XDG config by accident.
var passthroughEnvVars = []string{"PATH", "TERM", "LANG"}

func startProcess(ctx context.Context, binary string, args []string, spec harness.StartSpec) (*process, error) {
	// exec.Command, not CommandContext: a session process must outlive the context used to
	// start it (e.g. an HTTP request context), and is instead torn down via Close.
	cmd := exec.Command(binary, args...)
	cmd.Dir = spec.Cwd
	cmd.WaitDelay = 5 * time.Second

	env := []string{"HOME=" + spec.Home}
	for _, k := range passthroughEnvVars {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	p := &process{cmd: cmd, stdin: stdin, events: make(chan harness.Event, 256), sid: spec.SessionID, done: make(chan struct{})}

	var lastErr string
	var errMu sync.Mutex
	go func() { // stderr: keep the last line for the exit event
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			errMu.Lock()
			lastErr = sc.Text()
			errMu.Unlock()
		}
	}()

	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			for _, ev := range DecodeLine(sc.Bytes(), time.Now()) {
				p.events <- ev
			}
		}
		err := cmd.Wait()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			code = -1
		}
		errMu.Lock()
		exitErrText := lastErr
		errMu.Unlock()
		// This goroutine runs exactly once per process, so the exit event is sent and the
		// channels are closed exactly once regardless of how many times Close is called.
		p.events <- harness.Event{Type: harness.EventExit, At: time.Now(), ExitCode: code, Err: exitErrText}
		close(p.events)
		close(p.done)
	}()

	return p, nil
}

func (p *process) Events() <-chan harness.Event { return p.events }

func (p *process) write(b []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("claude: process closed")
	}
	_, err := p.stdin.Write(b)
	return err
}

// Send writes a user turn to the process's stdin.
func (p *process) Send(ctx context.Context, m harness.UserMessage) error {
	return p.write(EncodeUser(p.sid, m.Text))
}

// Decide answers a pending permission_request.
func (p *process) Decide(ctx context.Context, d harness.Decision) error {
	return p.write(EncodeDecision(d))
}

// Interrupt asks the CLI to stop the current turn. The CLI acknowledges with a
// control_response carrying response.response.still_queued (see testdata/PROTOCOL.md, fixture
// 05); the runner does not need to parse that acknowledgement, so DecodeLine intentionally
// discards all control_response lines.
func (p *process) Interrupt(ctx context.Context) error {
	return p.write(EncodeInterrupt(uuid.NewString()))
}

// Close closes stdin (signalling the CLI to exit), waits up to 10s for the process to exit on
// its own, then kills it. Safe to call more than once; only the first call has any effect.
func (p *process) Close(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		p.stdin.Close()
	}
	p.mu.Unlock()

	select {
	case <-p.done:
		return nil
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
		return nil
	case <-ctx.Done():
		_ = p.cmd.Process.Kill()
		return ctx.Err()
	}
}
