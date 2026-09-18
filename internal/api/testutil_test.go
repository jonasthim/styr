package api_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/auth"
	"github.com/jonasthim/styr/internal/crypto"
	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/events"
	"github.com/jonasthim/styr/internal/harness"
	"github.com/jonasthim/styr/internal/harness/fake"
	"github.com/jonasthim/styr/internal/notify"
	"github.com/jonasthim/styr/internal/sessions"
	"github.com/jonasthim/styr/internal/workspaces"
)

const testSecret = "0123456789abcdef0123456789abcdef" // 32+ bytes, for crypto.NewBox

// fakeTokenStore is a minimal in-memory api.TokenStore, standing in for
// T31's db.APITokens repository (which does not exist in this worktree; see
// the T36 card). Delete and GetByHash both scope by owner, matching the
// real repository's contract (Delete never removes another user's token;
// GetByHash is used by auth.Service.principalFromBearer, not this store's
// api.TokenStore interface, but the two share the same underlying rows in
// tests that seed both - see (*testEnv).apiTokens).
type fakeTokenStore struct {
	mu   sync.Mutex
	byID map[string]domain.APIToken
}

func newFakeTokenStore() *fakeTokenStore {
	return &fakeTokenStore{byID: map[string]domain.APIToken{}}
}

func (f *fakeTokenStore) Create(_ context.Context, t domain.APIToken) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[t.ID] = t
	return nil
}

func (f *fakeTokenStore) ListByUser(_ context.Context, userID string) ([]domain.APIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.APIToken, 0)
	for _, t := range f.byID {
		if t.UserID == userID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeTokenStore) Delete(_ context.Context, id, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.byID[id]
	if !ok || t.UserID != userID {
		return domain.ErrNotFound
	}
	delete(f.byID, id)
	return nil
}

