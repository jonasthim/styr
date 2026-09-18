package auth

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/config"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
)

// fakeOIDC is an in-process OpenID Connect provider: discovery document,
// JWKS and token endpoints backed by a freshly generated RSA key, used to
// exercise Service.BeginLogin/CompleteLogin against a real HTTP round trip
// without any network access outside httptest.
type fakeOIDC struct {
	t        *testing.T
	key      *rsa.PrivateKey
	kid      string
	clientID string
	srv      *httptest.Server

	mu      sync.Mutex
	nonce   string
	subject string
	email   string
	name    string
	picture string
}

func newFakeOIDC(t *testing.T, clientID string) *fakeOIDC {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	f := &fakeOIDC{t: t, key: key, kid: "test-kid", clientID: clientID}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", f.discovery)
	mux.HandleFunc("/keys", f.keys)
	mux.HandleFunc("/token", f.token)
	mux.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOIDC) issuer() string { return f.srv.URL }

// setUser configures the claims the next /token response embeds in its
// id_token. nonce should be read back from the "nonce" query parameter of
// the redirect URL BeginLogin returned, matching what the real browser
// round trip would preserve.
func (f *fakeOIDC) setUser(nonce, subject, email, name, picture string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nonce, f.subject, f.email, f.name, f.picture = nonce, subject, email, name, picture
}

func (f *fakeOIDC) discovery(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                f.srv.URL,
		"authorization_endpoint":                f.srv.URL + "/authorize",
		"token_endpoint":                        f.srv.URL + "/token",
		"jwks_uri":                              f.srv.URL + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_post", "client_secret_basic"},
		"claims_supported":                      []string{"sub", "email", "name", "preferred_username", "picture"},
	})
}

func (f *fakeOIDC) keys(w http.ResponseWriter, _ *http.Request) {
	pub := f.key.PublicKey
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"use": "sig",
			"kid": f.kid,
			"alg": "RS256",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}},
	})
}

func (f *fakeOIDC) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	claims := map[string]any{
		"iss": f.srv.URL, "aud": f.clientID, "sub": f.subject,
		"email": f.email, "name": f.name, "picture": f.picture,
		"nonce": f.nonce,
		"iat":   time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	f.mu.Unlock()

	idToken := f.mintIDToken(claims)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "test-access-token",
		"token_type":   "Bearer",
		"id_token":     idToken,
		"expires_in":   3600,
	})
}

