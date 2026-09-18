package templates

import _ "embed"

// grafanaSample is a realistic Grafana Alertmanager-compatible webhook body
// with one firing SystemdUnitFailed alert, shared by SamplePayload("grafana")
// and the Normalize tests.
//
//go:embed testdata/grafana_firing.json
var grafanaSample []byte

// genericSample is a realistic generic webhook body for SamplePayload("generic")
// and any other unrecognized kind.
var genericSample = []byte(`{"event":"deploy.finished","service":"styr","status":"ok","duration_ms":4231}`)

// githubSample is a realistic GitHub pull_request webhook body for
// SamplePayload("github").
var githubSample = []byte(`{
  "action": "opened",
  "number": 42,
  "pull_request": {
    "number": 42,
    "title": "Add trigger router",
    "html_url": "https://github.com/jonasthim/styr/pull/42"
  },
  "repository": {"full_name": "jonasthim/styr"},
  "sender": {"login": "jonasthim"}
}`)

// SamplePayload returns a realistic example webhook body for kind, used for
// "send test payload" and template dry-run preview in the UI. Unrecognized
// kinds fall back to the generic sample.
func SamplePayload(kind string) []byte {
	switch kind {
	case "grafana":
		return grafanaSample
	case "github":
		return githubSample
	default:
		return genericSample
	}
}
