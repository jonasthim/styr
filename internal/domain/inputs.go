package domain

import (
	"net/http"
	"net/url"
	"time"
)

// TemplateInput is the mutable shape of a template, shared by the API
// handlers and internal/triggers so both sides speak the same type.
// Shared asks for an owner-less (shared) template; it is only honoured for
// an admin actor.
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

// TriggerInput is the mutable shape of a trigger. Enabled is a pointer so a
// create can default it to true and an update can leave it untouched;
// CooldownS and StormCapPerHour fall back to their defaults when zero.
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

// RenderResult is a template dry run: the rendered title and prompt plus
// any rendering errors, so a caller can show a partial preview rather than
// only an error.
type RenderResult struct {
	Title  string
	Prompt string
	Errors []string
}

// RunView is a run with the rows it points at resolved: the session it
// drives, the delivery that caused it and the template it was rendered
// from. Session, Delivery and Template are nil when absent or unreadable.
type RunView struct {
	Run      Run
	Session  *Session
	Delivery *Delivery
	Template *Template
}

// Inbound is one inbound webhook call to `/hooks/{slug}`, handed to the
// trigger router by the HTTP layer. Query carries the request's query
// parameters so the `?secret=` authentication fallback (ADR-010) works for
// senders that cannot set headers.
type Inbound struct {
	Slug    string
	Body    []byte
	Headers http.Header
	Query   url.Values
	Now     time.Time
}
