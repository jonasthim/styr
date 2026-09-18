package stats

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jonasthim/styr/internal/sessions"
)

// defaultCostsWindow and maxCostsWindow bound Costs' days parameter.
const (
	defaultCostsWindow = 30
	maxCostsWindow     = 365
	topTemplatesLimit  = 5
	dayLayout          = "2006-01-02"
)

// DayCost is one day's totals in the cost dashboard.
type DayCost struct {
	Day      string
	USD      float64
	Sessions int
}

// NamedCost is one row of a cost breakdown (by owner, by origin, or by
// template): a name, its summed cost and how many rows contributed to it.
type NamedCost struct {
	Name  string
	USD   float64
	Count int
}

// Costs is the cost dashboard: totals per day, per owning user, per origin
// and the top templates by spend, over a trailing window of days.
type Costs struct {
	Days         []DayCost
	ByUser       []NamedCost
	ByOrigin     []NamedCost
	TopTemplates []NamedCost
	TotalUSD     float64
	Window       int
}

// Costs aggregates sessions.cost_usd over the trailing `days` days (default
// 30, capped at 365) by day, by owning user and by origin, plus the top 5
// templates by summed runs.cost_usd. Members see only their own sessions'
// costs plus unattended (no-owner) ones; admins see every session's costs —
// the same visibility rule Sessions.ListVisible already applies, so Costs
// reuses it rather than duplicating the owner-match/owner-nil/admin clause.
func (s *Service) Costs(ctx context.Context, days int, actor sessions.Actor) (Costs, error) {
	window := days
	if window <= 0 {
		window = defaultCostsWindow
	}
	if window > maxCostsWindow {
		window = maxCostsWindow
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	since := today.AddDate(0, 0, -(window - 1))

	all, err := s.sessionsRepo.ListVisible(ctx, actor.UserID, actor.IsAdmin)
	if err != nil {
		return Costs{}, fmt.Errorf("costs: list sessions: %w", err)
	}

	dayOrder := make([]string, window)
	dayIndex := make(map[string]*DayCost, window)
	for i := 0; i < window; i++ {
		d := since.AddDate(0, 0, i).Format(dayLayout)
		dayOrder[i] = d
		dayIndex[d] = &DayCost{Day: d}
	}

	ownerTotals := map[string]*NamedCost{}
	originTotals := map[string]*NamedCost{}
	var total float64

	for _, sess := range all {
		if sess.LastActiveAt.Before(since) {
			continue
		}
		if dc, ok := dayIndex[sess.LastActiveAt.UTC().Format(dayLayout)]; ok {
			dc.USD += sess.CostUSD
			dc.Sessions++
		}

		owner := ownerKey(sess.OwnerID)
		if ownerTotals[owner] == nil {
			ownerTotals[owner] = &NamedCost{}
		}
		ownerTotals[owner].USD += sess.CostUSD
		ownerTotals[owner].Count++

		origin := string(sess.Origin)
		if originTotals[origin] == nil {
			originTotals[origin] = &NamedCost{Name: origin}
		}
		originTotals[origin].USD += sess.CostUSD
		originTotals[origin].Count++

		total += sess.CostUSD
	}

	days2 := make([]DayCost, len(dayOrder))
	for i, d := range dayOrder {
		days2[i] = *dayIndex[d]
	}

	byOrigin := make([]NamedCost, 0, len(originTotals))
	for _, nc := range originTotals {
		byOrigin = append(byOrigin, *nc)
	}
	sortByUSDDesc(byOrigin)

	topTemplates, err := s.topTemplates(ctx, since, actor)
	if err != nil {
		return Costs{}, err
	}

	return Costs{
		Days:         days2,
		ByUser:       s.namedByOwner(ctx, ownerTotals),
		ByOrigin:     byOrigin,
		TopTemplates: topTemplates,
		TotalUSD:     total,
		Window:       window,
	}, nil
}

// namedByOwner resolves each owner key in totals (see ownerKey: "" for
// unattended sessions) to a NamedCost carrying its display name, one lookup
// per distinct owner rather than per session.
func (s *Service) namedByOwner(ctx context.Context, totals map[string]*NamedCost) []NamedCost {
	cache := make(map[string]string, len(totals))
	out := make([]NamedCost, 0, len(totals))
	for owner, nc := range totals {
		name, ok := cache[owner]
		if !ok {
			name = s.ownerDisplayName(ctx, owner)
			cache[owner] = name
		}
		out = append(out, NamedCost{Name: name, USD: nc.USD, Count: nc.Count})
	}
	sortByUSDDesc(out)
	return out
}

// ownerDisplayName resolves a single owner key (see ownerKey) to a display
// name: "unattended" for no owner, the user's display name (falling back to
// email, then to the raw id if the user can no longer be loaded).
func (s *Service) ownerDisplayName(ctx context.Context, owner string) string {
	if owner == "" {
		return "unattended"
	}
	u, err := s.users.GetByID(ctx, owner)
	if err != nil {
		s.logger.Warn("stats: resolve owner display name failed", "user_id", owner, "error", err)
		return owner
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Email
}

// topTemplates returns the top templatesLimit templates by summed
// runs.cost_usd for runs started since `since`, restricted to runs whose
// session is visible to actor (own sessions plus unattended ones for a
// member, everything for an admin — the same rule Sessions.ListVisible
// applies). A direct join rather than N+1 repo calls, since this aggregates
// across every run in the window.
func (s *Service) topTemplates(ctx context.Context, since time.Time, actor sessions.Actor) ([]NamedCost, error) {
	query := `
		SELECT t.name, SUM(r.cost_usd) AS usd, COUNT(*) AS cnt
		FROM runs r
		JOIN sessions s ON s.id = r.session_id
		JOIN templates t ON t.id = r.template_id
		WHERE r.started_at >= ? AND r.template_id IS NOT NULL`
	args := []any{formatTime(since)}
	if !actor.IsAdmin {
		query += ` AND (s.owner_user_id = ? OR s.owner_user_id IS NULL)`
		args = append(args, actor.UserID)
	}
	query += ` GROUP BY t.id ORDER BY usd DESC LIMIT ?`
	args = append(args, topTemplatesLimit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("costs: top templates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []NamedCost
	for rows.Next() {
		var nc NamedCost
		if err := rows.Scan(&nc.Name, &nc.USD, &nc.Count); err != nil {
			return nil, fmt.Errorf("costs: scan top template: %w", err)
		}
		out = append(out, nc)
	}
	return out, rows.Err()
}

// sortByUSDDesc sorts a NamedCost slice by USD descending, in place.
func sortByUSDDesc(rows []NamedCost) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].USD > rows[j].USD })
}
