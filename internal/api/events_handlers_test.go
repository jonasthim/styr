package api_test

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"
)

// sseClient opens a GET /api/v1/events stream and returns a reader
// positioned right after the response headers, plus a cleanup func.
func (e *testEnv) sseClient(t *testing.T, client *http.Client) (*bufio.Reader, func()) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.ts.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /events = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}
	if xb := resp.Header.Get("X-Accel-Buffering"); xb != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", xb)
	}
	return bufio.NewReader(resp.Body), func() { _ = resp.Body.Close() }
}

// readSSEEventFrame reads one "event: ...\ndata: ...\n\n" frame from r in a
// goroutine and reports it on the returned channel, so callers can select
// against a timeout without blocking forever on a stream that never sends.
func readSSEEventFrame(r *bufio.Reader) <-chan [2]string {
	out := make(chan [2]string, 1)
	go func() {
		var eventLine, dataLine string
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(line, "event: "):
				eventLine = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				dataLine = strings.TrimPrefix(line, "data: ")
			case line == "" && eventLine != "":
				out <- [2]string{eventLine, dataLine}
				return
			}
		}
	}()
	return out
}

// readSSEEvent reads one frame from r, failing the test if none arrives
// within timeout.
func readSSEEvent(t *testing.T, r *bufio.Reader, timeout time.Duration) (event, data string) {
	t.Helper()
	select {
	case frame := <-readSSEEventFrame(r):
		return frame[0], frame[1]
	case <-time.After(timeout):
		t.Fatal("timed out waiting for an SSE frame")
		return "", ""
	}
}

// assertNoSSEEvent fails the test if a frame arrives on r within timeout.
func assertNoSSEEvent(t *testing.T, r *bufio.Reader, timeout time.Duration) {
	t.Helper()
	select {
	case frame := <-readSSEEventFrame(r):
		t.Fatalf("unexpected SSE frame: event=%q data=%q", frame[0], frame[1])
	case <-time.After(timeout):
	}
}

func TestEvents_DeliversToOwnerNotToOtherMember(t *testing.T) {
	e := newEnv(t)
	owner, ownerClient := e.memberClient("sse-owner@example.com")
	_, otherClient := e.memberClient("sse-other@example.com")
	e.seedToken(owner.ID)
	ws := e.seedWorkspace("interactive")

	ownerStream, closeOwner := e.sseClient(t, ownerClient)
	defer closeOwner()
	otherStream, closeOther := e.sseClient(t, otherClient)
	defer closeOther()

	var created struct {
		ID string `json:"id"`
	}
	status := e.doJSON(ownerClient, http.MethodPost, "/api/v1/sessions", map[string]any{
		"workspace_id": ws.ID, "profile_id": "interactive", "title": "sse test", "prompt": "hi",
	}, &created)
	if status != http.StatusCreated {
		t.Fatalf("POST /sessions = %d, want 201", status)
	}

	kind, data := readSSEEvent(t, ownerStream, 5*time.Second)
	if kind == "" {
		t.Fatalf("owner received no event")
	}
	if !strings.Contains(data, created.ID) {
		t.Errorf("event data = %q, want it to mention session %s", data, created.ID)
	}

	// The other member's stream must not deliver this owner-scoped event.
	assertNoSSEEvent(t, otherStream, 300*time.Millisecond)
}
