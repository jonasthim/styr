// plan.go handles the ExitPlanMode permission request: the CLI's way of surfacing a finished
// plan-mode plan over the permission channel instead of a distinct message type. See
// testdata/PROTOCOL.md "Plan mode in -p" for the recorded shape (fixture 08).
package claude

import (
	"encoding/json"

	"github.com/jonasthim/styr/internal/harness"
)

// planFromInput extracts the "plan" string field from an ExitPlanMode control_request's raw
// input, or "" when absent or not a string.
func planFromInput(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var in struct {
		Plan string `json:"plan"`
	}
	if json.Unmarshal(raw, &in) != nil {
		return ""
	}
	return in.Plan
}

// IsPlanExit reports whether req is the CLI asking to exit plan mode with a finished plan
// (tool name "ExitPlanMode"). Styr's session service uses this to route the permission request
// to the Plan card instead of the ordinary tool-approval UI.
func IsPlanExit(req harness.PermissionRequest) bool {
	return req.ToolName == "ExitPlanMode"
}
