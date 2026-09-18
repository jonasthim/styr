package domain

import (
	"encoding/json"
	"time"
)

// DeliveryStatus is the outcome of one inbound webhook delivery after the
// router applies auth, dedupe, cooldown and storm-cap checks.
type DeliveryStatus string

const (
	DeliveryAccepted DeliveryStatus = "accepted"
	DeliveryDeduped  DeliveryStatus = "deduped"
	DeliveryCooldown DeliveryStatus = "cooldown"
	DeliveryStorm    DeliveryStatus = "storm"
	DeliveryRejected DeliveryStatus = "rejected"
	DeliveryFailed   DeliveryStatus = "failed"
	DeliverySkipped  DeliveryStatus = "skipped"
)

// Delivery is one logged inbound webhook call for a trigger.
type Delivery struct {
	ID         string
	TriggerID  string
	ReceivedAt time.Time
	Status     DeliveryStatus
	Reason     string
	DedupeKey  string
	Payload    json.RawMessage
	RunID      *string
}
