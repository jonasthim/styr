package api_test

import (
	"net/http"
	"testing"
)

func TestProfilesList_IncludesBuiltins(t *testing.T) {
	e := newEnv(t)
	var out []map[string]any
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/profiles", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /profiles = %d, want 200", status)
	}
	if len(out) < 3 {
		t.Fatalf("profiles = %+v, want the 3 seeded builtins", out)
	}
}

func TestProfilesPatch_RejectsBypassPermissionsMode(t *testing.T) {
	e := newEnv(t)
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/profiles/interactive", map[string]any{
		"mode": "bypassPermissions",
	}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH /profiles/interactive mode=bypassPermissions = %d, want 422", status)
	}
}

func TestProfilesPatch_BuiltinOnlyAllowsTurnsAndTimeout(t *testing.T) {
	e := newEnv(t)

	// max_turns and approval_timeout are allowed on a builtin profile.
	var out map[string]any
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/profiles/interactive", map[string]any{
		"max_turns": 50, "approval_timeout": 900,
	}, &out)
	if status != http.StatusOK {
		t.Fatalf("PATCH /profiles/interactive max_turns+approval_timeout = %d, want 200", status)
	}
	if out["max_turns"].(float64) != 50 {
		t.Errorf("max_turns = %v, want 50", out["max_turns"])
	}

	// name is not.
	status = e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/profiles/interactive", map[string]any{
		"name": "renamed",
	}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH /profiles/interactive name = %d, want 422", status)
	}
}

func TestProfilesCreate_RequiresAdmin(t *testing.T) {
	e := newEnv(t)
	_, memberClient := e.memberClient("member@example.com")
	status := e.doJSON(memberClient, http.MethodPost, "/api/v1/profiles", map[string]any{
		"name": "custom", "mode": "default",
	}, nil)
	if status != http.StatusForbidden {
		t.Fatalf("POST /profiles as member = %d, want 403", status)
	}

	status = e.doJSON(e.adminClient, http.MethodPost, "/api/v1/profiles", map[string]any{
		"name": "custom", "mode": "default",
	}, nil)
	if status != http.StatusCreated {
		t.Fatalf("POST /profiles as admin = %d, want 201", status)
	}
}

// Builtin rows may have their model and effort changed: those are an operator preference,
// not part of what makes a builtin profile safe.
func TestProfilesPatch_BuiltinAcceptsModelAndEffort(t *testing.T) {
	e := newEnv(t)
	var out struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/profiles/investigate",
		map[string]any{"model": "haiku", "effort": "low"}, &out)
	if status != http.StatusOK {
		t.Fatalf("PATCH /profiles/investigate = %d, want 200", status)
	}
	if out.Model != "haiku" || out.Effort != "low" {
		t.Errorf("model/effort = %q/%q, want haiku/low", out.Model, out.Effort)
	}
}

func TestProfilesPatch_UnknownEffortIs422(t *testing.T) {
	e := newEnv(t)
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/profiles/investigate",
		map[string]any{"effort": "turbo"}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH /profiles/investigate with effort=turbo = %d, want 422", status)
	}
}

func TestProfilesList_SeedsModelAndEffortOnUnattendedBuiltins(t *testing.T) {
	e := newEnv(t)
	var out []struct {
		ID     string `json:"id"`
		Model  string `json:"model"`
		Effort string `json:"effort"`
	}
	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/profiles", nil, &out); status != http.StatusOK {
		t.Fatalf("GET /profiles = %d, want 200", status)
	}
	for _, p := range out {
		switch p.ID {
		case "investigate", "remediate":
			if p.Model != "sonnet" || p.Effort != "medium" {
				t.Errorf("%s: model/effort = %q/%q, want sonnet/medium", p.ID, p.Model, p.Effort)
			}
		case "interactive":
			if p.Model != "" || p.Effort != "" {
				t.Errorf("interactive: model/effort = %q/%q, want the CLI defaults", p.Model, p.Effort)
			}
		}
	}
}
