package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/workspaces"
)

// registerWorkspacesRoutes mounts workspace routes. Visibility (owner,
// shared, or admin) and mutation rights are enforced by workspaces.Service,
// not here; only the "path" source's admin-only rule surfaces as a plain
// 403 from the service. The access routes (T64) are admin-only at the
// router level too, same as /users and /settings, since GetAccess/SetAccess
// answer 403 for a non-admin actor anyway — the explicit auth.RequireAdmin
// here just keeps that consistent with the rest of the admin surface and
// avoids a round trip through the service for an obviously unauthorized
// caller.
func registerWorkspacesRoutes(r chi.Router, d *Deps) {
	r.Get("/workspaces", handleWorkspacesList(d))
	r.Post("/workspaces", handleWorkspacesCreate(d))
	r.Get("/workspaces/{id}", handleWorkspacesGet(d))
	r.Patch("/workspaces/{id}", handleWorkspacesPatch(d))
	r.Delete("/workspaces/{id}", handleWorkspacesDelete(d))
	r.Post("/workspaces/{id}/retry", handleWorkspacesRetry(d))
	r.With(auth.RequireAdmin).Get("/workspaces/{id}/access", handleWorkspaceAccessGet(d))
	r.With(auth.RequireAdmin).Put("/workspaces/{id}/access", handleWorkspaceAccessPut(d))
}

