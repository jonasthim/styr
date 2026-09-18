package pipelines

import (
	"fmt"

	"github.com/jonasthim/styr/internal/domain"
)

// ValidationError wraps domain.ErrInvalid with the full list of Problems
// that made a pipeline definition invalid, so a caller (internal/api) can
// render every problem — not just the first — as {line, message} pairs.
// errors.Is(err, domain.ErrInvalid) keeps working against it (Unwrap
// returns the sentinel), and errors.As(err, &validationErr) recovers the
// full Problems slice.
type ValidationError struct {
	Problems []Problem
}

// Error satisfies the error interface. It leads with domain.ErrInvalid's
// own text (so a bare %v still reads as "invalid: ...", matching every
// other domain.ErrInvalid-wrapping error in this codebase) followed by the
// first problem, for callers that only log err.Error() rather than
// inspecting Problems.
func (e *ValidationError) Error() string {
	if len(e.Problems) == 0 {
		return domain.ErrInvalid.Error()
	}
	return fmt.Sprintf("%s: %s", domain.ErrInvalid, e.Problems[0].Message)
}

// Unwrap makes errors.Is(err, domain.ErrInvalid) true for a *ValidationError.
func (e *ValidationError) Unwrap() error { return domain.ErrInvalid }
