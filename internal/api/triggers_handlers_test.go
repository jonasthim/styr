package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/domain"
)

type triggerOut struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Slug            string  `json:"slug"`
	Kind            string  `json:"kind"`
	SecretHint      string  `json:"secret_hint"`
	SecretHash      *string `json:"secret_hash"` // must never be sent by the server
	TemplateID      string  `json:"template_id"`
	PipelineID      *string `json:"pipeline_id"`
	Enabled         bool    `json:"enabled"`
	CooldownS       int     `json:"cooldown_s"`
	StormCapPerHour int     `json:"storm_cap_per_hour"`
}

type triggerCreateOut struct {
	Trigger triggerOut `json:"trigger"`
	Secret  string     `json:"secret"`
}

type triggerSecretOut struct {
	Secret string `json:"secret"`
}

func sampleTrigger() domain.Trigger {
	now := time.Now()
	return domain.Trigger{
		ID: "trig-1", Name: "prod alerts", Slug: "prod-alerts", Kind: domain.TriggerGrafana,
		SecretHash: "should-never-be-sent", SecretHint: "abcd1234", TemplateID: "tmpl-1", Enabled: true,
		CooldownS: 600, StormCapPerHour: 10, CreatedAt: now, UpdatedAt: now,
	}
}

func TestTriggersCreate_ReturnsSecretOnlyOnce(t *testing.T) {
	e := newEnv(t)
	e.triggers.CreateTriggerFn = func(_ context.Context, _ api.Actor, in domain.TriggerInput) (domain.Trigger, string, error) {
		trig := sampleTrigger()
		trig.Name = in.Name
		return trig, "styr_whs_plaintext_secret", nil
	}

	var out triggerCreateOut
	_, raw := e.doJSONHeaders(e.adminClient, http.MethodPost, "/api/v1/triggers", map[string]any{
		"name": "prod alerts", "kind": "grafana", "template_id": "tmpl-1",
	}, &out, true)
	if out.Secret != "styr_whs_plaintext_secret" {
		t.Fatalf("secret = %q, want the plaintext secret", out.Secret)
	}
	if out.Trigger.ID != "trig-1" {
		t.Fatalf("trigger.id = %q", out.Trigger.ID)
	}
	if string(raw) == "" {
		t.Fatal("empty response body")
	}
	if containsSubstring(string(raw), "should-never-be-sent") {
		t.Fatalf("response leaked the secret hash: %s", raw)
	}
	if containsSubstring(string(raw), `"secret_hash"`) {
		t.Fatalf("response includes a secret_hash field: %s", raw)
	}
}

func TestTriggersCreate_BothTemplateAndPipelineIs422(t *testing.T) {
	e := newEnv(t)
	e.triggers.CreateTriggerFn = func(context.Context, api.Actor, domain.TriggerInput) (domain.Trigger, string, error) {
		t.Fatal("CreateTrigger must not be called when both template_id and pipeline_id are set")
		return domain.Trigger{}, "", nil
	}
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/triggers", map[string]any{
		"name": "prod alerts", "kind": "grafana", "template_id": "tmpl-1", "pipeline_id": "pipe-1",
	}, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
	if body.Error.Code != "invalid" {
		t.Fatalf("error code = %q, want invalid", body.Error.Code)
	}
}

func TestTriggersCreate_NeitherTemplateNorPipelineIs422(t *testing.T) {
	e := newEnv(t)
	e.triggers.CreateTriggerFn = func(context.Context, api.Actor, domain.TriggerInput) (domain.Trigger, string, error) {
		t.Fatal("CreateTrigger must not be called when neither template_id nor pipeline_id is set")
		return domain.Trigger{}, "", nil
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/triggers", map[string]any{
		"name": "prod alerts", "kind": "grafana",
	}, nil)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", status)
	}
}

