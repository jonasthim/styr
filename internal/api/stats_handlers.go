package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jonasthim/styr/internal/stats"
)

// defaultGanttWindow and maxGanttWindow bound GET /stats/gantt's [from, to]
// window: the default when both query params are omitted, and the largest
// window the endpoint will compute (a fleet Gantt over events is O(events
// in the window); an unbounded window would let one request read the
// entire events table).
const (
	defaultGanttWindow = 6 * time.Hour
	maxGanttWindow     = 7 * 24 * time.Hour
)

// registerStatsRoutes mounts the read-only fleet stats routes: the Gantt
// (session activity timeline) and the cost dashboard.
func registerStatsRoutes(r chi.Router, d *Deps) {
	r.Get("/stats/gantt", handleStatsGantt(d))
	r.Get("/stats/costs", handleStatsCosts(d))
}

type segmentDTO struct {
	Kind  string    `json:"kind"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type ganttLaneDTO struct {
	SessionID string       `json:"session_id"`
	Title     string       `json:"title"`
	Owner     string       `json:"owner"`
	State     string       `json:"state"`
	Segments  []segmentDTO `json:"segments"`
}

type ganttDTO struct {
	Lanes []ganttLaneDTO `json:"lanes"`
}

func ganttDTOFrom(lanes []stats.Lane) ganttDTO {
	out := ganttDTO{Lanes: make([]ganttLaneDTO, 0, len(lanes))}
	for _, l := range lanes {
		segs := make([]segmentDTO, 0, len(l.Segments))
		for _, s := range l.Segments {
			segs = append(segs, segmentDTO{Kind: s.Kind, Start: s.Start, End: s.End})
		}
		out.Lanes = append(out.Lanes, ganttLaneDTO{
			SessionID: l.SessionID, Title: l.Title, Owner: l.Owner, State: l.State, Segments: segs,
		})
	}
	return out
}

// handleStatsGantt is GET /api/v1/stats/gantt?from=&to= (both RFC3339,
// both optional: to defaults to now, from to 6h before now). 422 when from
// is not strictly before to, or the window exceeds 7 days.
func handleStatsGantt(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		now := time.Now().UTC()
		to := now
		if s := q.Get("to"); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "to must be an RFC3339 timestamp")
				return
			}
			to = t
		}
		from := now.Add(-defaultGanttWindow)
		if s := q.Get("from"); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "from must be an RFC3339 timestamp")
				return
			}
			from = t
		}
		if !from.Before(to) {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "from must be before to")
			return
		}
		if to.Sub(from) > maxGanttWindow {
			writeErrorCode(w, http.StatusUnprocessableEntity, "invalid", "the window may not exceed 7 days")
			return
		}
		lanes, err := d.Stats.Gantt(r.Context(), from, to, actorFrom(r))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, ganttDTOFrom(lanes))
	}
}

type dayCostDTO struct {
	Day      string  `json:"day"`
	USD      float64 `json:"usd"`
	Sessions int     `json:"sessions"`
}

type namedCostDTO struct {
	Name  string  `json:"name"`
	USD   float64 `json:"usd"`
	Count int     `json:"count"`
}

func namedCostDTOsFrom(in []stats.NamedCost) []namedCostDTO {
	out := make([]namedCostDTO, 0, len(in))
	for _, nc := range in {
		out = append(out, namedCostDTO{Name: nc.Name, USD: nc.USD, Count: nc.Count})
	}
	return out
}

type costsDTO struct {
	Days         []dayCostDTO   `json:"days"`
	ByUser       []namedCostDTO `json:"by_user"`
	ByOrigin     []namedCostDTO `json:"by_origin"`
	TopTemplates []namedCostDTO `json:"top_templates"`
	TotalUSD     float64        `json:"total_usd"`
	Window       int            `json:"window"`
}

func costsDTOFrom(c stats.Costs) costsDTO {
	days := make([]dayCostDTO, 0, len(c.Days))
	for _, dc := range c.Days {
		days = append(days, dayCostDTO{Day: dc.Day, USD: dc.USD, Sessions: dc.Sessions})
	}
	return costsDTO{
		Days: days, ByUser: namedCostDTOsFrom(c.ByUser), ByOrigin: namedCostDTOsFrom(c.ByOrigin),
		TopTemplates: namedCostDTOsFrom(c.TopTemplates), TotalUSD: c.TotalUSD, Window: c.Window,
	}
}

// handleStatsCosts is GET /api/v1/stats/costs?days= (default 30, capped at
// 365 by the service).
func handleStatsCosts(d *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		days := 0
		if s := r.URL.Query().Get("days"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				days = n
			}
		}
		c, err := d.Stats.Costs(r.Context(), days, actorFrom(r))
		if err != nil {
			WriteError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, costsDTOFrom(c))
	}
}
