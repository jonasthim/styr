package schedules

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/jonasthim/styr/internal/domain"
)

// previewCount is how many upcoming fire times Preview returns.
const previewCount = 5

// cronParser accepts the standard 5-field cron expression (minute, hour,
// day-of-month, month, day-of-week — no seconds field) plus the
// descriptors (@hourly, @daily, @every 1h30m, ...).
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// parseCron validates a cron expression, wrapping the underlying parser's
// error in domain.ErrInvalid so callers can report it as a 422.
func parseCron(expr string) (cron.Schedule, error) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: cron expression is required", domain.ErrInvalid)
	}
	sched, err := cronParser.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid cron expression %q: %v", domain.ErrInvalid, expr, err)
	}
	return sched, nil
}

// Preview is the result of previewing a cron expression: its next few
// fire times and a human-readable description.
type Preview struct {
	Next        []time.Time
	Description string
}

// Preview validates cronExpr and returns its next previewCount fire times
// (from the service's clock) together with a hand-written description.
func (s *Service) Preview(cronExpr string) (Preview, error) {
	sched, err := parseCron(cronExpr)
	if err != nil {
		return Preview{}, err
	}
	next := make([]time.Time, 0, previewCount)
	t := s.now()
	for i := 0; i < previewCount; i++ {
		t = sched.Next(t)
		next = append(next, t)
	}
	return Preview{Next: next, Description: describeCron(cronExpr)}, nil
}

// weekdayNames maps a cron day-of-week field (0 and 7 both mean Sunday) to
// its name.
var weekdayNames = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// describeCron renders a cron expression as an English phrase for the
// common forms the Schedules UI cares about, falling back to the raw
// expression for anything else.
func describeCron(expr string) string {
	trimmed := strings.TrimSpace(expr)
	switch trimmed {
	case "@hourly":
		return "every hour"
	case "@daily", "@midnight":
		return "every day at 00:00"
	case "@weekly":
		return "every Sunday at 00:00"
	case "@monthly":
		return "on the 1st of the month at 00:00"
	case "@yearly", "@annually":
		return "once a year on Jan 1 at 00:00"
	}
	if rest, ok := strings.CutPrefix(trimmed, "@every "); ok {
		return "every " + strings.TrimSpace(rest)
	}

	fields := strings.Fields(trimmed)
	if len(fields) != 5 {
		return trimmed
	}
	minute, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]
	rest := dom == "*" && month == "*" && dow == "*"

	switch {
	case minute == "*" && hour == "*" && rest:
		return "every minute"
	case rest && hour == "*":
		if n, ok := everyN(minute); ok {
			return fmt.Sprintf("every %d minutes", n)
		}
		if isPlainNumber(minute) {
			return fmt.Sprintf("every hour at :%s", pad2(minute))
		}
	case rest && isPlainNumber(minute) && isPlainNumber(hour):
		return fmt.Sprintf("every day at %s:%s", pad2(hour), pad2(minute))
	case dom == "*" && month == "*" && isPlainNumber(minute) && isPlainNumber(hour) && isPlainNumber(dow):
		return fmt.Sprintf("every %s at %s:%s", weekdayName(dow), pad2(hour), pad2(minute))
	}
	return trimmed
}

// everyN reports the step of a "*/N" field, if that is its exact shape.
func everyN(field string) (int, bool) {
	rest, ok := strings.CutPrefix(field, "*/")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// isPlainNumber reports whether field is a single non-negative integer,
// with no cron range, list or step syntax.
func isPlainNumber(field string) bool {
	if field == "" {
		return false
	}
	_, err := strconv.Atoi(field)
	return err == nil
}

// pad2 zero-pads a plain numeric cron field to two digits.
func pad2(field string) string {
	n, _ := strconv.Atoi(field)
	return fmt.Sprintf("%02d", n)
}

// weekdayName maps a plain numeric day-of-week field to its name, treating
// 7 (some cron dialects' Sunday) the same as 0.
func weekdayName(field string) string {
	n, _ := strconv.Atoi(field)
	n %= 7
	if n < 0 {
		n += 7
	}
	return weekdayNames[n]
}