// mintIDToken hand-rolls an RS256 JWT: no JWT/JOSE library is in go.mod, and
// this is the only place that needs one.
func (f *fakeOIDC) mintIDToken(claims map[string]any) string {
	header := map[string]string{"alg": "RS256", "typ": "JWT", "kid": f.kid}
	hb, err := json.Marshal(header)
	if err != nil {
		f.t.Fatalf("marshal jwt header: %v", err)
	}
	cb, err := json.Marshal(claims)
	if err != nil {
		f.t.Fatalf("marshal jwt claims: %v", err)
	}
	signingInput := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if err != nil {
		f.t.Fatalf("sign id_token: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// beginLogin drives Service.BeginLogin and returns the parsed redirect URL
// plus the styr_pkce cookie it set, ready to be replayed onto a callback
// request.
func beginLogin(t *testing.T, svc *Service, slug string) (*url.URL, *http.Cookie) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login/"+slug, nil)
	redirectURL, err := svc.BeginLogin(rec, req, slug)
	if err != nil {
		t.Fatalf("BeginLogin: %v", err)
	}
	u, err := url.Parse(redirectURL)
	if err != nil {
		t.Fatalf("parse redirect url %q: %v", redirectURL, err)
	}
	var pkce *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == pkceCookieName {
			pkce = c
		}
	}
	if pkce == nil {
		t.Fatal("BeginLogin did not set the styr_pkce cookie")
	}
	return u, pkce
}

// callback builds a GET /api/v1/auth/callback request carrying pkce and the
// given query parameters, and runs CompleteLogin against it.
func callback(t *testing.T, svc *Service, pkce *http.Cookie, query url.Values) (domain.User, *httptest.ResponseRecorder, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/callback?"+query.Encode(), nil)
	req.AddCookie(pkce)
	rec := httptest.NewRecorder()
	usr, err := svc.CompleteLogin(rec, req)
	return usr, rec, err
}

func newProviderTestService(t *testing.T, fake *fakeOIDC, clientID string) (*Service, config.OIDCProvider, *db.Users) {
	t.Helper()
	users, logins := newTestRepos(t)
	provider := config.OIDCProvider{
		Name: "Test IdP", Issuer: fake.issuer(), ClientID: clientID, ClientSecret: "test-secret",
		Scopes: []string{"openid", "profile", "email"},
	}
	svc, err := New(users, logins, []config.OIDCProvider{provider}, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc, provider, users
}

func TestBeginLogin_RedirectContainsPKCEChallengeAndState(t *testing.T) {
	fake := newFakeOIDC(t, "test-client")
	svc, provider, _ := newProviderTestService(t, fake, "test-client")
	slug := slugify(provider.Name)

	u, _ := beginLogin(t, svc, slug)

	if u.Query().Get("code_challenge") == "" {
		t.Error("redirect URL missing code_challenge")
	}
	if u.Query().Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", u.Query().Get("code_challenge_method"))
	}
	if u.Query().Get("state") == "" {
		t.Error("redirect URL missing state")
	}
	if u.Query().Get("nonce") == "" {
		t.Error("redirect URL missing nonce")
	}
}

func TestCompleteLogin_MismatchedState_Fails(t *testing.T) {
	fake := newFakeOIDC(t, "test-client")
	svc, provider, _ := newProviderTestService(t, fake, "test-client")
	slug := slugify(provider.Name)

	u, pkce := beginLogin(t, svc, slug)
	fake.setUser(u.Query().Get("nonce"), "user-1", "alice@example.com", "Alice", "")

	query := url.Values{"code": {"test-code"}, "state": {"not-the-real-state"}}
	_, _, err := callback(t, svc, pkce, query)
	if err == nil {
		t.Fatal("CompleteLogin with mismatched state: want error, got nil")
	}
}

func TestCompleteLogin_FirstUser_BecomesAdmin(t *testing.T) {
	fake := newFakeOIDC(t, "test-client")
	svc, provider, _ := newProviderTestService(t, fake, "test-client")
	slug := slugify(provider.Name)

	u, pkce := beginLogin(t, svc, slug)
	fake.setUser(u.Query().Get("nonce"), "user-1", "alice@example.com", "Alice", "https://example.com/a.png")

	query := url.Values{"code": {"test-code"}, "state": {u.Query().Get("state")}}
	usr, rec, err := callback(t, svc, pkce, query)
	if err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	if usr.Role != domain.RoleAdmin {
		t.Errorf("first user role = %q, want admin", usr.Role)
	}
	if usr.Email != "alice@example.com" || usr.DisplayName != "Alice" || usr.AvatarURL != "https://example.com/a.png" {
		t.Errorf("user = %+v, want claims mapped from id_token", usr)
	}
	if usr.Issuer != fake.issuer() || usr.Subject != "user-1" {
		t.Errorf("user issuer/subject = %q/%q, want %q/%q", usr.Issuer, usr.Subject, fake.issuer(), "user-1")
	}

	var sessionSet bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" {
			sessionSet = true
		}
	}
	if !sessionSet {
		t.Error("expected CompleteLogin to set the styr_session cookie")
	}
}

func TestCompleteLogin_SecondDistinctSubject_BecomesMember(t *testing.T) {
	fake := newFakeOIDC(t, "test-client")
	svc, provider, _ := newProviderTestService(t, fake, "test-client")
	slug := slugify(provider.Name)

	// First login: becomes admin.
	u1, pkce1 := beginLogin(t, svc, slug)
	fake.setUser(u1.Query().Get("nonce"), "user-1", "alice@example.com", "Alice", "")
	first, _, err := callback(t, svc, pkce1, url.Values{"code": {"code-1"}, "state": {u1.Query().Get("state")}})
	if err != nil {
		t.Fatalf("first CompleteLogin: %v", err)
	}
	if first.Role != domain.RoleAdmin {
		t.Fatalf("first user role = %q, want admin", first.Role)
	}

	// Second, distinct subject: becomes member.
	u2, pkce2 := beginLogin(t, svc, slug)
	fake.setUser(u2.Query().Get("nonce"), "user-2", "bob@example.com", "Bob", "")
	second, _, err := callback(t, svc, pkce2, url.Values{"code": {"code-2"}, "state": {u2.Query().Get("state")}})
	if err != nil {
		t.Fatalf("second CompleteLogin: %v", err)
	}
	if second.Role != domain.RoleMember {
		t.Errorf("second distinct-subject user role = %q, want member", second.Role)
	}
}