type workspaceDTO struct {
	ID               string  `json:"id"`
	OwnerID          *string `json:"owner_id"`
	Name             string  `json:"name"`
	Path             string  `json:"path"`
	Source           string  `json:"source"`
	RepoURL          string  `json:"repo_url"`
	Branch           string  `json:"branch"`
	Managed          bool    `json:"managed"`
	State            string  `json:"state"`
	Error            string  `json:"error"`
	DefaultProfileID string  `json:"default_profile_id"`
	Worktrees        bool    `json:"worktrees"`
	BaseBranch       string  `json:"base_branch"`
	AutoCheckpoint   bool    `json:"auto_checkpoint"`
	// Access is only meaningful for a shared workspace (OwnerID nil):
	// "everyone" or "listed". See GET/PUT /workspaces/{id}/access for the
	// allowlist itself, which this DTO does not carry.
	Access    string    `json:"access"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func workspaceDTOFrom(ws domain.Workspace) workspaceDTO {
	return workspaceDTO{
		ID: ws.ID, OwnerID: ws.OwnerID, Name: ws.Name, Path: ws.Path,
		Source: string(ws.Source), RepoURL: ws.RepoURL, Branch: ws.Branch, Managed: ws.Managed,
		State: string(ws.State), Error: ws.Error, DefaultProfileID: ws.DefaultProfileID,
		Worktrees: ws.Worktrees, BaseBranch: ws.BaseBranch, AutoCheckpoint: ws.AutoCheckpoint,
		Access:    string(ws.Access),
		CreatedAt: ws.CreatedAt, UpdatedAt: ws.UpdatedAt,
	}
}

// handleWorkspacesList is GET /api/v1/workspaces: every workspace visible
// to the requesting user (owned, shared, or — for an admin — all).
func handleWorkspacesList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Workspaces.List(r.Context(), actorFrom(r))
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]workspaceDTO, 0, len(list))
		for _, ws := range list {
			out = append(out, workspaceDTOFrom(ws))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type workspaceCreateInput struct {
	Name             string `json:"name"`
	Source           string `json:"source"`
	RepoURL          string `json:"repo_url"`
	Branch           string `json:"branch"`
	Path             string `json:"path"`
	DefaultProfileID string `json:"default_profile_id"`
	Worktrees        bool   `json:"worktrees"`
	BaseBranch       string `json:"base_branch"`
	// AutoCheckpoint is a pointer so an omitted field keeps the default
	// (on) rather than turning checkpoints off.
	AutoCheckpoint *bool `json:"auto_checkpoint"`
}

// handleWorkspacesCreate is POST /api/v1/workspaces. A "git" source
// workspace is created in state "cloning" and cloned in the background; a
// "path" source workspace requires an admin actor.
func handleWorkspacesCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in workspaceCreateInput
		if err := decodeJSON(r, &in); err != nil || in.Name == "" || in.Source == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "name and source are required")
			return
		}
		ws, err := d.Workspaces.Create(r.Context(), actorFrom(r), workspaces.CreateInput{
			Name: in.Name, Source: in.Source, RepoURL: in.RepoURL, Branch: in.Branch,
			Path: in.Path, DefaultProfileID: in.DefaultProfileID, Worktrees: in.Worktrees,
			BaseBranch: in.BaseBranch, AutoCheckpoint: in.AutoCheckpoint,
		})
		if err != nil {
			writeWorkspaceError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, workspaceDTOFrom(ws))
	}
}

// handleWorkspacesGet is GET /api/v1/workspaces/{id}.
func handleWorkspacesGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		ws, err := d.Workspaces.Get(r.Context(), actorFrom(r), id)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, workspaceDTOFrom(ws))
	}
}

type workspacePatchInput struct {
	DefaultProfileID *string `json:"default_profile_id"`
	Worktrees        *bool   `json:"worktrees"`
	BaseBranch       *string `json:"base_branch"`
	AutoCheckpoint   *bool   `json:"auto_checkpoint"`
}

// handleWorkspacesPatch is PATCH /api/v1/workspaces/{id}: the owner or an
// admin may change the default profile and/or the worktrees flag.
func handleWorkspacesPatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var in workspacePatchInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		ws, err := d.Workspaces.Update(r.Context(), actorFrom(r), id, workspaces.UpdateInput{
			DefaultProfileID: in.DefaultProfileID, Worktrees: in.Worktrees,
			BaseBranch: in.BaseBranch, AutoCheckpoint: in.AutoCheckpoint,
		})
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, workspaceDTOFrom(ws))
	}
}

// handleWorkspacesDelete is DELETE /api/v1/workspaces/{id}: the owner or an
// admin may delete a workspace; 409 while any session on it is open,
// running or waiting.
func handleWorkspacesDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := d.Workspaces.Delete(r.Context(), actorFrom(r), id); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleWorkspacesRetry is POST /api/v1/workspaces/{id}/retry: re-runs a
// failed git clone.
func handleWorkspacesRetry(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		ws, err := d.Workspaces.Retry(r.Context(), actorFrom(r), id)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, workspaceDTOFrom(ws))
	}
}

type workspaceAccessUserDTO struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type workspaceAccessDTO struct {
	Access string                   `json:"access"`
	Users  []workspaceAccessUserDTO `json:"users"`
}

// handleWorkspaceAccessGet is GET /api/v1/workspaces/{id}/access
// (admin-only): a shared workspace's access mode plus, for "listed", the
// users on its allowlist.
func handleWorkspaceAccessGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		mode, userIDs, err := d.Workspaces.GetAccess(r.Context(), actorFrom(r), id)
		if err != nil {
			WriteError(w, err)
			return
		}
		users, err := d.Users.List(r.Context())
		if err != nil {
			WriteError(w, err)
			return
		}
		byID := make(map[string]domain.User, len(users))
		for _, u := range users {
			byID[u.ID] = u
		}
		out := workspaceAccessDTO{Access: string(mode), Users: make([]workspaceAccessUserDTO, 0, len(userIDs))}
		for _, uid := range userIDs {
			if u, ok := byID[uid]; ok {
				out.Users = append(out.Users, workspaceAccessUserDTO{ID: u.ID, DisplayName: u.DisplayName})
			}
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type workspaceAccessPutInput struct {
	Access  string   `json:"access"`
	UserIDs []string `json:"user_ids"`
}

// handleWorkspaceAccessPut is PUT /api/v1/workspaces/{id}/access
// (admin-only): replaces a shared workspace's access mode and, for
// "listed", its allowlist.
func handleWorkspaceAccessPut(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		var in workspaceAccessPutInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		mode := domain.WorkspaceAccessMode(in.Access)
		if mode != domain.WorkspaceAccessEveryone && mode != domain.WorkspaceAccessListed {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "access must be everyone or listed")
			return
		}
		if err := d.Workspaces.SetAccess(r.Context(), actorFrom(r), id, mode, in.UserIDs); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// writeWorkspaceError maps a workspaces.Service create error to the
// documented 422 error codes (invalid_url, invalid_path, name_taken) when
// it matches one of those specific sentinels, falling back to WriteError's
// generic domain-sentinel mapping (e.g. 403 for a member creating a "path"
// source workspace) otherwise.
func writeWorkspaceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspaces.ErrInvalidURL):
		writeErrorCode(w, http.StatusUnprocessableEntity, "invalid_url", err.Error())
	case errors.Is(err, workspaces.ErrInvalidPath):
		writeErrorCode(w, http.StatusUnprocessableEntity, "invalid_path", err.Error())
	case errors.Is(err, workspaces.ErrNameTaken):
		writeErrorCode(w, http.StatusUnprocessableEntity, "name_taken", err.Error())
	default:
		WriteError(w, err)
	}
}
