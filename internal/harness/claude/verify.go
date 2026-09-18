// verify.go checks that a Claude token actually works by running one minimal
// prompt through the CLI in JSON output mode. It implements api.TokenVerifier
// structurally (this package never imports internal/api).
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// defaultVerifyTimeout bounds a verification run when Verifier.Timeout is
// zero.
const defaultVerifyTimeout = 60 * time.Second

// Verifier verifies a Claude token by spawning Bin with the token in its
// environment and inspecting the CLI's JSON result. The zero value has Bin
// "" (unusable) and the default 60s timeout.
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

// verifyResult is the subset of the CLI's --output-format json result this
// package inspects.
type verifyResult struct {
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// Verify runs `<Bin> -p "Reply with pong." --max-turns 1 --output-format
// json` with the token in CLAUDE_CODE_OAUTH_TOKEN and a fresh, empty HOME
// (removed again before Verify returns), and succeeds only when the process
// exits 0 and the JSON result has "is_error":false. The returned error carries
// the CLI's last output line with the token redacted, so operators can tell a
// rejected token from a broken invocation.
func (v Verifier) Verify(ctx context.Context, token string) error {
	ctx, cancel := context.WithTimeout(ctx, v.timeout())
	defer cancel()

	home, err := os.MkdirTemp("", "styr-verify-*")
	if err != nil {
		return fmt.Errorf("claude: verify: create temp home: %w", err)
	}
	defer os.RemoveAll(home)

	cmd := exec.CommandContext(ctx, v.Bin,
		"-p", "Reply with pong.", "--max-turns", "1", "--output-format", "json")
	cmd.Env = []string{
		"CLAUDE_CODE_OAUTH_TOKEN=" + token,
		"HOME=" + home,
	}
	if path, ok := os.LookupEnv("PATH"); ok {
		cmd.Env = append(cmd.Env, "PATH="+path)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	// Anything quoted from the CLI is redacted first: a misbehaving CLI could
	// in principle echo the token, and the detail ends up in logs and in the
	// 422 message shown to the operator.
	redact := func(b []byte) string {
		return strings.ReplaceAll(string(b), token, "[redacted]")
	}
	detail := lastLine(redact(stderr.Bytes()))
	if detail == "" {
		detail = lastLine(redact(stdout.Bytes()))
	}

	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("claude: verify: timed out after %s", v.timeout())
		}
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			return fmt.Errorf("claude: verify: %s exited %d: %s", filepath.Base(v.Bin), ee.ExitCode(), orNone(detail))
		}
		return fmt.Errorf("claude: verify: could not start %s: %v", filepath.Base(v.Bin), redact([]byte(runErr.Error())))
	}

	var result verifyResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		return fmt.Errorf("claude: verify: could not parse CLI output: %s", orNone(detail))
	}
	if result.IsError {
		msg := strings.TrimSpace(redact([]byte(result.Result)))
		if msg == "" {
			msg = detail
		}
		return fmt.Errorf("claude: verify: token rejected: %s", orNone(truncate(msg, 200)))
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
