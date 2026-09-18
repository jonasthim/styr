package stats

import "time"

// formatTime renders t as the RFC3339Nano UTC string every table stores
// timestamps as (matches internal/db's private nowString, duplicated here
// since stats only ever reads through *db.DB, never through db's unexported
// helpers).
func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// parseTime parses a stored RFC3339Nano timestamp (matches internal/db's
// private parseTime): an empty or unparsable string yields the zero time so
// callers can treat "not set" uniformly.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}
