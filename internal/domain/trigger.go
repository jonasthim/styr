package domain

import "time"

// TriggerKind is the inbound webhook shape a trigger expects.
type TriggerKind string

const (
	TriggerGeneric TriggerKind = "generic"
	TriggerGrafana TriggerKind = "grafana"
	TriggerGitHub  TriggerKind = "github"
)

// Trigger is an inbound webhook endpoint (`/hooks/{slug}`) that renders a
// template and starts an unattended session, subject to dedupe, cooldown
// and a storm cap. SecretHash is the sha256 of the bearer secret shown to
// the owner once at creation/rotation; it is never marshalled to JSON —
// only SecretHint (a short display fragment) is exposed.
type Trigger struct {
	ID      string
	OwnerID *string // nil = shared, admin-created
	Name    string
	Slug    string
	Kind    TriggerKind

	SecretHash string `json:"-"`
	SecretHint string

	TemplateID string
	Enabled    bool

	DedupeKeyTemplate string
	CooldownS         int
	StormCapPerHour   int
	RunOnResolved     bool

	CreatedAt      time.Time
	UpdatedAt      time.Time
	LastDeliveryAt *time.Time
}
