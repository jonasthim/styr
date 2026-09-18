package templates

// DefaultDedupeKey returns the default dedupe key template for kind, used
// by RenderDedupe when a trigger has no dedupe_key_template of its own.
// Grafana alerts already carry a stable per-alert fingerprint, so the
// default groups by status and fingerprints; every other kind falls back to
// hashing the whole normalized payload, so identical deliveries dedupe and
// different ones don't.
func DefaultDedupeKey(kind string) string {
	if kind == "grafana" {
		return `{{ .status }}:{{ range .alerts }}{{ .fingerprint }},{{ end }}`
	}
	return `{{ json .payload | sha256 }}`
}

// RenderDedupe renders a trigger's dedupe key. An empty tmpl uses
// DefaultDedupeKey(kind).
func RenderDedupe(tmpl, kind string, vars Vars) (string, error) {
	if tmpl == "" {
		tmpl = DefaultDedupeKey(kind)
	}
	return Render(tmpl, vars)
}
