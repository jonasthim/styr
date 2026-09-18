package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/api"
	"github.com/jonasthim/styr/internal/stats"
)

type segmentOut struct {
	Kind  string    `json:"kind"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type ganttLaneOut struct {
	SessionID string       `json:"session_id"`
	Title     string       `json:"title"`
	Owner     string       `json:"owner"`
	State     string       `json:"state"`
	Segments  []segmentOut `json:"segments"`
}

type ganttOut struct {
	Lanes []ganttLaneOut `json:"lanes"`
}

type dayCostOut struct {
	Day      string  `json:"day"`
	USD      float64 `json:"usd"`
	Sessions int     `json:"sessions"`
}

type namedCostOut struct {
	Name  string  `json:"name"`
	USD   float64 `json:"usd"`
	Count int     `json:"count"`
}

type costsOut struct {
	Days         []dayCostOut   `json:"days"`
	ByUser       []namedCostOut `json:"by_user"`
	ByOrigin     []namedCostOut `json:"by_origin"`
	TopTemplates []namedCostOut `json:"top_templates"`
	TotalUSD     float64        `json:"total_usd"`
	Window       int            `json:"window"`
}

func TestStatsGantt_OK(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	e.stats.GanttFn = func(context.Context, time.Time, time.Time, api.Actor) ([]stats.Lane, error) {
		return []stats.Lane{{
			SessionID: "sess-1", Title: "investigate", Owner: "unattended", State: "open",
			Segments: []stats.Segment{{Kind: "running", Start: now.Add(-time.Hour), End: now}},
		}}, nil
	}

	var out ganttOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/stats/gantt", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /stats/gantt = %d, want 200", status)
	}
	if len(out.Lanes) != 1 || out.Lanes[0].SessionID != "sess-1" {
		t.Fatalf("out = %+v", out)
	}
	if len(out.Lanes[0].Segments) != 1 || out.Lanes[0].Segments[0].Kind != "running" {
		t.Fatalf("segments = %+v", out.Lanes[0].Segments)
	}
}

// TestStatsGantt_DefaultsToLast6Hours checks the documented default window
// (to=now, from=now-6h) when both query params are omitted.
func TestStatsGantt_DefaultsToLast6Hours(t *testing.T) {
	e := newEnv(t)
	var gotFrom, gotTo time.Time
	e.stats.GanttFn = func(_ context.Context, from, to time.Time, _ api.Actor) ([]stats.Lane, error) {
		gotFrom, gotTo = from, to
		return nil, nil
	}

	before := time.Now().UTC()
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/stats/gantt", nil, nil)
	after := time.Now().UTC()
	if status != http.StatusOK {
		t.Fatalf("GET /stats/gantt = %d, want 200", status)
	}
	if gotTo.Before(before) || gotTo.After(after) {
		t.Fatalf("to = %v, want between %v and %v", gotTo, before, after)
	}
	window := gotTo.Sub(gotFrom)
	if window < 6*time.Hour-time.Second || window > 6*time.Hour+time.Second {
		t.Fatalf("to-from = %v, want ~6h", window)
	}
}

func TestStatsGantt_FromNotBeforeToIs422(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	from := now.Format(time.RFC3339)
	to := now.Add(-time.Hour).Format(time.RFC3339)

	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/stats/gantt?from="+from+"&to="+to, nil, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("GET gantt with from >= to = %d, want 422", status)
	}
}

func TestStatsGantt_WindowOver7DaysIs422(t *testing.T) {
	e := newEnv(t)
	to := time.Now().UTC()
	from := to.Add(-8 * 24 * time.Hour)

	var body errorOut
	status := e.doJSON(e.adminClient, http.MethodGet,
		"/api/v1/stats/gantt?from="+from.Format(time.RFC3339)+"&to="+to.Format(time.RFC3339), nil, &body)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("GET gantt with an 8-day window = %d, want 422", status)
	}
}

func TestStatsCosts_DefaultWindow(t *testing.T) {
	e := newEnv(t)
	var gotDays int
	e.stats.CostsFn = func(_ context.Context, days int, _ api.Actor) (stats.Costs, error) {
		gotDays = days
		return stats.Costs{
			Days:         []stats.DayCost{{Day: "2026-09-19", USD: 1.5, Sessions: 2}},
			ByUser:       []stats.NamedCost{{Name: "unattended", USD: 1.5, Count: 2}},
			ByOrigin:     []stats.NamedCost{{Name: "schedule", USD: 1.5, Count: 2}},
			TopTemplates: []stats.NamedCost{{Name: "grafana", USD: 1.5, Count: 2}},
			TotalUSD:     1.5,
			Window:       30,
		}, nil
	}

	var out costsOut
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/stats/costs", nil, &out)
	if status != http.StatusOK {
		t.Fatalf("GET /stats/costs = %d, want 200", status)
	}
	if gotDays != 0 {
		t.Fatalf("days passed to service = %d, want 0 (no ?days=, service applies its own default)", gotDays)
	}
	if out.Window != 30 || out.TotalUSD != 1.5 {
		t.Fatalf("out = %+v", out)
	}
	if len(out.Days) != 1 || len(out.ByUser) != 1 || len(out.ByOrigin) != 1 || len(out.TopTemplates) != 1 {
		t.Fatalf("out = %+v", out)
	}
}

func TestStatsCosts_PassesDaysThrough(t *testing.T) {
	e := newEnv(t)
	var gotDays int
	e.stats.CostsFn = func(_ context.Context, days int, _ api.Actor) (stats.Costs, error) {
		gotDays = days
		return stats.Costs{Window: days}, nil
	}
	status := e.doJSON(e.adminClient, http.MethodGet, "/api/v1/stats/costs?days=7", nil, nil)
	if status != http.StatusOK {
		t.Fatalf("GET /stats/costs?days=7 = %d, want 200", status)
	}
	if gotDays != 7 {
		t.Fatalf("days passed to service = %d, want 7", gotDays)
	}
}
