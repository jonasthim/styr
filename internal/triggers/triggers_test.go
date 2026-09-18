package triggers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/templates"
)

var (
	admin  = Actor{UserID: "u-admin", IsAdmin: true}
	member = Actor{UserID: "u-member"}
	other  = Actor{UserID: "u-other"}
)

// base is the fixed instant tests build their timelines around. Whole
// seconds only: stored timestamps are RFC3339Nano, which trims trailing
// zeros, so sub-second values do not order correctly as strings.
var base = time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)

// stubStarter stands in for the run engine: it records what the router
// asked to start and can be made to fail.
type stubStarter struct {
	mu     sync.Mutex
	inputs []RunInput
	err    error
	nextID int
}

func (s *stubStarter) Start(_ context.Context, in RunInput) (domain.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputs = append(s.inputs, in)
	if s.err != nil {
		return domain.Run{}, s.err
	}
	s.nextID++
	return domain.Run{ID: fmt.Sprintf("run-%d", s.nextID), Outcome: domain.RunRunning}, nil
}

func (s *stubStarter) calls() []RunInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RunInput(nil), s.inputs...)
}

type fixture struct {
	svc        *Service
	repos      Repos
	starter    *stubStarter
	templateID string
	trigger    domain.Trigger
	secret     string
	now        time.Time
}

