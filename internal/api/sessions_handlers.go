package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/sessions"
)

// registerSessionsRoutes mounts session lifecycle routes. Visibility
// (owner, no-owner, or admin) is enforced by sessions.Service, not here.
func registerSessionsRoutes(r chi.Router, d *Deps) {
	r.Get("/sessions", handleSessionsList(d))
	r.Post("/sessions", handleSessionsCreate(d))
	r.Get("/sessions/{id}", handleSessionsGet(d))
	r.Post("/sessions/{id}/messages", handleSessionsMessage(d))
	r.Post("/sessions/{id}/interrupt", handleSessionsInterrupt(d))
	r.Post("/sessions/{id}/close", handleSessionsClose(d))
	r.Post("/sessions/{id}/model", handleSessionsModel(d))
	r.Get("/sessions/{id}/events", handleSessionsEvents(d))
}

type sessionDTO struct {
	ID          string  `json:"id"`
	OwnerID     *string `json:"owner_id"`
	Title       string  `json:"title"`
	WorkspaceID string  `json:"workspace_id"`
	ProfileID   string  `json:"profile_id"`
	Harness     string  `json:"harness"`
	State       string  `json:"state"`
	Origin      string  `json:"origin"`
	OriginRef   string  `json:"origin_ref"`
	Worktree    string  `json:"worktree"`
	Branch      string  `json:"branch"`
	BaseRef     string  `json:"base_ref"`
	// WorktreeShared reports whether this session's worktree started life as another
	// session's (created with worktree_path), rather than being created fresh for it.
	WorktreeShared bool      `json:"worktree_shared"`
	CreatedAt      time.Time `json:"created_at"`
	LastActiveAt   time.Time `json:"last_active_at"`
	NumTurns       int       `json:"num_turns"`
	CostUSD        float64   `json:"cost_usd"`
	TokensIn       int       `json:"tokens_in"`
	TokensOut      int       `json:"tokens_out"`
	NowLine        string    `json:"now_line"`
	Model          string    `json:"model"`
	Effort         string    `json:"effort"`
	// SlashCommands is what the CLI reported on its last init message, without the leading
	// slash; the composer's slash menu is built from it.
	SlashCommands []string `json:"slash_commands"`
	// DiffAdd and DiffDel are the worktree's line counts as of the last turn, for the
	// sessions list's diff badge.
	DiffAdd int `json:"diff_add"`
	DiffDel int `json:"diff_del"`
}

func sessionDTOFrom(s domain.Session) sessionDTO {
	return sessionDTO{
		ID: s.ID, OwnerID: s.OwnerID, Title: s.Title, WorkspaceID: s.WorkspaceID, ProfileID: s.ProfileID,
		Harness: s.Harness, State: string(s.State), Origin: string(s.Origin), OriginRef: s.OriginRef,
		Worktree: s.Worktree, Branch: s.Branch, BaseRef: s.BaseRef, WorktreeShared: s.WorktreeShared,
		CreatedAt: s.CreatedAt, LastActiveAt: s.LastActiveAt, NumTurns: s.NumTurns,
		CostUSD: s.CostUSD, TokensIn: s.TokensIn, TokensOut: s.TokensOut, NowLine: s.NowLine, Model: s.Model,
		Effort: s.Effort, SlashCommands: commandList(s.SlashCommands),
		DiffAdd: s.DiffAdd, DiffDel: s.DiffDel,
	}
}

type eventDTO struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"session_id"`
	Seq       int64           `json:"seq"`
	At        time.Time       `json:"at"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

func eventDTOFrom(e domain.Event) eventDTO {
	return eventDTO{ID: e.ID, SessionID: e.SessionID, Seq: e.Seq, At: e.At, Type: e.Type, Payload: e.Payload}
}

// handleSessionsList is GET /api/v1/sessions.
func handleSessionsList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Sessions.List(r.Context(), actorFrom(r))
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]sessionDTO, 0, len(list))
		for _, s := range list {
			out = append(out, sessionDTOFrom(s))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// commandList normalises a nil slash-command list to an empty array, so the JSON field is
// always an array and the frontend never has to guard against null.
func commandList(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

type sessionCreateInput struct {
	WorkspaceID string `json:"workspace_id"`
	ProfileID   string `json:"profile_id"`
	Title       string `json:"title"`
	Prompt      string `json:"prompt"`
	Model       string `json:"model"`
	Effort      string `json:"effort"`
	// WorktreePath starts the session on an existing worktree (another session's) instead of
	// a fresh one; the workspace must have worktrees enabled. See sessions.CreateInput.
	WorktreePath string `json:"worktree_path"`
}

// handleSessionsCreate is POST /api/v1/sessions: the session is owned by
// the requesting user, who must already have a verified Claude token.
func handleSessionsCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in sessionCreateInput
		if err := decodeJSON(r, &in); err != nil || in.WorkspaceID == "" || in.ProfileID == "" || in.Prompt == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "workspace_id, profile_id and prompt are required")
			return
		}
		actor := actorFrom(r)
		ownerID := actor.UserID
		sess, err := d.Sessions.Create(r.Context(), actor, sessions.CreateInput{
			WorkspaceID:  in.WorkspaceID,
			ProfileID:    in.ProfileID,
			Title:        in.Title,
			Prompt:       in.Prompt,
			Origin:       domain.OriginUI,
			Owner:        &ownerID,
			Model:        in.Model,
			Effort:       in.Effort,
			WorktreePath: in.WorktreePath,
		})
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, sessionDTOFrom(sess))
	}
}

// handleSessionsGet is GET /api/v1/sessions/{id}.
func handleSessionsGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		sess, err := d.Sessions.Get(r.Context(), actorFrom(r), id)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, sessionDTOFrom(sess))
	}
}

type sessionMessageInput struct {
	Text string `json:"text"`
}

// handleSessionsMessage is POST /api/v1/sessions/{id}/messages.
func handleSessionsMessage(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var in sessionMessageInput
		if err := decodeJSON(r, &in); err != nil || in.Text == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "text is required")
			return
		}
		if err := d.Sessions.Send(r.Context(), actorFrom(r), id, in.Text); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// handleSessionsInterrupt is POST /api/v1/sessions/{id}/interrupt.
func handleSessionsInterrupt(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := d.Sessions.Interrupt(r.Context(), actorFrom(r), id); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// handleSessionsClose is POST /api/v1/sessions/{id}/close.
func handleSessionsClose(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := d.Sessions.Close(r.Context(), actorFrom(r), id); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type sessionModelInput struct {
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

// handleSessionsModel is POST /api/v1/sessions/{id}/model: it restarts the session's process
// under the new model and effort, resuming the same session id.
func handleSessionsModel(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var in sessionModelInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		if err := d.Sessions.SwitchModel(r.Context(), actorFrom(r), id, in.Model, in.Effort); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// handleSessionsEvents is GET /api/v1/sessions/{id}/events?after=N&limit=500.
func handleSessionsEvents(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		limit := 500
		if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
			limit = v
		}
		list, err := d.Sessions.Events(r.Context(), actorFrom(r), id, after, limit)
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]eventDTO, 0, len(list))
		for _, e := range list {
			out = append(out, eventDTOFrom(e))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}
