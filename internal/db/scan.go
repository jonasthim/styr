package db

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// nowString returns t formatted as an RFC3339Nano UTC string, the canonical
// timestamp representation used by every table.
func nowString(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// parseTime parses a stored RFC3339Nano timestamp. An empty string yields
// the zero time so callers can treat "not set" uniformly.
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

// nullString converts a nullable text column into *string.
func nullString(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	s := ns.String
	return &s
}

// nullTime converts a nullable timestamp column into *time.Time.
func nullTime(ns sql.NullString) *time.Time {
	if !ns.Valid || ns.String == "" {
		return nil
	}
	t := parseTime(ns.String)
	return &t
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint
// failure, mapped by callers to domain.ErrConflict.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// boolToInt converts a bool to SQLite's 0/1 integer representation.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// marshalStringList encodes a string slice as a JSON array for a TEXT
// column, treating nil as an empty array.
func marshalStringList(items []string) string {
	if items == nil {
		items = []string{}
	}
	b, _ := json.Marshal(items)
	return string(b)
}

// unmarshalStringList decodes a JSON array TEXT column back into a string
// slice, treating an unparsable value as empty.
func unmarshalStringList(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}
