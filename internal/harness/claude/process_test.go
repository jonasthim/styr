// process_test.go tests BuildArgs and the real process lifecycle against the shell fake CLI
// (testdata/fake-claude/fake-claude.sh). No test here ever execs the real `claude` binary.
package claude

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/harness"
)

func spec() harness.StartSpec {
	return harness.StartSpec{SessionID: "0b4f9a2e-3f3e-4c0a-9d3a-3c5a4c1e2f10", Title: "t", Cwd: "/tmp", Home: "/tmp",
		Profile: harness.Profile{Mode: "default", AllowedTools: []string{"Read", "Bash(git *)"}, DisallowedTools: []string{"WebFetch"}, MaxTurns: 12}}
}

func TestBuildArgsNewSession(t *testing.T) {
	got := strings.Join(BuildArgs(spec()), " ")
	for _, want := range []string{
		"-p", "--input-format stream-json", "--output-format stream-json", "--verbose", "--include-partial-messages",
		"--replay-user-messages", "--session-id 0b4f9a2e-3f3e-4c0a-9d3a-3c5a4c1e2f10", "--name t",
		"--permission-prompt-tool stdio", "--permission-mode default", "--allowedTools Read,Bash(git *)",
		"--disallowedTools WebFetch", "--max-turns 12",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q lack %q", got, want)
		}
	}
	if strings.Contains(got, "--resume") || strings.Contains(got, "dangerously") || strings.Contains(got, "bypass") {
		t.Fatalf("forbidden or wrong flag in %q", got)
	}
}

func TestBuildArgsResume(t *testing.T) {
	s := spec()
	s.Resume = true
	got := strings.Join(BuildArgs(s), " ")
	if !strings.Contains(got, "--resume 0b4f9a2e-3f3e-4c0a-9d3a-3c5a4c1e2f10") || strings.Contains(got, "--session-id") {
		t.Fatalf("resume args wrong: %q", got)
	}
}

func TestBuildArgsWorktree(t *testing.T) {
	s := spec()
	s.Worktree = "styr-abc"
	if !strings.Contains(strings.Join(BuildArgs(s), " "), "--worktree styr-abc") {
		t.Fatal("worktree flag missing")
	}
}

func TestBuildArgsJSONSchemaAndSystemPromptPresentWhenSet(t *testing.T) {
	s := spec()
	s.JSONSchema = `{"type":"object"}`
	s.SystemPrompt = "You are terse."
	got := strings.Join(BuildArgs(s), " ")
	for _, want := range []string{`--json-schema {"type":"object"}`, "--append-system-prompt You are terse."} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q lack %q", got, want)
		}
	}
}

func TestBuildArgsJSONSchemaAndSystemPromptAbsentWhenEmpty(t *testing.T) {
	got := strings.Join(BuildArgs(spec()), " ")
	if strings.Contains(got, "--json-schema") || strings.Contains(got, "--append-system-prompt") {
		t.Fatalf("args %q should not contain --json-schema or --append-system-prompt when unset", got)
	}
}

