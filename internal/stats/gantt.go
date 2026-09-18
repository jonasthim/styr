package stats

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/sessions"
)

// ganttLaneCap is the maximum number of lanes Gantt returns, selected by
// last_active_at descending (docs/superpowers/plans/2026-09-19-styr-v0.4-schedules-loops.md,
// "Fleet Gantt data").
const ganttLaneCap = 200

// ganttLookback is how far before the requested window Gantt reads events
// from, so a segment that started before `from` (e.g. a turn still running
// when the window opens) is reconstructed instead of appearing to start
// exactly at `from`.
const ganttLookback = time.Hour

// minIdleSegment is the shortest gap between busy segments (or between a
// window edge and the nearest busy segment) that is reported as an idle
// segment; shorter gaps are omitted.
const minIdleSegment = 5 * time.Second

// Segment is one span of a session's timeline.
type Segment struct {
	Kind       string // running|waiting|idle
	Start, End time.Time
}

// Lane is one session's row in the fleet Gantt: its identity plus the
// segments that make up its timeline within the requested window.
type Lane struct {
	SessionID, Title, Owner, State string
	Segments                       []Segment
}

// Gantt returns one Lane per session visible to actor that was active in
// [from, to]: last_active_at >= from and created_at <= to, capped at 200
// sessions ordered by last_active_at descending. Each lane's segments are
// derived from that session's events in the window (extended backwards by
// ganttLookback so an already-open turn is reconstructed) and clipped to
// [from, to].
func (s *Service) Gantt(ctx context.Context, from, to time.Time, actor sessions.Actor) ([]Lane, error) {
	all, err := s.sessionsRepo.ListVisible(ctx, actor.UserID, actor.IsAdmin)
	if err != nil {
		return nil, fmt.Errorf("gantt: list sessions: %w", err)
	}

	selected := make([]domain.Session, 0, len(all))
	for _, sess := range all {
		if sess.LastActiveAt.Before(from) || sess.CreatedAt.After(to) {
			continue
		}
		selected = append(selected, sess)
	}
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].LastActiveAt.After(selected[j].LastActiveAt)
	})
	if len(selected) > ganttLaneCap {
		selected = selected[:ganttLaneCap]
	}
	if len(selected) == 0 {
		return []Lane{}, nil
	}

	ids := make([]string, len(selected))
	for i, sess := range selected {
		ids[i] = sess.ID
	}
	evByID, err := s.loadEvents(ctx, ids, from.Add(-ganttLookback), to)
	if err != nil {
		return nil, fmt.Errorf("gantt: load events: %w", err)
	}
	owners := s.ownerNames(ctx, selected)

	lanes := make([]Lane, 0, len(selected))
	for _, sess := range selected {
		lanes = append(lanes, Lane{
			SessionID: sess.ID,
			Title:     sess.Title,
			Owner:     owners[ownerKey(sess.OwnerID)],
			State:     string(sess.State),
			Segments:  deriveSegments(evByID[sess.ID], from, to),
		})
	}
	return lanes, nil
}

// eventRow is the minimal shape Gantt needs from the events table: only the
// type (to drive the running/waiting state machine) and when it happened.
type eventRow struct {
	sessionID string
	at        time.Time
	typ       string
}

