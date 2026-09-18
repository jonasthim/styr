package templates

// Seed is a template ready to be inserted for a fresh install (owner_user_id
// NULL: shared, admin-managed). See Seeded.
type Seed struct {
	Name           string
	Kind           string
	ProfileID      string
	TitleTemplate  string
	PromptTemplate string
	SystemPrompt   string
	ReportSchema   string
}

// grafanaPromptTemplate is the default prompt for the seeded "Grafana alert
// investigation" template. Kept verbatim from
// docs/superpowers/plans/2026-09-18-styr-v0.2-triggers.md ("Template
// rendering").
const grafanaPromptTemplate = `A Grafana alert is {{ .status }}. Investigate read-only and report.
{{ range .alerts }}- {{ .labels.alertname }} on {{ default "unknown" .labels.instance }}: {{ .annotations.summary }}
  {{ .annotations.description }} (since {{ .startsAt }})
{{ end }}
Use the workspace's runbooks and only read-only commands. Do not change anything.`

const grafanaReportSchema = `{"type":"object","required":["severity","diagnosis","proposed_action","confidence"],` +
	`"properties":{"severity":{"enum":["info","warning","critical"]},"diagnosis":{"type":"string"},` +
	`"evidence":{"type":"array","items":{"type":"string"}},"proposed_action":{"type":"string"},` +
	`"confidence":{"type":"number","minimum":0,"maximum":1},"resolved_itself":{"type":"boolean"}}}`

// Seeded returns the templates a fresh install ships with. Currently a
// single shared template, "Grafana alert investigation", for kind "grafana"
// under the "investigate" profile.
func Seeded() []Seed {
	return []Seed{
		{
			Name:           "Grafana alert investigation",
			Kind:           "grafana",
			ProfileID:      "investigate",
			TitleTemplate:  `{{ .status }}: {{ join ", " (alertnames .alerts) }}`,
			PromptTemplate: grafanaPromptTemplate,
			SystemPrompt:   "You are investigating a production alert read-only. Prefer runbooks and existing dashboards. Never change state.",
			ReportSchema:   grafanaReportSchema,
		},
	}
}
