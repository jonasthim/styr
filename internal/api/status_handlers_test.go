package api_test

import (
	"net/http"
	"testing"
)

func TestStatus_ReturnsDepsStatus(t *testing.T) {
	e := newEnv(t)
	var out struct {
		Version string `json:"version"`
		Slots   int    `json:"slots"`
	}
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/status", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /status = %d, want 200", status)
	}
	if out.Version != "test" {
		t.Errorf("version = %q, want test", out.Version)
	}
	if out.Slots != 4 {
		t.Errorf("slots = %d, want 4", out.Slots)
	}
}

func TestHealthz(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Get(e.ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", resp.StatusCode)
	}
}

func TestStatus_ModelsEffortsAndHiddenCommands(t *testing.T) {
	e := newEnv(t)
	var out struct {
		Models []struct {
			Alias string `json:"alias"`
			Label string `json:"label"`
		} `json:"models"`
		Efforts        []string `json:"efforts"`
		HiddenCommands []string `json:"hidden_commands"`
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/status", nil, &out); status != http.StatusOK {
		t.Fatalf("GET /status = %d, want 200", status)
	}

	wantModels := map[string]string{"fable": "Fable 5.1", "opus": "Opus 5", "sonnet": "Sonnet 5", "haiku": "Haiku 4.5"}
	if len(out.Models) != len(wantModels) {
		t.Fatalf("models = %+v, want %d entries", out.Models, len(wantModels))
	}
	for _, m := range out.Models {
		if wantModels[m.Alias] != m.Label {
			t.Errorf("model %q label = %q, want %q", m.Alias, m.Label, wantModels[m.Alias])
		}
	}

	wantEfforts := []string{"low", "medium", "high", "xhigh", "max"}
	if len(out.Efforts) != len(wantEfforts) {
		t.Fatalf("efforts = %v, want %v", out.Efforts, wantEfforts)
	}
	for i, want := range wantEfforts {
		if out.Efforts[i] != want {
			t.Errorf("efforts[%d] = %q, want %q", i, out.Efforts[i], want)
		}
	}

	if len(out.HiddenCommands) == 0 {
		t.Fatal("hidden_commands is empty, want the built-ins the slash menu must hide")
	}
	found := false
	for _, c := range out.HiddenCommands {
		if c == "clear" {
			found = true
		}
	}
	if !found {
		t.Errorf("hidden_commands = %v, want it to include clear", out.HiddenCommands)
	}
}

// GET /status lists every harness this build knows, with whether its binary answered
// `--version` at startup, so the UI can offer (or explain) the choice.
func TestStatus_Harnesses(t *testing.T) {
	e := newEnv(t)
	var out struct {
		Harnesses []struct {
			Kind      string `json:"kind"`
			Available bool   `json:"available"`
			Version   string `json:"version"`
			Bin       string `json:"bin"`
		} `json:"harnesses"`
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/status", nil, &out); status != http.StatusOK {
		t.Fatalf("GET /status = %d, want 200", status)
	}
	if len(out.Harnesses) != 2 {
		t.Fatalf("harnesses = %+v, want claude and codex", out.Harnesses)
	}
	byKind := map[string]bool{}
	for _, h := range out.Harnesses {
		byKind[h.Kind] = h.Available
	}
	if avail, ok := byKind["claude"]; !ok || !avail {
		t.Errorf("harnesses = %+v, want claude available", out.Harnesses)
	}
	if avail, ok := byKind["codex"]; !ok || avail {
		t.Errorf("harnesses = %+v, want codex listed as unavailable", out.Harnesses)
	}
}
