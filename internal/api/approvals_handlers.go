package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// registerApprovalsRoutes mounts the pending-approvals queue.
func registerApprovalsRoutes(r chi.Router, d *Deps) {
	r.Get("/approvals", handleApprovalsList(d))
	r.Post("/approvals/{id}", handleApprovalsDecide(d))
	r.Post("/approvals/{id}/snooze", handleApprovalsSnooze(d))
}

type approvalDTO struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"session_id"`
	SessionTitle string          `json:"session_title"`
	NowLine      string          `json:"now_line"`
	RequestID    string          `json:"request_id"`
	Tool         string          `json:"tool"`
	Input        json.RawMessage `json:"input"`
	Risk         string          `json:"risk"`
	State        string          `json:"state"`
	CreatedAt    time.Time       `json:"created_at"`
	SnoozedUntil *time.Time      `json:"snoozed_until"`
	UpdatedInput json.RawMessage `json:"updated_input,omitempty"`
	Message      string          `json:"message,omitempty"`
	// Plan is the plan markdown an ExitPlanMode approval carries, so the
	// inbox can render the plan card; empty for every other tool.
	Plan string `json:"plan,omitempty"`
}

// handleApprovalsList is GET /api/v1/approvals?state=pending. Only
// state=pending (the default and only supported filter) is implemented.
func handleApprovalsList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor := actorFrom(r)
		list, err := d.Sessions.PendingApprovals(r.Context(), actor)
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]approvalDTO, 0, len(list))
		for _, ap := range list {
			dto := approvalDTO{
				ID: ap.ID, SessionID: ap.SessionID, RequestID: ap.RequestID, Tool: ap.Tool, Input: ap.Input,
				Risk: string(ap.Risk), State: string(ap.State), CreatedAt: ap.CreatedAt,
				SnoozedUntil: ap.SnoozedUntil, UpdatedInput: ap.UpdatedInput, Message: ap.Message,
				Plan: ap.Plan,
			}
			// session_title and now_line go through the service so
			// visibility applies the same way it does to every other read.
			if sess, err := d.Sessions.Get(r.Context(), actor, ap.SessionID); err == nil {
				dto.SessionTitle = sess.Title
				dto.NowLine = sess.NowLine
			}
			out = append(out, dto)
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type approvalDecideInput struct {
	Decision     string          `json:"decision"` // "allow" | "deny"
	UpdatedInput json.RawMessage `json:"updated_input,omitempty"`
	Message      string          `json:"message,omitempty"`
	// Plan is the plan markdown an ExitPlanMode approval carries, so the
	// inbox can render the plan card; empty for every other tool.
	Plan string `json:"plan,omitempty"`
}

// handleApprovalsDecide is POST /api/v1/approvals/{id}.
func handleApprovalsDecide(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var in approvalDecideInput
		if err := decodeJSON(r, &in); err != nil || (in.Decision != "allow" && in.Decision != "deny") {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", `decision must be "allow" or "deny"`)
			return
		}
		allow := in.Decision == "allow"
		if err := d.Sessions.Decide(r.Context(), actorFrom(r), id, allow, in.UpdatedInput, in.Message); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type approvalSnoozeInput struct {
	Until time.Time `json:"until"`
}

// handleApprovalsSnooze is POST /api/v1/approvals/{id}/snooze.
func handleApprovalsSnooze(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var in approvalSnoozeInput
		if err := decodeJSON(r, &in); err != nil || in.Until.IsZero() {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "until is required")
			return
		}
		if err := d.Sessions.Snooze(r.Context(), actorFrom(r), id, in.Until); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