// loadEvents reads every event for sessionIDs in [from, to], ordered by
// session then seq, in one query rather than one per session (the events
// table can hold many rows per session; N+1 repo calls would not scale to a
// 200-lane Gantt).
func (s *Service) loadEvents(ctx context.Context, sessionIDs []string, from, to time.Time) (map[string][]eventRow, error) {
	placeholders := make([]string, len(sessionIDs))
	args := make([]any, 0, len(sessionIDs)+2)
	for i, id := range sessionIDs {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, formatTime(from), formatTime(to))

	query := `SELECT session_id, at, type FROM events
		WHERE session_id IN (` + strings.Join(placeholders, ",") + `)
		AND at >= ? AND at <= ?
		ORDER BY session_id, seq`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("load events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string][]eventRow, len(sessionIDs))
	for rows.Next() {
		var sessionID, at, typ string
		if err := rows.Scan(&sessionID, &at, &typ); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		out[sessionID] = append(out[sessionID], eventRow{sessionID: sessionID, at: parseTime(at), typ: typ})
	}
	return out, rows.Err()
}

// ownerKey maps a session's nullable owner to the map key used by
// ownerNames: "" for no owner, the user id otherwise.
func ownerKey(id *string) string {
	if id == nil {
		return ""
	}
	return *id
}

// ownerNames resolves the display name of every distinct owner among sess,
// one lookup per distinct owner id (not per session) so a Gantt full of
// sessions from a handful of users costs a handful of lookups, not one per
// lane. A user that can no longer be loaded falls back to their id; a
// lookup failure is logged and does not fail the whole request.
func (s *Service) ownerNames(ctx context.Context, sess []domain.Session) map[string]string {
	out := map[string]string{"": "unattended"}
	for _, se := range sess {
		id := ownerKey(se.OwnerID)
		if id == "" {
			continue
		}
		if _, ok := out[id]; ok {
			continue
		}
		u, err := s.users.GetByID(ctx, id)
		if err != nil {
			s.logger.Warn("stats: resolve owner display name failed", "user_id", id, "error", err)
			out[id] = id
			continue
		}
		name := u.DisplayName
		if name == "" {
			name = u.Email
		}
		out[id] = name
	}
	return out
}

// deriveSegments turns evs (a session's events, already ordered by seq,
// spanning from ganttLookback before `from` through `to`) into a timeline
// clipped to [from, to]: running and waiting segments from the event state
// machine (see busySegments), with the gaps between them (and between the
// window edges and the nearest segment) filled in as idle, omitting idle
// gaps shorter than minIdleSegment.
func deriveSegments(evs []eventRow, from, to time.Time) []Segment {
	busy := busySegments(evs, to)
	clipped := clipSegments(busy, from, to)
	return fillIdle(clipped, from, to)
}

// busySegments runs the running/waiting state machine over evs:
//
//   - "user" closes whatever segment is open and opens "running" (a user
//     turn always starts a running segment; per the plan, "running" lasts
//     until the next result/exit unless a permission_request interrupts it).
//   - "permission_request" closes the open segment (typically "running")
//     and opens "waiting": approvals are not events, so the CLI resuming is
//     approximated by the next tool_use/text/result, per the plan.
//   - "tool_use"/"text" closes an open "waiting" segment and reopens
//     "running" (the approval was answered); they are no-ops while already
//     running.
//   - "result"/"exit" closes whatever segment is open; the turn (or the
//     process) is done until the next user event.
//
// Any segment still open at the end of evs is closed at windowEnd, so an
// in-progress turn shows as running/waiting through the end of the
// requested window rather than being dropped.
func busySegments(evs []eventRow, windowEnd time.Time) []Segment {
	var segs []Segment
	var open *Segment
	closeAt := func(end time.Time) {
		if open == nil {
			return
		}
		open.End = end
		segs = append(segs, *open)
		open = nil
	}
	for _, e := range evs {
		switch e.typ {
		case "user":
			closeAt(e.at)
			open = &Segment{Kind: "running", Start: e.at}
		case "permission_request":
			closeAt(e.at)
			open = &Segment{Kind: "waiting", Start: e.at}
		case "tool_use", "text":
			if open != nil && open.Kind == "waiting" {
				closeAt(e.at)
				open = &Segment{Kind: "running", Start: e.at}
			}
		case "result", "exit":
			closeAt(e.at)
		}
	}
	closeAt(windowEnd)
	return segs
}

// clipSegments clips each of segs to [from, to], dropping any that end up
// with zero or negative length (entirely outside the window).
func clipSegments(segs []Segment, from, to time.Time) []Segment {
	out := make([]Segment, 0, len(segs))
	for _, sg := range segs {
		start, end := sg.Start, sg.End
		if start.Before(from) {
			start = from
		}
		if end.After(to) {
			end = to
		}
		if !end.After(start) {
			continue
		}
		out = append(out, Segment{Kind: sg.Kind, Start: start, End: end})
	}
	return out
}

// fillIdle interleaves segs (already clipped to [from, to], in order) with
// idle segments for the gaps between them and between the window edges and
// the nearest segment, omitting any gap shorter than minIdleSegment.
func fillIdle(segs []Segment, from, to time.Time) []Segment {
	out := make([]Segment, 0, len(segs)*2+1)
	cursor := from
	for _, sg := range segs {
		if sg.Start.Sub(cursor) >= minIdleSegment {
			out = append(out, Segment{Kind: "idle", Start: cursor, End: sg.Start})
		}
		out = append(out, sg)
		cursor = sg.End
	}
	if to.Sub(cursor) >= minIdleSegment {
		out = append(out, Segment{Kind: "idle", Start: cursor, End: to})
	}
	return out
}
