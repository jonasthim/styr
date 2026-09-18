// Package pipelines parses and validates the YAML pipeline definitions
// described in docs/superpowers/plans/2026-09-19-styr-v0.5-pipelines.md: a
// small DAG of steps, each backed by a template, chained by `needs` and
// optionally fanned out by `foreach`. It has no dependency on the data
// layer or the executor — Parse and Validate work on plain bytes and a
// caller-supplied TemplateLookup.
package pipelines

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultTimeout is applied when a definition's yaml omits "timeout".
const DefaultTimeout = 2 * time.Hour

// MaxTimeout is the longest a whole pipeline run may take.
const MaxTimeout = 24 * time.Hour

// MaxSteps is the largest number of steps a definition may declare.
const MaxSteps = 20

// MaxRetries is the largest per-step retry budget.
const MaxRetries = 3

// Definition is a parsed, but not necessarily valid, pipeline: call
// Validate to run the full rule set from the plan.
type Definition struct {
	Name      string
	Workspace string
	Timeout   time.Duration
	Steps     []Step

	// line-tracking populated by Parse from the yaml.v3 node tree, so
	// Validate can attach an accurate line number to a problem even though
	// Step itself carries none. Nil/zero for a Definition built by hand
	// (rather than through Parse); Validate then reports Line: 0.
	stepLines   []int // parallel to Steps
	timeoutLine int
	stepsLine   int
}

// Step is one node of the pipeline DAG.
type Step struct {
	ID       string
	Template string
	Needs    []string
	With     map[string]string
	Worktree string // "" (own), "own" or "shared"
	Retries  int
	Foreach  string
}

// Problem is one validation or parse failure, with the yaml source line
// when one could be determined (0 otherwise).
type Problem struct {
	Line    int
	Message string
}

// TemplateLookup resolves a template name (as written in a step's
// `template` field) to its id. Implementations are expected to already be
// scoped to the pipeline's workspace.
type TemplateLookup interface {
	TemplateExists(name string) (id string, ok bool)
}

// --- yaml decoding ---

// rawStep mirrors Step for yaml decoding, plus the line its mapping node
// starts on.
type rawStep struct {
	ID       string            `yaml:"id"`
	Template string            `yaml:"template"`
	Needs    []string          `yaml:"needs"`
	With     map[string]string `yaml:"with"`
	Worktree string            `yaml:"worktree"`
	Retries  int               `yaml:"retries"`
	Foreach  string            `yaml:"foreach"`

	line int
}

// UnmarshalYAML decodes the step normally but also captures the mapping
// node's line, which plain struct decoding discards.
func (s *rawStep) UnmarshalYAML(value *yaml.Node) error {
	type plain rawStep
	var p plain
	if err := value.Decode(&p); err != nil {
		return err
	}
	*s = rawStep(p)
	s.line = value.Line
	return nil
}

// rawDefinition mirrors Definition for yaml decoding, plus the lines of
// the "timeout" and "steps" keys.
type rawDefinition struct {
	Name      string    `yaml:"name"`
	Workspace string    `yaml:"workspace"`
	Timeout   string    `yaml:"timeout"`
	Steps     []rawStep `yaml:"steps"`

	timeoutLine int
	stepsLine   int
}

func (r *rawDefinition) UnmarshalYAML(value *yaml.Node) error {
	type plain rawDefinition
	var p plain
	if err := value.Decode(&p); err != nil {
		return err
	}
	*r = rawDefinition(p)
	for i := 0; i+1 < len(value.Content); i += 2 {
		key := value.Content[i]
		switch key.Value {
		case "timeout":
			r.timeoutLine = value.Content[i+1].Line
		case "steps":
			r.stepsLine = key.Line
		}
	}
	return nil
}

var yamlErrorLineRe = regexp.MustCompile(`line (\d+)`)

// yamlErrorLine best-effort extracts a "line N" reference from a yaml.v3
// error, falling back to 0 (unknown) when the message carries none.
func yamlErrorLine(err error) int {
	m := yamlErrorLineRe.FindStringSubmatch(err.Error())
	if m == nil {
		return 0
	}
	n, convErr := strconv.Atoi(m[1])
	if convErr != nil {
		return 0
	}
	return n
}

