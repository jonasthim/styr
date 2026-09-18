package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/domain"
)

type deliveryOut struct {
	ID        string          `json:"id"`
	TriggerID string          `json:"trigger_id"`
	Status    string          `json:"status"`
	Payload   json.RawMessage `json:"payload"`
	RunID     *string         `json:"run_id"`
}

func TestDeliveriesReplay_AcceptedWithRunID(t *testing.T) {
	e := newEnv(t)
	runID := "run-1"
	e.triggers.ReplayFn = func(_ context.Context, _ api.Actor, deliveryID string) (domain.Delivery, error) {
		return domain.Delivery{
			ID: deliveryID, TriggerID: "trig-1", ReceivedAt: time.Now(),
			Status: domain.DeliveryAccepted, RunID: &runID,
		}, nil
	}

	var out struct {
		RunID *string `json:"run_id"`
	}
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/deliveries/del-1/replay", nil, &out)
	if status != http.StatusAccepted {
		t.Fatalf("POST replay = %d, want 202", status)
	}
	if out.RunID == nil || *out.RunID != "run-1" {
		t.Fatalf("run_id = %v, want run-1", out.RunID)
	}
}

func TestDeliveriesReplay_NotFound(t *testing.T) {
	e := newEnv(t)
	e.triggers.ReplayFn = func(context.Context, api.Actor, string) (domain.Delivery, error) {
		return domain.Delivery{}, domain.ErrNotFound
	}
	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodPost, "/api/v1/deliveries/missing/replay", nil, &body)
	if status != http.StatusNotFound {
		t.Fatalf("POST replay missing = %d, want 404", status)
	}
}
