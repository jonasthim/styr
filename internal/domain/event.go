package domain

import (
	"encoding/json"
	"time"
)

// Event is one entry in a session's append-only event log.
type Event struct {
	ID        int64
	SessionID string
	Seq       int64
	At        time.Time
	Type      string
	Payload   json.RawMessage
}

// AuditEntry is one entry in the server-wide append-only audit log.
type AuditEntry struct {
	ID     int64
	At     time.Time
	Actor  string
	Action string
	Target string
	Detail json.RawMessage
}
