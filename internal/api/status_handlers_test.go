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
