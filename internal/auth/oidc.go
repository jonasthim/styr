package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/domain"
)

// defaultScopes mirrors config.defaultOIDCScopes; config.Load already fills
// empty Scopes in, but tests construct config.OIDCProvider values directly,
// so runtimeFor applies the same default defensively.
var defaultScopes = []string{"openid", "profile", "email"}

// providerRuntime is what runtimeFor lazily builds and caches per provider
// slug once OIDC discovery has completed.
type providerRuntime struct {
	cfg          config.OIDCProvider
	oauth2Config oauth2.Config
	verifier     *oidc.IDTokenVerifier
}

// runtimeFor lazily discovers (oidc.NewProvider) and caches the OIDC
// runtime for slug. See the New doc comment for why discovery happens here
// rather than eagerly in New.
func (s *Service) runtimeFor(ctx context.Context, slug string) (*providerRuntime, error) {
	s.mu.Lock()
	if rt, ok := s.runtimes[slug]; ok {
		s.mu.Unlock()
		return rt, nil
	}
	s.mu.Unlock()

	cfg, ok := s.providerConfig(slug)
	if !ok {
		return nil, fmt.Errorf("auth: unknown provider %q", slug)
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover oidc provider %q: %w", cfg.Name, err)
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = defaultScopes
	}
	rt := &providerRuntime{
		cfg:      cfg,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth2Config: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  s.baseURL + callbackPath,
			Endpoint:     provider.Endpoint(),
			Scopes:       scopes,
		},
	}

	s.mu.Lock()
	// Another goroutine may have finished discovery first; keep whichever
	// runtime landed first so every caller shares one oauth2 config.
	if existing, ok := s.runtimes[slug]; ok {
		s.mu.Unlock()
		return existing, nil
	}
	s.runtimes[slug] = rt
	s.mu.Unlock()
	return rt, nil
}

// BeginLogin starts the PKCE authorization code flow for the provider named
// by slug: it sets the short-lived styr_pkce cookie (state, verifier, nonce,
// slug) and returns the provider's authorization URL, which the caller
// (the HTTP handler) redirects the browser to.
func (s *Service) BeginLogin(w http.ResponseWriter, r *http.Request, slug string) (redirectURL string, err error) {
	rt, err := s.runtimeFor(r.Context(), slug)
	if err != nil {
		return "", err
	}
	state, err := randToken(32)
	if err != nil {
		return "", err
	}
	nonce, err := randToken(32)
	if err != nil {
		return "", err
	}
	verifier := oauth2.GenerateVerifier()

	if err := setPKCECookie(w, s.secure, pkceState{State: state, Verifier: verifier, Nonce: nonce, Slug: slug}); err != nil {
		return "", err
	}

	return rt.oauth2Config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// idClaims is the subset of OIDC claims Styr uses: sub comes from
// IDToken.Subject instead, and is not repeated here.
type idClaims struct {
	Email             string `json:"email"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Picture           string `json:"picture"`
}

// resolveName applies the fallback chain from the task card: name, then
// preferred_username, then email.
func (c idClaims) resolveName() string {
	if c.Name != "" {
		return c.Name
	}
	if c.PreferredUsername != "" {
		return c.PreferredUsername
	}
	return c.Email
}

// CompleteLogin finishes the flow at GET /api/v1/auth/callback: it verifies
// the styr_pkce cookie's state against the callback's state query
// parameter, exchanges the authorization code (with the PKCE verifier),
// verifies the ID token (signature, issuer, audience) and its nonce,
// upserts the local user (see upsertUser) and sets the styr_session cookie.
func (s *Service) CompleteLogin(w http.ResponseWriter, r *http.Request) (domain.User, error) {
	ctx := r.Context()
	st, err := readPKCECookie(r)
	if err != nil {
		return domain.User{}, fmt.Errorf("read pkce cookie: %w", err)
	}
	clearPKCECookie(w, s.secure)

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		return domain.User{}, fmt.Errorf("oidc provider returned error: %s", errParam)
	}
	state := r.URL.Query().Get("state")
	if state == "" || state != st.State {
		return domain.User{}, errors.New("oidc: state mismatch")
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		return domain.User{}, errors.New("oidc: callback is missing the code parameter")
	}

	rt, err := s.runtimeFor(ctx, st.Slug)
	if err != nil {
		return domain.User{}, err
	}

	tok, err := rt.oauth2Config.Exchange(ctx, code, oauth2.VerifierOption(st.Verifier))
	if err != nil {
		return domain.User{}, fmt.Errorf("exchange oidc code: %w", err)
	}
	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return domain.User{}, errors.New("oidc: token response has no id_token")
	}
	idToken, err := rt.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return domain.User{}, fmt.Errorf("verify id_token: %w", err)
	}
	if idToken.Nonce != st.Nonce {
		return domain.User{}, errors.New("oidc: nonce mismatch")
	}

	var claims idClaims
	if err := idToken.Claims(&claims); err != nil {
		return domain.User{}, fmt.Errorf("decode id_token claims: %w", err)
	}
	claims.Name = claims.resolveName()

	usr, err := s.upsertUser(ctx, rt.cfg.Issuer, idToken.Subject, claims)
	if err != nil {
		return domain.User{}, err
	}

	if _, err := s.createSession(ctx, w, usr.ID, r); err != nil {
		return domain.User{}, fmt.Errorf("create session: %w", err)
	}
	return usr, nil
}

// upsertUser maps a verified (issuer, subject) to a local user: the first
// user ever created becomes admin, every user after that is a member.
//
// For an existing user, this refreshes email/display_name/avatar_url via
// db.Users.UpdateProfile whenever the identity provider's claims disagree
// with the stored row, then always calls db.Users.TouchLogin.
func (s *Service) upsertUser(ctx context.Context, issuer, subject string, claims idClaims) (domain.User, error) {
	existing, err := s.users.GetBySubject(ctx, issuer, subject)
	if err == nil {
		if existing.Email != claims.Email || existing.DisplayName != claims.Name || existing.AvatarURL != claims.Picture {
			if err := s.users.UpdateProfile(ctx, existing.ID, claims.Email, claims.Name, claims.Picture); err != nil {
				return domain.User{}, fmt.Errorf("update profile: %w", err)
			}
			existing.Email = claims.Email
			existing.DisplayName = claims.Name
			existing.AvatarURL = claims.Picture
		}
		if err := s.users.TouchLogin(ctx, existing.ID); err != nil {
			return domain.User{}, fmt.Errorf("touch login: %w", err)
		}
		return *existing, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.User{}, fmt.Errorf("look up user: %w", err)
	}

	// firstUserMu serializes count-then-create: without it, two concurrent
	// first logins (distinct subjects, both seeing count == 0) could both
	// decide to become admin.
	s.firstUserMu.Lock()
	defer s.firstUserMu.Unlock()

	role := domain.RoleMember
	count, err := s.users.Count(ctx)
	if err != nil {
		return domain.User{}, fmt.Errorf("count users: %w", err)
	}
	if count == 0 {
		role = domain.RoleAdmin
	}

	now := time.Now()
	usr := domain.User{
		ID: uuid.NewString(), Issuer: issuer, Subject: subject,
		Email: claims.Email, DisplayName: claims.Name, AvatarURL: claims.Picture,
		Role: role, CreatedAt: now, LastLoginAt: now,
	}
	if err := s.users.Create(ctx, usr); err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	return usr, nil
}
