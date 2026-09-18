package domain

import "time"

// ReviewSide names which side of a diff a review comment is anchored to:
// the old (pre-change) or new (post-change) line numbering.
type ReviewSide string

const (
	ReviewSideOld ReviewSide = "old"
	ReviewSideNew ReviewSide = "new"
)

// ValidReviewSide reports whether s is one of the two sides a review
// comment may be anchored to.
func ValidReviewSide(s ReviewSide) bool {
	return s == ReviewSideOld || s == ReviewSideNew
}

// ReviewComment is one inline comment a human left on a session's diff.
// Comments accumulate until they are sent to the session as one review
// message, at which point SentAt is stamped and they are never sent again.
type ReviewComment struct {
	ID        string
	SessionID string
	Path      string
	Line      int
	Side      ReviewSide
	Body      string
	AuthorID  string
	// AuthorName is the author's display name (falling back to their email).
	// It is resolved on read, never stored on the row.
	AuthorName string
	CreatedAt  time.Time
	SentAt     *time.Time
}

// Checkpoint is one commit Styr made in a session's worktree after a turn,
// so the work can be rewound to the state it was in at that point.
type Checkpoint struct {
	ID        string
	SessionID string
	CommitSHA string
	// Turn is the session's cumulative turn count when the checkpoint was
	// taken.
	Turn      int
	Summary   string
	CreatedAt time.Time
}
