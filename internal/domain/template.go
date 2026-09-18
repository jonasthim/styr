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

	// LoopUntil is the report field that ends a loop started from this
	// template; empty means the template does not loop. LoopMax caps how
	// many iterations such a loop may run.
	LoopUntil string
	LoopMax   int

	CreatedAt time.Time
	UpdatedAt time.Time
}
