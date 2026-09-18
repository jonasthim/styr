package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
)

// waitForApprovals polls GET /api/v1/approvals until it returns at least
// one entry or the 5s deadline passes.
func waitForApprovals(t *testing.T, e *testEnv, client *http.Client) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var out []map[string]any
		status := e.doJSON(client, http.MethodGet, "/api/v1/approvals?state=pending", nil, &out)
		if status != http.StatusOK {
			t.Fatalf("GET /approvals = %d, want 200", status)
		}
		if len(out) > 0 {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatal("no approval appeared within 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestApprovals_EndToEndDecide(t *testing.T) {
	e := newEnv(t, fake.Step{
		Events: []harness.Event{{
			Type: harness.EventPermission,
			Permission: &harness.PermissionRequest{
				RequestID: "req-1", ToolName: "Bash", Input: json.RawMessage(`{"command":"ls"}`),
			},
		}},
		WaitForDecision: true,
	})
	ws := e.seedWorkspace("interactive")
	owner, ownerClient := e.memberClient("approver@example.com")
	e.seedToken(owner.ID)

	var created struct {
		ID string `json:"id"`
	}
	status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "needs approval", "prompt": "do it",
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /sessions = %d, want 201", status)
	}

	pending := waitForApprovals(t, e, ownerClient)
	ap := pending[0]
	if ap["session_title"] != "needs approval" {
		t.Errorf("session_title = %v, want %q", ap["session_title"], "needs approval")
	}
	if _, ok := ap["now_line"]; !ok {
		t.Errorf("approval is missing now_line")
	}
	approvalID, _ := ap["id"].(string)
	if approvalID == "" {
		t.Fatalf("approval has no id: %+v", ap)
	}

	// Another member does not see it (visibility applies to approvals too).
	_, otherClient := e.memberClient("bystander@example.com")
	var otherPending []map[string]any
	if status := e.doJSON(otherClient, http.MethodGet, "/api/v1/approvals?state=pending", nil, &otherPending); status != http.StatusOK {
		t.Fatalf("GET /approvals as bystander = %d, want 200 (empty list, not an error)", status)
	}
	if len(otherPending) != 0 {
		t.Errorf("bystander sees %d pending approvals, want 0", len(otherPending))
	}

	status = e.doJSON(ownerClient, http.MethodPost, "/api/v1/approvals/"+approvalID, map[string]any{"decision": "allow"}, nil)
	if status != http.StatusNoContent {
		t.Fatalf("POST /approvals/%s = %d, want 204", approvalID, status)
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		if len(e.harness.Procs) > 0 && len(e.harness.Procs[0].Decisions) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fake process never recorded the decision")
		}
		time.Sleep(10 * time.Millisecond)
	}
	dec := e.harness.Procs[0].Decisions[0]
	if dec.RequestID != "req-1" || !dec.Allow {
		t.Errorf("decision = %+v, want RequestID=req-1 Allow=true", dec)
	}
}

func TestApprovalsSnooze(t *testing.T) {
	e := newEnv(t, fake.Step{
		Events: []harness.Event{{
			Type:       harness.EventPermission,
			Permission: &harness.PermissionRequest{RequestID: "req-2", ToolName: "Bash", Input: json.RawMessage(`{}`)},
		}},
		WaitForDecision: true,
	})
	ws := e.seedWorkspace("interactive")
	owner, ownerClient := e.memberClient("snoozer@example.com")
	e.seedToken(owner.ID)

	var created struct {
		ID string `json:"id"`
	}
	e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "t", "prompt": "do it",
	}, &created)

	pending := waitForApprovals(t, e, ownerClient)
	approvalID, _ := pending[0]["id"].(string)

	status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/approvals/"+approvalID+"/snooze",
		map[string]any{"until": time.Now().Add(time.Hour).Format(time.RFC3339)}, nil)
	if status != http.StatusNoContent {
		t.Fatalf("POST /approvals/%s/snooze = %d, want 204", approvalID, status)
	}
}
