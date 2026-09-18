package domain

import (
	"net/http"
	"net/url"
	"time"
)

// TemplateInput is the create/update payload for a Template, shared by the
// internal/triggers service (which validates and persists it) and
// internal/api (which decodes it from a request body). Shared marks a
// template as owned by nobody (OwnerID nil) rather than the acting user;
// only an admin actor may set it, a rule the service enforces.
type TemplateInput struct {
	Name           string
	WorkspaceID    string
	ProfileID      string
	TitleTemplate  string
	PromptTemplate string
	SystemPrompt   string
	ReportSchema   string
	Shared         bool
}

// TriggerInput is the create/update payload for a Trigger. Enabled is a
// pointer so update can distinguish "leave enabled as-is" (nil) from an
// explicit true/false; create treats nil as enabled.
type TriggerInput struct {
	Name              string
	Kind              string
	TemplateID        string
	DedupeKeyTemplate string
	CooldownS         int
	StormCapPerHour   int
	RunOnResolved     bool
	Shared            bool
	Enabled           *bool
}

// RenderResult is the outcome of a template dry-run render: the rendered
// title and prompt, plus any template-execution errors (e.g. a bad
// text/template expression) collected rather than failing the whole call.
type RenderResult struct {
	Title  string
	Prompt string
	Errors []string
}

// RunView is a Run together with the records a UI needs to display it
// without a second round trip: the session it started (nil if the session
// row is gone), the delivery that caused it (nil for a run started some
// other way), and the template it was rendered from (nil if the template
// was since deleted).
type RunView struct {
	Run      Run
	Session  *Session
	Delivery *Delivery
	Template *Template
}

// Inbound is one inbound call to POST /hooks/{slug}. It carries transport
// details (headers, query, raw body) because the trigger router needs them
// for secret/HMAC verification — an exception to this package's usual
// "no transport concerns" rule, accepted here so internal/api and
// internal/triggers can share one definition instead of two.
type Inbound struct {
	Slug    string
	Body    []byte
	Headers http.Header
	Query   url.Values
	Now     time.Time
}
