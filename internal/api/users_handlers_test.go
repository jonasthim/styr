package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jonasthim/styr/internal/domain"
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

func TestUsersPatch_AcceptsViewerRole(t *testing.T) {
	e := newEnv(t)
	member, _ := e.memberClient("viewer-target@example.com")

	var out map[string]any
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/users/"+member.ID, map[string]string{"role": "viewer"}, &out)
	if status != http.StatusOK {
		t.Fatalf("PATCH /users/%s role=viewer = %d, want 200", member.ID, status)
	}
	if out["role"] != "viewer" {
		t.Errorf("role = %v, want viewer", out["role"])
	}
}

func TestUsersPatch_RejectsUnknownRole(t *testing.T) {
	e := newEnv(t)
	member, _ := e.memberClient("bad-role-target@example.com")

	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/users/"+member.ID, map[string]string{"role": "superuser"}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("PATCH /users/%s role=superuser = %d, want 422", member.ID, status)
	}
}

// TestUsersPatch_AdminCannotDemoteThemselves confirms the server-side
// backstop behind UsersTable.tsx's own disabled-row guard: even a
// hand-crafted request cannot change the caller's own role away from admin.
func TestUsersPatch_AdminCannotDemoteThemselves(t *testing.T) {
	e := newEnv(t)

	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/users/"+e.adminID, map[string]string{"role": "member"}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("admin demoting themselves = %d, want 422", status)
	}

	u, err := e.users.GetByID(context.Background(), e.adminID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if u.Role != domain.RoleAdmin {
		t.Errorf("role after refused self-demote = %q, want admin", u.Role)
	}
}

// TestUsersPatch_FirstUserStaysAdmin confirms the earliest-created user
// (the dev-bypass admin from newEnv) can't be demoted even by a *different*
// admin — there is no guarantee another admin already exists to fix it.
func TestUsersPatch_FirstUserStaysAdmin(t *testing.T) {
	e := newEnv(t)
	// Promote a second user to admin so the request is genuinely "a
	// different admin", not the self-demote case already covered above.
	other, otherClient := e.memberClient("second-admin@example.com")
	if status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/users/"+other.ID, map[string]string{"role": "admin"}, nil); status != http.StatusOK {
		t.Fatalf("promote second admin = %d, want 200", status)
	}

	status := e.doJSON(otherClient, http.MethodPatch, "/api/v1/users/"+e.adminID, map[string]string{"role": "member"}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("demoting the first user = %d, want 422", status)
	}

	u, err := e.users.GetByID(context.Background(), e.adminID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if u.Role != domain.RoleAdmin {
		t.Errorf("first user's role after refused demote = %q, want admin", u.Role)
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
