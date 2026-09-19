package codex

import (
	"slices"
	"strings"
	"testing"

	"github.com/jonasthim/styr/internal/harness"
)

func TestKind(t *testing.T) {
	if got := New("").Kind(); got != harness.KindCodex {
		t.Errorf("Kind() = %q, want codex", got)
	}
}

func TestSandboxModeFollowsTheProfile(t *testing.T) {
	tests := []struct {
		name    string
		profile harness.Profile
		want    string
	}{
		{"default writes in the workspace", harness.Profile{Mode: "default"}, sandboxWorkspaceWrite},
		{"acceptEdits writes in the workspace", harness.Profile{Mode: "acceptEdits"}, sandboxWorkspaceWrite},
		{"auto writes in the workspace", harness.Profile{Mode: "auto"}, sandboxWorkspaceWrite},
		{"plan is read-only", harness.Profile{Mode: "plan"}, sandboxReadOnly},
		{"dontAsk is read-only", harness.Profile{Mode: "dontAsk"}, sandboxReadOnly},
		{
			"the investigate profile is read-only",
			harness.Profile{Mode: "default", DisallowedTools: []string{"Edit", "Write", "NotebookEdit"}},
			sandboxReadOnly,
		},
		{
			"disallowing only Edit is not enough to be read-only",
			harness.Profile{Mode: "default", DisallowedTools: []string{"Edit"}},
			sandboxWorkspaceWrite,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SandboxMode(tt.profile); got != tt.want {
				t.Errorf("SandboxMode(%+v) = %q, want %q", tt.profile, got, tt.want)
			}
		})
	}
}

func specFor(profile harness.Profile) harness.StartSpec {
	return harness.StartSpec{
		SessionID: "0a4b2f26-3f5b-4c3a-9f6a-0f1d2c3b4a59",
		Cwd:       "/ws/session",
		Home:      "/home/styr/u1",
		Profile:   profile,
	}
}

func TestBuildArgs(t *testing.T) {
	schemaSpec := specFor(harness.Profile{Mode: "default"})
	schemaSpec.JSONSchema = "/tmp/styr-codex-schema-1.json" // Start rewrites the schema to its file
	modelSpec := specFor(harness.Profile{Mode: "plan"})
	modelSpec.Model = "gpt-5.4-codex"

	tests := []struct {
		name    string
		spec    harness.StartSpec
		prompt  string
		resume  string
		want    []string
		absent  []string
		wantEnd string
	}{
		{
			name:    "a first turn is a fresh exec with the workspace and its sandbox",
			spec:    specFor(harness.Profile{Mode: "default"}),
			prompt:  "hello",
			want:    []string{"exec", "--json", "-C", "/ws/session", "--sandbox", "workspace-write", "--skip-git-repo-check", "hello"},
			absent:  []string{"resume", "--output-schema", "-m"},
			wantEnd: "hello",
		},
		{
			name:   "an investigate turn is read-only",
			spec:   specFor(harness.Profile{Mode: "default", DisallowedTools: []string{"Edit", "Write"}}),
			prompt: "look around",
			want:   []string{"--sandbox", "read-only"},
		},
		{
			name:   "a later turn resumes the thread and restates the sandbox as a config override",
			spec:   specFor(harness.Profile{Mode: "plan"}),
			prompt: "and then?",
			resume: "01a0b707-ce58-7660-b301-4939ce14c766",
			want: []string{
				"exec", "resume", "01a0b707-ce58-7660-b301-4939ce14c766", "--json",
				"-c", "sandbox_mode=read-only", "--skip-git-repo-check", "and then?",
			},
			// `codex exec resume` accepts neither of these (see testdata/PROTOCOL.md).
			absent:  []string{"-C", "--sandbox"},
			wantEnd: "and then?",
		},
		{
			name:   "a schema is passed as the path of the file Start wrote",
			spec:   schemaSpec,
			prompt: "report",
			want:   []string{"--output-schema", "/tmp/styr-codex-schema-1.json"},
		},
		{
			name:   "the model is passed as -m",
			spec:   modelSpec,
			prompt: "hi",
			want:   []string{"-m", "gpt-5.4-codex"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := BuildArgs(tt.spec, tt.prompt, tt.resume)
			if !containsSeq(args, tt.want) {
				t.Errorf("BuildArgs = %v\nwant it to contain, in order: %v", args, tt.want)
			}
			for _, a := range tt.absent {
				if slices.Contains(args, a) {
					t.Errorf("BuildArgs = %v, must not contain %q", args, a)
				}
			}
			if tt.wantEnd != "" && args[len(args)-1] != tt.wantEnd {
				t.Errorf("BuildArgs last arg = %q, want the prompt %q", args[len(args)-1], tt.wantEnd)
			}
		})
	}
}

// TestBuildArgsNeverBypassesTheSandbox guards the Global Constraints rule for the Codex CLI:
// neither of the CLI's two bypass flags (skip approvals and sandboxing; run hooks without
// persisted trust) may ever be emitted, whatever the profile asks for. The check is on the
// shape of the argument rather than on the exact flag names, which are deliberately not written
// out in this repository.
func TestBuildArgsNeverBypassesTheSandbox(t *testing.T) {
	for _, mode := range []string{"default", "acceptEdits", "plan", "dontAsk", "auto"} {
		for _, resume := range []string{"", "01a0b707-ce58-7660-b301-4939ce14c766"} {
			spec := specFor(harness.Profile{Mode: mode, AllowedTools: []string{"Bash"}})
			spec.JSONSchema = "/tmp/s.json"
			spec.Model = "gpt-5.4-codex"
			for _, a := range BuildArgs(spec, "[fixture:01] go", resume) {
				if strings.Contains(a, "dangerous") || strings.Contains(a, "full-access") {
					t.Fatalf("mode %q resume %q produced forbidden argument %q", mode, resume, a)
				}
			}
		}
	}
}

func TestStartRejectsAnInvalidSpec(t *testing.T) {
	if _, err := New("codex").Start(t.Context(), harness.StartSpec{}); err == nil {
		t.Fatal("Start with an empty spec returned no error")
	}
}

// containsSeq reports whether want appears in args as a contiguous subsequence.
func containsSeq(args, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for i := 0; i+len(want) <= len(args); i++ {
		if slices.Equal(args[i:i+len(want)], want) {
			return true
		}
	}
	return false
}
