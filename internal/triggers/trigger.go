package triggers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
)

// secretPrefix marks a Styr-issued webhook secret, mirroring the
// `styr_pat_` prefix used for API tokens.
const secretPrefix = "styr_whs_"

// secretBytes is how much entropy a webhook secret carries.
const secretBytes = 32

// defaultCooldownS and defaultStormCap match the column defaults in
// migration 00003, applied here when a create omits them.
const (
	defaultCooldownS = 600
	defaultStormCap  = 10
)

// maxSlugLen keeps a generated slug short enough to read in a URL.
const maxSlugLen = 48

// slugStrip matches every run of characters not allowed in a slug.
var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// CreateTrigger stores a new trigger and returns it together with its
// secret, which is shown to the operator exactly once: only the sha256 hash
// and a six-character hint are persisted.
func (s *Service) CreateTrigger(ctx context.Context, actor Actor, in domain.TriggerInput) (domain.Trigger, string, error) {
	kind, err := validateTriggerKind(in.Kind)
	if err != nil {
		return domain.Trigger{}, "", err
	}
	if strings.TrimSpace(in.Name) == "" {
		return domain.Trigger{}, "", fmt.Errorf("%w: name is required", domain.ErrInvalid)
	}
	if in.TemplateID == "" {
		return domain.Trigger{}, "", fmt.Errorf("%w: template_id is required", domain.ErrInvalid)
	}
	// A trigger may only point at a template the actor can see; the FK
	// alone would happily accept someone else's private template.
	if _, err := s.GetTemplate(ctx, actor, in.TemplateID); err != nil {
		return domain.Trigger{}, "", err
	}

	slug, err := s.uniqueSlug(ctx, slugify(in.Name))
	if err != nil {
		return domain.Trigger{}, "", err
	}
	secret, err := newSecret()
	if err != nil {
		return domain.Trigger{}, "", err
	}

	now := s.now()
	tr := domain.Trigger{
		ID:                uuid.NewString(),
		OwnerID:           ownerFor(actor, in.Shared),
		Name:              strings.TrimSpace(in.Name),
		Slug:              slug,
		Kind:              kind,
		SecretHash:        hashSecret(secret),
		SecretHint:        secretHint(secret),
		TemplateID:        in.TemplateID,
		Enabled:           in.Enabled == nil || *in.Enabled,
		DedupeKeyTemplate: in.DedupeKeyTemplate,
		CooldownS:         orDefault(in.CooldownS, defaultCooldownS),
		StormCapPerHour:   orDefault(in.StormCapPerHour, defaultStormCap),
		RunOnResolved:     in.RunOnResolved,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.repos.Triggers.Create(ctx, tr); err != nil {
		return domain.Trigger{}, "", wrapConstraint(err)
	}
	return tr, secret, nil
}

// ListTriggers returns every trigger visible to actor.
func (s *Service) ListTriggers(ctx context.Context, actor Actor) ([]domain.Trigger, error) {
	return s.repos.Triggers.ListVisible(ctx, actor.UserID, actor.IsAdmin)
}

// GetTrigger returns one trigger, or domain.ErrNotFound when actor cannot
// see it.
func (s *Service) GetTrigger(ctx context.Context, actor Actor, id string) (domain.Trigger, error) {
	tr, err := s.repos.Triggers.Get(ctx, id)
	if err != nil {
		return domain.Trigger{}, err
	}
	if !visibleOwner(actor, tr.OwnerID) {
		return domain.Trigger{}, fmt.Errorf("trigger %s: %w", id, domain.ErrNotFound)
	}
	return *tr, nil
}

// UpdateTrigger replaces a trigger's mutable configuration. The slug is
// deliberately immutable: it is the public webhook URL, and renaming a
// trigger must not silently break the sender configured against it.
func (s *Service) UpdateTrigger(ctx context.Context, actor Actor, id string, in domain.TriggerInput) (domain.Trigger, error) {
	existing, err := s.mutableTrigger(ctx, actor, id, "update")
	if err != nil {
		return domain.Trigger{}, err
	}
	kind, err := validateTriggerKind(in.Kind)
	if err != nil {
		return domain.Trigger{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return domain.Trigger{}, fmt.Errorf("%w: name is required", domain.ErrInvalid)
	}
	if in.TemplateID == "" {
		return domain.Trigger{}, fmt.Errorf("%w: template_id is required", domain.ErrInvalid)
	}
	if in.TemplateID != existing.TemplateID {
		if _, err := s.GetTemplate(ctx, actor, in.TemplateID); err != nil {
			return domain.Trigger{}, err
		}
	}

	updated := existing
	if actor.IsAdmin && in.Shared {
		updated.OwnerID = nil
	}
	updated.Name = strings.TrimSpace(in.Name)
	updated.Kind = kind
	updated.TemplateID = in.TemplateID
	updated.DedupeKeyTemplate = in.DedupeKeyTemplate
	updated.CooldownS = orDefault(in.CooldownS, defaultCooldownS)
	updated.StormCapPerHour = orDefault(in.StormCapPerHour, defaultStormCap)
	updated.RunOnResolved = in.RunOnResolved
	if in.Enabled != nil {
		updated.Enabled = *in.Enabled
	}
	updated.UpdatedAt = s.now()

	if err := s.repos.Triggers.Update(ctx, updated); err != nil {
		return domain.Trigger{}, wrapConstraint(err)
	}
	return updated, nil
}

// DeleteTrigger removes a trigger and, by cascade, its deliveries.
func (s *Service) DeleteTrigger(ctx context.Context, actor Actor, id string) error {
	if _, err := s.mutableTrigger(ctx, actor, id, "delete"); err != nil {
		return err
	}
	return s.repos.Triggers.Delete(ctx, id)
}

// RotateSecret issues a fresh secret for a trigger, invalidating the old
// one immediately, and returns it for the one time it can be read.
func (s *Service) RotateSecret(ctx context.Context, actor Actor, id string) (string, error) {
	if _, err := s.mutableTrigger(ctx, actor, id, "rotate the secret of"); err != nil {
		return "", err
	}
	secret, err := newSecret()
	if err != nil {
		return "", err
	}
	if err := s.repos.Triggers.SetSecret(ctx, id, hashSecret(secret), secretHint(secret)); err != nil {
		return "", err
	}
	s.logger.Info("triggers: secret rotated", "trigger_id", id, "actor", actor.UserID)
	return secret, nil
}

// mutableTrigger loads a trigger actor may change, or returns
// domain.ErrNotFound (invisible) / domain.ErrForbidden (visible but not
// theirs). verb names the attempted operation in the error message.
func (s *Service) mutableTrigger(ctx context.Context, actor Actor, id, verb string) (domain.Trigger, error) {
	tr, err := s.GetTrigger(ctx, actor, id)
	if err != nil {
		return domain.Trigger{}, err
	}
	if !mutableBy(actor, tr.OwnerID) {
		return domain.Trigger{}, fmt.Errorf("%w: only the owner or an admin may %s this trigger", domain.ErrForbidden, verb)
	}
	return tr, nil
}

// uniqueSlug returns base, or base-2, base-3, ... when a trigger already
// owns it.
func (s *Service) uniqueSlug(ctx context.Context, base string) (string, error) {
	candidate := base
	for n := 2; n < 1000; n++ {
		_, err := s.repos.Triggers.GetBySlug(ctx, candidate)
		if errors.Is(err, domain.ErrNotFound) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
	return "", fmt.Errorf("%w: too many triggers named like %q", domain.ErrConflict, base)
}

// slugify turns a trigger name into a URL path segment.
func slugify(name string) string {
	s := slugStrip.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-")
	if len(s) > maxSlugLen {
		s = strings.Trim(s[:maxSlugLen], "-")
	}
	if s == "" {
		s = "trigger"
	}
	return s
}

// newSecret mints a webhook secret: the styr_whs_ prefix plus 32 random
// bytes in unpadded base64url.
func newSecret() (string, error) {
	buf := make([]byte, secretBytes)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", fmt.Errorf("triggers: generate secret: %w", err)
	}
	return secretPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashSecret returns the hex sha256 of a secret: what the triggers table
// stores, and what an inbound secret is compared against.
func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// secretHint returns the safe-to-display tail of a secret.
func secretHint(secret string) string {
	if len(secret) <= 6 {
		return secret
	}
	return secret[len(secret)-6:]
}

// secretMatches compares a presented secret against a stored hash in
// constant time.
func secretMatches(presented, storedHash string) bool {
	if presented == "" || storedHash == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(hashSecret(presented)), []byte(storedHash)) == 1
}

// validateTriggerKind accepts the three inbound shapes the schema allows.
func validateTriggerKind(kind string) (domain.TriggerKind, error) {
	switch domain.TriggerKind(kind) {
	case domain.TriggerGeneric, domain.TriggerGrafana, domain.TriggerGitHub:
		return domain.TriggerKind(kind), nil
	case "":
		return domain.TriggerGeneric, nil
	default:
		return "", fmt.Errorf("%w: unknown trigger kind %q", domain.ErrInvalid, kind)
	}
}

// orDefault applies a column default to a non-positive input value.
func orDefault(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}