func (f *fakeTokenStore) GetByHash(_ context.Context, hash string) (*domain.APIToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.byID {
		if t.TokenHash == hash {
			cp := t
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (f *fakeTokenStore) TouchUsed(_ context.Context, id string, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	now := time.Now()
	t.LastUsedAt = &now
	f.byID[id] = t
	return nil
}

// stubVerifier is a settable api.TokenVerifier: nil error (the default)
// means every token verifies; setErr makes every call fail, as the real
// verifier does for a bad token.
type stubVerifier struct {
	mu  sync.Mutex
	err error
}

func (v *stubVerifier) Verify(context.Context, string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.err
}

func (v *stubVerifier) setErr(err error) {
	v.mu.Lock()
	v.err = err
	v.mu.Unlock()
}

// testEnv bundles a running api.NewRouter server with direct repository
// access for seeding, and clients for an admin (via the dev-user bypass)
// and members (via a hand-minted login_sessions cookie).
type testEnv struct {
	t *testing.T

	ts *httptest.Server

	users         *db.Users
	tokens        *db.Tokens
	workspaces    *db.Workspaces
	workspaceSvc  *workspaces.Service
	profiles      *db.Profiles
	sessions      *db.Sessions
	events        *db.Events
	approvals     *db.Approvals
	logins        *db.LoginSessions
	box           *crypto.Box
	bus           *events.Bus
	harness       *fake.Harness
	verifier      *stubVerifier
	tokenStore    *fakeTokenStore
	svc           *sessions.Service
	usersDir      string
	notifications *db.NotificationChannels

	triggers *fakeTriggersService
	runs     *fakeRunsEngine
	notifier *fakeNotifier

	adminClient *http.Client
	adminID     string
}

// newEnv opens a fresh temp sqlite database, wires every repository and the
// sessions service to a fake harness that replays steps, and starts an
// httptest server for api.NewRouter. The admin client is authenticated via
// the auth.Service dev-user bypass.
func newEnv(t *testing.T, steps ...fake.Step) *testEnv {
	t.Helper()
	return newEnvWithDevUser(t, "admin@example.com", steps...)
}

// newEnvNoDevUser is newEnv with the dev-user bypass disabled, for tests
// that need a genuinely unauthenticated request to be rejected (the dev
// bypass would otherwise auto-provision an admin for any cookie-less
// request).
func newEnvNoDevUser(t *testing.T, steps ...fake.Step) *testEnv {
	t.Helper()
	return newEnvWithDevUser(t, "", steps...)
}

func newEnvWithDevUser(t *testing.T, devUser string, steps ...fake.Step) *testEnv {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "styr.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	e := &testEnv{
		t:             t,
		users:         db.NewUsers(database),
		tokens:        db.NewTokens(database),
		workspaces:    db.NewWorkspaces(database),
		profiles:      db.NewProfiles(database),
		sessions:      db.NewSessions(database),
		events:        db.NewEvents(database),
		approvals:     db.NewApprovals(database),
		logins:        db.NewLoginSessions(database),
		bus:           events.New(),
		harness:       fake.New(steps...),
		verifier:      &stubVerifier{},
		tokenStore:    newFakeTokenStore(),
		usersDir:      t.TempDir(),
		notifications: db.NewNotificationChannels(database),
		triggers:      newFakeTriggersService(),
		runs:          newFakeRunsEngine(),
		notifier:      newFakeNotifier(),
	}
	e.workspaceSvc = workspaces.New(e.workspaces, e.sessions, e.bus, e.usersDir, nil)

	box, err := crypto.NewBox(testSecret)
	if err != nil {
		t.Fatalf("crypto.NewBox: %v", err)
	}
	e.box = box

	e.svc = sessions.New(sessions.Repos{
		Sessions:   e.sessions,
		Events:     e.events,
		Approvals:  e.approvals,
		Workspaces: e.workspaces,
		Profiles:   e.profiles,
		Tokens:     e.tokens,
		Audit:      db.NewAudit(database),
	}, e.harness, e.bus, box, sessions.Options{
		MaxOpen:     4,
		IdleTimeout: time.Hour,
		UsersDir:    e.usersDir,
		ServiceHome: t.TempDir(),
	})

	authSvc, err := auth.New(e.users, e.logins, nil, "http://example.com", false, devUser)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	authSvc.WithAPITokens(e.tokenStore)

	d := &api.Deps{
		Auth:           authSvc,
		TokenStore:     e.tokenStore,
		Sessions:       e.svc,
		Users:          e.users,
		Tokens:         e.tokens,
		Workspaces:     e.workspaceSvc,
		WorkspacesRepo: e.workspaces,
		Profiles:       e.profiles,
		Audit:          db.NewAudit(database),
		Bus:            e.bus,
		Box:            box,
		Verifier:       e.verifier,
		Triggers:       e.triggers,
		Runs:           e.runs,
		Notifications:  e.notifications,
		Notifier:       e.notifier,
		Status: func() api.StatusInfo {
			return api.StatusInfo{Version: "test", ClaudeVersion: "test", OpenProcesses: 0, Slots: 4, QueueDepth: 0}
		},
		Version:         "test",
		MaxOpenSessions: 4,
		IdleTimeout:     time.Hour,
	}

	e.ts = httptest.NewServer(api.NewRouter(d, nil))
	t.Cleanup(e.ts.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	e.adminClient = &http.Client{Jar: jar}

	if devUser != "" {
		// Trigger the dev bypass so the admin user and its session cookie
		// exist before tests start asserting against it.
		var me struct {
			ID string `json:"id"`
		}
		e.doJSON(e.adminClient, http.MethodGet, "/api/v1/me", nil, &me)
		e.adminID = me.ID
	}

	return e
}

// memberClient creates a member user directly and mints a login session by
// inserting a login_sessions row and setting the styr_session cookie to the
// matching raw token — the same shape auth.Service's own createSession
// produces (see internal/auth/cookies.go: sha256 hex of the raw token,
// cookie name "styr_session").
func (e *testEnv) memberClient(email string) (domain.User, *http.Client) {
	e.t.Helper()
	ctx := context.Background()
	now := time.Now()
	usr := domain.User{
		ID: email, Issuer: "test", Subject: email, Email: email, DisplayName: email,
		Role: domain.RoleMember, CreatedAt: now, LastLoginAt: now, Prefs: json.RawMessage(`{}`),
	}
	if err := e.users.Create(ctx, usr); err != nil {
		e.t.Fatalf("create member %s: %v", email, err)
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		e.t.Fatalf("rand: %v", err)
	}
	rawHex := hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(rawHex))
	hash := hex.EncodeToString(sum[:])
	if _, err := e.logins.Create(ctx, usr.ID, hash, now.Add(24*time.Hour), "test"); err != nil {
		e.t.Fatalf("create login session: %v", err)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		e.t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{Jar: jar}
	u, err := url.Parse(e.ts.URL)
	if err != nil {
		e.t.Fatalf("parse server url: %v", err)
	}
	jar.SetCookies(u, []*http.Cookie{{Name: "styr_session", Value: rawHex, Path: "/"}})
	return usr, client
}

// seedWorkspace creates a workspace whose path is a temp directory
// containing a .git directory, so POST /workspaces path validation
// (exercised elsewhere) and session creation both succeed against it.
func (e *testEnv) seedWorkspace(profileID string) domain.Workspace {
	e.t.Helper()
	dir := e.t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		e.t.Fatalf("mkdir .git: %v", err)
	}
	now := time.Now()
	ws := domain.Workspace{
		ID: "ws-" + profileID, Name: "ws-" + profileID, Path: dir, DefaultProfileID: profileID,
		Source: domain.WorkspaceSourcePath, State: domain.WorkspaceReady, CreatedAt: now, UpdatedAt: now,
	}
	if err := e.workspaces.Create(context.Background(), ws); err != nil {
		e.t.Fatalf("create workspace: %v", err)
	}
	return ws
}

// seedToken seals and stores a Claude token directly for userID, bypassing
// the verify-then-store HTTP flow, for tests that need a working token
// already in place.
func (e *testEnv) seedToken(userID string) {
	e.t.Helper()
	ciphertext, nonce, err := e.box.Seal([]byte("sk-ant-test"))
	if err != nil {
		e.t.Fatalf("seal token: %v", err)
	}
	if err := e.tokens.Set(context.Background(), userID, ciphertext, nonce, "test"); err != nil {
		e.t.Fatalf("set token: %v", err)
	}
}

// doJSON sends a request with body JSON-encoded (nil for no body) and
// decodes a JSON response into out (nil to skip). State-changing methods
// get the CSRF header automatically. Returns the response status.
func (e *testEnv) doJSON(client *http.Client, method, path string, body, out any) int {
	e.t.Helper()
	status, _ := e.doJSONHeaders(client, method, path, body, out, true)
	return status
}

// doJSONHeaders is doJSON with control over whether the CSRF header is
// sent, and returns the raw response body alongside the status.
func (e *testEnv) doJSONHeaders(client *http.Client, method, path string, body, out any, csrf bool) (int, []byte) {
	e.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.ts.URL+path, reader)
	if err != nil {
		e.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if csrf {
		req.Header.Set("X-Requested-With", "styr")
	}
	resp, err := client.Do(req)
	if err != nil {
		e.t.Fatalf("do request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatalf("read body: %v", err)
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("unmarshal body %q: %v", raw, err)
		}
	}
	return resp.StatusCode, raw
}

// createResultStep is the single scripted turn most session handler tests need: the fake
// harness answers the first Send with a successful result, so the session settles on open.
func createResultStep() fake.Step {
	return fake.Step{Events: []harness.Event{
		{Type: harness.EventResult, Result: &harness.Result{Subtype: "success", NumTurns: 1}},
	}}
}

// fakeCall records one call made to a fake service, for tests that assert
// on what was passed through (e.g. deliveries limit, runs list filters).
type fakeCall struct {
	method string
	actor  api.Actor
	args   []any
}

// fakeTriggersService is an in-memory api.TriggersService: it records every
// call and, for each method, either invokes the matching *Fn (set by the
// test to return canned data or an error) or falls back to a default that
// makes an unconfigured call fail loudly rather than silently succeed with
// a zero value.
type fakeTriggersService struct {
	mu    sync.Mutex
	calls []fakeCall

	CreateTemplateFn func(ctx context.Context, actor api.Actor, in domain.TemplateInput) (domain.Template, error)
	ListTemplatesFn  func(ctx context.Context, actor api.Actor) ([]domain.Template, error)
	GetTemplateFn    func(ctx context.Context, actor api.Actor, id string) (domain.Template, error)
	UpdateTemplateFn func(ctx context.Context, actor api.Actor, id string, in domain.TemplateInput) (domain.Template, error)
	DeleteTemplateFn func(ctx context.Context, actor api.Actor, id string) error
	RenderTemplateFn func(ctx context.Context, actor api.Actor, id, kind string, payload []byte) (domain.RenderResult, error)

	CreateTriggerFn func(ctx context.Context, actor api.Actor, in domain.TriggerInput) (domain.Trigger, string, error)
	ListTriggersFn  func(ctx context.Context, actor api.Actor) ([]domain.Trigger, error)
	GetTriggerFn    func(ctx context.Context, actor api.Actor, id string) (domain.Trigger, error)
	UpdateTriggerFn func(ctx context.Context, actor api.Actor, id string, in domain.TriggerInput) (domain.Trigger, error)
	DeleteTriggerFn func(ctx context.Context, actor api.Actor, id string) error
	RotateSecretFn  func(ctx context.Context, actor api.Actor, id string) (string, error)

	ListDeliveriesFn func(ctx context.Context, actor api.Actor, triggerID string, limit int) ([]domain.Delivery, error)
	ReplayFn         func(ctx context.Context, actor api.Actor, deliveryID string) (domain.Delivery, error)
	TestFn           func(ctx context.Context, actor api.Actor, triggerID string, payload []byte, force bool) (domain.Delivery, error)
	DeliverFn        func(ctx context.Context, in domain.Inbound) (domain.Delivery, error)
}

func newFakeTriggersService() *fakeTriggersService { return &fakeTriggersService{} }

func (f *fakeTriggersService) record(method string, actor api.Actor, args ...any) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{method: method, actor: actor, args: args})
	f.mu.Unlock()
}

