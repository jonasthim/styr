package triggers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/templates"
)

// maxPayloadBytes is the largest inbound body Styr accepts and the size a
// stored payload is truncated to (256 KiB).
const maxPayloadBytes = 256 * 1024

// genericDedupeWindow is how long an identical generic (or github) payload
// keeps deduping. Grafana deliveries dedupe on the alert fingerprint
// instead, which stays meaningful indefinitely.
const genericDedupeWindow = 24 * time.Hour

// defaultDeliveryLimit and maxDeliveryLimit bound ListDeliveries.
const (
	defaultDeliveryLimit = 50
	maxDeliveryLimit     = 500
)

// Deliver is the `/hooks/{slug}` pipeline: authenticate the caller, log
// what was decided about the payload, and start a run when it is accepted.
// It returns domain.ErrTooLarge, domain.ErrUnknownTrigger or
// domain.ErrBadSecret for the three refusals that never reach the delivery
// log; every other outcome is a delivery row.
func (s *Service) Deliver(ctx context.Context, in domain.Inbound) (domain.Delivery, error) {
	if len(in.Body) > maxPayloadBytes {
		return domain.Delivery{}, fmt.Errorf("%w: %d bytes exceeds the %d byte limit", domain.ErrTooLarge, len(in.Body), maxPayloadBytes)
	}
	tr, err := s.repos.Triggers.GetBySlug(ctx, in.Slug)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Delivery{}, fmt.Errorf("trigger %q: %w", in.Slug, domain.ErrUnknownTrigger)
		}
		return domain.Delivery{}, err
	}
	// A disabled trigger is indistinguishable from a missing one on the
	// wire: a sender learns nothing about which slugs exist.
	if !tr.Enabled {
		return domain.Delivery{}, fmt.Errorf("trigger %q: %w", in.Slug, domain.ErrUnknownTrigger)
	}
	if !authenticate(*tr, in) {
		s.logger.Warn("triggers: bad secret", "trigger_id", tr.ID, "slug", tr.Slug)
		return domain.Delivery{}, fmt.Errorf("trigger %q: %w", in.Slug, domain.ErrBadSecret)
	}

	now := in.Now
	if now.IsZero() {
		now = s.now()
	}
	return s.process(ctx, *tr, in.Body, now, false, "")
}

// authenticate checks the presented secret against the trigger's stored
// hash. Three carriers are accepted (ADR-010): the `X-Styr-Secret` header,
// an `Authorization: Bearer` header, and a `secret` query parameter for
// senders — GitHub among them — that cannot set custom headers.
func authenticate(tr domain.Trigger, in domain.Inbound) bool {
	for _, presented := range presentedSecrets(in) {
		if secretMatches(presented, tr.SecretHash) {
			return true
		}
	}
	return false
}

