// Package domain holds the core Styr data types shared by the database
// layer, services and API — no persistence or transport concerns here.
package domain

import "errors"

// Sentinel errors returned by repositories. Repositories wrap the
// underlying cause with %w so callers can use errors.Is against these.
var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrForbidden = errors.New("forbidden")
	ErrInvalid   = errors.New("invalid")
)
