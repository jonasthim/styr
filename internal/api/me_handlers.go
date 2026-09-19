package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/domain"
)

// registerMeRoutes mounts the current user's own profile and Claude token.
func registerMeRoutes(r chi.Router, d *Deps) {
	r.Get("/me", handleMeGet(d))
	r.Patch("/me", handleMePatch(d))
	r.Put("/me/claude-token", handleMeTokenPut(d))
	r.Delete("/me/claude-token", handleMeTokenDelete(d))
	r.Put("/me/codex-key", handleMeCodexKeyPut(d))
	r.Delete("/me/codex-key", handleMeCodexKeyDelete(d))
}

// codexKeyLabelSuffix is how many trailing characters of an OpenAI API key
// the label keeps. Four, not the six a Claude token's label keeps: an API
// key has far less structure in front of its random tail, so fewer
// characters are shown.
const codexKeyLabelSuffix = 4

// codexKeyLabel is the only thing derived from a key that is ever stored or
// returned: an ellipsis and its last few characters.
func codexKeyLabel(key string) string {
	if len(key) <= codexKeyLabelSuffix {
		return "…" + key
	}
	return "…" + key[len(key)-codexKeyLabelSuffix:]
}

type claudeTokenDTO struct {
	Present    bool       `json:"present"`
	Label      string     `json:"label"`
	VerifiedAt *time.Time `json:"verified_at"`
}

// codexKeyDTO is the per-user (and service-wide) Codex credential summary.
// Unlike a Claude token there is no verified_at: a key is verified before it
// is stored and never re-verified afterwards, so "present" already implies
// "verified once".
type codexKeyDTO struct {
	Present bool   `json:"present"`
	Label   string `json:"label"`
}

type meDTO struct {
	ID          string          `json:"id"`
	Email       string          `json:"email"`
	DisplayName string          `json:"display_name"`
	AvatarURL   string          `json:"avatar_url"`
	Role        string          `json:"role"`
	Prefs       json.RawMessage `json:"prefs"`
	ClaudeToken claudeTokenDTO  `json:"claude_token"`
	CodexKey    codexKeyDTO     `json:"codex_key"`
}

// claudeTokenFor loads the claude-token summary for userID, never
// returning the token itself.
func claudeTokenFor(d *Deps, r *http.Request, userID string) (claudeTokenDTO, error) {
	_, _, label, verifiedAt, err := d.Tokens.Get(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return claudeTokenDTO{}, nil
		}
		return claudeTokenDTO{}, err
	}
	return claudeTokenDTO{Present: true, Label: label, VerifiedAt: verifiedAt}, nil
}

// codexKeyFor loads the codex-key summary for userID, never returning the
// key itself. A build with no Codex credential store simply reports absent.
func codexKeyFor(d *Deps, r *http.Request, userID string) (codexKeyDTO, error) {
	if d.CodexCreds == nil {
		return codexKeyDTO{}, nil
	}
	_, _, label, _, err := d.CodexCreds.Get(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return codexKeyDTO{}, nil
		}
		return codexKeyDTO{}, err
	}
	return codexKeyDTO{Present: true, Label: label}, nil
}

func meDTOFor(d *Deps, r *http.Request, u domain.User) (meDTO, error) {
	tok, err := claudeTokenFor(d, r, u.ID)
	if err != nil {
		return meDTO{}, err
	}
	key, err := codexKeyFor(d, r, u.ID)
	if err != nil {
		return meDTO{}, err
	}
	return meDTO{
		ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, AvatarURL: u.AvatarURL,
		Role: string(u.Role), Prefs: u.Prefs, ClaudeToken: tok, CodexKey: key,
	}, nil
}

// handleMeGet is GET /api/v1/me.
func handleMeGet(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := principal(r)
		dto, err := meDTOFor(d, r, p.User)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, dto)
	}
}

type mePatchInput struct {
	Prefs json.RawMessage `json:"prefs"`
}

// handleMePatch is PATCH /api/v1/me: currently only prefs are editable.
func handleMePatch(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := principal(r)
		var in mePatchInput
		if err := decodeJSON(r, &in); err != nil {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "invalid request body")
			return
		}
		if len(in.Prefs) > 0 {
			if err := d.Users.UpdatePrefs(r.Context(), p.User.ID, in.Prefs); err != nil {
				WriteError(w, err)
				return
			}
			p.User.Prefs = in.Prefs
		}
		dto, err := meDTOFor(d, r, p.User)
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, dto)
	}
}