// Parse decodes yamlText into a Definition. The Problems it can return are
// limited to what is detectable without a TemplateLookup: malformed yaml
// and an unparsable timeout duration (which falls back to DefaultTimeout
// so the rest of the definition still parses). Call Validate for the full
// rule set from the plan (unique ids, acyclic needs, template existence,
// with/foreach syntax, limits).
func Parse(yamlText []byte) (Definition, []Problem) {
	var raw rawDefinition
	if err := yaml.Unmarshal(yamlText, &raw); err != nil {
		return Definition{}, []Problem{{Line: yamlErrorLine(err), Message: "invalid yaml: " + err.Error()}}
	}

	var problems []Problem
	def := Definition{
		Name:        raw.Name,
		Workspace:   raw.Workspace,
		timeoutLine: raw.timeoutLine,
		stepsLine:   raw.stepsLine,
	}

	switch {
	case raw.Timeout == "":
		def.Timeout = DefaultTimeout
	default:
		d, err := time.ParseDuration(raw.Timeout)
		if err != nil {
			problems = append(problems, Problem{Line: raw.timeoutLine, Message: fmt.Sprintf("timeout: invalid duration %q", raw.Timeout)})
			def.Timeout = DefaultTimeout
		} else {
			def.Timeout = d
		}
	}

	def.Steps = make([]Step, len(raw.Steps))
	def.stepLines = make([]int, len(raw.Steps))
	for i, rs := range raw.Steps {
		def.Steps[i] = Step{
			ID: rs.ID, Template: rs.Template, Needs: rs.Needs, With: rs.With,
			Worktree: rs.Worktree, Retries: rs.Retries, Foreach: rs.Foreach,
		}
		def.stepLines[i] = rs.line
	}
	return def, problems
}

// lineForStep returns the yaml line of Steps[i], or 0 when unknown (a
// Definition not built through Parse).
func (d Definition) lineForStep(i int) int {
	if i >= 0 && i < len(d.stepLines) {
		return d.stepLines[i]
	}
	return 0
}

// Validate runs the full rule set from the plan's "Validation" section:
// unique ids, needs referring to earlier ids, acyclic needs, templates
// existing (via lookup), with/foreach parsing as Go templates, a valid
// worktree mode, "shared" requiring at least one need, a retries budget of
// 0..3, at most MaxSteps steps and a timeout of at most MaxTimeout. A nil
// lookup skips the template-existence check (useful for structural-only
// validation before a workspace is known).
func (d Definition) Validate(lookup TemplateLookup) []Problem {
	var problems []Problem

	if n := len(d.Steps); n > MaxSteps {
		problems = append(problems, Problem{Line: d.stepsLine,
			Message: fmt.Sprintf("pipeline has %d steps, maximum is %d", n, MaxSteps)})
	}

	timeout := d.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	if timeout > MaxTimeout {
		problems = append(problems, Problem{Line: d.timeoutLine,
			Message: fmt.Sprintf("timeout %s exceeds the maximum of %s", timeout, MaxTimeout)})
	}

	_, cyclic, idIndex := d.topo()
	cyclicSet := make(map[string]bool, len(cyclic))
	for _, id := range cyclic {
		cyclicSet[id] = true
	}

	for i, s := range d.Steps {
		if s.ID != "" && idIndex[s.ID] != i {
			problems = append(problems, Problem{Line: d.lineForStep(i),
				Message: fmt.Sprintf("duplicate step id %q", s.ID)})
		}

		switch s.Worktree {
		case "", "own", "shared":
		default:
			problems = append(problems, Problem{Line: d.lineForStep(i),
				Message: fmt.Sprintf("step %q: worktree %q must be \"\", \"own\" or \"shared\"", s.ID, s.Worktree)})
		}
		if s.Worktree == "shared" && len(s.Needs) == 0 {
			problems = append(problems, Problem{Line: d.lineForStep(i),
				Message: fmt.Sprintf("step %q: worktree \"shared\" requires at least one need", s.ID)})
		}
		if s.Retries < 0 || s.Retries > MaxRetries {
			problems = append(problems, Problem{Line: d.lineForStep(i),
				Message: fmt.Sprintf("step %q: retries %d must be between 0 and %d", s.ID, s.Retries, MaxRetries)})
		}
		if lookup != nil {
			if _, ok := lookup.TemplateExists(s.Template); !ok {
				problems = append(problems, Problem{Line: d.lineForStep(i),
					Message: fmt.Sprintf("step %q: template %q not found", s.ID, s.Template)})
			}
		}

		keys := make([]string, 0, len(s.With))
		for k := range s.With {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := parseCheck(s.With[k]); err != nil {
				problems = append(problems, Problem{Line: d.lineForStep(i),
					Message: fmt.Sprintf("step %q: with[%q]: %v", s.ID, k, err)})
			}
		}
		if s.Foreach != "" {
			if err := parseCheck(s.Foreach); err != nil {
				problems = append(problems, Problem{Line: d.lineForStep(i),
					Message: fmt.Sprintf("step %q: foreach: %v", s.ID, err)})
			}
		}

		// Only the first occurrence of an id carries the "real" needs
		// graph edges (topo built it that way); a duplicate's needs are
		// left unchecked since the duplicate itself is already reported.
		if s.ID == "" || idIndex[s.ID] != i {
			continue
		}
		for _, need := range s.Needs {
			if _, ok := idIndex[need]; !ok {
				problems = append(problems, Problem{Line: d.lineForStep(i),
					Message: fmt.Sprintf("step %q needs unknown step %q", s.ID, need)})
				continue
			}
			if cyclicSet[s.ID] || cyclicSet[need] {
				continue // covered by the cycle problem below
			}
			if idIndex[need] >= i {
				problems = append(problems, Problem{Line: d.lineForStep(i),
					Message: fmt.Sprintf("step %q needs %q, which is not defined before it", s.ID, need)})
			}
		}
	}

	for _, id := range cyclic {
		problems = append(problems, Problem{Line: d.lineForStep(idIndex[id]),
			Message: fmt.Sprintf("step %q is part of a needs cycle", id)})
	}

	return problems
}

