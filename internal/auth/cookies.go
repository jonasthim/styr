package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Cookie names and lifetimes. See the Task 12 card in
// docs/superpowers/plans/2026-09-18-styr-v0.1.md for the exact contract.
const (
	sessionCookieName = "styr_session"
	pkceCookieName    = "styr_pkce"

	pkceCookieTTL    = 10 * time.Minute
	sessionCookieTTL = 30 * 24 * time.Hour
	touchInterval    = 5 * time.Minute

	callbackPath   = "/api/v1/auth/callback"
	authPathPrefix = "/api/v1/auth"
)

// pkceState is the JSON payload of the short-lived styr_pkce cookie set by
// BeginLogin and consumed by CompleteLogin. It is not signed: it only holds
// the PKCE code verifier, the anti-CSRF state, the OIDC nonce and the
// provider slug, none of which are useful to anyone but the browser that
// received the cookie and is about to complete the same login.
type pkceState struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
	Slug     string `json:"slug"`
}

// randToken returns n cryptographically random bytes, base64url-encoded
// (unpadded).
func randToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken returns the hex-encoded sha256 of a session cookie's raw value,
// the form stored at rest (db.LoginSessions never sees the raw token).
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// setPKCECookie sets the short-lived styr_pkce cookie holding st.
func setPKCECookie(w http.ResponseWriter, secure bool, st pkceState) error {
	payload, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("encode pkce cookie: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     pkceCookieName,
		Value:    base64.RawURLEncoding.EncodeToString(payload),
		Path:     authPathPrefix,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(pkceCookieTTL),
		MaxAge:   int(pkceCookieTTL.Seconds()),
	})
	return nil
}

// readPKCECookie decodes the styr_pkce cookie carried by r.
func readPKCECookie(r *http.Request) (pkceState, error) {
	c, err := r.Cookie(pkceCookieName)
	if err != nil || c.Value == "" {
		return pkceState{}, fmt.Errorf("missing %s cookie", pkceCookieName)
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		return pkceState{}, fmt.Errorf("decode pkce cookie: %w", err)
	}
	var st pkceState
	if err := json.Unmarshal(raw, &st); err != nil {
		return pkceState{}, fmt.Errorf("parse pkce cookie: %w", err)
	}
	return st, nil
}

// clearPKCECookie deletes the styr_pkce cookie once BeginLogin's flow has
// finished (successfully or not).
func clearPKCECookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: pkceCookieName, Value: "", Path: authPathPrefix,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(0, 0),
	})
}

// setSessionCookie sets the styr_session cookie to raw, the random token
// whose sha256 hash is stored in login_sessions.
func setSessionCookie(w http.ResponseWriter, secure bool, raw string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
	})
}

// clearSessionCookie deletes the styr_session cookie (Logout).
func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode,
		MaxAge: -1, Expires: time.Unix(0, 0),
	})
}
