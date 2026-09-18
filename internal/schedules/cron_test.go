package schedules

import (
	"errors"
	"testing"
	"time"

	"github.com/jonasthim/styr/internal/domain"
)

func TestParseCron_Valid(t *testing.T) {
	for _, expr := range []string{
		"*/5 * * * *", "0 9 * * 1", "@daily", "@hourly", "@weekly", "@every 1h30m", "  0 0 1 1 *  ",
	} {
		if _, err := parseCron(expr); err != nil {
			t.Errorf("parseCron(%q): %v, want nil error", expr, err)
		}
	}
}

func TestParseCron_Invalid(t *testing.T) {
	for _, expr := range []string{"", "   ", "not a cron", "* * * *", "60 * * * *", "* * * * * *"} {
		_, err := parseCron(expr)
		if !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("parseCron(%q): err = %v, want ErrInvalid", expr, err)
		}
	}
}

func TestParseCron_NextComputation(t *testing.T) {
	sched, err := parseCron("*/5 * * * *")
	if err != nil {
		t.Fatalf("parseCron: %v", err)
	}
	after := time.Date(2026, 9, 19, 10, 2, 0, 0, time.UTC)
	got := sched.Next(after)
	want := time.Date(2026, 9, 19, 10, 5, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("Next(%v) = %v, want %v", after, got, want)
	}

	daily, err := parseCron("@daily")
	if err != nil {
		t.Fatalf("parseCron @daily: %v", err)
	}
	gotDaily := daily.Next(time.Date(2026, 9, 19, 10, 0, 0, 0, time.UTC))
	wantDaily := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	if !gotDaily.Equal(wantDaily) {
		t.Fatalf("Next(@daily) = %v, want %v", gotDaily, wantDaily)
	}
}

func TestDescribeCron(t *testing.T) {
	cases := map[string]string{
		"* * * * *":    "every minute",
		"*/5 * * * *":  "every 5 minutes",
		"*/15 * * * *": "every 15 minutes",
		"0 * * * *":    "every hour at :00",
		"30 * * * *":   "every hour at :30",
		"0 9 * * *":    "every day at 09:00",
		"5 8 * * *":    "every day at 08:05",
		"0 9 * * 1":    "every Monday at 09:00",
		"0 9 * * 0":    "every Sunday at 09:00",
		"@daily":       "every day at 00:00",
		"@midnight":    "every day at 00:00",
		"@hourly":      "every hour",
		"@weekly":      "every Sunday at 00:00",
		"@monthly":     "on the 1st of the month at 00:00",
		"@yearly":      "once a year on Jan 1 at 00:00",
		"@every 1h30m": "every 1h30m",
		"1,2 * * * *":  "1,2 * * * *", // list syntax falls back to the raw expression
		"* * 1 1 *":    "* * 1 1 *",   // unmatched shape falls back to the raw expression
	}
	for expr, want := range cases {
		if got := describeCron(expr); got != want {
			t.Errorf("describeCron(%q) = %q, want %q", expr, got, want)
		}
	}
}

func TestService_Preview(t *testing.T) {
	now := time.Date(2026, 9, 19, 10, 2, 0, 0, time.UTC)
	svc := New(Repos{}, nil, nil, func() time.Time { return now }, nil)

	for _, tc := range []struct {
		expr string
		desc string
	}{
		{"*/5 * * * *", "every 5 minutes"},
		{"0 9 * * 1", "every Monday at 09:00"},
		{"@daily", "every day at 00:00"},
	} {
		preview, err := svc.Preview(tc.expr)
		if err != nil {
			t.Fatalf("Preview(%q): %v", tc.expr, err)
		}
		if len(preview.Next) != 5 {
			t.Fatalf("Preview(%q).Next len = %d, want 5", tc.expr, len(preview.Next))
		}
		for i := 1; i < len(preview.Next); i++ {
			if !preview.Next[i].After(preview.Next[i-1]) {
				t.Fatalf("Preview(%q).Next not increasing: %v", tc.expr, preview.Next)
			}
		}
		if !preview.Next[0].After(now) {
			t.Fatalf("Preview(%q).Next[0] = %v, want after %v", tc.expr, preview.Next[0], now)
		}
		if preview.Description != tc.desc {
			t.Fatalf("Preview(%q).Description = %q, want %q", tc.expr, preview.Description, tc.desc)
		}
	}

	if _, err := svc.Preview("nonsense"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Preview(invalid): err = %v, want ErrInvalid", err)
	}
}
