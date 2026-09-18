package domain

import "time"

// Template is a reusable prompt (Go text/template) rendered against a
// trigger delivery's payload to start an unattended session.
type Template struct {
	ID      string
	OwnerID *string // nil = shared, admin-created
	Name    string

	WorkspaceID string
	ProfileID   string

	TitleTemplate  string
	PromptTemplate string
	SystemPrompt   string
	ReportSchema   string // JSON schema text; empty = no structured report expected

	CreatedAt time.Time
	UpdatedAt time.Time
}