// lastCall returns the most recent recorded call, for tests asserting on
// what a handler passed through (e.g. the delivery limit or run filter).
func (f *fakeTriggersService) lastCall() (fakeCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return fakeCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}

var errFakeNotConfigured = errors.New("fake: method not configured for this test")

func (f *fakeTriggersService) CreateTemplate(ctx context.Context, actor api.Actor, in domain.TemplateInput) (domain.Template, error) {
	f.record("CreateTemplate", actor, in)
	if f.CreateTemplateFn != nil {
		return f.CreateTemplateFn(ctx, actor, in)
	}
	return domain.Template{}, errFakeNotConfigured
}

func (f *fakeTriggersService) ListTemplates(ctx context.Context, actor api.Actor) ([]domain.Template, error) {
	f.record("ListTemplates", actor)
	if f.ListTemplatesFn != nil {
		return f.ListTemplatesFn(ctx, actor)
	}
	return nil, nil
}

func (f *fakeTriggersService) GetTemplate(ctx context.Context, actor api.Actor, id string) (domain.Template, error) {
	f.record("GetTemplate", actor, id)
	if f.GetTemplateFn != nil {
		return f.GetTemplateFn(ctx, actor, id)
	}
	return domain.Template{}, fmt.Errorf("get template: %w", domain.ErrNotFound)
}