func TestCompleteLogin_ExistingUser_ChangedDisplayName_UpdatesStoredUser(t *testing.T) {
	fake := newFakeOIDC(t, "test-client")
	svc, provider, users := newProviderTestService(t, fake, "test-client")
	slug := slugify(provider.Name)

	u1, pkce1 := beginLogin(t, svc, slug)
	fake.setUser(u1.Query().Get("nonce"), "user-1", "alice@example.com", "Alice", "https://example.com/old.png")
	first, _, err := callback(t, svc, pkce1, url.Values{"code": {"code-1"}, "state": {u1.Query().Get("state")}})
	if err != nil {
		t.Fatalf("first CompleteLogin: %v", err)
	}

	// Re-login with a changed display name (and avatar), same subject.
	u2, pkce2 := beginLogin(t, svc, slug)
	fake.setUser(u2.Query().Get("nonce"), "user-1", "alice@example.com", "Alice Updated", "https://example.com/new.png")
	second, _, err := callback(t, svc, pkce2, url.Values{"code": {"code-2"}, "state": {u2.Query().Get("state")}})
	if err != nil {
		t.Fatalf("second CompleteLogin: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("re-login created a new user: %q != %q", second.ID, first.ID)
	}
	if second.DisplayName != "Alice Updated" || second.AvatarURL != "https://example.com/new.png" {
		t.Fatalf("CompleteLogin returned stale profile: %+v", second)
	}

	reloaded, err := users.GetByID(t.Context(), second.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if reloaded.DisplayName != "Alice Updated" {
		t.Errorf("stored DisplayName = %q, want %q", reloaded.DisplayName, "Alice Updated")
	}
	if reloaded.AvatarURL != "https://example.com/new.png" {
		t.Errorf("stored AvatarURL = %q, want %q", reloaded.AvatarURL, "https://example.com/new.png")
	}
}

func TestCompleteLogin_ExistingUser_TouchesLogin(t *testing.T) {
	fake := newFakeOIDC(t, "test-client")
	svc, provider, users := newProviderTestService(t, fake, "test-client")
	slug := slugify(provider.Name)

	u1, pkce1 := beginLogin(t, svc, slug)
	fake.setUser(u1.Query().Get("nonce"), "user-1", "alice@example.com", "Alice", "")
	first, _, err := callback(t, svc, pkce1, url.Values{"code": {"code-1"}, "state": {u1.Query().Get("state")}})
	if err != nil {
		t.Fatalf("first CompleteLogin: %v", err)
	}

	u2, pkce2 := beginLogin(t, svc, slug)
	fake.setUser(u2.Query().Get("nonce"), "user-1", "alice@example.com", "Alice", "")
	second, _, err := callback(t, svc, pkce2, url.Values{"code": {"code-2"}, "state": {u2.Query().Get("state")}})
	if err != nil {
		t.Fatalf("second CompleteLogin: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("re-login created a new user: %q != %q", second.ID, first.ID)
	}

	// upsertUser returns the user as read before TouchLogin runs, so check
	// the persisted row directly for the touch's effect.
	reloaded, err := users.GetByID(t.Context(), second.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if reloaded.LastLoginAt.Before(first.CreatedAt) {
		t.Errorf("LastLoginAt did not advance past creation: created=%v last_login=%v", first.CreatedAt, reloaded.LastLoginAt)
	}
}

// Regression test for the count-then-create race in upsertUser: without
// firstUserMu serializing "count users, then create with role admin iff
// count == 0", 20 concurrent first logins with distinct subjects could each
// observe count == 0 and each create an admin row. Exercises upsertUser
// directly (rather than the full OIDC round trip) since the race is in that
// one method, not in token exchange or verification.
func TestUpsertUser_ConcurrentFirstLogins_ExactlyOneAdmin(t *testing.T) {
	users, logins := newTestRepos(t)
	svc, err := New(users, logins, nil, "http://localhost:8080", false, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	const n = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			claims := idClaims{Email: fmt.Sprintf("u%d@example.com", i), Name: fmt.Sprintf("User %d", i)}
			_, err := svc.upsertUser(t.Context(), "concurrent-issuer", fmt.Sprintf("subject-%d", i), claims)
			errCh <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("upsertUser: %v", err)
		}
	}

	all, err := users.List(t.Context())
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(all) != n {
		t.Fatalf("created %d users, want %d", len(all), n)
	}
	admins := 0
	for _, u := range all {
		if u.Role == domain.RoleAdmin {
			admins++
		}
	}
	if admins != 1 {
		t.Fatalf("admin count = %d, want exactly 1", admins)
	}
}
