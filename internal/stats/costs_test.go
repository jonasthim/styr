package stats

import (
	"context"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/db"
	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/sessions"
)

// costsFixture seeds two days' worth of sessions (today and yesterday) with
// distinct owners and origins, plus two templates each with one run, shared
// by the admin/member Costs tests below.
type costsFixture struct {
	ws               domain.Workspace
	userA, userB     domain.User
	today, yesterday time.Time
	tplA, tplB       domain.Template
}

func seedCostsFixture(t *testing.T, ctx context.Context, d *db.DB) costsFixture {
	t.Helper()
	ws := seedWorkspace(t, ctx, d, "ws1")
	userA := seedUser(t, ctx, d, "userA", "Alice")
	userB := seedUser(t, ctx, d, "userB", "Bob")

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)

	// Owned by Alice, today, origin ui.
	seedSession(t, ctx, d, seedSessionArgs{
		id: "sess1", workspaceID: ws.ID, ownerID: &userA.ID, title: "alice today",
		state: domain.SessionClosed, origin: domain.OriginUI, costUSD: 1.50,
		createdAt: today, lastActiveAt: today,
	})
	// Owned by Bob, yesterday, origin webhook.
	seedSession(t, ctx, d, seedSessionArgs{
		id: "sess2", workspaceID: ws.ID, ownerID: &userB.ID, title: "bob yesterday",
		state: domain.SessionClosed, origin: domain.OriginWebhook, costUSD: 2.25,
		createdAt: yesterday, lastActiveAt: yesterday,
	})
	// Unattended (nil owner), today, origin schedule.
	seedSession(t, ctx, d, seedSessionArgs{
		id: "sess3", workspaceID: ws.ID, ownerID: nil, title: "unattended today",
		state: domain.SessionClosed, origin: domain.OriginSchedule, costUSD: 0.75,
		createdAt: today, lastActiveAt: today,
	})
	// Outside any reasonable window: 40 days back, owned by Alice.
	seedSession(t, ctx, d, seedSessionArgs{
		id: "sessOld", workspaceID: ws.ID, ownerID: &userA.ID, title: "old",
		state: domain.SessionClosed, origin: domain.OriginUI, costUSD: 100,
		createdAt: today.AddDate(0, 0, -40), lastActiveAt: today.AddDate(0, 0, -40),
	})

	tplA := seedTemplate(t, ctx, d, "tplA", ws.ID, "template A")
	tplB := seedTemplate(t, ctx, d, "tplB", ws.ID, "template B")
	seedRun(t, ctx, d, "sess1", tplA.ID, 3.00, today) // Alice's session
	seedRun(t, ctx, d, "sess2", tplB.ID, 4.00, today) // Bob's session

	return costsFixture{ws: ws, userA: userA, userB: userB, today: today, yesterday: yesterday, tplA: tplA, tplB: tplB}
}

func namedCostByName(rows []NamedCost, name string) (NamedCost, bool) {
	for _, r := range rows {
		if r.Name == name {
			return r, true
		}
	}
	return NamedCost{}, false
}

