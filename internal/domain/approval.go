package domain

import (
	"encoding/json"
	"time"
)

// ApprovalState is where a permission request is in its lifecycle.
type ApprovalState string

const (
	ApprovalPending ApprovalState = "pending"
	ApprovalAllowed ApprovalState = "allowed"
	ApprovalDenied  ApprovalState = "denied"
	ApprovalExpired ApprovalState = "expired"
)

// RiskTier classifies how dangerous a tool call is.
type RiskTier string

const (
	RiskRead        RiskTier = "read"
	RiskWrite       RiskTier = "write"
	RiskExec        RiskTier = "exec"
	RiskDestructive RiskTier = "destructive"
)

// Approval is a permission request raised by a session for a tool call.
type Approval struct {
	ID           string
	SessionID    string
	RequestID    string
	Tool         string
	Input        json.RawMessage
	Risk         RiskTier
	State        ApprovalState
	CreatedAt    time.Time
	DecidedBy    *string
	DecidedAt    *time.Time
	SnoozedUntil *time.Time
	UpdatedInput json.RawMessage
	Message      string
	// Plan is the plan markdown an ExitPlanMode request carries (see
	// harness.PermissionRequest.Plan); empty for every other tool.
	Plan string
}