// topo computes the pipeline's dependency graph over the first occurrence
// of each step id: levels are the topological groups Order() exposes
// (steps in a level depend only on steps in earlier levels), and cyclic
// lists every id that never reached indegree 0 (a needs cycle, including a
// self-need). idIndex maps every step id to the index of its first
// occurrence, for both this method's own use and Validate's line lookups.
func (d Definition) topo() (levels [][]string, cyclic []string, idIndex map[string]int) {
	idIndex = map[string]int{}
	for i, s := range d.Steps {
		if s.ID == "" {
			continue
		}
		if _, ok := idIndex[s.ID]; !ok {
			idIndex[s.ID] = i
		}
	}

	indegree := make(map[string]int, len(idIndex))
	dependents := map[string][]string{} // need id -> ids that need it
	for id := range idIndex {
		indegree[id] = 0
	}
	for i, s := range d.Steps {
		if s.ID == "" || idIndex[s.ID] != i {
			continue
		}
		for _, need := range s.Needs {
			if _, ok := idIndex[need]; !ok {
				continue // unknown id, reported separately by Validate
			}
			indegree[s.ID]++
			dependents[need] = append(dependents[need], s.ID)
		}
	}

	var queue []string
	for id := range idIndex {
		if indegree[id] == 0 {
			queue = append(queue, id)
		}
	}
	processed := make(map[string]bool, len(idIndex))
	for len(queue) > 0 {
		sort.Slice(queue, func(a, b int) bool { return idIndex[queue[a]] < idIndex[queue[b]] })
		levels = append(levels, append([]string(nil), queue...))
		var next []string
		for _, id := range queue {
			processed[id] = true
			for _, dep := range dependents[id] {
				indegree[dep]--
				if indegree[dep] == 0 {
					next = append(next, dep)
				}
			}
		}
		queue = next
	}

	for id := range idIndex {
		if !processed[id] {
			cyclic = append(cyclic, id)
		}
	}
	sort.Slice(cyclic, func(a, b int) bool { return idIndex[cyclic[a]] < idIndex[cyclic[b]] })
	return levels, cyclic, idIndex
}

// Order returns the pipeline's steps grouped into topological levels: every
// step in a level depends only on steps in earlier levels, so a level's
// steps can run in parallel. A step that is part of a needs cycle (an
// invalid definition; see Validate) is omitted from every level.
func (d Definition) Order() [][]string {
	levels, _, _ := d.topo()
	return levels
}

// Node is one step in a Graph.
type Node struct {
	ID       string
	Template string
	Worktree string
	Foreach  bool
}

// Edge is a needs dependency: From must succeed before To starts.
type Edge struct {
	From string
	To   string
}

// Graph is a rendering-friendly view of the pipeline's DAG.
type Graph struct {
	Nodes []Node
	Edges []Edge
}

// Graph returns every step as a Node (in definition order) and every needs
// relationship as an Edge, for the frontend's graph preview.
func (d Definition) Graph() Graph {
	g := Graph{Nodes: make([]Node, len(d.Steps))}
	for i, s := range d.Steps {
		g.Nodes[i] = Node{ID: s.ID, Template: s.Template, Worktree: s.Worktree, Foreach: s.Foreach != ""}
		for _, need := range s.Needs {
			g.Edges = append(g.Edges, Edge{From: need, To: s.ID})
		}
	}
	return g
}

// validationFuncs mirrors the function names (not the behaviour) of
// internal/templates' render funcMap, so a with/foreach template using
// them parses successfully here without this package importing
// internal/templates (which would require executing against a Vars map we
// don't have yet at validation time — see parseCheck).
var validationFuncs = template.FuncMap{
	"lower":      func(s string) string { return s },
	"upper":      func(s string) string { return s },
	"join":       func(string, any) (string, error) { return "", nil },
	"default":    func(fallback, value any) any { return value },
	"truncate":   func(int, string) string { return "" },
	"json":       func(any) (string, error) { return "", nil },
	"now":        func() string { return "" },
	"sha256":     func(string) string { return "" },
	"alertnames": func(any) []string { return nil },
}

// parseCheck reports whether text is syntactically a valid Go template
// (using the same option and function names as internal/templates.Render).
// It only parses, never executes: a with/foreach value legitimately
// references data (steps.<id>.report, .item, .payload) that does not exist
// yet at validation time, so executing it against an empty Vars would
// reject perfectly valid definitions (e.g. the plan's own fix-ci example).
func parseCheck(text string) error {
	_, err := template.New("pipelines-validate").Option("missingkey=zero").Funcs(validationFuncs).Parse(text)
	return err
}
