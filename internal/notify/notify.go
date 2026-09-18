// Package notify delivers outbound notifications about unattended runs to
// operator-configured channels: ntfy and a generic webhook. It has no
// dependency on the data layer: Channel and Event are plain inputs supplied
// by the caller (a decrypted token, not a channel ID to look up), and
// Publish never returns an error — delivery is fire-and-forget but
// synchronous, so callers (and tests) can rely on it having been attempted
// by the time Publish returns.
package notify

// Event is one thing that happened to a run, to notify operators about.
type Event struct {
	Kind     string // "run.finished" | "run.needs_human" | "run.failed"
	Title    string
	Body     string
	URL      string // deep link to the run
	Priority int    // 1-5, ntfy convention; 0 means unset
	Tags     []string
}

// Channel is one configured notification destination. Token is the plain,
// already-decrypted secret (the caller is responsible for sealing/opening
// it with internal/crypto before and after this package sees it) — Send and
// Publish never log it.
type Channel struct {
	ID     string
	Kind   string // "ntfy" | "webhook"
	Name   string
	URL    string
	Token  string
	Events []string // Event.Kind values this channel wants
}