func (f *fakeTriggersService) UpdateTemplate(ctx context.Context, actor api.Actor, id string, in domain.TemplateInput) (domain.Template, error) {
	f.record("UpdateTemplate", actor, id, in)
	if f.UpdateTemplateFn != nil {
		return f.UpdateTemplateFn(ctx, actor, id, in)
	}
	return domain.Template{}, errFakeNotConfigured
}

func (f *fakeTriggersService) DeleteTemplate(ctx context.Context, actor api.Actor, id string) error {
	f.record("DeleteTemplate", actor, id)
	if f.DeleteTemplateFn != nil {
		return f.DeleteTemplateFn(ctx, actor, id)
	}
	return nil
}

func (f *fakeTriggersService) RenderTemplate(ctx context.Context, actor api.Actor, id, kind string, payload []byte) (domain.RenderResult, error) {
	f.record("RenderTemplate", actor, id, kind, payload)
	if f.RenderTemplateFn != nil {
		return f.RenderTemplateFn(ctx, actor, id, kind, payload)
	}
	return domain.RenderResult{}, errFakeNotConfigured
}

func (f *fakeTriggersService) CreateTrigger(ctx context.Context, actor api.Actor, in domain.TriggerInput) (domain.Trigger, string, error) {
	f.record("CreateTrigger", actor, in)
	if f.CreateTriggerFn != nil {
		return f.CreateTriggerFn(ctx, actor, in)
	}
	return domain.Trigger{}, "", errFakeNotConfigured
}

func (f *fakeTriggersService) ListTriggers(ctx context.Context, actor api.Actor) ([]domain.Trigger, error) {
	f.record("ListTriggers", actor)
	if f.ListTriggersFn != nil {
		return f.ListTriggersFn(ctx, actor)
	}
	return nil, nil
}

func (f *fakeTriggersService) GetTrigger(ctx context.Context, actor api.Actor, id string) (domain.Trigger, error) {
	f.record("GetTrigger", actor, id)
	if f.GetTriggerFn != nil {
		return f.GetTriggerFn(ctx, actor, id)
	}
	return domain.Trigger{}, fmt.Errorf("get trigger: %w", domain.ErrNotFound)
}

func (f *fakeTriggersService) UpdateTrigger(ctx context.Context, actor api.Actor, id string, in domain.TriggerInput) (domain.Trigger, error) {
	f.record("UpdateTrigger", actor, id, in)
	if f.UpdateTriggerFn != nil {
		return f.UpdateTriggerFn(ctx, actor, id, in)
	}
	return domain.Trigger{}, errFakeNotConfigured
}

