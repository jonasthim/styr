package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// recordingHandler is a minimal slog.Handler that captures every log line
// (message plus attrs, rendered to a plain string) so a test can assert on
// what was logged without a secret leaking into it.
type recordingHandler struct {
	mu    *sync.Mutex
	lines *[]string
}

func newRecordingHandler() (*recordingHandler, func() []string) {
	var mu sync.Mutex
	var lines []string
	h := &recordingHandler{mu: &mu, lines: &lines}
	snapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(lines))
		copy(out, lines)
		return out
	}
	return h, snapshot
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Level.String())
	b.WriteString(": ")
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value.Any())
		return true
	})
	h.mu.Lock()
	*h.lines = append(*h.lines, b.String())
	h.mu.Unlock()
	return nil
}

func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func TestSendNtfyHeadersAndBearer(t *testing.T) {
	var gotMethod, gotBody, gotTitle, gotPriority, gotTags, gotClick, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		gotTitle = r.Header.Get("Title")
		gotPriority = r.Header.Get("Priority")
		gotTags = r.Header.Get("Tags")
		gotClick = r.Header.Get("Click")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := New(srv.Client(), slog.New(slog.DiscardHandler))
	ch := Channel{ID: "c1", Kind: "ntfy", URL: srv.URL, Token: "secret-token-123"}
	ev := Event{Kind: "run.finished", Title: "Run finished", Body: "run abc123 finished", URL: "https://styr.local/runs/abc123", Priority: 4, Tags: []string{"white_check_mark", "run"}}

	if err := svc.Send(context.Background(), ch, ev); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q", gotMethod)
	}
	if gotBody != ev.Body {
		t.Fatalf("body = %q, want %q", gotBody, ev.Body)
	}
	if gotTitle != ev.Title {
		t.Fatalf("Title header = %q", gotTitle)
	}
	if gotPriority != "4" {
		t.Fatalf("Priority header = %q, want 4", gotPriority)
	}
	if gotTags != "white_check_mark,run" {
		t.Fatalf("Tags header = %q", gotTags)
	}
	if gotClick != ev.URL {
		t.Fatalf("Click header = %q", gotClick)
	}
	if gotAuth != "Bearer secret-token-123" {
		t.Fatalf("Authorization header = %q", gotAuth)
	}
}

