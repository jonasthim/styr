package pipelines

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeLookup is a TemplateLookup backed by a fixed set of known template
// names, standing in for a workspace-scoped db.Templates.GetByName.
type fakeLookup map[string]string

func (f fakeLookup) TemplateExists(name string) (string, bool) {
	id, ok := f[name]
	return id, ok
}

// fixCiLookup knows every template name the plan's fix-ci example refers
// to.
var fixCiLookup = fakeLookup{
	"CI failure triage":    "tpl-triage",
	"Apply fix":            "tpl-fix",
	"Run tests and report": "tpl-verify",
	"Review file":          "tpl-review",
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func TestParse_GoodFixCI(t *testing.T) {
	def, problems := Parse(readFixture(t, "good_fix_ci.yaml"))
	if len(problems) != 0 {
		t.Fatalf("Parse problems = %+v, want none", problems)
	}
	if def.Name != "fix-ci" {
		t.Fatalf("Name = %q, want fix-ci", def.Name)
	}
	if def.Workspace != "styr" {
		t.Fatalf("Workspace = %q, want styr", def.Workspace)
	}
	if def.Timeout != 2*time.Hour {
		t.Fatalf("Timeout = %v, want 2h", def.Timeout)
	}
	if len(def.Steps) != 4 {
		t.Fatalf("len(Steps) = %d, want 4", len(def.Steps))
	}
	wantIDs := []string{"triage", "fix", "verify", "review-each"}
	for i, id := range wantIDs {
		if def.Steps[i].ID != id {
			t.Fatalf("Steps[%d].ID = %q, want %q", i, def.Steps[i].ID, id)
		}
	}
	if def.Steps[1].Worktree != "own" || def.Steps[1].Retries != 1 {
		t.Fatalf("fix step = %+v, want worktree own, retries 1", def.Steps[1])
	}
	if def.Steps[2].Worktree != "shared" {
		t.Fatalf("verify step worktree = %q, want shared", def.Steps[2].Worktree)
	}
	if def.Steps[3].Foreach == "" {
		t.Fatalf("review-each step has no foreach")
	}
}

func TestParse_DefaultTimeout(t *testing.T) {
	def, problems := Parse([]byte("name: x\nworkspace: styr\nsteps:\n  - id: a\n    template: t\n"))
	if len(problems) != 0 {
		t.Fatalf("problems = %+v, want none", problems)
	}
	if def.Timeout != DefaultTimeout {
		t.Fatalf("Timeout = %v, want default %v", def.Timeout, DefaultTimeout)
	}
}

func TestParse_InvalidTimeoutDuration(t *testing.T) {
	def, problems := Parse([]byte("name: x\nworkspace: styr\ntimeout: not-a-duration\nsteps:\n  - id: a\n    template: t\n"))
	if len(problems) != 1 {
		t.Fatalf("problems = %+v, want exactly one", problems)
	}
	if problems[0].Line != 3 {
		t.Fatalf("problem line = %d, want 3", problems[0].Line)
	}
	// Parse still returns a usable Definition, falling back to the default.
	if def.Timeout != DefaultTimeout {
		t.Fatalf("Timeout = %v, want default fallback %v", def.Timeout, DefaultTimeout)
	}
}

func TestParse_InvalidYAML(t *testing.T) {
	def, problems := Parse([]byte("name: [unterminated\n"))
	if len(problems) != 1 {
		t.Fatalf("problems = %+v, want exactly one", problems)
	}
	if def.Name != "" || len(def.Steps) != 0 {
		t.Fatalf("def = %+v, want zero value on parse failure", def)
	}
}

func TestValidate_Fixtures(t *testing.T) {
	cases := []struct {
		file string
		want []Problem
	}{
		{
			file: "good_fix_ci.yaml",
			want: nil,
		},
		{
			file: "bad_duplicate_id.yaml",
			want: []Problem{
				{Line: 6, Message: `duplicate step id "triage"`},
			},
		},
		{
			file: "bad_forward_need.yaml",
			want: []Problem{
				{Line: 4, Message: `step "verify" needs "fix", which is not defined before it`},
			},
		},
		{
			file: "bad_cycle.yaml",
			want: []Problem{
				{Line: 4, Message: `step "a" is part of a needs cycle`},
				{Line: 7, Message: `step "b" is part of a needs cycle`},
			},
		},
		{
			file: "bad_unknown_template.yaml",
			want: []Problem{
				{Line: 4, Message: `step "triage": template "Does Not Exist" not found`},
			},
		},
		{
			file: "bad_with_template.yaml",
			want: []Problem{
				{Line: 4, Message: `step "triage": with["alert"]: template: pipelines-validate:1: unclosed action`},
			},
		},
		{
			file: "bad_shared_without_needs.yaml",
			want: []Problem{
				{Line: 4, Message: `step "triage": worktree "shared" requires at least one need`},
			},
		},
		{
			file: "bad_too_many_steps.yaml",
			want: []Problem{
				{Line: 3, Message: `pipeline has 21 steps, maximum is 20`},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			def, parseProblems := Parse(readFixture(t, tc.file))
			if len(parseProblems) != 0 {
				t.Fatalf("Parse problems = %+v, want none", parseProblems)
			}
			got := def.Validate(fixCiLookup)
			if len(got) != len(tc.want) {
				t.Fatalf("Validate(%s) = %+v, want %+v", tc.file, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("Validate(%s)[%d] = %+v, want %+v", tc.file, i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestValidate_RetriesOutOfRange(t *testing.T) {
	def := Definition{
		Name: "x", Workspace: "styr", Timeout: time.Hour,
		Steps: []Step{{ID: "a", Template: "CI failure triage", Retries: 4}},
	}
	problems := def.Validate(fixCiLookup)
	if len(problems) != 1 || problems[0].Message != `step "a": retries 4 must be between 0 and 3` {
		t.Fatalf("problems = %+v", problems)
	}
	if problems[0].Line != 0 {
		t.Fatalf("Line = %d, want 0 for a hand-built Definition", problems[0].Line)
	}
}

func TestValidate_NegativeRetries(t *testing.T) {
	def := Definition{
		Name: "x", Workspace: "styr", Timeout: time.Hour,
		Steps: []Step{{ID: "a", Template: "CI failure triage", Retries: -1}},
	}
	if problems := def.Validate(fixCiLookup); len(problems) != 1 {
		t.Fatalf("problems = %+v, want one", problems)
	}
}

func TestValidate_BadWorktreeMode(t *testing.T) {
	def := Definition{
		Name: "x", Workspace: "styr", Timeout: time.Hour,
		Steps: []Step{{ID: "a", Template: "CI failure triage", Worktree: "bogus"}},
	}
	problems := def.Validate(fixCiLookup)
	if len(problems) != 1 || problems[0].Message != `step "a": worktree "bogus" must be "", "own" or "shared"` {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestValidate_TimeoutTooLong(t *testing.T) {
	def := Definition{
		Name: "x", Workspace: "styr", Timeout: 25 * time.Hour,
		Steps: []Step{{ID: "a", Template: "CI failure triage"}},
	}
	problems := def.Validate(fixCiLookup)
	if len(problems) != 1 || problems[0].Message != "timeout 25h0m0s exceeds the maximum of 24h0m0s" {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestValidate_ZeroTimeoutDefaultsWithoutProblem(t *testing.T) {
	def := Definition{
		Name: "x", Workspace: "styr",
		Steps: []Step{{ID: "a", Template: "CI failure triage"}},
	}
	if problems := def.Validate(fixCiLookup); len(problems) != 0 {
		t.Fatalf("problems = %+v, want none (zero Timeout defaults to 2h)", problems)
	}
}

func TestValidate_UnknownNeed(t *testing.T) {
	def := Definition{
		Name: "x", Workspace: "styr", Timeout: time.Hour,
		Steps: []Step{{ID: "a", Template: "CI failure triage", Needs: []string{"nope"}}},
	}
	problems := def.Validate(fixCiLookup)
	if len(problems) != 1 || problems[0].Message != `step "a" needs unknown step "nope"` {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestValidate_NilLookupSkipsTemplateCheck(t *testing.T) {
	def := Definition{
		Name: "x", Workspace: "styr", Timeout: time.Hour,
		Steps: []Step{{ID: "a", Template: "whatever, not looked up"}},
	}
	if problems := def.Validate(nil); len(problems) != 0 {
		t.Fatalf("problems = %+v, want none", problems)
	}
}

func TestOrder_FixCI(t *testing.T) {
	def, problems := Parse(readFixture(t, "good_fix_ci.yaml"))
	if len(problems) != 0 {
		t.Fatalf("Parse problems = %+v", problems)
	}
	levels := def.Order()
	want := [][]string{
		{"triage"},
		{"fix", "review-each"},
		{"verify"},
	}
	if len(levels) != len(want) {
		t.Fatalf("Order() = %+v, want %+v", levels, want)
	}
	for i := range want {
		if len(levels[i]) != len(want[i]) {
			t.Fatalf("Order()[%d] = %+v, want %+v", i, levels[i], want[i])
		}
		for j := range want[i] {
			if levels[i][j] != want[i][j] {
				t.Fatalf("Order()[%d] = %+v, want %+v", i, levels[i], want[i])
			}
		}
	}
}

func TestOrder_CycleOmitsCyclicSteps(t *testing.T) {
	def, problems := Parse(readFixture(t, "bad_cycle.yaml"))
	if len(problems) != 0 {
		t.Fatalf("Parse problems = %+v", problems)
	}
	if levels := def.Order(); len(levels) != 0 {
		t.Fatalf("Order() = %+v, want no levels (every step is cyclic)", levels)
	}
}

func TestGraph_FixCI(t *testing.T) {
	def, problems := Parse(readFixture(t, "good_fix_ci.yaml"))
	if len(problems) != 0 {
		t.Fatalf("Parse problems = %+v", problems)
	}
	g := def.Graph()
	if len(g.Nodes) != 4 {
		t.Fatalf("len(Nodes) = %d, want 4", len(g.Nodes))
	}
	wantNodes := []Node{
		{ID: "triage", Template: "CI failure triage"},
		{ID: "fix", Template: "Apply fix", Worktree: "own"},
		{ID: "verify", Template: "Run tests and report", Worktree: "shared"},
		{ID: "review-each", Template: "Review file", Foreach: true},
	}
	for i, want := range wantNodes {
		if g.Nodes[i] != want {
			t.Fatalf("Nodes[%d] = %+v, want %+v", i, g.Nodes[i], want)
		}
	}
	wantEdges := []Edge{
		{From: "triage", To: "fix"},
		{From: "fix", To: "verify"},
		{From: "triage", To: "review-each"},
	}
	if len(g.Edges) != len(wantEdges) {
		t.Fatalf("Edges = %+v, want %+v", g.Edges, wantEdges)
	}
	for i, want := range wantEdges {
		if g.Edges[i] != want {
			t.Fatalf("Edges[%d] = %+v, want %+v", i, g.Edges[i], want)
		}
	}
}
