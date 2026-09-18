package stats

import (
	"context"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/sessions"
)

func TestGantt_SegmentKindsAndBoundaries(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	ws := seedWorkspace(t, ctx, d, "ws1")
	owner := seedUser(t, ctx, d, "u1", "Alice")

	from := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	t0 := from.Add(1 * time.Minute)
	t1 := t0.Add(10 * time.Second) // tool_use
	t2 := t1.Add(5 * time.Second)  // permission_request
	t3 := t2.Add(30 * time.Second) // text: approval answered
	t4 := t3.Add(20 * time.Second) // result

	sess := seedSession(t, ctx, d, seedSessionArgs{
		id: "sess1", workspaceID: ws.ID, ownerID: &owner.ID, title: "fix the thing",
		state: domain.SessionOpen, origin: domain.OriginUI,
		createdAt: from, lastActiveAt: t4,
	})
	seedEvent(t, ctx, d, sess.ID, 1, t0, "user")
	seedEvent(t, ctx, d, sess.ID, 2, t1, "tool_use")
	seedEvent(t, ctx, d, sess.ID, 3, t2, "permission_request")
	seedEvent(t, ctx, d, sess.ID, 4, t3, "text")
	seedEvent(t, ctx, d, sess.ID, 5, t4, "result")

	svc := newService(t, d)
	lanes, err := svc.Gantt(ctx, from, to, sessions.Actor{IsAdmin: true})
	if err != nil {
		t.Fatalf("Gantt: %v", err)
	}
	if len(lanes) != 1 {
		t.Fatalf("len(lanes) = %d, want 1", len(lanes))
	}
	lane := lanes[0]
	if lane.SessionID != sess.ID || lane.Title != "fix the thing" || lane.Owner != "Alice" || lane.State != string(domain.SessionOpen) {
		t.Fatalf("lane = %+v", lane)
	}

	// Busy segments: running [t0,t2) (user through the permission request),
	// waiting [t2,t3) (blocked until the approval is answered), running
	// [t3,t4) (resumed through the result). The gap from `from` to t0 and
	// from t4 to `to` are both under minIdleSegment... t0 is 1 minute after
	// `from`, so that gap IS idle; t4 to `to` (about 58 minutes) is also
	// idle. So we expect: idle, running, waiting, running, idle.
	wantKinds := []string{"idle", "running", "waiting", "running", "idle"}
	if len(lane.Segments) != len(wantKinds) {
		t.Fatalf("segments = %+v, want kinds %v", lane.Segments, wantKinds)
	}
	for i, want := range wantKinds {
		if lane.Segments[i].Kind != want {
			t.Fatalf("segment[%d].Kind = %q, want %q (segments: %+v)", i, lane.Segments[i].Kind, want, lane.Segments)
		}
	}

	running1 := lane.Segments[1]
	if !running1.Start.Equal(t0) || !running1.End.Equal(t2) {
		t.Fatalf("running[0] = %+v, want [%v,%v)", running1, t0, t2)
	}
	waiting := lane.Segments[2]
	if !waiting.Start.Equal(t2) || !waiting.End.Equal(t3) {
		t.Fatalf("waiting = %+v, want [%v,%v)", waiting, t2, t3)
	}
	running2 := lane.Segments[3]
	if !running2.Start.Equal(t3) || !running2.End.Equal(t4) {
		t.Fatalf("running[1] = %+v, want [%v,%v)", running2, t3, t4)
	}
}

