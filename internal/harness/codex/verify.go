// verify.go checks that an OpenAI API key actually works by running one minimal, read-only
// `codex exec` turn with the key in the child's environment. It implements api.TokenVerifier
// structurally (this package never imports internal/api), exactly as claude.Verifier does.
package codex

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// defaultVerifyTimeout bounds a verification run when Verifier.Timeout is zero. A `codex exec`
// turn round-trips to the API, so the budget matches claude.Verifier's.
const defaultVerifyTimeout = 60 * time.Second

// Verifier verifies an OpenAI API key by spawning Bin with the key in OPENAI_API_KEY and
// inspecting the turn's outcome. The zero value has Bin "" (unusable) and the default 60s
// timeout.
type Verifier struct {
	Bin     string
	Timeout time.Duration
}

// timeout returns v.Timeout, or defaultVerifyTimeout when it is unset.
func (v Verifier) timeout() time.Duration {
	if v.Timeout > 0 {
		return v.Timeout
	}
	return defaultVerifyTimeout
}

// Verify runs
//
//	<Bin> exec --skip-git-repo-check --sandbox read-only -C <tmp> "Reply with pong."
//
// with the key in OPENAI_API_KEY and a fresh, empty HOME and working directory (both removed
// again before Verify returns), and succeeds only when the process exits 0. The sandbox is
// read-only and the working directory is an empty temp directory, so a verification turn can
// neither read nor change anything of the operator's; neither of the CLI's bypass flags is
// emitted here or anywhere else in this package.
//
// The returned error carries the CLI's last output line with the key redacted, so operators
// can tell a rejected key from a broken invocation. The key itself is never logged.
func (v Verifier) Verify(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, v.timeout())
	defer cancel()

	home, err := os.MkdirTemp("", "styr-codex-verify-home-*")
	if err != nil {
		return fmt.Errorf("codex: verify: create temp home: %w", err)
	}
	defer os.RemoveAll(home)

	cwd, err := os.MkdirTemp("", "styr-codex-verify-cwd-*")
	if err != nil {
		return fmt.Errorf("codex: verify: create temp dir: %w", err)
	}
	defer os.RemoveAll(cwd)

	cmd := exec.CommandContext(ctx, v.Bin,
		"exec", "--skip-git-repo-check", "--sandbox", sandboxReadOnly, "-C", cwd, "Reply with pong.")
	cmd.Dir = cwd
	cmd.Env = []string{
		"OPENAI_API_KEY=" + key,
		"HOME=" + home,
	}
	if path, ok := os.LookupEnv("PATH"); ok {
		cmd.Env = append(cmd.Env, "PATH="+path)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// Anything quoted from the CLI is redacted first: a misbehaving CLI could echo the key,
	// and the detail ends up in logs and in the 422 message shown to the operator.
	redact := func(b []byte) string {
		return strings.ReplaceAll(string(b), key, "[redacted]")
	}
	detail := lastLine(redact(stderr.Bytes()))
	if detail == "" {
		detail = lastLine(redact(stdout.Bytes()))
	}

	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("codex: verify: timed out after %s", v.timeout())
		}
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			return fmt.Errorf("codex: verify: key rejected: %s exited %d: %s", filepath.Base(v.Bin), ee.ExitCode(), orNone(detail))
		}
		return fmt.Errorf("codex: verify: could not start %s: %v", filepath.Base(v.Bin), redact([]byte(runErr.Error())))
	}
	return nil
}

// lastLine returns the last non-empty line of s, truncated to 200 characters.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return truncate(l, 200)
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func orNone(s string) string {
	if s == "" {
		return "no output"
	}
	return s
}