func presentedSecrets(in domain.Inbound) []string {
	var out []string
	if in.Headers != nil {
		if v := strings.TrimSpace(in.Headers.Get("X-Styr-Secret")); v != "" {
			out = append(out, v)
		}
		if v := bearerToken(in.Headers); v != "" {
			out = append(out, v)
		}
	}
	if in.Query != nil {
		if v := strings.TrimSpace(in.Query.Get("secret")); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func bearerToken(h http.Header) string {
	const prefix = "Bearer "
	v := h.Get("Authorization")
	if len(v) > len(prefix) && strings.EqualFold(v[:len(prefix)], prefix) {
		return strings.TrimSpace(v[len(prefix):])
	}
	return ""
}

// ListDeliveries returns the newest deliveries for a trigger the actor can
// see. Delivery visibility follows the trigger's.
func (s *Service) ListDeliveries(ctx context.Context, actor Actor, triggerID string, limit int) ([]domain.Delivery, error) {
	if _, err := s.GetTrigger(ctx, actor, triggerID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultDeliveryLimit
	}
	if limit > maxDeliveryLimit {
		limit = maxDeliveryLimit
	}
	return s.repos.Deliveries.ListByTrigger(ctx, triggerID, limit)
}

// Replay runs the pipeline again for a delivery's stored payload, forcing
// past dedupe, cooldown and the storm cap. It never touches the original
// delivery: the replay is logged as its own row, pointing back at the one
// it came from.
func (s *Service) Replay(ctx context.Context, actor Actor, deliveryID string) (domain.Delivery, error) {
	dl, err := s.repos.Deliveries.Get(ctx, deliveryID)
	if err != nil {
		return domain.Delivery{}, err
	}
	tr, err := s.mutableTrigger(ctx, actor, dl.TriggerID, "replay a delivery of")
	if err != nil {
		return domain.Delivery{}, err
	}
	return s.process(ctx, tr, dl.Payload, s.now(), true, "replay of "+dl.ID)
}

// Test runs the pipeline for a payload the operator supplied (or the
// kind's sample payload when it is empty), optionally forcing past dedupe,
// cooldown and the storm cap.
func (s *Service) Test(ctx context.Context, actor Actor, triggerID string, payload []byte, force bool) (domain.Delivery, error) {
	tr, err := s.mutableTrigger(ctx, actor, triggerID, "send a test payload to")
	if err != nil {
		return domain.Delivery{}, err
	}
	if len(payload) == 0 {
		payload = templates.SamplePayload(string(tr.Kind))
	}
	return s.process(ctx, tr, payload, s.now(), force, "test")
}

// process is the shared body of Deliver, Replay and Test: normalise the
// payload, decide what to do with it, write exactly one delivery row, and
// start the run when it was accepted. The trigger's last_delivery_at is
// stamped whatever the decision was, so an operator can see that something
// arrived even when nothing ran.
func (s *Service) process(ctx context.Context, tr domain.Trigger, payload []byte, now time.Time, force bool, acceptedReason string) (domain.Delivery, error) {
	defer func() {
		if err := s.repos.Triggers.TouchDelivery(ctx, tr.ID, now); err != nil {
			s.logger.Error("triggers: touch last delivery", "trigger_id", tr.ID, "error", err)
		}
	}()

	dl := domain.Delivery{
		ID:         uuid.NewString(),
		TriggerID:  tr.ID,
		ReceivedAt: now,
		Payload:    truncatePayload(payload),
	}

	vars, err := templates.Normalize(string(tr.Kind), dl.Payload)
	if err != nil {
		return s.record(ctx, dl, domain.DeliveryRejected, "invalid payload: "+err.Error())
	}
	key, err := templates.RenderDedupe(tr.DedupeKeyTemplate, string(tr.Kind), vars)
	if err != nil {
		return s.record(ctx, dl, domain.DeliveryFailed, "dedupe key: "+err.Error())
	}
	dl.DedupeKey = key

	if status, reason, ok := s.gate(ctx, tr, vars, key, now, force); !ok {
		return s.record(ctx, dl, status, reason)
	}

	dl.Status = domain.DeliveryAccepted
	dl.Reason = acceptedReason
	if err := s.repos.Deliveries.Create(ctx, dl); err != nil {
		return domain.Delivery{}, err
	}

	run, err := s.engine.Start(ctx, RunInput{
		TemplateID: tr.TemplateID,
		TriggerID:  tr.ID,
		DeliveryID: dl.ID,
		Vars:       vars,
		Origin:     domain.OriginWebhook,
	})
	if err != nil {
		// The delivery row already exists (the run references it), so the
		// failure is recorded on the row rather than losing it.
		reason := "start run: " + err.Error()
		s.logger.Error("triggers: start run", "trigger_id", tr.ID, "delivery_id", dl.ID, "error", err)
		if sErr := s.repos.Deliveries.SetStatus(ctx, dl.ID, domain.DeliveryFailed, reason); sErr != nil {
			s.logger.Error("triggers: mark delivery failed", "delivery_id", dl.ID, "error", sErr)
		}
		dl.Status = domain.DeliveryFailed
		dl.Reason = reason
		return dl, nil
	}

	if err := s.repos.Deliveries.SetRun(ctx, dl.ID, run.ID); err != nil {
		s.logger.Error("triggers: link delivery to run", "delivery_id", dl.ID, "run_id", run.ID, "error", err)
	}
	runID := run.ID
	dl.RunID = &runID
	return dl, nil
}

// gate applies the four reasons a delivery does not start a run. force
// (replay, or an explicit forced test) skips dedupe, cooldown and the storm
// cap, but never the resolved-alert rule: a trigger that opted out of
// resolved alerts means it.
func (s *Service) gate(ctx context.Context, tr domain.Trigger, vars templates.Vars, key string, now time.Time, force bool) (domain.DeliveryStatus, string, bool) {
	if tr.Kind == domain.TriggerGrafana && !tr.RunOnResolved && grafanaStatus(vars) == "resolved" {
		return domain.DeliverySkipped, "resolved", false
	}
	if force {
		return domain.DeliveryAccepted, "", true
	}

	last, err := s.repos.Deliveries.LastAcceptedByKey(ctx, tr.ID, key)
	switch {
	case err == nil:
		cooldown := time.Duration(tr.CooldownS) * time.Second
		if now.Sub(last.ReceivedAt) < cooldown {
			return domain.DeliveryCooldown, fmt.Sprintf("within the %s cooldown of delivery %s", cooldown, last.ID), false
		}
		// Grafana keys carry the alert status and fingerprints, so an
		// identical key means the very same alert in the very same state:
		// it never stops being a duplicate. Other kinds hash the payload,
		// where an identical body a day later is worth looking at again.
		if tr.Kind == domain.TriggerGrafana || now.Sub(last.ReceivedAt) < genericDedupeWindow {
			return domain.DeliveryDeduped, "same payload as delivery " + last.ID, false
		}
	case errors.Is(err, domain.ErrNotFound):
		// No accepted delivery with this key yet: nothing to dedupe against.
	default:
		return domain.DeliveryFailed, "dedupe lookup: " + err.Error(), false
	}

	if tr.StormCapPerHour > 0 {
		count, err := s.repos.Deliveries.CountSince(ctx, tr.ID, now.Add(-time.Hour), domain.DeliveryAccepted)
		if err != nil {
			return domain.DeliveryFailed, "storm cap lookup: " + err.Error(), false
		}
		if count >= tr.StormCapPerHour {
			return domain.DeliveryStorm, fmt.Sprintf("storm cap of %d runs per hour reached", tr.StormCapPerHour), false
		}
	}
	return domain.DeliveryAccepted, "", true
}

// record writes a delivery that did not start a run.
func (s *Service) record(ctx context.Context, dl domain.Delivery, status domain.DeliveryStatus, reason string) (domain.Delivery, error) {
	dl.Status = status
	dl.Reason = reason
	if err := s.repos.Deliveries.Create(ctx, dl); err != nil {
		return domain.Delivery{}, err
	}
	s.logger.Info("triggers: delivery not run", "trigger_id", dl.TriggerID, "delivery_id", dl.ID, "status", status, "reason", reason)
	return dl, nil
}

// grafanaStatus reads the normalised top-level alert status ("firing" or
// "resolved") from a grafana payload.
func grafanaStatus(vars templates.Vars) string {
	status, _ := vars["status"].(string)
	return status
}

// truncatePayload bounds what is stored for a delivery. Deliver rejects an
// oversized body outright; Replay and Test can still be handed one, and a
// truncated record is better than an unbounded row.
func truncatePayload(payload []byte) json.RawMessage {
	if len(payload) > maxPayloadBytes {
		payload = payload[:maxPayloadBytes]
	}
	return json.RawMessage(payload)
}