func TestSendNtfyNoTokenOmitsAuthorization(t *testing.T) {
	var gotAuth string
	var sawAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, sawAuth = r.Header.Get("Authorization"), r.Header.Get("Authorization") != ""
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := New(srv.Client(), slog.New(slog.DiscardHandler))
	ch := Channel{ID: "c1", Kind: "ntfy", URL: srv.URL}
	if err := svc.Send(context.Background(), ch, Event{Kind: "run.finished", Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sawAuth {
		t.Fatalf("unexpected Authorization header: %q", gotAuth)
	}
}

func TestSendWebhookBodyAndHeader(t *testing.T) {
	var gotEventHeader, gotContentType string
	var gotPayload webhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEventHeader = r.Header.Get("X-Styr-Event")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode webhook body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := New(srv.Client(), slog.New(slog.DiscardHandler))
	ch := Channel{ID: "c1", Kind: "webhook", URL: srv.URL}
	ev := Event{Kind: "run.failed", Title: "Run failed", Body: "boom", URL: "https://styr.local/runs/xyz", Priority: 5, Tags: []string{"alert"}}

	if err := svc.Send(context.Background(), ch, ev); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotEventHeader != "run.failed" {
		t.Fatalf("X-Styr-Event = %q", gotEventHeader)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q", gotContentType)
	}
	if gotPayload.Kind != ev.Kind || gotPayload.Title != ev.Title || gotPayload.Body != ev.Body ||
		gotPayload.URL != ev.URL || gotPayload.Priority != ev.Priority || len(gotPayload.Tags) != 1 || gotPayload.Tags[0] != "alert" {
		t.Fatalf("payload = %+v", gotPayload)
	}
	if gotPayload.SentAt == "" {
		t.Fatal("sent_at is empty")
	}
	if _, err := time.Parse(time.RFC3339, gotPayload.SentAt); err != nil {
		t.Fatalf("sent_at %q not RFC3339: %v", gotPayload.SentAt, err)
	}
}

func TestSendRetriesOnceOn500ThenSucceeds(t *testing.T) {
	oldDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldDelay }()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := New(srv.Client(), slog.New(slog.DiscardHandler))
	ch := Channel{ID: "c1", Kind: "webhook", URL: srv.URL}
	if err := svc.Send(context.Background(), ch, Event{Kind: "run.finished"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls = %d, want 2 (one retry)", got)
	}
}

func TestSendDoesNotRetryOn4xx(t *testing.T) {
	oldDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldDelay }()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	svc := New(srv.Client(), slog.New(slog.DiscardHandler))
	ch := Channel{ID: "c1", Kind: "webhook", URL: srv.URL}
	if err := svc.Send(context.Background(), ch, Event{Kind: "run.finished"}); err == nil {
		t.Fatal("want error for 400 response")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("calls = %d, want 1 (no retry on 4xx)", got)
	}
}

func TestSendFailsAfterOneRetry(t *testing.T) {
	oldDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldDelay }()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	svc := New(srv.Client(), slog.New(slog.DiscardHandler))
	ch := Channel{ID: "c1", Kind: "webhook", URL: srv.URL}
	if err := svc.Send(context.Background(), ch, Event{Kind: "run.finished"}); err == nil {
		t.Fatal("want error after retry also fails")
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls = %d, want 2 (one retry, still failing)", got)
	}
}

func TestPublishFiltersByEvents(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := New(srv.Client(), slog.New(slog.DiscardHandler))
	channels := []Channel{
		{ID: "wants-it", Kind: "webhook", URL: srv.URL, Events: []string{"run.finished", "run.failed"}},
		{ID: "does-not-want-it", Kind: "webhook", URL: srv.URL, Events: []string{"run.needs_human"}},
		{ID: "also-wants-it", Kind: "ntfy", URL: srv.URL, Events: []string{"run.finished"}},
	}
	svc.Publish(context.Background(), channels, Event{Kind: "run.finished", Body: "x"})

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("calls = %d, want 2 (only channels subscribed to run.finished)", got)
	}
}

func TestPublishNeverPanicsOnUnknownChannelKind(t *testing.T) {
	svc := New(http.DefaultClient, slog.New(slog.DiscardHandler))
	channels := []Channel{{ID: "weird", Kind: "carrier-pigeon", URL: "http://example.invalid", Events: []string{"run.finished"}}}
	svc.Publish(context.Background(), channels, Event{Kind: "run.finished"})
}

func TestPublishTokenNeverLogged(t *testing.T) {
	const secret = "ntfy-super-secret-token-9f8e7d"

	oldDelay := retryDelay
	retryDelay = 10 * time.Millisecond
	defer func() { retryDelay = oldDelay }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fail every request so Send logs a warning that includes the
		// returned error, and retries once, to give the token maximum
		// opportunity to leak into a log line if it ever would.
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	handler, snapshot := newRecordingHandler()
	svc := New(srv.Client(), slog.New(handler))
	ch := Channel{ID: "c1", Kind: "ntfy", URL: srv.URL, Token: secret, Events: []string{"run.finished"}}
	svc.Publish(context.Background(), []Channel{ch}, Event{Kind: "run.finished", Title: "t", Body: "b"})

	lines := snapshot()
	if len(lines) == 0 {
		t.Fatal("expected log lines from Publish")
	}
	for _, line := range lines {
		if strings.Contains(line, secret) {
			t.Fatalf("log line leaked token: %q", line)
		}
	}
}
