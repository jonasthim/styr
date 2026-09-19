// harness.go implements the harness.Harness interface for the OpenAI Codex CLI: it derives a
// sandbox policy from the session's profile and builds the argv for one turn (see process.go
// for the one-process-per-turn lifecycle and codec.go for the wire format).
package codex

import (
	"context"

	"github.com/jonasthim/styr/internal/harness"
)

// Sandbox policies Styr is willing to hand to `codex exec`. The CLI also accepts
// danger-full-access; Styr never emits it, and neither of the CLI's two bypass flags appears
// anywhere in this package.
const (
	sandboxReadOnly       = "read-only"
	sandboxWorkspaceWrite = "workspace-write"
)

// Harness execs the Codex CLI (or a compatible fake) found at binary.
type Harness struct{ binary string }

// New returns a Harness that executes binary (default "codex") found on PATH. The caller is
// responsible for pointing binary at the real CLI, a fake, or an absolute path; New never
// resolves or validates the path itself.
func New(binary string) harness.Harness {
	if binary == "" {
		binary = "codex"
	}
	return &Harness{binary: binary}
}

// Kind identifies this Harness as driving the Codex CLI.
func (h *Harness) Kind() harness.Kind { return harness.KindCodex }

// SandboxMode returns the `codex exec --sandbox` policy for a profile. Codex has no host-side
// approval channel — the sandbox policy chosen at process start *is* the permission model (see
// docs/DECISIONS.md ADR-018) — so a profile that must not write has to be mapped to
// read-only up front rather than by denying tool uses one at a time:
//
//   - plan and dontAsk are read-only by intent;
//   - so is any profile that disallows both Edit and Write, which is how Styr's `investigate`
//     profile expresses "look, do not touch";
//   - everything else (default, acceptEdits, auto) gets workspace-write, i.e. writes confined
//     to the session's own cwd.
func SandboxMode(p harness.Profile) string {
	switch p.Mode {
	case "plan", "dontAsk":
		return sandboxReadOnly
	}
	if disallows(p.DisallowedTools, "Edit") && disallows(p.DisallowedTools, "Write") {
		return sandboxReadOnly
	}
	return sandboxWorkspaceWrite
}

func disallows(tools []string, name string) bool {
	for _, t := range tools {
		if t == name {
			return true
		}
	}
	return false
}

// BuildArgs returns the argv (without the binary itself) for one turn: a fresh `codex exec`
// when resumeThread is empty, `codex exec resume <resumeThread>` otherwise. prompt is always
// the final positional argument, because `codex exec` takes exactly one prompt per process and
// cannot be given a second turn over stdin (see testdata/PROTOCOL.md "One process per turn").
//
// spec.JSONSchema is expected to hold the *path* of a schema file rather than the schema text:
// unlike Claude Code's --json-schema, `--output-schema` takes a file. Start writes
// harness.StartSpec.JSONSchema's content to a temp file and replaces the field with its path
// before calling BuildArgs.
//
// `codex exec resume` accepts neither -C nor --sandbox, so a resumed turn gets its working
// directory from the child process's cwd (set by process.go from spec.Cwd) and re-states the
// sandbox policy as a `-c sandbox_mode=…` config override. Neither of the CLI's two bypass
// flags (skip approvals and sandboxing; run hooks without persisted trust) is ever emitted, and
// there is no code path here that could produce one.
func BuildArgs(spec harness.StartSpec, prompt string, resumeThread string) []string {
	args := []string{"exec"}
	if resumeThread != "" {
		args = append(args, "resume", resumeThread)
	}
	args = append(args, "--json")
	if resumeThread == "" {
		args = append(args, "-C", spec.Cwd, "--sandbox", SandboxMode(spec.Profile))
	} else {
		// The value is parsed as TOML and falls back to the raw string when that fails, which
		// is what happens for an unquoted mode name.
		args = append(args, "-c", "sandbox_mode="+SandboxMode(spec.Profile))
	}
	// Styr isolates a session in its own workspace directory, which is usually but not always
	// a git checkout; the CLI's own repo check would refuse the ones that are not.
	args = append(args, "--skip-git-repo-check")
	if spec.Model != "" {
		args = append(args, "-m", spec.Model)
	}
	if spec.JSONSchema != "" {
		args = append(args, "--output-schema", spec.JSONSchema)
	}
	return append(args, prompt)
}

// Start validates spec and returns a Process for it. No OS process is spawned yet: a Codex
// session is one process per turn, so the first `codex exec` runs when the caller sends the
// first user turn.
func (h *Harness) Start(ctx context.Context, spec harness.StartSpec) (harness.Process, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	return newProcess(h.binary, spec)
}