func TestGantt_IdleGapOmission(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	ws := seedWorkspace(t, ctx, d, "ws1")

	from := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)

	// Session A: two turns separated by a 2s gap (< minIdleSegment):
	// user -> result -> [2s] -> user -> result. No idle segment between them.
	a0 := from
	a1 := a0.Add(1 * time.Second) // result
	a2 := a1.Add(2 * time.Second) // user
	a3 := a2.Add(1 * time.Second) // result
	sessA := seedSession(t, ctx, d, seedSessionArgs{
		id: "sessA", workspaceID: ws.ID, title: "short gap", state: domain.SessionOpen,
		origin: domain.OriginUI, createdAt: from, lastActiveAt: a3,
	})
	seedEvent(t, ctx, d, sessA.ID, 1, a0, "user")
	seedEvent(t, ctx, d, sessA.ID, 2, a1, "result")
	seedEvent(t, ctx, d, sessA.ID, 3, a2, "user")
	seedEvent(t, ctx, d, sessA.ID, 4, a3, "result")

	// Session B: same shape, but the gap between the two turns is 10s (>=
	// minIdleSegment): an idle segment must appear between them.
	b0 := from
	b1 := b0.Add(1 * time.Second)  // result
	b2 := b1.Add(10 * time.Second) // user
	b3 := b2.Add(1 * time.Second)  // result
	sessB := seedSession(t, ctx, d, seedSessionArgs{
		id: "sessB", workspaceID: ws.ID, title: "long gap", state: domain.SessionOpen,
		origin: domain.OriginUI, createdAt: from, lastActiveAt: b3,
	})
	seedEvent(t, ctx, d, sessB.ID, 1, b0, "user")
	seedEvent(t, ctx, d, sessB.ID, 2, b1, "result")
	seedEvent(t, ctx, d, sessB.ID, 3, b2, "user")
	seedEvent(t, ctx, d, sessB.ID, 4, b3, "result")

	svc := newService(t, d)
	lanes, err := svc.Gantt(ctx, from, to, sessions.Actor{IsAdmin: true})
	if err != nil {
		t.Fatalf("Gantt: %v", err)
	}
	byID := map[string][]Segment{}
	for _, l := range lanes {
		byID[l.SessionID] = l.Segments
	}

	segsA := byID[sessA.ID]
	// running[a0,a1), running[a2,a3), idle[a3,to) — the 2s gap is omitted.
	if len(segsA) != 3 || segsA[0].Kind != "running" || segsA[1].Kind != "running" || segsA[2].Kind != "idle" {
		t.Fatalf("session A segments = %+v, want running, running, idle (2s gap omitted)", segsA)
	}
	if !segsA[0].End.Equal(a1) || !segsA[1].Start.Equal(a2) {
		t.Fatalf("session A segments should be adjacent across the 2s gap: %+v", segsA)
	}

	segsB := byID[sessB.ID]
	// running[b0,b1), idle[b1,b2), running[b2,b3), idle[b3,to).
	wantKindsB := []string{"running", "idle", "running", "idle"}
	if len(segsB) != len(wantKindsB) {
		t.Fatalf("session B segments = %+v, want kinds %v", segsB, wantKindsB)
	}
	for i, want := range wantKindsB {
		if segsB[i].Kind != want {
			t.Fatalf("session B segment[%d].Kind = %q, want %q (segments: %+v)", i, segsB[i].Kind, want, segsB)
		}
	}
	if !segsB[1].Start.Equal(b1) || !segsB[1].End.Equal(b2) {
		t.Fatalf("session B idle gap = %+v, want [%v,%v)", segsB[1], b1, b2)
	}
}

func TestGantt_Visibility(t *testing.T) {
	ctx := context.Background()
	d := testOpenDB(t)
	ws := seedWorkspace(t, ctx, d, "ws1")
	userA := seedUser(t, ctx, d, "userA", "Alice")
	userB := seedUser(t, ctx, d, "userB", "Bob")

	from := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	active := from.Add(time.Minute)

	seedSession(t, ctx, d, seedSessionArgs{
		id: "sessA", workspaceID: ws.ID, ownerID: &userA.ID, title: "a's session",
		state: domain.SessionOpen, origin: domain.OriginUI, createdAt: from, lastActiveAt: active,
	})
	seedSession(t, ctx, d, seedSessionArgs{
		id: "sessB", workspaceID: ws.ID, ownerID: &userB.ID, title: "b's session",
		state: domain.SessionOpen, origin: domain.OriginUI, createdAt: from, lastActiveAt: active,
	})
	seedSession(t, ctx, d, seedSessionArgs{
		id: "sessUnattended", workspaceID: ws.ID, ownerID: nil, title: "unattended",
		state: domain.SessionOpen, origin: domain.OriginSchedule, createdAt: from, lastActiveAt: active,
	})
	// Outside the window: last_active_at before `from`.
	seedSession(t, ctx, d, seedSessionArgs{
		id: "sessOld", workspaceID: ws.ID, ownerID: &userA.ID, title: "old",
		state: domain.SessionClosed, origin: domain.OriginUI, createdAt: from.Add(-2 * time.Hour), lastActiveAt: from.Add(-time.Hour),
	})

	svc := newService(t, d)

	memberLanes, err := svc.Gantt(ctx, from, to, sessions.Actor{UserID: userA.ID})
	if err != nil {
		t.Fatalf("Gantt (member): %v", err)
	}
	memberIDs := laneIDs(memberLanes)
	wantMember := map[string]bool{"sessA": true, "sessUnattended": true}
	if len(memberIDs) != len(wantMember) {
		t.Fatalf("member sees %v, want %v", memberIDs, wantMember)
	}
	for id := range wantMember {
		if !memberIDs[id] {
			t.Fatalf("member missing session %q; got %v", id, memberIDs)
		}
	}
	if memberIDs["sessB"] || memberIDs["sessOld"] {
		t.Fatalf("member should not see sessB (owned by another user) or sessOld (outside window): %v", memberIDs)
	}

	adminLanes, err := svc.Gantt(ctx, from, to, sessions.Actor{IsAdmin: true})
	if err != nil {
		t.Fatalf("Gantt (admin): %v", err)
	}
	adminIDs := laneIDs(adminLanes)
	for _, id := range []string{"sessA", "sessB", "sessUnattended"} {
		if !adminIDs[id] {
			t.Fatalf("admin missing session %q; got %v", id, adminIDs)
		}
	}
	if adminIDs["sessOld"] {
		t.Fatalf("admin should not see sessOld: outside the window regardless of role: %v", adminIDs)
	}
}

func laneIDs(lanes []Lane) map[string]bool {
	out := make(map[string]bool, len(lanes))
	for _, l := range lanes {
		out[l.SessionID] = true
	}
	return out
}