func TestTriggersCreate_PipelineIDOnly_OK(t *testing.T) {
	e := newEnv(t)
	e.triggers.CreateTriggerFn = func(_ context.Context, _ api.Actor, in domain.TriggerInput) (domain.Trigger, string, error) {
		if in.TemplateID != "" {
			t.Fatalf("template_id = %q, want empty", in.TemplateID)
		}
		if in.PipelineID == nil || *in.PipelineID != "pipe-1" {
			t.Fatalf("pipeline_id = %v, want pipe-1", in.PipelineID)
		}
		trig := sampleTrigger()
		trig.TemplateID = ""
		trig.PipelineID = in.PipelineID
		return trig, "styr_whs_plaintext_secret", nil
	}
	var out triggerCreateOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/triggers", map[string]any{
		"name": "prod alerts", "kind": "grafana", "pipeline_id": "pipe-1",
	}, &out)
	if status != http.StatusCreated {
		t.Fatalf("status = %d, want 201", status)
	}
	if out.Trigger.PipelineID == nil || *out.Trigger.PipelineID != "pipe-1" {
		t.Fatalf("trigger.pipeline_id = %v, want pipe-1", out.Trigger.PipelineID)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestTriggersList_NeverIncludesSecretHash(t *testing.T) {
	e := newEnv(t)
	e.triggers.ListTriggersFn = func(context.Context, api.Actor) ([]domain.Trigger, error) {
		return []domain.Trigger{sampleTrigger()}, nil
	}

	var out []triggerOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/triggers", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /triggers = %d, want 200", status)
	}
	if len(out) != 1 {
		t.Fatalf("out = %+v", out)
	}
	if out[0].SecretHash != nil {
		t.Fatalf("secret_hash present in list response: %+v", out[0])
	}
	if out[0].SecretHint != "abcd1234" {
		t.Fatalf("secret_hint = %q, want abcd1234", out[0].SecretHint)
	}
}

func TestTriggersGet_OK(t *testing.T) {
	e := newEnv(t)
	e.triggers.GetTriggerFn = func(_ context.Context, _ api.Actor, id string) (domain.Trigger, error) {
		trig := sampleTrigger()
		trig.ID = id
		return trig, nil
	}
	var out triggerOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/triggers/trig-1", nil, &out)
	if status != http.StatusOK || out.ID != "trig-1" {
		t.Fatalf("GET /triggers/trig-1 = %d, out = %+v", status, out)
	}
}

func TestTriggersPatch_OK(t *testing.T) {
	e := newEnv(t)
	e.triggers.UpdateTriggerFn = func(_ context.Context, _ api.Actor, id string, in domain.TriggerInput) (domain.Trigger, error) {
		trig := sampleTrigger()
		trig.ID = id
		if in.Enabled != nil {
			trig.Enabled = *in.Enabled
		}
		return trig, nil
	}
	var out triggerOut
	status := e.doJSON(e.adminClient, http.MethodPatch, "/api/v1/triggers/trig-1", map[string]any{
		"name": "prod alerts", "kind": "grafana", "template_id": "tmpl-1", "enabled": false,
	}, &out)
	if status != http.StatusOK {
		t.Fatalf("PATCH /triggers/trig-1 = %d, want 200", status)
	}
	if out.Enabled {
		t.Fatalf("enabled = true, want false after patch")
	}
}

func TestTriggersDelete_OK(t *testing.T) {
	e := newEnv(t)
	e.triggers.DeleteTriggerFn = func(context.Context, api.Actor, string) error { return nil }
	status := e.doJSON(e.adminClient, http.MethodDelete, "/api/v1/triggers/trig-1", nil, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DELETE /triggers/trig-1 = %d, want 204", status)
	}
}

func TestTriggersRotateSecret_ReturnsSecretOnly(t *testing.T) {
	e := newEnv(t)
	e.triggers.RotateSecretFn = func(context.Context, api.Actor, string) (string, error) {
		return "styr_whs_rotated_secret", nil
	}
	var out triggerSecretOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/triggers/trig-1/rotate-secret", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("POST rotate-secret = %d, want 200", status)
	}
	if out.Secret != "styr_whs_rotated_secret" {
		t.Fatalf("secret = %q", out.Secret)
	}
}

func TestTriggersDeliveries_DefaultAndExplicitLimit(t *testing.T) {
	e := newEnv(t)
	e.triggers.ListDeliveriesFn = func(context.Context, api.Actor, string, int) ([]domain.Delivery, error) {
		return nil, nil
	}

	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/triggers/trig-1/deliveries", nil, nil); status != http.StatusOK {
		t.Fatalf("GET deliveries (default limit) = %d, want 200", status)
	}
	call, ok := e.triggers.lastCall()
	if !ok || call.method != "ListDeliveries" || call.args[1] != 50 {
		t.Fatalf("last call = %+v, ok=%v, want ListDeliveries with default limit 50", call, ok)
	}

	if status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/triggers/trig-1/deliveries?limit=5", nil, nil); status != http.StatusOK {
		t.Fatalf("GET deliveries (limit=5) = %d, want 200", status)
	}
	call, ok = e.triggers.lastCall()
	if !ok || call.args[1] != 5 {
		t.Fatalf("last call = %+v, ok=%v, want limit 5", call, ok)
	}
}

