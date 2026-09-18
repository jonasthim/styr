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

	// ErrUnknownTrigger, ErrBadSecret and ErrTooLarge are returned by
	// internal/triggers.Service.Deliver (and matched with errors.Is by
	// internal/api's inbound hook handler) for the three failure modes
	// POST /hooks/{slug} maps to specific status codes: 404 when the slug
	// does not match an enabled trigger, 401 when the bearer secret (or,
	// for every kind, the shared secret carried in a header or ?secret=) does not match, and 413
	// when the body exceeds the inbound size limit.
	ErrUnknownTrigger = errors.New("unknown trigger")
	ErrBadSecret      = errors.New("bad secret")
	ErrTooLarge       = errors.New("payload too large")
)
