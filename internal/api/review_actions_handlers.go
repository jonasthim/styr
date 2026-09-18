package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/sessions"
)

// handleReviewCommentsList is GET /api/v1/sessions/{id}/comments.
func handleReviewCommentsList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Sessions.Comments(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]reviewCommentDTO, 0, len(list))
		for _, c := range list {
			out = append(out, reviewCommentDTOFrom(c))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type reviewCommentInput struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Side string `json:"side"`
	Body string `json:"body"`
}

// handleReviewCommentCreate is POST /api/v1/sessions/{id}/comments.
func handleReviewCommentCreate(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in reviewCommentInput
		if err := decodeJSON(r, &in); err != nil || in.Path == "" || in.Body == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "path and body are required")
			return
		}
		c, err := d.Sessions.AddComment(r.Context(), actorFrom(r), chi.URLParam(r, "id"), sessions.CommentInput{
			Path: in.Path, Line: in.Line, Side: domain.ReviewSide(in.Side), Body: in.Body,
		})
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, reviewCommentDTOFrom(c))
	}
}

// handleReviewCommentDelete is DELETE /api/v1/sessions/{id}/comments/{cid}.
func handleReviewCommentDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := d.Sessions.DeleteComment(r.Context(), actorFrom(r), chi.URLParam(r, "id"), chi.URLParam(r, "cid"))
		if err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleReviewSend is POST /api/v1/sessions/{id}/review: every unsent
// comment goes to the session as one user message.
func handleReviewSend(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Sessions.SendReview(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

type sessionCommitInput struct {
	Message string `json:"message"`
}

// handleSessionCommit is POST /api/v1/sessions/{id}/commit.
func handleSessionCommit(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in sessionCommitInput
		if err := decodeJSON(r, &in); err != nil || in.Message == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "message is required")
			return
		}
		sha, err := d.Sessions.Commit(r.Context(), actorFrom(r), chi.URLParam(r, "id"), in.Message)
		if err != nil {
			writeReviewError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"sha": sha})
	}
}

type sessionPRInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Base  string `json:"base"`
}

// handleSessionPR is POST /api/v1/sessions/{id}/pr: push the branch and open
// a pull request. 409 with code gh_unavailable or no_remote when the
// environment cannot publish it.
func handleSessionPR(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in sessionPRInput
		if err := decodeJSON(r, &in); err != nil || in.Title == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "title is required")
			return
		}
		url, err := d.Sessions.CreatePR(r.Context(), actorFrom(r), chi.URLParam(r, "id"), in.Title, in.Body, in.Base)
		if err != nil {
			writeReviewError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"url": url})
	}
}

// handleCheckpointsList is GET /api/v1/sessions/{id}/checkpoints.
func handleCheckpointsList(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := d.Sessions.Checkpoints(r.Context(), actorFrom(r), chi.URLParam(r, "id"))
		if err != nil {
			WriteError(w, err)
			return
		}
		out := make([]checkpointDTO, 0, len(list))
		for _, c := range list {
			out = append(out, checkpointDTOFrom(c))
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

type sessionRewindInput struct {
	CheckpointID string `json:"checkpoint_id"`
}

// handleSessionRewind is POST /api/v1/sessions/{id}/rewind.
func handleSessionRewind(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in sessionRewindInput
		if err := decodeJSON(r, &in); err != nil || in.CheckpointID == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "checkpoint_id is required")
			return
		}
		if err := d.Sessions.Rewind(r.Context(), actorFrom(r), chi.URLParam(r, "id"), in.CheckpointID); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}

// handleSessionDiscard is POST /api/v1/sessions/{id}/discard: the worktree
// and its branch are removed and the session is closed.
func handleSessionDiscard(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := d.Sessions.Discard(r.Context(), actorFrom(r), chi.URLParam(r, "id")); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
