// harness.go implements the harness.Harness interface for the Claude Code CLI: it builds argv
// from a harness.StartSpec and execs the binary (see process.go for the process lifecycle).
package claude

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jonasthim/styr/internal/harness"
)

// Harness execs the Claude Code CLI (or a compatible fake) found at binary.
type Harness struct{ binary string }

// New returns a Harness that executes binary (default "claude") found on PATH. The caller is
// responsible for pointing binary at the real CLI, a fake, or an absolute path; New never
// resolves or validates the path itself.
func New(binary string) harness.Harness {
	if binary == "" {
		binary = "claude"
	}
	return &Harness{binary: binary}
}

// Kind identifies this Harness as driving the Claude Code CLI.
func (h *Harness) Kind() harness.Kind { return harness.KindClaude }

// BuildArgs returns the argv (without the binary itself) for spec. --verbose is required
// alongside --output-format stream-json in -p mode (observed against CLI 2.1.276; see
// testdata/PROTOCOL.md "Other observations"). The flag that skips all permission checks
// (Global Constraints) is never emitted: harness.StartSpec.Validate rejects that permission
// mode before BuildArgs is reached in normal use, and BuildArgs itself has no code path that
// could add it or the CLI's equivalent skip-permissions flag.
func BuildArgs(spec harness.StartSpec) []string {
	args := []string{"-p",
		"--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
		"--include-partial-messages", "--replay-user-messages",
		"--permission-prompt-tool", "stdio",
		"--permission-mode", spec.Profile.Mode,
	}
	if spec.Resume {
		args = append(args, "--resume", spec.SessionID)
	} else {
		args = append(args, "--session-id", spec.SessionID)
	}
	if spec.Title != "" {
		args = append(args, "--name", spec.Title)
	}
	if len(spec.Profile.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(spec.Profile.AllowedTools, ","))
	}
	if len(spec.Profile.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(spec.Profile.DisallowedTools, ","))
	}
	if spec.Profile.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(spec.Profile.MaxTurns))
	}
	if spec.Worktree != "" {
		args = append(args, "--worktree", spec.Worktree)
	}
	if spec.JSONSchema != "" {
		args = append(args, "--json-schema", spec.JSONSchema)
	}
	if spec.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", spec.SystemPrompt)
	}
	return args
}

// Start validates spec and starts a new Claude Code CLI process for it.
func (h *Harness) Start(ctx context.Context, spec harness.StartSpec) (harness.Process, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	p, err := startProcess(ctx, h.binary, BuildArgs(spec), spec)
	if err != nil {
		return nil, fmt.Errorf("claude: start: %w", err)
	}
	return p, nil
}
