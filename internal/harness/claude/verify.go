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
	IsError bool `json:"is_error"`
}

// Verify runs `<Bin> -p "Reply with pong." --max-turns 1 --output-format
// json` with the token in CLAUDE_CODE_OAUTH_TOKEN and a fresh, empty HOME
// (removed again before Verify returns), and succeeds only when the process
// exits 0 and the JSON result has "is_error":false. token is never included
// in the returned error or logged.
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

	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	// Stderr is intentionally discarded: it is not attributed to the CLI's
	// JSON contract and must never end up in a returned error, since a
	// misbehaving CLI could in principle echo the token there.
	runErr := cmd.Run()
	if runErr != nil {
		return fmt.Errorf("claude: verify: process exited with an error")
	}

	var result verifyResult
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result); err != nil {
		return errors.New("claude: verify: could not parse CLI output")
	}
	if result.IsError {
		return errors.New("claude: verify: token was rejected")
	}
	return nil
}
