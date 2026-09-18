package templates

import (
	"encoding/json"
	"fmt"
)

// grafanaFields are the top-level Alertmanager-compatible webhook fields
// Normalize lifts into Vars for kind "grafana", alongside the full decoded
// payload. See docs/superpowers/plans/2026-09-18-styr-v0.2-triggers.md
// ("Template rendering").
var grafanaFields = []string{
	"status", "alerts", "commonLabels", "commonAnnotations", "groupLabels",
	"title", "message", "externalURL",
}

// Normalize decodes raw (the body of an inbound trigger delivery) into Vars
// for template rendering, according to kind:
//
//   - "generic": {"payload": <decoded JSON>}, or {"payload": {"raw": <raw
//     string>}} when raw is not valid JSON.
//   - "grafana": the Alertmanager-compatible webhook fields (status, alerts,
//     commonLabels, commonAnnotations, groupLabels, title, message,
//     externalURL) lifted to the top level, plus "payload" holding the full
//     decoded object. raw must be valid JSON.
//   - "github": {"payload": <decoded JSON>} plus, when present, "action",
//     "repo" (repository.full_name), "sender" (sender.login) and "number"
//     (pull_request.number or issue.number). raw must be valid JSON.
func Normalize(kind string, raw []byte) (Vars, error) {
	switch kind {
	case "generic":
		return normalizeGeneric(raw), nil
	case "grafana":
		return normalizeGrafana(raw)
	case "github":
		return normalizeGitHub(raw)
	default:
		return nil, fmt.Errorf("templates: normalize: unknown kind %q", kind)
	}
}

func normalizeGeneric(raw []byte) Vars {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err == nil {
		return Vars{"payload": decoded}
	}
	return Vars{"payload": map[string]any{"raw": string(raw)}}
}

func normalizeGrafana(raw []byte) (Vars, error) {
	m, err := decodeObject(raw)
	if err != nil {
		return nil, fmt.Errorf("templates: normalize grafana: %w", err)
	}
	vars := Vars{"payload": m}
	for _, key := range grafanaFields {
		if v, ok := m[key]; ok {
			vars[key] = v
			continue
		}
		vars[key] = zeroFor(key)
	}
	return vars, nil
}

// zeroFor returns a safe default for a grafana field missing from the
// payload, so a template can still range over .alerts or index .commonLabels
// without a nil-map or nil-slice surprise.
func zeroFor(key string) any {
	switch key {
	case "alerts":
		return []any{}
	case "commonLabels", "commonAnnotations", "groupLabels":
		return map[string]any{}
	default:
		return ""
	}
}

func normalizeGitHub(raw []byte) (Vars, error) {
	m, err := decodeObject(raw)
	if err != nil {
		return nil, fmt.Errorf("templates: normalize github: %w", err)
	}
	vars := Vars{"payload": m}
	if action, ok := m["action"]; ok {
		vars["action"] = action
	}
	if repo, ok := m["repository"].(map[string]any); ok {
		if fullName, ok := repo["full_name"]; ok {
			vars["repo"] = fullName
		}
	}
	if sender, ok := m["sender"].(map[string]any); ok {
		if login, ok := sender["login"]; ok {
			vars["sender"] = login
		}
	}
	if pr, ok := m["pull_request"].(map[string]any); ok {
		if number, ok := pr["number"]; ok {
			vars["number"] = number
		}
	} else if issue, ok := m["issue"].(map[string]any); ok {
		if number, ok := issue["number"]; ok {
			vars["number"] = number
		}
	}
	return vars, nil
}

func decodeObject(raw []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
