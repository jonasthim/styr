// Package risk classifies tool calls emitted by the Claude Code CLI into risk
// tiers, and renders a short human-readable summary of each call.
package risk

import (
	"encoding/json"
	"regexp"

	"github.com/jonasthim/styr/internal/domain"
)

// sshPrefix matches an optional leading "ssh <host> " (with an optional
// "-p PORT" before the host) on a Bash command line. It is stripped before
// matching the read-only command prefixes below, but destructive patterns
// are matched against the whole command including this prefix.
var sshPrefix = regexp.MustCompile(`^ssh\s+(-p\s+\S+\s+)?\S+\s+`)

// destructivePatterns are checked, in order, against the whole Bash command
// (including any ssh prefix). A match on any of them classifies the call as
// domain.RiskDestructive.
var destructivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f|-[a-zA-Z]*f[a-zA-Z]*r)\b`),
	regexp.MustCompile(`\bgit\s+push\s+.*--force`),
	regexp.MustCompile(`\bgit\s+reset\s+--hard`),
	regexp.MustCompile(`\bdd\s+if=`),
	regexp.MustCompile(`\bmkfs`),
	regexp.MustCompile(`\bshutdown\b`),
	regexp.MustCompile(`\breboot\b`),
	regexp.MustCompile(`\bpct\s+(destroy|stop)\b`),
	regexp.MustCompile(`\bsystemctl\s+(stop|disable|mask)\b`),
	regexp.MustCompile(`DROP\s+TABLE`),
	regexp.MustCompile(`\btruncate\b`),
	regexp.MustCompile(`>\s*/dev/sd`),
}

// readOnlyPrefix matches a Bash command (after stripping any ssh prefix)
// that starts with a known read-only command.
var readOnlyPrefix = regexp.MustCompile(`^(cat|ls|grep|rg|find|head|tail|less|journalctl|systemctl\s+status|git\s+status|git\s+log|git\s+diff|df|du|free|uptime|ps|top|curl|wget\s+-qO-|dig|ping|kubectl\s+get|docker\s+ps|docker\s+logs|pct\s+list|pct\s+status|qm\s+list)(\s|$)`)

// readTools are always domain.RiskRead regardless of input.
var readTools = map[string]bool{
	"Read":            true,
	"Glob":            true,
	"Grep":            true,
	"LS":              true,
	"WebSearch":       true,
	"TodoWrite":       true,
	"Task":            true,
	"AskUserQuestion": true,
	// ExitPlanMode only asks to leave plan mode with a finished plan; it writes nothing, and
	// Styr routes it to the plan card rather than the ordinary tool-approval UI.
	"ExitPlanMode": true,
}

// writeTools are always domain.RiskWrite regardless of input.
var writeTools = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"MultiEdit":    true,
	"NotebookEdit": true,
}

// Classify maps a tool call to a risk tier. input is the raw tool input JSON.
func Classify(tool string, input json.RawMessage) domain.RiskTier {
	switch {
	case readTools[tool]:
		return domain.RiskRead
	case tool == "WebFetch":
		return domain.RiskExec
	case writeTools[tool]:
		return domain.RiskWrite
	case tool == "Bash":
		return classifyBash(inputField(input, "command"))
	default:
		return domain.RiskExec
	}
}

func classifyBash(command string) domain.RiskTier {
	if command == "" {
		return domain.RiskExec
	}
	for _, p := range destructivePatterns {
		if p.MatchString(command) {
			return domain.RiskDestructive
		}
	}
	stripped := sshPrefix.ReplaceAllString(command, "")
	if readOnlyPrefix.MatchString(stripped) {
		return domain.RiskRead
	}
	return domain.RiskExec
}

// fileFields are, in priority order, the input fields Summary looks at for
// tools whose summary names a file, pattern or path.
var fileFields = []string{"file_path", "pattern", "path"}

// Summary returns a one-line human description, e.g. "Bash: systemctl
// restart vector", "Edit: src/auth.go".
func Summary(tool string, input json.RawMessage) string {
	switch tool {
	case "Bash":
		return "Bash: " + truncate(inputField(input, "command"), 120)
	case "Edit", "Write", "Read", "Grep", "Glob":
		for _, f := range fileFields {
			if v := inputField(input, f); v != "" {
				return tool + ": " + v
			}
		}
		return tool
	default:
		return tool
	}
}

// inputField returns the string value of field in the raw tool input JSON,
// or "" if input is not a JSON object or the field is absent or not a string.
func inputField(input json.RawMessage, field string) string {
	if len(input) == 0 {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(input, &m); err != nil {
		return ""
	}
	raw, ok := m[field]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// truncate cuts s to at most n bytes.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