func fakeBinary(t *testing.T) string {
	t.Helper()
	bin, err := filepath.Abs("../../../testdata/fake-claude/fake-claude.sh")
	if err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestProcessReplaysFixtureAndAnswersPermission(t *testing.T) {
	bin := fakeBinary(t)
	fix, _ := filepath.Abs("testdata/03_permission_bash.jsonl")
	s := spec()
	s.Cwd, s.Home = t.TempDir(), t.TempDir()
	// process.go only forwards PATH/TERM/LANG plus spec.Env to the child (it does not
	// inherit the rest of the test process's environment), so the fixture selection for the
	// shell fake must travel through spec.Env, not t.Setenv.
	s.Env = map[string]string{"FAKE_CLAUDE_FIXTURE": fix, "FAKE_CLAUDE_DELAY": "0"}
	h := New(bin)
	p, err := h.Start(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Send(context.Background(), harness.UserMessage{Text: "go"}); err != nil {
		t.Fatal(err)
	}
	var sawPerm, sawResult bool
	deadline := time.After(10 * time.Second)
	for !sawResult {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				t.Fatal("events closed before result")
			}
			switch ev.Type {
			case harness.EventPermission:
				sawPerm = true
				_ = p.Decide(context.Background(), harness.Decision{RequestID: ev.Permission.RequestID, Allow: true})
			case harness.EventResult:
				sawResult = true
			}
		case <-deadline:
			t.Fatal("timeout")
		}
	}
	if !sawPerm {
		t.Fatal("no permission request seen")
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	for ev := range p.Events() {
		if ev.Type == harness.EventExit && ev.ExitCode != 0 {
			t.Fatalf("exit code %d (%s)", ev.ExitCode, ev.Err)
		}
	}
}

func TestCloseOnIdleProcessYieldsExitAndClosesChannel(t *testing.T) {
	bin := fakeBinary(t)
	s := spec()
	s.Cwd, s.Home = t.TempDir(), t.TempDir()
	s.Env = map[string]string{"FAKE_CLAUDE_DELAY": "0"}
	h := New(bin)
	p, err := h.Start(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}
	// Calling Close a second time must not panic or send a second exit event.
	if err := p.Close(ctx); err != nil {
		t.Fatal(err)
	}

	var sawExit bool
	var exitCount int
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-p.Events():
			if !ok {
				if !sawExit {
					t.Fatal("channel closed without an exit event")
				}
				if exitCount != 1 {
					t.Fatalf("saw %d exit events, want 1", exitCount)
				}
				return
			}
			if ev.Type == harness.EventExit {
				sawExit = true
				exitCount++
				if ev.ExitCode != 0 {
					t.Fatalf("exit code = %d, want 0 (%s)", ev.ExitCode, ev.Err)
				}
			}
		case <-deadline:
			t.Fatal("timeout waiting for exit and channel close")
		}
	}
}

func TestBuildArgsModelFallbackAndEffort(t *testing.T) {
	s := spec()
	s.Model, s.FallbackModel, s.Effort = "sonnet", "haiku", "high"
	got := strings.Join(BuildArgs(s), " ")
	for _, want := range []string{"--model sonnet", "--fallback-model haiku", "--effort high"} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q lack %q", got, want)
		}
	}
}

func TestBuildArgsModelFallbackAndEffortAbsentWhenEmpty(t *testing.T) {
	got := strings.Join(BuildArgs(spec()), " ")
	for _, unwanted := range []string{"--model", "--fallback-model", "--effort"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("args %q should not contain %q when unset", got, unwanted)
		}
	}
}

func TestStartRejectsUnknownEffort(t *testing.T) {
	s := spec()
	s.Cwd, s.Home = t.TempDir(), t.TempDir()
	s.Effort = "turbo"
	if _, err := New(fakeBinary(t)).Start(context.Background(), s); err == nil {
		t.Fatal("Start accepted an unknown effort level")
	}
}

func TestBuiltinListsAreDisjointAndNonEmpty(t *testing.T) {
	headless, hidden := HeadlessBuiltins(), HiddenBuiltins()
	if len(headless) == 0 || len(hidden) == 0 {
		t.Fatalf("headless=%v hidden=%v, want both non-empty", headless, hidden)
	}
	in := map[string]bool{}
	for _, c := range headless {
		in[c] = true
	}
	for _, c := range hidden {
		if in[c] {
			t.Errorf("%q is both headless and hidden", c)
		}
	}
	// The spike's two headline findings, so a future edit cannot quietly invert them.
	if !in["compact"] {
		t.Error("compact must be a headless builtin")
	}
	hiddenSet := map[string]bool{}
	for _, c := range hidden {
		hiddenSet[c] = true
	}
	if !hiddenSet["clear"] {
		t.Error("clear must be hidden: it re-inits under a new session id")
	}
}

// HeadlessBuiltins and HiddenBuiltins must hand out copies: a caller mutating the returned
// slice must not corrupt the package's own lists.
func TestBuiltinListsAreCopies(t *testing.T) {
	first := HiddenBuiltins()
	first[0] = "mutated"
	if HiddenBuiltins()[0] == "mutated" {
		t.Fatal("HiddenBuiltins returned its backing array")
	}
}
