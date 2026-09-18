package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

// APITokenPrefix is prepended to every generated personal API token's raw
// secret, so a bearer credential is recognisable at a glance (and so
// principalFromBearer can cheaply reject anything that is obviously not one
// before hitting the database).
const APITokenPrefix = "styr_pat_"

// apiTokenPrefixLen is how many characters of the raw token (including
// APITokenPrefix) are kept as the row's Prefix, shown in the token list so
// an owner can recognise which token is which without ever seeing the
// secret again.
const apiTokenPrefixLen = 12

// APITokenStore is the subset of the api_tokens repository (T31's
// db.APITokens) Authenticate needs to resolve a bearer token. Defined here
// rather than imported so this package does not depend on internal/db's
// concrete repository type.
type APITokenStore interface {
	GetByHash(ctx context.Context, hash string) (*domain.APIToken, error)
	TouchUsed(ctx context.Context, id string, at time.Time) error
}

// GenerateAPIToken creates a new personal API token: raw is the secret
// shown to its owner once (APITokenPrefix + base64url(32 random bytes)),
// hash is its sha256 hex digest (the only form stored at rest, via
// db.APITokens.Create), and prefix is the first apiTokenPrefixLen
// characters of raw, kept alongside hash so the owner can recognise a
// token in the list without its secret ever being retrievable again.
func GenerateAPIToken() (raw, hash, prefix string, err error) {
	secret, err := randToken(32)
	if err != nil {
		return "", "", "", fmt.Errorf("generate api token: %w", err)
	}
	raw = APITokenPrefix + secret
	hash = hashToken(raw)
	prefix = raw
	if len(prefix) > apiTokenPrefixLen {
		prefix = prefix[:apiTokenPrefixLen]
	}
	return raw, hash, prefix, nil
}

// WithAPITokens wires the api_tokens repository into the service so
// Authenticate accepts a personal API token's Authorization: Bearer header
// (see principalFromBearer below). It returns s for chaining after New; the
// zero Service (no WithAPITokens call) simply never matches a bearer
// token, so a composition root can wire this up optionally, once its
// api_tokens repository exists.
func (s *Service) WithAPITokens(store APITokenStore) *Service {
	s.apiTokens = store
	return s
}

// principalFromBearer resolves a personal API token carried by r's
// Authorization: Bearer header, if any, into a Principal with TokenAuth
// set. It never errors outward: an absent apiTokens store, a missing or
// malformed header, an unknown hash (including one already deleted by its
// owner), or an expired token all just report ok=false, exactly like a
// missing or invalid session cookie — Authenticate (service.go) treats
// every case the same way and lets RequireUser answer 401 once a principal
// is actually required.
//
// TouchUsed is called on every successful match rather than throttled: the
// card that introduced this (T36) keeps it simple deliberately, unlike the
// styr_session cookie's touchInterval-gated Touch.
func (s *Service) principalFromBearer(ctx context.Context, r *http.Request) (*Principal, bool) {
	if s.apiTokens == nil {
		return nil, false
	}
	authz := r.Header.Get("Authorization")
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authz, bearerPrefix) {
		return nil, false
	}
	raw := strings.TrimPrefix(authz, bearerPrefix)
	if !strings.HasPrefix(raw, APITokenPrefix) {
		return nil, false
	}
	tok, err := s.apiTokens.GetByHash(ctx, hashToken(raw))
	if err != nil {
		return nil, false
	}
	if tok.ExpiresAt != nil && time.Now().After(*tok.ExpiresAt) {
		return nil, false
	}
	usr, err := s.users.GetByID(ctx, tok.UserID)
	if err != nil {
		return nil, false
	}
	_ = s.apiTokens.TouchUsed(ctx, tok.ID, time.Now())
	return &Principal{User: *usr, TokenAuth: true}, true
}
