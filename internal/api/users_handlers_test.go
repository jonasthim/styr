package api_test

import (
	"net/http"
	"testing"
)

func TestUsersList_RequiresAdmin(t *testing.T) {
	e := newEnv(t)
	_, memberClient := e.memberClient("member@example.com")

	status := e.doJSON(memberClient, http.MethodGet, "/api/v1/users", nil, nil)
	if status != http.StatusForbidden {
		t.Fatalf("GET /users as member = %d, want 403", status)
	}

	var out []map[string]any
	status = e.doJSON(e.adminClient, http.MethodGet, "/api/v1/users", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /users as admin = %d, want 200", status)
	}
	if len(out) < 1 {
		t.Fatalf("users = %+v, want at least the admin", out)
	}
}

func TestUsersPatch_ChangesRole(t *testing.T) {
	e := newEnv(t)
	member, _ := e.memberClient("member2@example.com")

	var out map[string]any
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/users/"+member.ID, map[string]string{"role": "admin"}, &out)
	if status != http.StatusOK {
		t.Fatalf("PATCH /users/%s = %d, want 200", member.ID, status)
	}
	if out["role"] != "admin" {
		t.Errorf("role = %v, want admin", out["role"])
	}
}

func TestSettingsGet_RequiresAdminAndReportsLimits(t *testing.T) {
	e := newEnv(t)
	_, memberClient := e.memberClient("member3@example.com")
	if status := e.doJSON(memberClient, http.MethodGet, "/api/v1/settings", nil, nil); status != http.StatusForbidden {
		t.Fatalf("GET /settings as member = %d, want 403", status)
	}

	var out struct {
		MaxOpenSessions int `json:"max_open_sessions"`
		ServiceToken    struct {
			Present bool `json:"present"`
		} `json:"service_token"`
	}
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/settings", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /settings as admin = %d, want 200", status)
	}
	if out.MaxOpenSessions != 4 {
		t.Errorf("max_open_sessions = %d, want 4", out.MaxOpenSessions)
	}
	if out.ServiceToken.Present {
		t.Errorf("service_token.present = true before any is set")
	}
}

func TestSettingsServiceTokenPut_VerifiesAndStores(t *testing.T) {
	e := newEnv(t)
	status := e.doJSON(e.adminClient, http.MethodPut, "/api/v1/settings/service-token", map[string]string{"token": "sk-svc-token"}, nil)
	if status != http.StatusNoContent {
		t.Fatalf("PUT /settings/service-token = %d, want 204", status)
	}

	var out struct {
		ServiceToken struct {
			Present bool `json:"present"`
		} `json:"service_token"`
	}
	e.doJSON(e.adminClient, http.MethodGet, "/api/v1/settings", nil, &out)
	if !out.ServiceToken.Present {
		t.Fatalf("service_token.present = false after PUT")
	}
}