func (f *fakeTriggersService) DeleteTrigger(ctx context.Context, actor api.Actor, id string) error {
	f.record("DeleteTrigger", actor, id)
	if f.DeleteTriggerFn != nil {
		return f.DeleteTriggerFn(ctx, actor, id)
	}
	return nil
}

func (f *fakeTriggersService) RotateSecret(ctx context.Context, actor api.Actor, id string) (string, error) {
	f.record("RotateSecret", actor, id)
	if f.RotateSecretFn != nil {
		return f.RotateSecretFn(ctx, actor, id)
	}
	return "", errFakeNotConfigured
}

func (f *fakeTriggersService) ListDeliveries(ctx context.Context, actor api.Actor, triggerID string, limit int) ([]domain.Delivery, error) {
	f.record("ListDeliveries", actor, triggerID, limit)
	if f.ListDeliveriesFn != nil {
		return f.ListDeliveriesFn(ctx, actor, triggerID, limit)
	}
	return nil, nil
}

func (f *fakeTriggersService) Replay(ctx context.Context, actor api.Actor, deliveryID string) (domain.Delivery, error) {
	f.record("Replay", actor, deliveryID)
	if f.ReplayFn != nil {
		return f.ReplayFn(ctx, actor, deliveryID)
	}
	return domain.Delivery{}, errFakeNotConfigured
}

func (f *fakeTriggersService) Test(ctx context.Context, actor api.Actor, triggerID string, payload []byte, force bool) (domain.Delivery, error) {
	f.record("Test", actor, triggerID, payload, force)
	if f.TestFn != nil {
		return f.TestFn(ctx, actor, triggerID, payload, force)
	}
	return domain.Delivery{}, errFakeNotConfigured
}

func (f *fakeTriggersService) Deliver(ctx context.Context, in domain.Inbound) (domain.Delivery, error) {
	f.record("Deliver", api.Actor{}, in)
	if f.DeliverFn != nil {
		return f.DeliverFn(ctx, in)
	}
	return domain.Delivery{}, fmt.Errorf("deliver: %w", domain.ErrUnknownTrigger)
}

// fakeRunsEngine is an in-memory api.RunsEngine, recording calls the same
// way fakeTriggersService does.
type fakeRunsEngine struct {
	mu    sync.Mutex
	calls []fakeCall

	GetFn  func(ctx context.Context, id string) (domain.RunView, error)
	ListFn func(ctx context.Context, f domain.RunFilter) ([]domain.RunView, error)
}

func newFakeRunsEngine() *fakeRunsEngine { return &fakeRunsEngine{} }

func (f *fakeRunsEngine) record(method string, args ...any) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{method: method, args: args})
	f.mu.Unlock()
}

func (f *fakeRunsEngine) lastCall() (fakeCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return fakeCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}

func (f *fakeRunsEngine) Get(ctx context.Context, id string) (domain.RunView, error) {
	f.record("Get", id)
	if f.GetFn != nil {
		return f.GetFn(ctx, id)
	}
	return domain.RunView{}, fmt.Errorf("get run: %w", domain.ErrNotFound)
}

func (f *fakeRunsEngine) List(ctx context.Context, filter domain.RunFilter) ([]domain.RunView, error) {
	f.record("List", filter)
	if f.ListFn != nil {
		return f.ListFn(ctx, filter)
	}
	return nil, nil
}

// fakeNotifierCall records one Send call: the channel (including whatever
// token the handler decrypted and passed in) and the event.
type fakeNotifierCall struct {
	channel notify.Channel
	event   notify.Event
}

// fakeNotifier is an in-memory api.Notifier: it records every Send call
// (so a test can assert the token it received was already decrypted) and
// never performs a real HTTP request.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []fakeNotifierCall

	SendFn func(ctx context.Context, ch notify.Channel, ev notify.Event) error
}

func newFakeNotifier() *fakeNotifier { return &fakeNotifier{} }

func (f *fakeNotifier) Send(ctx context.Context, ch notify.Channel, ev notify.Event) error {
	f.mu.Lock()
	f.calls = append(f.calls, fakeNotifierCall{channel: ch, event: ev})
	f.mu.Unlock()
	if f.SendFn != nil {
		return f.SendFn(ctx, ch, ev)
	}
	return nil
}

func (f *fakeNotifier) lastCall() (fakeNotifierCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return fakeNotifierCall{}, false
	}
	return f.calls[len(f.calls)-1], true
}
