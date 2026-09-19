package api

import (
	"net/http"
	"runtime/debug"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/claude"
)

// registerStatusRoutes mounts the server status summary.
func registerStatusRoutes(r chi.Router, d *Deps) {
	r.Get("/status", handleStatus(d))
}

// apiVersion is the API's own major.major.minor version, independent of
// Deps.Version (the styr binary's own version). Within the 1.x line the API
// only changes additively; see docs/API.md's compatibility promise.
const apiVersion = "1.0.0"

// registerVersionRoutes mounts GET /api/v1/version. It is deliberately
// wired up in router.go outside the RequireUser group, unauthenticated
// like /healthz: it exists so a client or a monitoring probe can learn
// what it is talking to before it has any credentials.
func registerVersionRoutes(r chi.Router, d *Deps) {
	r.Get("/version", handleVersion(d))
}

// versionResponse is GET /api/v1/version's body.
type versionResponse struct {
	// Version is the styr binary's own release version (Deps.Version, set
	// at build time from the VERSION file; "dev" outside a release build).
	Version string `json:"version"`
	// APIVersion is the stable API's own version (docs/openapi.yaml's
	// info.version), which only changes additively within the 1.x line.
	APIVersion string `json:"api_version"`
	// Commit is the short git revision the running binary was built from,
	// read from the Go build's embedded VCS stamp (see `go version -m`).
	// A binary built without VCS info (e.g. via `go test`, or outside a
	// git checkout) reports "unknown" rather than failing the request.
	Commit string `json:"commit"`
}

// handleVersion is GET /api/v1/version.
func handleVersion(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		WriteJSON(w, http.StatusOK, versionResponse{
			Version:    d.Version,
			APIVersion: apiVersion,
			Commit:     buildCommit(),
		})
	}
}

// buildCommit reads the git revision the running binary was built from out
// of its embedded build info (Go stamps this automatically on `go build`
// from a git checkout; `go test` binaries do not carry it, hence the
// "unknown" fallback). Truncated to a short hash, the same length `git
// rev-parse --short` typically produces.
func buildCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			if s.Value == "" {
				break
			}
			return s.Value
		}
	}
	return "unknown"
}

// modelOption is one entry of the model picker: the alias passed to the CLI as --model, and
// the label shown in the UI.
type modelOption struct {
	Alias string `json:"alias"`
	Label string `json:"label"`
}

// statusModels is the static alias list Styr offers. The CLI accepts full model names too
// (and reports one back on init), but only these four are offered as choices.
var statusModels = []modelOption{
	{Alias: "fable", Label: "Fable 5.1"},
	{Alias: "opus", Label: "Opus 5"},
	{Alias: "sonnet", Label: "Sonnet 5"},
	{Alias: "haiku", Label: "Haiku 4.5"},
}

// statusResponse is GET /api/v1/status: the live counters from Deps.Status plus the static
// choices the UI needs to build its model, effort and slash-command controls.
type statusResponse struct {
	StatusInfo
	Models  []modelOption `json:"models"`
	Efforts []string      `json:"efforts"`
	// HiddenCommands are the CLI built-ins the composer's slash menu must filter out of a
	// session's reported command list (see internal/harness/claude/builtins.go).
	HiddenCommands []string `json:"hidden_commands"`
}

// handleStatus is GET /api/v1/status.
func handleStatus(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, statusResponse{
			StatusInfo:     d.Status(),
			Models:         statusModels,
			Efforts:        harness.ValidEfforts,
			HiddenCommands: claude.HiddenBuiltins(),
		})
	}
}
