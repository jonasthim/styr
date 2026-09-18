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

// Sentinel errors returned by the inbound trigger router
// (internal/triggers), mapped by the HTTP layer to 404, 401 and 413
// respectively.
var (
	ErrUnknownTrigger = errors.New("unknown trigger")
	ErrBadSecret      = errors.New("bad secret")
	ErrTooLarge       = errors.New("payload too large")
)
