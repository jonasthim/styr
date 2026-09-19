// registry.go holds the set of Harness implementations a server was built with, keyed by
// Kind. Styr drives more than one agentic CLI (Claude Code and the OpenAI Codex CLI as of
// v1.0), and which one a session runs under is a per-session choice stored on the session
// row, so the sessions service needs to look a Harness up by name at start and at resume
// rather than holding a single one for the lifetime of the process.
package harness

import "sort"

// Registry maps a Kind to the Harness that drives it. The zero value is not usable; build one
// with New. A Registry is read-only once wiring is finished (the composition root registers
// every harness before the first request), so Get and Kinds take no lock.
type Registry struct {
	byKind map[Kind]Harness
}

// New returns an empty Registry.
func New() *Registry { return &Registry{byKind: make(map[Kind]Harness)} }

// Register adds h under its own Kind, replacing any harness previously registered for that
// kind. A nil h is ignored, so a composition root that could not build one harness still
// serves the others.
func (r *Registry) Register(h Harness) {
	if h == nil {
		return
	}
	r.byKind[h.Kind()] = h
}

// Get returns the Harness registered for kind. The second result is false when no harness was
// registered for it — a session row naming a harness this build does not have, or a request
// asking for one, which callers turn into a user-visible error rather than a panic.
func (r *Registry) Get(kind Kind) (Harness, bool) {
	h, ok := r.byKind[kind]
	return h, ok
}

// Kinds returns every registered kind, sorted, so status responses and error messages list
// them in a stable order.
func (r *Registry) Kinds() []Kind {
	out := make([]Kind, 0, len(r.byKind))
	for k := range r.byKind {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// SingleRegistry is the one-harness Registry: the shape almost every test wants, and the
// smallest change for a caller that used to hold a single Harness.
func SingleRegistry(h Harness) *Registry {
	r := New()
	r.Register(h)
	return r
}

// ValidKind reports whether kind is one of the kinds Styr knows how to store on a session row.
// It says nothing about whether this build has a Harness for it — that is Registry.Get.
func ValidKind(kind Kind) bool {
	switch kind {
	case KindClaude, KindCodex:
		return true
	}
	return false
}