func TestTriggersTest_PassesPayloadAndForce(t *testing.T) {
	e := newEnv(t)
	var gotPayload []byte
	var gotForce bool
	e.triggers.TestFn = func(_ context.Context, _ api.Actor, triggerID string, payload []byte, force bool) (domain.Delivery, error) {
		gotPayload, gotForce = payload, force
		return domain.Delivery{ID: "del-1", TriggerID: triggerID, Status: domain.DeliveryAccepted, Payload: payload}, nil
	}
	var out struct {
		DeliveryID string  `json:"delivery_id"`
		Status     string  `json:"status"`
		RunID      *string `json:"run_id"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/triggers/trig-1/test",
		map[string]any{"payload": map[string]any{"a": 1}, "force": true}, &out)
	if status != http.StatusOK {
		t.Fatalf("POST test = %d, want 200", status)
	}
	if !gotForce {
		t.Fatal("force not passed through")
	}
	if string(gotPayload) != `{"a":1}` {
		t.Fatalf("payload = %q", gotPayload)
	}
	// Same {delivery_id, status, run_id?} shape as POST /hooks/{slug}.
	if out.DeliveryID != "del-1" || out.Status != "accepted" {
		t.Fatalf("out = %+v", out)
	}
}

func TestTriggersSample_ReturnsSamplePayload(t *testing.T) {
	e := newEnv(t)
	_, raw := e.doJSONHeaders(e.adminClient, http.MethodGet, "/api/v1/triggers/samples/grafana", nil, nil, false)
	if !containsSubstring(string(raw), "alerts") {
		t.Fatalf("grafana sample body = %s, want it to contain alert fields", raw)
	}
}

func TestTriggersSample_EveryKnownKind(t *testing.T) {
	e := newEnv(t)
	for _, kind := range []string{"generic", "grafana", "github"} {
		status, _ := e.doJSONHeaders(e.adminClient, http.MethodGet, "/api/v1/triggers/samples/"+kind, nil, nil, false)
		if status != http.StatusOK {
			t.Fatalf("GET samples/%s = %d, want 200", kind, status)
		}
	}
}

func TestTriggersSample_UnknownKindIs404(t *testing.T) {
	e := newEnv(t)
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/triggers/samples/carrier-pigeon", nil, &body)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

// A samples request must resolve to the literal /triggers/samples/{kind}
// route rather than being swallowed by /triggers/{id}, which would call
// GetTrigger instead of serving a sample.
func TestTriggersSample_TakesPriorityOverTriggerIDRoute(t *testing.T) {
	e := newEnv(t)
	e.triggers.GetTriggerFn = func(context.Context, api.Actor, string) (domain.Trigger, error) {
		t.Fatal("GetTrigger must not be called for /triggers/samples/{kind}")
		return domain.Trigger{}, nil
	}
	status, _ := e.doJSONHeaders(e.adminClient, http.MethodGet, "/api/v1/triggers/samples/grafana", nil, nil, false)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
}

func TestTriggersTest_ErrorMapping(t *testing.T) {
	e := newEnv(t)
	// /triggers/{id}/test is an authenticated route, so its errors go
	// through WriteError's ordinary domain-sentinel mapping — unlike POST
	// /hooks/{slug}, ErrUnknownTrigger has no special case here.
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"not-found", domain.ErrNotFound, http.StatusNotFound},
		{"invalid", domain.ErrInvalid, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e.triggers.TestFn = func(context.Context, api.Actor, string, []byte, bool) (domain.Delivery, error) {
				return domain.Delivery{}, c.err
			}
			status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/triggers/trig-1/test", map[string]any{"payload": map[string]any{}}, nil)
			if status != c.want {
				t.Fatalf("status = %d, want %d", status, c.want)
			}
		})
	}
}