// newFixture builds a service over a temp database with two users, a ready
// workspace, a shared template and one trigger of the given kind.
func newFixture(t *testing.T, kind domain.TriggerKind, tune func(in *domain.TriggerInput)) *fixture {
	t.Helper()

	database, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	ctx := context.Background()
	users := db.NewUsers(database)
	for _, u := range []struct {
		id   string
		role domain.Role
	}{{admin.UserID, domain.RoleAdmin}, {member.UserID, domain.RoleMember}, {other.UserID, domain.RoleMember}} {
		if err := users.Create(ctx, domain.User{
			ID: u.id, Issuer: "test", Subject: u.id, Email: u.id + "@example.com",
			DisplayName: u.id, Role: u.role, CreatedAt: base, LastLoginAt: base,
		}); err != nil {
			t.Fatalf("create user %s: %v", u.id, err)
		}
	}
	if err := db.NewWorkspaces(database).Create(ctx, domain.Workspace{
		ID: "ws-1", Name: "ws", Path: t.TempDir(), DefaultProfileID: "investigate",
		Source: domain.WorkspaceSourcePath, State: domain.WorkspaceReady, CreatedAt: base, UpdatedAt: base,
	}); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	f := &fixture{
		repos: Repos{
			Templates:  db.NewTemplates(database),
			Triggers:   db.NewTriggers(database),
			Deliveries: db.NewDeliveries(database),
		},
		starter: &stubStarter{},
		now:     base,
	}
	f.svc = New(f.repos, f.starter, nil, slog.New(slog.DiscardHandler), func() time.Time { return f.now })

	tpl, err := f.svc.CreateTemplate(ctx, admin, domain.TemplateInput{
		Name: "Alert investigation", WorkspaceID: "ws-1", ProfileID: "investigate",
		TitleTemplate: `{{ .status }}`, PromptTemplate: `Investigate.`, Shared: true,
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	f.templateID = tpl.ID

	in := domain.TriggerInput{Name: "Grafana alerts", Kind: string(kind), TemplateID: tpl.ID, Shared: true}
	if tune != nil {
		tune(&in)
	}
	tr, secret, err := f.svc.CreateTrigger(ctx, admin, in)
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	f.trigger, f.secret = tr, secret
	return f
}

func (f *fixture) inbound(body []byte, at time.Time) domain.Inbound {
	return domain.Inbound{
		Slug:    f.trigger.Slug,
		Body:    body,
		Headers: http.Header{"X-Styr-Secret": []string{f.secret}},
		Now:     at,
	}
}

func grafanaPayload(status, fingerprint string) []byte {
	return []byte(fmt.Sprintf(`{"status":%q,"alerts":[{"status":%q,"fingerprint":%q,`+
		`"labels":{"alertname":"SystemdUnitFailed","instance":"ct-142"},`+
		`"annotations":{"summary":"the unit failed"},"startsAt":"2026-09-18T09:59:00Z"}]}`,
		status, status, fingerprint))
}

func mustDeliver(t *testing.T, f *fixture, in domain.Inbound) domain.Delivery {
	t.Helper()
	dl, err := f.svc.Deliver(context.Background(), in)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	return dl
}

func TestCreateTriggerMintsASecretAndStoresOnlyItsHash(t *testing.T) {
	f := newFixture(t, domain.TriggerGrafana, nil)

	if !strings.HasPrefix(f.secret, "styr_whs_") {
		t.Fatalf("secret = %q, want the styr_whs_ prefix", f.secret)
	}
	if len(f.secret) < 40 {
		t.Fatalf("secret is only %d characters", len(f.secret))
	}
	if strings.Contains(f.trigger.SecretHash, f.secret) || f.trigger.SecretHash == f.secret {
		t.Fatal("the stored hash must not be the secret itself")
	}
	if f.trigger.SecretHash != hashSecret(f.secret) {
		t.Fatal("stored hash does not match sha256 of the secret")
	}
	if want := f.secret[len(f.secret)-6:]; f.trigger.SecretHint != want {
		t.Fatalf("hint = %q, want %q", f.trigger.SecretHint, want)
	}
	if f.trigger.Slug != "grafana-alerts" {
		t.Fatalf("slug = %q", f.trigger.Slug)
	}
	if f.trigger.CooldownS != defaultCooldownS || f.trigger.StormCapPerHour != defaultStormCap {
		t.Fatalf("defaults not applied: %+v", f.trigger)
	}
	if !f.trigger.Enabled {
		t.Fatal("a new trigger should be enabled")
	}
	if f.trigger.OwnerID != nil {
		t.Fatalf("owner = %v, want nil for a shared trigger", *f.trigger.OwnerID)
	}
}

func TestCreateTriggerSuffixesAClashingSlug(t *testing.T) {
	f := newFixture(t, domain.TriggerGrafana, nil)
	ctx := context.Background()

	second, _, err := f.svc.CreateTrigger(ctx, admin, domain.TriggerInput{
		Name: "Grafana alerts", Kind: "grafana", TemplateID: f.templateID, Shared: true,
	})
	if err != nil {
		t.Fatalf("create second trigger: %v", err)
	}
	if second.Slug != "grafana-alerts-2" {
		t.Fatalf("slug = %q, want grafana-alerts-2", second.Slug)
	}

	third, _, err := f.svc.CreateTrigger(ctx, admin, domain.TriggerInput{
		Name: "Grafana alerts!", Kind: "grafana", TemplateID: f.templateID, Shared: true,
	})
	if err != nil {
		t.Fatalf("create third trigger: %v", err)
	}
	if third.Slug != "grafana-alerts-3" {
		t.Fatalf("slug = %q, want grafana-alerts-3", third.Slug)
	}
}

func TestRotateSecretInvalidatesTheOldOne(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, nil)
	ctx := context.Background()

	fresh, err := f.svc.RotateSecret(ctx, admin, f.trigger.ID)
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if fresh == f.secret {
		t.Fatal("rotation returned the same secret")
	}
	if _, err := f.svc.Deliver(ctx, f.inbound([]byte(`{"a":1}`), base)); !errors.Is(err, domain.ErrBadSecret) {
		t.Fatalf("delivering with the old secret: err = %v, want ErrBadSecret", err)
	}
	in := f.inbound([]byte(`{"a":1}`), base)
	in.Headers = http.Header{"X-Styr-Secret": []string{fresh}}
	if _, err := f.svc.Deliver(ctx, in); err != nil {
		t.Fatalf("delivering with the new secret: %v", err)
	}
}

func TestDeliverHappyPathAcceptsAndStartsARun(t *testing.T) {
	f := newFixture(t, domain.TriggerGrafana, nil)

	dl := mustDeliver(t, f, f.inbound(grafanaPayload("firing", "abc123"), base))

	if dl.Status != domain.DeliveryAccepted {
		t.Fatalf("status = %s (%s), want accepted", dl.Status, dl.Reason)
	}
	if dl.RunID == nil || *dl.RunID != "run-1" {
		t.Fatalf("run id = %v", dl.RunID)
	}
	if dl.DedupeKey != "firing:abc123," {
		t.Fatalf("dedupe key = %q", dl.DedupeKey)
	}

	stored, err := f.repos.Deliveries.Get(context.Background(), dl.ID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if stored.Status != domain.DeliveryAccepted || stored.RunID == nil || *stored.RunID != "run-1" {
		t.Fatalf("stored delivery = %+v", stored)
	}

	calls := f.starter.calls()
	if len(calls) != 1 {
		t.Fatalf("started %d runs, want 1", len(calls))
	}
	if calls[0].TemplateID != f.templateID || calls[0].TriggerID != f.trigger.ID || calls[0].DeliveryID != dl.ID {
		t.Fatalf("run input = %+v", calls[0])
	}
	if calls[0].Origin != domain.OriginWebhook {
		t.Fatalf("origin = %s", calls[0].Origin)
	}
	if status, _ := calls[0].Vars["status"].(string); status != "firing" {
		t.Fatalf("vars = %+v", calls[0].Vars)
	}

	tr, err := f.repos.Triggers.Get(context.Background(), f.trigger.ID)
	if err != nil {
		t.Fatalf("get trigger: %v", err)
	}
	if tr.LastDeliveryAt == nil || !tr.LastDeliveryAt.Equal(base) {
		t.Fatalf("last_delivery_at = %v, want %v", tr.LastDeliveryAt, base)
	}
}

func TestDeliverAuthentication(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, nil)
	ctx := context.Background()
	body := []byte(`{"event":"x"}`)

	cases := []struct {
		name string
		in   domain.Inbound
		want error
	}{
		{
			name: "x-styr-secret header",
			in:   domain.Inbound{Slug: f.trigger.Slug, Body: body, Headers: http.Header{"X-Styr-Secret": []string{f.secret}}, Now: base},
		},
		{
			name: "authorization bearer",
			in:   domain.Inbound{Slug: f.trigger.Slug, Body: body, Headers: http.Header{"Authorization": []string{"Bearer " + f.secret}}, Now: base},
		},
		{
			name: "secret query parameter",
			in:   domain.Inbound{Slug: f.trigger.Slug, Body: body, Query: url.Values{"secret": []string{f.secret}}, Now: base},
		},
		{
			name: "wrong secret",
			in:   domain.Inbound{Slug: f.trigger.Slug, Body: body, Headers: http.Header{"X-Styr-Secret": []string{"styr_whs_nope"}}, Now: base},
			want: domain.ErrBadSecret,
		},
		{
			name: "no secret at all",
			in:   domain.Inbound{Slug: f.trigger.Slug, Body: body, Now: base},
			want: domain.ErrBadSecret,
		},
		{
			name: "unknown slug",
			in:   domain.Inbound{Slug: "nope", Body: body, Headers: http.Header{"X-Styr-Secret": []string{f.secret}}, Now: base},
			want: domain.ErrUnknownTrigger,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Force every accepted case past dedupe by making each body unique.
			in := tc.in
			in.Body = []byte(fmt.Sprintf(`{"event":%q}`, tc.name))
			_, err := f.svc.Deliver(ctx, in)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Deliver: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestDeliverRejectsADisabledTrigger(t *testing.T) {
	disabled := false
	f := newFixture(t, domain.TriggerGeneric, func(in *domain.TriggerInput) { in.Enabled = &disabled })

	_, err := f.svc.Deliver(context.Background(), f.inbound([]byte(`{"a":1}`), base))
	if !errors.Is(err, domain.ErrUnknownTrigger) {
		t.Fatalf("err = %v, want ErrUnknownTrigger", err)
	}
	if got := f.starter.calls(); len(got) != 0 {
		t.Fatalf("started %d runs for a disabled trigger", len(got))
	}
}

func TestDeliverRejectsAnOversizedBody(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, nil)

	body := make([]byte, maxPayloadBytes+1)
	for i := range body {
		body[i] = 'x'
	}
	_, err := f.svc.Deliver(context.Background(), f.inbound(body, base))
	if !errors.Is(err, domain.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	stored, err := f.repos.Deliveries.ListByTrigger(context.Background(), f.trigger.ID, 10)
	if err != nil {
		t.Fatalf("list deliveries: %v", err)
	}
	if len(stored) != 0 {
		t.Fatalf("an oversized body should not be logged, got %d deliveries", len(stored))
	}
}

func TestDeliverRejectsAnUnparseablePayload(t *testing.T) {
	f := newFixture(t, domain.TriggerGrafana, nil)

	dl := mustDeliver(t, f, f.inbound([]byte(`not json at all`), base))
	if dl.Status != domain.DeliveryRejected {
		t.Fatalf("status = %s, want rejected", dl.Status)
	}
	if !strings.Contains(dl.Reason, "invalid payload") {
		t.Fatalf("reason = %q", dl.Reason)
	}
	if got := f.starter.calls(); len(got) != 0 {
		t.Fatalf("started %d runs", len(got))
	}
}

func TestDeliverSkipsResolvedAlertsUnlessAsked(t *testing.T) {
	f := newFixture(t, domain.TriggerGrafana, nil)

	dl := mustDeliver(t, f, f.inbound(grafanaPayload("resolved", "abc123"), base))
	if dl.Status != domain.DeliverySkipped || dl.Reason != "resolved" {
		t.Fatalf("delivery = %+v, want skipped/resolved", dl)
	}
	if got := f.starter.calls(); len(got) != 0 {
		t.Fatalf("started %d runs for a resolved alert", len(got))
	}

	resolving := newFixture(t, domain.TriggerGrafana, func(in *domain.TriggerInput) { in.RunOnResolved = true })
	dl = mustDeliver(t, resolving, resolving.inbound(grafanaPayload("resolved", "abc123"), base))
	if dl.Status != domain.DeliveryAccepted {
		t.Fatalf("delivery = %+v, want accepted when run_on_resolved is set", dl)
	}
}

func TestDeliverCooldownThenDedupeForGrafana(t *testing.T) {
	f := newFixture(t, domain.TriggerGrafana, func(in *domain.TriggerInput) { in.CooldownS = 600 })
	payload := grafanaPayload("firing", "abc123")

	if dl := mustDeliver(t, f, f.inbound(payload, base)); dl.Status != domain.DeliveryAccepted {
		t.Fatalf("first delivery = %+v", dl)
	}

	// Inside the cooldown window.
	dl := mustDeliver(t, f, f.inbound(payload, base.Add(5*time.Minute)))
	if dl.Status != domain.DeliveryCooldown {
		t.Fatalf("status = %s (%s), want cooldown", dl.Status, dl.Reason)
	}

	// Past the cooldown, but the same alert in the same state: still a
	// duplicate, indefinitely.
	dl = mustDeliver(t, f, f.inbound(payload, base.Add(3*time.Hour)))
	if dl.Status != domain.DeliveryDeduped {
		t.Fatalf("status = %s (%s), want deduped", dl.Status, dl.Reason)
	}

	// A different alert fingerprint is a different key and runs.
	dl = mustDeliver(t, f, f.inbound(grafanaPayload("firing", "deadbeef"), base.Add(3*time.Hour)))
	if dl.Status != domain.DeliveryAccepted {
		t.Fatalf("status = %s (%s), want accepted", dl.Status, dl.Reason)
	}
	if got := f.starter.calls(); len(got) != 2 {
		t.Fatalf("started %d runs, want 2", len(got))
	}
}

func TestDeliverGenericDedupeWindowExpires(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, func(in *domain.TriggerInput) { in.CooldownS = 60 })
	payload := []byte(`{"event":"deploy.finished"}`)

	if dl := mustDeliver(t, f, f.inbound(payload, base)); dl.Status != domain.DeliveryAccepted {
		t.Fatalf("first delivery = %+v", dl)
	}
	dl := mustDeliver(t, f, f.inbound(payload, base.Add(2*time.Hour)))
	if dl.Status != domain.DeliveryDeduped {
		t.Fatalf("status = %s (%s), want deduped within 24h", dl.Status, dl.Reason)
	}
	dl = mustDeliver(t, f, f.inbound(payload, base.Add(25*time.Hour)))
	if dl.Status != domain.DeliveryAccepted {
		t.Fatalf("status = %s (%s), want accepted after the 24h window", dl.Status, dl.Reason)
	}
}

func TestDeliverStormCap(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, func(in *domain.TriggerInput) {
		in.CooldownS = 1
		in.StormCapPerHour = 2
	})

	for i := 0; i < 2; i++ {
		body := []byte(fmt.Sprintf(`{"event":%d}`, i))
		if dl := mustDeliver(t, f, f.inbound(body, base.Add(time.Duration(i)*time.Minute))); dl.Status != domain.DeliveryAccepted {
			t.Fatalf("delivery %d = %+v", i, dl)
		}
	}

	dl := mustDeliver(t, f, f.inbound([]byte(`{"event":99}`), base.Add(3*time.Minute)))
	if dl.Status != domain.DeliveryStorm {
		t.Fatalf("status = %s (%s), want storm", dl.Status, dl.Reason)
	}

	// An hour later the window has rolled over and deliveries run again.
	dl = mustDeliver(t, f, f.inbound([]byte(`{"event":100}`), base.Add(90*time.Minute)))
	if dl.Status != domain.DeliveryAccepted {
		t.Fatalf("status = %s (%s), want accepted once the window rolled over", dl.Status, dl.Reason)
	}
}

func TestDeliverRecordsAFailedRunStart(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, nil)
	f.starter.err = errors.New("no service token")

	dl := mustDeliver(t, f, f.inbound([]byte(`{"a":1}`), base))
	if dl.Status != domain.DeliveryFailed {
		t.Fatalf("status = %s, want failed", dl.Status)
	}
	if !strings.Contains(dl.Reason, "no service token") {
		t.Fatalf("reason = %q", dl.Reason)
	}
	stored, err := f.repos.Deliveries.Get(context.Background(), dl.ID)
	if err != nil {
		t.Fatalf("get delivery: %v", err)
	}
	if stored.Status != domain.DeliveryFailed {
		t.Fatalf("stored status = %s", stored.Status)
	}
	// A failed delivery never blocks the next one.
	f.starter.err = nil
	if dl := mustDeliver(t, f, f.inbound([]byte(`{"a":1}`), base.Add(time.Hour))); dl.Status != domain.DeliveryAccepted {
		t.Fatalf("retry = %+v, want accepted", dl)
	}
}

func TestReplayForcesPastDedupe(t *testing.T) {
	f := newFixture(t, domain.TriggerGrafana, nil)
	ctx := context.Background()
	first := mustDeliver(t, f, f.inbound(grafanaPayload("firing", "abc123"), base))

	// Without forcing, an identical payload would dedupe.
	if dl := mustDeliver(t, f, f.inbound(grafanaPayload("firing", "abc123"), base.Add(time.Second))); dl.Status != domain.DeliveryCooldown {
		t.Fatalf("control delivery = %+v, want cooldown", dl)
	}

	replay, err := f.svc.Replay(ctx, admin, first.ID)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if replay.Status != domain.DeliveryAccepted {
		t.Fatalf("status = %s (%s), want accepted", replay.Status, replay.Reason)
	}
	if replay.Reason != "replay of "+first.ID {
		t.Fatalf("reason = %q", replay.Reason)
	}
	if replay.ID == first.ID {
		t.Fatal("a replay must be logged as its own delivery")
	}
	if string(replay.Payload) != string(first.Payload) {
		t.Fatalf("replayed payload = %s", replay.Payload)
	}
	if got := f.starter.calls(); len(got) != 2 {
		t.Fatalf("started %d runs, want 2", len(got))
	}
}

func TestTestSendsTheSamplePayloadAndCanForce(t *testing.T) {
	f := newFixture(t, domain.TriggerGrafana, nil)
	ctx := context.Background()

	dl, err := f.svc.Test(ctx, admin, f.trigger.ID, nil, false)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if dl.Status != domain.DeliveryAccepted {
		t.Fatalf("status = %s (%s)", dl.Status, dl.Reason)
	}
	if string(dl.Payload) != string(templates.SamplePayload("grafana")) {
		t.Fatalf("payload = %s, want the grafana sample", dl.Payload)
	}

	// Same payload again: deduped without force, accepted with it.
	again, err := f.svc.Test(ctx, admin, f.trigger.ID, nil, false)
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if again.Status != domain.DeliveryCooldown && again.Status != domain.DeliveryDeduped {
		t.Fatalf("status = %s, want a dedupe of some kind", again.Status)
	}
	forced, err := f.svc.Test(ctx, admin, f.trigger.ID, nil, true)
	if err != nil {
		t.Fatalf("Test forced: %v", err)
	}
	if forced.Status != domain.DeliveryAccepted {
		t.Fatalf("forced status = %s (%s)", forced.Status, forced.Reason)
	}
}

func TestDeliveriesAndTriggerVisibility(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, nil)
	ctx := context.Background()

	// A member's own trigger, against their own template.
	tpl, err := f.svc.CreateTemplate(ctx, member, domain.TemplateInput{
		Name: "Member template", WorkspaceID: "ws-1", ProfileID: "investigate", PromptTemplate: "go",
	})
	if err != nil {
		t.Fatalf("create member template: %v", err)
	}
	mine, _, err := f.svc.CreateTrigger(ctx, member, domain.TriggerInput{Name: "Mine", Kind: "generic", TemplateID: tpl.ID})
	if err != nil {
		t.Fatalf("create member trigger: %v", err)
	}

	if _, err := f.svc.GetTrigger(ctx, other, mine.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another member reading it: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.ListDeliveries(ctx, other, mine.ID, 10); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("another member listing deliveries: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.GetTrigger(ctx, admin, mine.ID); err != nil {
		t.Fatalf("admin reading it: %v", err)
	}
	// The shared trigger is visible to everyone but only mutable by an admin.
	if _, err := f.svc.GetTrigger(ctx, member, f.trigger.ID); err != nil {
		t.Fatalf("member reading the shared trigger: %v", err)
	}
	if err := f.svc.DeleteTrigger(ctx, member, f.trigger.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("member deleting the shared trigger: err = %v, want ErrForbidden", err)
	}

	list, err := f.svc.ListTriggers(ctx, member)
	if err != nil {
		t.Fatalf("ListTriggers: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("member sees %d triggers, want their own plus the shared one", len(list))
	}

	// Deliveries are listed newest first, bounded by limit.
	mustDeliver(t, f, f.inbound([]byte(`{"a":1}`), base))
	mustDeliver(t, f, f.inbound([]byte(`{"a":2}`), base.Add(time.Hour)))
	deliveries, err := f.svc.ListDeliveries(ctx, member, f.trigger.ID, 1)
	if err != nil {
		t.Fatalf("ListDeliveries: %v", err)
	}
	if len(deliveries) != 1 || string(deliveries[0].Payload) != `{"a":2}` {
		t.Fatalf("deliveries = %+v", deliveries)
	}
}

func TestUpdateAndDeleteTrigger(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, nil)
	ctx := context.Background()

	disabled := false
	updated, err := f.svc.UpdateTrigger(ctx, admin, f.trigger.ID, domain.TriggerInput{
		Name: "Renamed", Kind: "grafana", TemplateID: f.templateID, CooldownS: 30,
		StormCapPerHour: 3, RunOnResolved: true, Enabled: &disabled,
	})
	if err != nil {
		t.Fatalf("UpdateTrigger: %v", err)
	}
	if updated.Slug != f.trigger.Slug {
		t.Fatalf("slug changed to %q: the webhook URL must be stable", updated.Slug)
	}
	if updated.Name != "Renamed" || updated.Kind != domain.TriggerGrafana || updated.CooldownS != 30 ||
		updated.StormCapPerHour != 3 || !updated.RunOnResolved || updated.Enabled {
		t.Fatalf("updated = %+v", updated)
	}
	if updated.SecretHash != f.trigger.SecretHash {
		t.Fatal("an update must not disturb the secret")
	}

	if _, err := f.svc.UpdateTrigger(ctx, admin, f.trigger.ID, domain.TriggerInput{
		Name: "Renamed", Kind: "carrier-pigeon", TemplateID: f.templateID,
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown kind: err = %v, want ErrInvalid", err)
	}

	if err := f.svc.DeleteTrigger(ctx, admin, f.trigger.ID); err != nil {
		t.Fatalf("DeleteTrigger: %v", err)
	}
	if _, err := f.svc.GetTrigger(ctx, admin, f.trigger.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("after delete: err = %v, want ErrNotFound", err)
	}
}

func TestCreateTriggerRejectsAnInvisibleTemplate(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, nil)
	ctx := context.Background()

	tpl, err := f.svc.CreateTemplate(ctx, member, domain.TemplateInput{
		Name: "Private", WorkspaceID: "ws-1", ProfileID: "investigate", PromptTemplate: "go",
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if _, _, err := f.svc.CreateTrigger(ctx, other, domain.TriggerInput{
		Name: "Sneaky", Kind: "generic", TemplateID: tpl.ID,
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestTemplateCRUDAndVisibility(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, nil)
	ctx := context.Background()

	if _, err := f.svc.CreateTemplate(ctx, member, domain.TemplateInput{Name: "", WorkspaceID: "ws-1", ProfileID: "investigate", PromptTemplate: "x"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty name: err = %v, want ErrInvalid", err)
	}
	if _, err := f.svc.CreateTemplate(ctx, member, domain.TemplateInput{Name: "n", WorkspaceID: "ws-1", ProfileID: "investigate"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty prompt: err = %v, want ErrInvalid", err)
	}
	if _, err := f.svc.CreateTemplate(ctx, member, domain.TemplateInput{Name: "n", WorkspaceID: "nope", ProfileID: "investigate", PromptTemplate: "x"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unknown workspace: err = %v, want ErrInvalid", err)
	}

	// A member asking for a shared template gets an owned one instead.
	mine, err := f.svc.CreateTemplate(ctx, member, domain.TemplateInput{
		Name: "Mine", WorkspaceID: "ws-1", ProfileID: "investigate", PromptTemplate: "x", Shared: true,
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if mine.OwnerID == nil || *mine.OwnerID != member.UserID {
		t.Fatalf("owner = %v, want the member", mine.OwnerID)
	}

	if _, err := f.svc.GetTemplate(ctx, other, mine.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.UpdateTemplate(ctx, member, f.templateID, domain.TemplateInput{
		Name: "hijack", WorkspaceID: "ws-1", ProfileID: "investigate", PromptTemplate: "x",
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("member updating the shared template: err = %v, want ErrForbidden", err)
	}

	updated, err := f.svc.UpdateTemplate(ctx, member, mine.ID, domain.TemplateInput{
		Name: "Mine, renamed", WorkspaceID: "ws-1", ProfileID: "investigate",
		PromptTemplate: "y", SystemPrompt: "s", ReportSchema: `{"type":"object"}`,
	})
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if updated.Name != "Mine, renamed" || updated.PromptTemplate != "y" || updated.SystemPrompt != "s" {
		t.Fatalf("updated = %+v", updated)
	}
	if updated.OwnerID == nil {
		t.Fatal("an update must not silently share a private template")
	}

	if err := f.svc.DeleteTemplate(ctx, other, mine.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := f.svc.DeleteTemplate(ctx, member, mine.ID); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
}

func TestRenderTemplateDryRun(t *testing.T) {
	f := newFixture(t, domain.TriggerGrafana, nil)
	ctx := context.Background()

	tpl, err := f.svc.CreateTemplate(ctx, admin, domain.TemplateInput{
		Name: "Rendered", WorkspaceID: "ws-1", ProfileID: "investigate", Shared: true,
		TitleTemplate: `{{ .status }}: {{ join ", " (alertnames .alerts) }}`,
		PromptTemplate: `{{ range .alerts }}{{ .labels.alertname }} on {{ .labels.instance }}
{{ end }}`,
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	got, err := f.svc.RenderTemplate(ctx, admin, tpl.ID, "grafana", grafanaPayload("firing", "abc123"))
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if len(got.Errors) != 0 {
		t.Fatalf("errors = %v", got.Errors)
	}
	if got.Title != "firing: SystemdUnitFailed" {
		t.Fatalf("title = %q", got.Title)
	}
	if !strings.Contains(got.Prompt, "SystemdUnitFailed on ct-142") {
		t.Fatalf("prompt = %q", got.Prompt)
	}

	// No payload at all falls back to the kind's sample.
	sample, err := f.svc.RenderTemplate(ctx, admin, tpl.ID, "grafana", nil)
	if err != nil {
		t.Fatalf("RenderTemplate sample: %v", err)
	}
	if sample.Prompt == "" {
		t.Fatal("rendering the sample payload produced nothing")
	}

	bad, err := f.svc.RenderTemplate(ctx, admin, tpl.ID, "grafana", []byte("not json"))
	if err != nil {
		t.Fatalf("RenderTemplate invalid: %v", err)
	}
	if len(bad.Errors) == 0 {
		t.Fatal("want a reported error for an unparseable payload")
	}
}

func TestEnsureSeededIsIdempotent(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, nil)
	ctx := context.Background()

	if err := f.svc.EnsureSeeded(ctx, "ws-1"); err != nil {
		t.Fatalf("EnsureSeeded: %v", err)
	}
	if err := f.svc.EnsureSeeded(ctx, "ws-1"); err != nil {
		t.Fatalf("EnsureSeeded again: %v", err)
	}

	all, err := f.svc.ListTemplates(ctx, admin)
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	var seeded int
	for _, tpl := range all {
		if tpl.Name != "Grafana alert investigation" {
			continue
		}
		seeded++
		if tpl.OwnerID != nil {
			t.Fatal("the seeded template must be shared")
		}
		if tpl.WorkspaceID != "ws-1" || tpl.ProfileID != "investigate" {
			t.Fatalf("seeded template = %+v", tpl)
		}
		if tpl.ReportSchema == "" {
			t.Fatal("the seeded template should carry a report schema")
		}
	}
	if seeded != 1 {
		t.Fatalf("seeded the template %d times, want once", seeded)
	}

	if err := f.svc.EnsureSeeded(ctx, ""); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("EnsureSeeded with no workspace: err = %v, want ErrInvalid", err)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Grafana alerts":        "grafana-alerts",
		"  Spaced  Out  ":       "spaced-out",
		"!!!":                   "trigger",
		"Ünïcødé":               "n-c-d",
		"CamelCase/Slash":       "camelcase-slash",
		strings.Repeat("a", 80): strings.Repeat("a", maxSlugLen),
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTemplateLoopFields(t *testing.T) {
	f := newFixture(t, domain.TriggerGeneric, nil)
	ctx := context.Background()

	// A loop with no budget would iterate forever.
	if _, err := f.svc.CreateTemplate(ctx, member, domain.TemplateInput{
		Name: "Endless", WorkspaceID: "ws-1", ProfileID: "investigate", PromptTemplate: "x",
		LoopUntil: "done",
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("loop_until without loop_max: err = %v, want ErrInvalid", err)
	}
	if _, err := f.svc.CreateTemplate(ctx, member, domain.TemplateInput{
		Name: "Endless", WorkspaceID: "ws-1", ProfileID: "investigate", PromptTemplate: "x",
		LoopUntil: "done", LoopMax: 0,
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("loop_max 0: err = %v, want ErrInvalid", err)
	}

	tpl, err := f.svc.CreateTemplate(ctx, member, domain.TemplateInput{
		Name: "Until fixed", WorkspaceID: "ws-1", ProfileID: "investigate", PromptTemplate: "x",
		LoopUntil: " done ", LoopMax: 3,
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if tpl.LoopUntil != "done" || tpl.LoopMax != 3 {
		t.Fatalf("loop fields = %q/%d", tpl.LoopUntil, tpl.LoopMax)
	}

	got, err := f.svc.GetTemplate(ctx, member, tpl.ID)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.LoopUntil != "done" || got.LoopMax != 3 {
		t.Fatalf("stored loop fields = %q/%d", got.LoopUntil, got.LoopMax)
	}

	// A template without loop fields is not a loop, whatever loop_max says.
	plain, err := f.svc.UpdateTemplate(ctx, member, tpl.ID, domain.TemplateInput{
		Name: "Until fixed", WorkspaceID: "ws-1", ProfileID: "investigate", PromptTemplate: "x",
		LoopMax: 7,
	})
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if plain.LoopUntil != "" || plain.LoopMax != 7 {
		t.Fatalf("cleared loop = %q/%d", plain.LoopUntil, plain.LoopMax)
	}

	if _, err := f.svc.UpdateTemplate(ctx, member, tpl.ID, domain.TemplateInput{
		Name: "Until fixed", WorkspaceID: "ws-1", ProfileID: "investigate", PromptTemplate: "x",
		LoopUntil: "done",
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("update to an endless loop: err = %v, want ErrInvalid", err)
	}
}