func TestCosts_Admin_ByDayUserOriginAndTopTemplates(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	fx := seedCostsFixture(t, ctx, d)

	svc := newService(t, d)
	costs, err := svc.Costs(ctx, 2, sessions.Actor{IsAdmin: true})
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}

	if costs.Window != 2 {
		t.Fatalf("Window = %d, want 2", costs.Window)
	}
	if len(costs.Days) != 2 {
		t.Fatalf("len(Days) = %d, want 2", len(costs.Days))
	}
	wantYesterday := fx.yesterday.Format(dayLayout)
	wantToday := fx.today.Format(dayLayout)
	if costs.Days[0].Day != wantYesterday || costs.Days[1].Day != wantToday {
		t.Fatalf("Days = %+v, want days %q then %q", costs.Days, wantYesterday, wantToday)
	}
	if costs.Days[0].USD != 2.25 || costs.Days[0].Sessions != 1 {
		t.Fatalf("yesterday = %+v, want USD 2.25, Sessions 1", costs.Days[0])
	}
	if costs.Days[1].USD != 2.25 || costs.Days[1].Sessions != 2 {
		t.Fatalf("today = %+v, want USD 2.25, Sessions 2", costs.Days[1])
	}
	if got, want := costs.TotalUSD, 4.50; got != want {
		t.Fatalf("TotalUSD = %v, want %v (100-cost session outside the window must be excluded)", got, want)
	}

	alice, ok := namedCostByName(costs.ByUser, "Alice")
	if !ok || alice.USD != 1.50 || alice.Count != 1 {
		t.Fatalf("ByUser Alice = %+v, ok=%v, want USD 1.50 Count 1", alice, ok)
	}
	bob, ok := namedCostByName(costs.ByUser, "Bob")
	if !ok || bob.USD != 2.25 || bob.Count != 1 {
		t.Fatalf("ByUser Bob = %+v, ok=%v, want USD 2.25 Count 1", bob, ok)
	}
	unattended, ok := namedCostByName(costs.ByUser, "unattended")
	if !ok || unattended.USD != 0.75 || unattended.Count != 1 {
		t.Fatalf("ByUser unattended = %+v, ok=%v, want USD 0.75 Count 1", unattended, ok)
	}

	ui, ok := namedCostByName(costs.ByOrigin, "ui")
	if !ok || ui.USD != 1.50 {
		t.Fatalf("ByOrigin ui = %+v, ok=%v, want USD 1.50", ui, ok)
	}
	webhook, ok := namedCostByName(costs.ByOrigin, "webhook")
	if !ok || webhook.USD != 2.25 {
		t.Fatalf("ByOrigin webhook = %+v, ok=%v, want USD 2.25", webhook, ok)
	}
	schedule, ok := namedCostByName(costs.ByOrigin, "schedule")
	if !ok || schedule.USD != 0.75 {
		t.Fatalf("ByOrigin schedule = %+v, ok=%v, want USD 0.75", schedule, ok)
	}

	if len(costs.TopTemplates) != 2 {
		t.Fatalf("TopTemplates = %+v, want 2 entries", costs.TopTemplates)
	}
	if costs.TopTemplates[0].Name != "template B" || costs.TopTemplates[0].USD != 4.00 {
		t.Fatalf("TopTemplates[0] = %+v, want template B at 4.00 (admin sees every run)", costs.TopTemplates[0])
	}
	if costs.TopTemplates[1].Name != "template A" || costs.TopTemplates[1].USD != 3.00 {
		t.Fatalf("TopTemplates[1] = %+v, want template A at 3.00", costs.TopTemplates[1])
	}
}

func TestCosts_Member_SeesOwnAndUnattendedOnly(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	fx := seedCostsFixture(t, ctx, d)

	svc := newService(t, d)
	costs, err := svc.Costs(ctx, 2, sessions.Actor{UserID: fx.userA.ID})
	if err != nil {
		t.Fatalf("Costs: %v", err)
	}

	// Alice owns sess1 (today, 1.50) and can see the unattended sess3
	// (today, 0.75); Bob's sess2 (yesterday, 2.25) must not count.
	if got, want := costs.TotalUSD, 2.25; got != want {
		t.Fatalf("TotalUSD = %v, want %v", got, want)
	}
	if costs.Days[0].USD != 0 || costs.Days[0].Sessions != 0 {
		t.Fatalf("yesterday = %+v, want zero (Bob's session is not visible)", costs.Days[0])
	}
	if costs.Days[1].USD != 2.25 || costs.Days[1].Sessions != 2 {
		t.Fatalf("today = %+v, want USD 2.25, Sessions 2", costs.Days[1])
	}
	if _, ok := namedCostByName(costs.ByUser, "Bob"); ok {
		t.Fatalf("ByUser must not include Bob for a member actor: %+v", costs.ByUser)
	}

	// Only template A's run belongs to a visible session (sess1, Alice's).
	if len(costs.TopTemplates) != 1 || costs.TopTemplates[0].Name != "template A" {
		t.Fatalf("TopTemplates = %+v, want only template A", costs.TopTemplates)
	}
}

func TestCosts_DefaultAndMaxWindow(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	seedWorkspace(t, ctx, d, "ws1")

	svc := newService(t, d)

	costs, err := svc.Costs(ctx, 0, sessions.Actor{IsAdmin: true})
	if err != nil {
		t.Fatalf("Costs(0): %v", err)
	}
	if costs.Window != defaultCostsWindow {
		t.Fatalf("Window = %d, want default %d", costs.Window, defaultCostsWindow)
	}
	if len(costs.Days) != defaultCostsWindow {
		t.Fatalf("len(Days) = %d, want %d", len(costs.Days), defaultCostsWindow)
	}

	costs, err = svc.Costs(ctx, 10000, sessions.Actor{IsAdmin: true})
	if err != nil {
		t.Fatalf("Costs(10000): %v", err)
	}
	if costs.Window != maxCostsWindow {
		t.Fatalf("Window = %d, want capped max %d", costs.Window, maxCostsWindow)
	}
}
