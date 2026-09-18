// Package templates renders unattended-session prompts and titles from
// inbound trigger payloads. It has no dependency on the data layer: every
// function here takes plain inputs (a byte payload, a template string, a
// Vars map) and returns plain outputs, so it can be developed, tested and
// reused independently of internal/triggers and internal/runs.
package templates

// Vars is the data made available to a template during rendering. Values
// come either from Normalize (structured fields extracted from a webhook
// payload) or are assembled by a caller for a dry-run render.
type Vars map[string]any
