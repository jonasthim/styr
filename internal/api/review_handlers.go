package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/gitops"
	"github.com/jonasthim/styr/internal/sessions"
)

// registerReviewRoutes mounts the review surface of a session that runs in a
// git worktree: its diff, inline comments, checkpoints and the actions that
// publish or throw the work away. Visibility is enforced by
// sessions.Service; a session with no worktree answers 422.
func registerReviewRoutes(r chi.Router, d *Deps) {
	r.Get("/sessions/{id}/diff", handleSessionDiff(d))
	r.Get("/sessions/{id}/diff/file", handleSessionFileDiff(d))
	r.Get("/sessions/{id}/comments", handleReviewCommentsList(d))
	r.Post("/sessions/{id}/comments", handleReviewCommentCreate(d))
	r.Delete("/sessions/{id}/comments/{cid}", handleReviewCommentDelete(d))
	r.Post("/sessions/{id}/review", handleReviewSend(d))
	r.Post("/sessions/{id}/commit", handleSessionCommit(d))
	r.Post("/sessions/{id}/pr", handleSessionPR(d))
	r.Get("/sessions/{id}/checkpoints", handleCheckpointsList(d))
	r.Post("/sessions/{id}/rewind", handleSessionRewind(d))
	r.Post("/sessions/{id}/discard", handleSessionDiscard(d))
	r.Get("/sessions/{id}/patch", handleSessionPatch(d))
}

// writeReviewError maps the review service's two publishing sentinels to
// their documented 409 error codes, deferring to WriteError otherwise.
func writeReviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sessions.ErrGHUnavailable):
		writeErrorCode(w, http.StatusConflict, "gh_unavailable", err.Error())
	case errors.Is(err, sessions.ErrNoRemote):
		writeErrorCode(w, http.StatusConflict, "no_remote", err.Error())
	default:
		WriteError(w, err)
	}
}

type fileChangeDTO struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path"`
	Status  string `json:"status"`
	Add     int    `json:"add"`
	Del     int    `json:"del"`
	// Binary marks a file git has no line-by-line diff for.
	Binary bool `json:"binary"`
}

type reviewDiffDTO struct {
	BaseRef  string          `json:"base_ref"`
	Branch   string          `json:"branch"`
	Files    []fileChangeDTO `json:"files"`
	TotalAdd int             `json:"total_add"`
	TotalDel int             `json:"total_del"`
	Dirty    bool            `json:"dirty"`
}

func reviewDiffDTOFrom(d sessions.ReviewDiff) reviewDiffDTO {
	files := make([]fileChangeDTO, 0, len(d.Files))
	for _, f := range d.Files {
		files = append(files, fileChangeDTO{
			Path: f.Path, OldPath: f.OldPath, Status: string(f.Status), Add: f.Add, Del: f.Del,
			Binary: f.Binary,
		})
	}
	return reviewDiffDTO{
		BaseRef: d.BaseRef, Branch: d.Branch, Files: files,
		TotalAdd: d.TotalAdd, TotalDel: d.TotalDel, Dirty: d.Dirty,
	}
}

type diffLineDTO struct {
	Type  string `json:"type"`
	OldNo int    `json:"old_no"`
	NewNo int    `json:"new_no"`
	Text  string `json:"text"`
}

type diffHunkDTO struct {
	OldStart int           `json:"old_start"`
	OldLines int           `json:"old_lines"`
	NewStart int           `json:"new_start"`
	NewLines int           `json:"new_lines"`
	Lines    []diffLineDTO `json:"lines"`
}

type fileDiffDTO struct {
	Path      string        `json:"path"`
	OldPath   string        `json:"old_path"`
	Status    string        `json:"status"`
	Binary    bool          `json:"binary"`
	Truncated bool          `json:"truncated"`
	Hunks     []diffHunkDTO `json:"hunks"`
}

func fileDiffDTOFrom(fd gitops.FileDiff) fileDiffDTO {
	hunks := make([]diffHunkDTO, 0, len(fd.Hunks))
	for _, h := range fd.Hunks {
		lines := make([]diffLineDTO, 0, len(h.Lines))
		for _, l := range h.Lines {
			lines = append(lines, diffLineDTO{Type: string(l.Type), OldNo: l.OldNo, NewNo: l.NewNo, Text: l.Text})
		}
		hunks = append(hunks, diffHunkDTO{
			OldStart: h.OldStart, OldLines: h.OldLines, NewStart: h.NewStart, NewLines: h.NewLines, Lines: lines,
		})
	}
	return fileDiffDTO{
		Path: fd.Path, OldPath: fd.OldPath, Status: string(fd.Status),
		Binary: fd.Binary, Truncated: fd.Truncated, Hunks: hunks,
	}
}

type reviewCommentDTO struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"session_id"`
	Path       string     `json:"path"`
	Line       int        `json:"line"`
	Side       string     `json:"side"`
	Body       string     `json:"body"`
	AuthorID   string     `json:"author_id"`
	AuthorName string     `json:"author_name"`
	CreatedAt  time.Time  `json:"created_at"`
	SentAt     *time.Time `json:"sent_at"`
}

func reviewCommentDTOFrom(c domain.ReviewComment) reviewCommentDTO {
	return reviewCommentDTO{
		ID: c.ID, SessionID: c.SessionID, Path: c.Path, Line: c.Line, Side: string(c.Side),
		Body: c.Body, AuthorID: c.AuthorID, AuthorName: c.AuthorName, CreatedAt: c.CreatedAt, SentAt: c.SentAt,
	}
}

type checkpointDTO struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	CommitSHA string    `json:"commit_sha"`
	Turn      int       `json:"turn"`
	Summary   string    `json:"summary"`
	CreatedAt time.Time `json:"created_at"`
}

func checkpointDTOFrom(c domain.Checkpoint) checkpointDTO {
	return checkpointDTO{
		ID: c.ID, SessionID: c.SessionID, CommitSHA: c.CommitSHA,
		Turn: c.Turn, Summary: c.Summary, CreatedAt: c.CreatedAt,
	}
}

// handleSessionDiff is GET /api/v1/sessions/{id}/diff.
func handleSessionDiff(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		diff, err := d.Sessions.Diff(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, reviewDiffDTOFrom(diff))
	}
}

// handleSessionFileDiff is GET /api/v1/sessions/{id}/diff/file?path=.
func handleSessionFileDiff(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "path is required")
			return
		}
		fd, err := d.Sessions.FileDiff(r.Context(), actorFrom(r), chi.URLParam(r, "id"), path)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, fileDiffDTOFrom(fd))
	}
}

// handleSessionPatch is GET /api/v1/sessions/{id}/patch: the whole diff as a
// downloadable patch.
func handleSessionPatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		patch, err := d.Sessions.Patch(r.Context(), actorFrom(r), id)
		if err != nil {
			WriteError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/x-diff; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+id+`.diff"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(patch)
	}
}
