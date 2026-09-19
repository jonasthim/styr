package harness_test

import (
	"context"
	"testing"

	"github.com/jonasthim/styr/internal/harness"
)

// stubHarness is a Harness that only reports a kind; Start is never called by these tests.
type stubHarness struct{ kind harness.Kind }

func (s stubHarness) Kind() harness.Kind { return s.kind }

func (s stubHarness) Start(context.Context, harness.StartSpec) (harness.Process, error) {
	return nil, nil
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := harness.New()
	if _, ok := r.Get(harness.KindClaude); ok {
		t.Fatal("empty registry returned a harness")
	}

	claude := stubHarness{kind: harness.KindClaude}
	codex := stubHarness{kind: harness.KindCodex}
	r.Register(claude)
	r.Register(codex)

	got, ok := r.Get(harness.KindCodex)
	if !ok {
		t.Fatal("codex harness not found after Register")
	}
	if got.Kind() != harness.KindCodex {
		t.Fatalf("Get(codex).Kind() = %q", got.Kind())
	}
	if _, ok := r.Get("gemini"); ok {
		t.Fatal("unregistered kind reported as present")
	}
}

func TestRegistry_RegisterReplacesSameKind(t *testing.T) {
	r := harness.New()
	r.Register(stubHarness{kind: harness.KindClaude})
	r.Register(stubHarness{kind: harness.KindClaude})
	if kinds := r.Kinds(); len(kinds) != 1 {
		t.Fatalf("Kinds() = %v, want one entry", kinds)
	}
}

func TestRegistry_RegisterNilIsIgnored(t *testing.T) {
	r := harness.New()
	r.Register(nil)
	if kinds := r.Kinds(); len(kinds) != 0 {
		t.Fatalf("Kinds() = %v, want empty", kinds)
	}
}

func TestRegistry_KindsIsSorted(t *testing.T) {
	r := harness.New()
	r.Register(stubHarness{kind: harness.KindCodex})
	r.Register(stubHarness{kind: harness.KindClaude})
	kinds := r.Kinds()
	want := []harness.Kind{harness.KindClaude, harness.KindCodex}
	if len(kinds) != len(want) {
		t.Fatalf("Kinds() = %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("Kinds() = %v, want %v", kinds, want)
		}
	}
}

func TestSingleRegistry(t *testing.T) {
	h := stubHarness{kind: harness.KindClaude}
	r := harness.SingleRegistry(h)
	if got, ok := r.Get(harness.KindClaude); !ok || got.Kind() != harness.KindClaude {
		t.Fatalf("SingleRegistry did not register the harness")
	}
	if _, ok := r.Get(harness.KindCodex); ok {
		t.Fatal("SingleRegistry registered more than the one harness")
	}
}

func TestValidKind(t *testing.T) {
	for _, k := range []harness.Kind{harness.KindClaude, harness.KindCodex} {
		if !harness.ValidKind(k) {
			t.Fatalf("ValidKind(%q) = false", k)
		}
	}
	for _, k := range []harness.Kind{"", "fake", "gemini"} {
		if harness.ValidKind(k) {
			t.Fatalf("ValidKind(%q) = true", k)
		}
	}
}