type tokenPutInput struct {
	Token string `json:"token"`
}

// handleMeTokenPut is PUT /api/v1/me/claude-token: verifies the token
// before storing anything, and never echoes it back.
func handleMeTokenPut(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := principal(r)
		var in tokenPutInput
		if err := decodeJSON(r, &in); err != nil || in.Token == "" {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "token is required")
			return
		}
		if err := d.Verifier.Verify(r.Context(), in.Token); err != nil {
			slog.Warn("claude token verification failed", "scope", "user", "err", err)
			writeErrorCode(w, http.StatusUnprocessableEntity, "token_invalid", "the token could not be verified: "+strings.TrimPrefix(err.Error(), "claude: verify: "))
			return
		}
		ciphertext, nonce, err := d.Box.Seal([]byte(in.Token))
		if err != nil {
			WriteError(w, err)
			return
		}
		label := "…" + crypto.Suffix(in.Token)
		if err := d.Tokens.Set(r.Context(), p.User.ID, ciphertext, nonce, label); err != nil {
			WriteError(w, err)
			return
		}
		if err := d.Tokens.MarkVerified(r.Context(), p.User.ID); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleMeTokenDelete is DELETE /api/v1/me/claude-token. Deleting an
// already-absent token is not an error.
func handleMeTokenDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := principal(r)
		if err := d.Tokens.Delete(r.Context(), p.User.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type codexKeyPutInput struct {
	Key string `json:"key"`
}

// verifyAndSealCodexKey is the shared body of PUT /me/codex-key and PUT
// /settings/codex-key: it verifies the key by running one read-only Codex
// turn, then seals it. Nothing is written when verification fails, and the
// key itself is never logged or echoed — only the error the CLI reported,
// with the key already redacted out of it by the verifier.
func verifyAndSealCodexKey(d *Deps, w http.ResponseWriter, r *http.Request, scope string) (ciphertext, nonce []byte, label, key string, ok bool) {
	if d.CodexCreds == nil || d.CodexVerifier == nil {
		writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "the Codex harness is not configured on this server")
		return nil, nil, "", "", false
	}
	var in codexKeyPutInput
	if err := decodeJSON(r, &in); err != nil || in.Key == "" {
		writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "key is required")
		return nil, nil, "", "", false
	}
	if err := d.CodexVerifier.Verify(r.Context(), in.Key); err != nil {
		slog.Warn("codex key verification failed", "scope", scope, "err", err)
		writeErrorCode(w, http.StatusUnprocessableEntity, "key_invalid", "the key could not be verified: "+strings.TrimPrefix(err.Error(), "codex: verify: "))
		return nil, nil, "", "", false
	}
	ciphertext, nonce, err := d.Box.Seal([]byte(in.Key))
	if err != nil {
		WriteError(w, err)
		return nil, nil, "", "", false
	}
	return ciphertext, nonce, codexKeyLabel(in.Key), in.Key, true
}

// handleMeCodexKeyPut is PUT /api/v1/me/codex-key: the OpenAI API key the
// user's Codex sessions run with. Verified before anything is stored, and
// never echoed back.
func handleMeCodexKeyPut(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := principal(r)
		ciphertext, nonce, label, _, ok := verifyAndSealCodexKey(d, w, r, "user")
		if !ok {
			return
		}
		if err := d.CodexCreds.Set(r.Context(), p.User.ID, ciphertext, nonce, label); err != nil {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleMeCodexKeyDelete is DELETE /api/v1/me/codex-key. Deleting an
// already-absent key is not an error.
func handleMeCodexKeyDelete(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if d.CodexCreds == nil {
			writeErrorCode(w, http.StatusNotImplemented, "not_implemented", "the Codex harness is not configured on this server")
			return
		}
		p, _ := principal(r)
		if err := d.CodexCreds.Delete(r.Context(), p.User.ID); err != nil && !errors.Is(err, domain.ErrNotFound) {
			WriteError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
