package api

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/domain"
)

// registerWorkspacesRoutes mounts workspace listing (any signed-in user)
// and management (admin only).
func registerWorkspacesRoutes(r chi.Router, d *Deps) {
	r.Get("/workspaces", handleWorkspacesList(d))
	r.With(auth.RequireAdmin).Post("/workspaces", handleWorkspacesCreate(d))
	r.With(auth.RequireAdmin).Patch("/workspaces/{id}", handleWorkspacesPatch(d))
	r.With(auth.RequireAdmin).Delete("/workspaces/{id}", handleWorkspacesDelete(d))
}

type workspaceDTO struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Path             string `json:"path"`
	DefaultProfileID string `json:"default_profile_id"`
	Worktrees        bool   `json:"worktrees"`
}

func workspaceDTOFrom(ws domain.Workspace) workspaceDTO {
	return workspaceDTO{ID: ws.ID, Name: ws.Name, Path: ws.Path, DefaultProfileID: ws.DefaultProfileID, Worktrees: ws.Worktrees}
}

// handleWorkspacesList is GET /api/v1/workspaces.
func handleWorkspacesList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Workspaces.List(r.Context())
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

type workspaceInput struct {
	Name             string `json:"name"`
	Path             string `json:"path"`
	DefaultProfileID string `json:"default_profile_id"`
	Worktrees        bool   `json:"worktrees"`
}

// validateWorkspacePath enforces the workspace path rules: it must exist,
// be a directory, and contain a .git entry (directory or file, as in a git
// worktree checkout).
func validateWorkspacePath(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
		return false
	}
	return true
}

// handleWorkspacesCreate is POST /api/v1/workspaces.
func handleWorkspacesCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in workspaceInput
		if err := decodeJSON(r, &in); err != nil || in.Name == "" || in.Path == "" || in.DefaultProfileID == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "name, path and default_profile_id are required")
			return
		}
		if !validateWorkspacePath(in.Path) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid_path", "path must be an existing directory containing a git repository")
			return
		}
		ws := domain.Workspace{
			ID: uuid.NewString(), Name: in.Name, Path: in.Path,
			DefaultProfileID: in.DefaultProfileID, Worktrees: in.Worktrees, CreatedAt: time.Now(),
		}
		if err := d.Workspaces.Create(r.Context(), ws); err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, workspaceDTOFrom(ws))
	}
}

// handleWorkspacesPatch is PATCH /api/v1/workspaces/{id}.
func handleWorkspacesPatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		ws, err := d.Workspaces.Get(r.Context(), id)
		if err != nil {
			WriteError(w, err)
			return
		}
		var in workspaceInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		if in.Name != "" {
			ws.Name = in.Name
		}
		if in.Path != "" && in.Path != ws.Path {
			if !validateWorkspacePath(in.Path) {
				writeErrorCode(w, http.StatusUnprocessableEntity, "invalid_path", "path must be an existing directory containing a git repository")
				return
			}
			ws.Path = in.Path
		}
		if in.DefaultProfileID != "" {
			ws.DefaultProfileID = in.DefaultProfileID
		}
		ws.Worktrees = in.Worktrees
		if err := d.Workspaces.Update(r.Context(), *ws); err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, workspaceDTOFrom(*ws))
	}
}

// handleWorkspacesDelete is DELETE /api/v1/workspaces/{id}.
func handleWorkspacesDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if err := d.Workspaces.Delete(r.Context(), id); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
