package db

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jonasthim/styr/internal/domain"
)

// seedDeliveryFixtures creates the chain of rows a delivery's trigger_id
// foreign key needs, returning the trigger id.
func seedDeliveryFixtures(t *testing.T, d *DB) string {
	t.Helper()
	tplID := seedTriggerFixtures(t, d)
	tr := newTestTrigger(tplID, "delivery-trigger", "delivery-trigger-slug")
	if err := NewTriggers(d).Create(context.Background(), tr); err != nil {
		t.Fatalf("seed trigger: %v", err)
	}
	return tr.ID
}

func newTestDelivery(triggerID string, status domain.DeliveryStatus, dedupeKey string, receivedAt time.Time) domain.Delivery {
	return domain.Delivery{
		ID:         uuid.NewString(),
		TriggerID:  triggerID,
		ReceivedAt: receivedAt,
		Status:     status,
		DedupeKey:  dedupeKey,
		Payload:    json.RawMessage(`{"status":"firing"}`),
	}
}

func TestDeliveries_CreateGetSetRun(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	trID := seedDeliveryFixtures(t, database)
	deliveries := NewDeliveries(database)

	dl := newTestDelivery(trID, domain.DeliveryAccepted, "firing:abc", time.Now())
	if err := deliveries.Create(ctx, dl); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := deliveries.Get(ctx, dl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.DeliveryAccepted || got.DedupeKey != "firing:abc" || got.RunID != nil {
		t.Fatalf("Get = %+v, want matching %+v", got, dl)
	}
	if string(got.Payload) != string(dl.Payload) {
		t.Fatalf("Get payload = %s, want %s", got.Payload, dl.Payload)
	}

	if err := deliveries.SetRun(ctx, dl.ID, "run-1"); err != nil {
		t.Fatalf("SetRun: %v", err)
	}
	got, err = deliveries.Get(ctx, dl.ID)
	if err != nil {
		t.Fatalf("Get after SetRun: %v", err)
	}
	if got.RunID == nil || *got.RunID != "run-1" {
		t.Fatalf("Get after SetRun RunID = %v, want run-1", got.RunID)
	}

	if err := deliveries.SetRun(ctx, "no-such-id", "run-1"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("SetRun unknown id: err = %v, want ErrNotFound", err)
	}
	if _, err := deliveries.Get(ctx, "no-such-id"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestDeliveries_ListByTrigger_NewestFirst(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	trID := seedDeliveryFixtures(t, database)
	deliveries := NewDeliveries(database)

	base := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 3; i++ {
		dl := newTestDelivery(trID, domain.DeliveryAccepted, "k", base.Add(time.Duration(i)*time.Minute))
		if err := deliveries.Create(ctx, dl); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}

	list, err := deliveries.ListByTrigger(ctx, trID, 50)
	if err != nil {
		t.Fatalf("ListByTrigger: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("ListByTrigger len = %d, want 3", len(list))
	}
	for i := 0; i < len(list)-1; i++ {
		if list[i].ReceivedAt.Before(list[i+1].ReceivedAt) {
			t.Fatalf("ListByTrigger not newest-first: %v before %v", list[i].ReceivedAt, list[i+1].ReceivedAt)
		}
	}

	limited, err := deliveries.ListByTrigger(ctx, trID, 1)
	if err != nil {
		t.Fatalf("ListByTrigger limited: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("ListByTrigger limited len = %d, want 1", len(limited))
	}
}

func TestDeliveries_CountSince(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	trID := seedDeliveryFixtures(t, database)
	deliveries := NewDeliveries(database)

	now := time.Now()
	inWindow := []time.Time{now.Add(-50 * time.Minute), now.Add(-10 * time.Minute)}
	outsideWindow := now.Add(-2 * time.Hour)

	for _, at := range inWindow {
		if err := deliveries.Create(ctx, newTestDelivery(trID, domain.DeliveryAccepted, "k", at)); err != nil {
			t.Fatalf("Create in-window: %v", err)
		}
	}
	if err := deliveries.Create(ctx, newTestDelivery(trID, domain.DeliveryAccepted, "k", outsideWindow)); err != nil {
		t.Fatalf("Create outside window: %v", err)
	}
	// A rejected delivery in the window must not count toward the storm cap.
	if err := deliveries.Create(ctx, newTestDelivery(trID, domain.DeliveryRejected, "k", now.Add(-5*time.Minute))); err != nil {
		t.Fatalf("Create rejected: %v", err)
	}

	since := now.Add(-1 * time.Hour)
	count, err := deliveries.CountSince(ctx, trID, since, domain.DeliveryAccepted)
	if err != nil {
		t.Fatalf("CountSince: %v", err)
	}
	if count != 2 {
		t.Fatalf("CountSince = %d, want 2", count)
	}
}

func TestDeliveries_LastAcceptedByKey_LatestWins(t *testing.T) {
	ctx := context.Background()
	database := testOpenDB(t)
	trID := seedDeliveryFixtures(t, database)
	deliveries := NewDeliveries(database)

	now := time.Now()
	older := newTestDelivery(trID, domain.DeliveryAccepted, "same-key", now.Add(-30*time.Minute))
	newer := newTestDelivery(trID, domain.DeliveryAccepted, "same-key", now.Add(-5*time.Minute))
	deduped := newTestDelivery(trID, domain.DeliveryDeduped, "same-key", now)
	for _, dl := range []domain.Delivery{older, newer, deduped} {
		if err := deliveries.Create(ctx, dl); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := deliveries.LastAcceptedByKey(ctx, trID, "same-key")
	if err != nil {
		t.Fatalf("LastAcceptedByKey: %v", err)
	}
	if got.ID != newer.ID {
		t.Fatalf("LastAcceptedByKey id = %s, want newer delivery %s (latest wins)", got.ID, newer.ID)
	}

	if _, err := deliveries.LastAcceptedByKey(ctx, trID, "no-such-key"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("LastAcceptedByKey unknown key: err = %v, want ErrNotFound", err)
	}
}
