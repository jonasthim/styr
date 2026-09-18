package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jonasthim/styr/internal/domain"
)

// postHookRaw posts body to /hooks/{slug} with no cookie jar, no CSRF
// header and no auth of any kind, matching how a real webhook sender (e.g.
// Grafana) calls this endpoint. It returns the status and body.
func postHookRaw(t *testing.T, e *testEnv, slug string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, e.ts.URL+"/hooks/"+slug, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		raw = append(raw, buf[:n]...)
		if rerr != nil {
			break
		}
	}
	return resp.StatusCode, raw
}

func TestHooksDeliver_WorksWithoutCookieOrCSRF(t *testing.T) {
	e := newEnv(t)
	runID := "run-99"
	e.triggers.DeliverFn = func(_ context.Context, in domain.Inbound) (domain.Delivery, error) {
		if in.Slug != "prod-alerts" {
			t.Fatalf("slug = %q, want prod-alerts", in.Slug)
		}
		return domain.Delivery{ID: "del-1", TriggerID: "trig-1", Status: domain.DeliveryAccepted, RunID: &runID}, nil
	}

	status, raw := postHookRaw(t, e, "prod-alerts", []byte(`{"status":"firing"}`))
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body = %s", status, raw)
	}
	var out struct {
		DeliveryID string  `json:"delivery_id"`
		Status     string  `json:"status"`
		RunID      *string `json:"run_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, raw)
	}
	if out.DeliveryID != "del-1" || out.Status != "accepted" || out.RunID == nil || *out.RunID != "run-99" {
		t.Fatalf("out = %+v", out)
	}
}

func TestHooksDeliver_UnknownTrigger404(t *testing.T) {
	e := newEnv(t)
	e.triggers.DeliverFn = func(context.Context, domain.Inbound) (domain.Delivery, error) {
		return domain.Delivery{}, domain.ErrUnknownTrigger
	}
	status, _ := postHookRaw(t, e, "no-such-slug", []byte(`{}`))
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestHooksDeliver_BadSecret401(t *testing.T) {
	e := newEnv(t)
	e.triggers.DeliverFn = func(context.Context, domain.Inbound) (domain.Delivery, error) {
		return domain.Delivery{}, domain.ErrBadSecret
	}
	status, _ := postHookRaw(t, e, "prod-alerts", []byte(`{}`))
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
}

func TestHooksDeliver_OtherErrorIs500NoDetail(t *testing.T) {
	e := newEnv(t)
	e.triggers.DeliverFn = func(context.Context, domain.Inbound) (domain.Delivery, error) {
		return domain.Delivery{}, errPlain("boom: db connection to 10.0.0.5 refused")
	}
	status, raw := postHookRaw(t, e, "prod-alerts", []byte(`{}`))
	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	if strings.Contains(string(raw), "10.0.0.5") || strings.Contains(string(raw), "boom") {
		t.Fatalf("500 response leaked internal error detail: %s", raw)
	}
}

type errPlain string

func (e errPlain) Error() string { return string(e) }

func TestHooksDeliver_RejectsOversizedBodyWith413(t *testing.T) {
	e := newEnv(t)
	e.triggers.DeliverFn = func(context.Context, domain.Inbound) (domain.Delivery, error) {
		t.Fatal("Deliver must not be called for an oversized body")
		return domain.Delivery{}, nil
	}
	big := bytes.Repeat([]byte("a"), 300*1024) // 300 KiB > the 256 KiB limit
	status, _ := postHookRaw(t, e, "prod-alerts", big)
	if status != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", status)
	}
}

func TestHooksDeliver_AcceptsBodyAtTheLimit(t *testing.T) {
	e := newEnv(t)
	e.triggers.DeliverFn = func(_ context.Context, in domain.Inbound) (domain.Delivery, error) {
		return domain.Delivery{ID: "del-2", Status: domain.DeliveryAccepted}, nil
	}
	// Comfortably under the 256 KiB limit (leaving room for JSON framing),
	// to distinguish "the handler forwards a normal body" from "the 413
	// test above is the only path exercised".
	body := append([]byte(`{"raw":"`), bytes.Repeat([]byte("a"), 200*1024)...)
	body = append(body, []byte(`"}`)...)
	status, _ := postHookRaw(t, e, "prod-alerts", body)
	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", status)
	}
}
