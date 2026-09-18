package triggers

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/jonasthim/styr/internal/domain"
	"github.com/jonasthim/styr/internal/templates"
)

// CreateTemplate stores a new template owned by actor, or a shared
// (owner-less) one when an admin asks for it.
func (s *Service) CreateTemplate(ctx context.Context, actor Actor, in domain.TemplateInput) (domain.Template, error) {
	if err := validateTemplate(in); err != nil {
		return domain.Template{}, err
	}
	now := s.now()
	tpl := domain.Template{
		ID:             uuid.NewString(),
		OwnerID:        ownerFor(actor, in.Shared),
		Name:           strings.TrimSpace(in.Name),
		WorkspaceID:    in.WorkspaceID,
		ProfileID:      in.ProfileID,
		TitleTemplate:  in.TitleTemplate,
		PromptTemplate: in.PromptTemplate,
		SystemPrompt:   in.SystemPrompt,
		ReportSchema:   in.ReportSchema,
		LoopUntil:      strings.TrimSpace(in.LoopUntil),
		LoopMax:        in.LoopMax,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repos.Templates.Create(ctx, tpl); err != nil {
		return domain.Template{}, wrapConstraint(err)
	}
	return tpl, nil
}

// ListTemplates returns every template visible to actor.
func (s *Service) ListTemplates(ctx context.Context, actor Actor) ([]domain.Template, error) {
	return s.repos.Templates.ListVisible(ctx, actor.UserID, actor.IsAdmin)
}

// GetTemplate returns one template, or domain.ErrNotFound when actor
// cannot see it.
func (s *Service) GetTemplate(ctx context.Context, actor Actor, id string) (domain.Template, error) {
	tpl, err := s.repos.Templates.Get(ctx, id)
	if err != nil {
		return domain.Template{}, err
	}
	if !visibleOwner(actor, tpl.OwnerID) {
		return domain.Template{}, fmt.Errorf("template %s: %w", id, domain.ErrNotFound)
	}
	return *tpl, nil
}

// UpdateTemplate replaces a template's mutable fields. Ownership only ever
// widens: an admin can promote a template to shared, but an update never
// takes a shared template into private ownership.
func (s *Service) UpdateTemplate(ctx context.Context, actor Actor, id string, in domain.TemplateInput) (domain.Template, error) {
	existing, err := s.GetTemplate(ctx, actor, id)
	if err != nil {
		return domain.Template{}, err
	}
	if !mutableBy(actor, existing.OwnerID) {
		return domain.Template{}, fmt.Errorf("%w: only the owner or an admin may update this template", domain.ErrForbidden)
	}
	if err := validateTemplate(in); err != nil {
		return domain.Template{}, err
	}

	updated := existing
	if actor.IsAdmin && in.Shared {
		updated.OwnerID = nil
	}
	updated.Name = strings.TrimSpace(in.Name)
	updated.WorkspaceID = in.WorkspaceID
	updated.ProfileID = in.ProfileID
	updated.TitleTemplate = in.TitleTemplate
	updated.PromptTemplate = in.PromptTemplate
	updated.SystemPrompt = in.SystemPrompt
	updated.ReportSchema = in.ReportSchema
	updated.LoopUntil = strings.TrimSpace(in.LoopUntil)
	updated.LoopMax = in.LoopMax
	updated.UpdatedAt = s.now()

	if err := s.repos.Templates.Update(ctx, updated); err != nil {
		return domain.Template{}, wrapConstraint(err)
	}
	return updated, nil
}

// DeleteTemplate removes a template actor owns (or any template, for an
// admin).
func (s *Service) DeleteTemplate(ctx context.Context, actor Actor, id string) error {
	existing, err := s.GetTemplate(ctx, actor, id)
	if err != nil {
		return err
	}
	if !mutableBy(actor, existing.OwnerID) {
		return fmt.Errorf("%w: only the owner or an admin may delete this template", domain.ErrForbidden)
	}
	return s.repos.Templates.Delete(ctx, id)
}

// RenderTemplate is a dry run: it normalises payload as kind (falling back
// to the kind's sample payload when payload is empty) and renders the
// template's title and prompt against it. Rendering problems come back in
// Errors rather than as an error, so the caller can still show whichever
// half rendered.
func (s *Service) RenderTemplate(ctx context.Context, actor Actor, id string, kind string, payload []byte) (domain.RenderResult, error) {
	tpl, err := s.GetTemplate(ctx, actor, id)
	if err != nil {
		return domain.RenderResult{}, err
	}
	if kind == "" {
		kind = string(domain.TriggerGeneric)
	}
	if len(payload) == 0 {
		payload = templates.SamplePayload(kind)
	}

	var out domain.RenderResult
	vars, err := templates.Normalize(kind, payload)
	if err != nil {
		out.Errors = append(out.Errors, err.Error())
		vars = templates.Vars{"payload": map[string]any{}}
	}
	if tpl.TitleTemplate != "" {
		title, err := templates.Render(tpl.TitleTemplate, vars)
		if err != nil {
			out.Errors = append(out.Errors, err.Error())
		} else {
			out.Title = title
		}
	}
	prompt, err := templates.Render(tpl.PromptTemplate, vars)
	if err != nil {
		out.Errors = append(out.Errors, err.Error())
	} else {
		out.Prompt = prompt
	}
	return out, nil
}

// EnsureSeeded creates the templates a fresh install ships with (currently
// the shared "Grafana alert investigation" template) bound to workspaceID,
// once: a seed whose name already exists among the shared templates is left
// alone, including when an operator has since edited it.
func (s *Service) EnsureSeeded(ctx context.Context, workspaceID string) error {
	if workspaceID == "" {
		return fmt.Errorf("%w: a workspace is required to seed templates", domain.ErrInvalid)
	}
	// ListVisible with an empty user id and no admin flag returns exactly
	// the shared (owner-less) templates.
	existing, err := s.repos.Templates.ListVisible(ctx, "", false)
	if err != nil {
		return err
	}
	have := make(map[string]bool, len(existing))
	for _, tpl := range existing {
		have[tpl.Name] = true
	}

	now := s.now()
	for _, seed := range templates.Seeded() {
		if have[seed.Name] {
			continue
		}
		tpl := domain.Template{
			ID:             uuid.NewString(),
			OwnerID:        nil,
			Name:           seed.Name,
			WorkspaceID:    workspaceID,
			ProfileID:      seed.ProfileID,
			TitleTemplate:  seed.TitleTemplate,
			PromptTemplate: seed.PromptTemplate,
			SystemPrompt:   seed.SystemPrompt,
			ReportSchema:   seed.ReportSchema,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
		if err := s.repos.Templates.Create(ctx, tpl); err != nil {
			return wrapConstraint(err)
		}
		s.logger.Info("triggers: seeded template", "template_id", tpl.ID, "name", tpl.Name, "workspace_id", workspaceID)
	}
	return nil
}

// validateTemplate rejects a template that could not start a session.
// Template syntax itself is not validated here: a template only fails on
// the payload it is rendered against, which RenderTemplate reports.
func validateTemplate(in domain.TemplateInput) error {
	switch {
	case strings.TrimSpace(in.Name) == "":
		return fmt.Errorf("%w: name is required", domain.ErrInvalid)
	case in.WorkspaceID == "":
		return fmt.Errorf("%w: workspace_id is required", domain.ErrInvalid)
	case in.ProfileID == "":
		return fmt.Errorf("%w: profile_id is required", domain.ErrInvalid)
	case strings.TrimSpace(in.PromptTemplate) == "":
		return fmt.Errorf("%w: prompt_template is required", domain.ErrInvalid)
	case strings.TrimSpace(in.LoopUntil) != "" && in.LoopMax < 1:
		// A looping template without a budget would iterate forever.
		return fmt.Errorf("%w: loop_max must be at least 1 when loop_until is set", domain.ErrInvalid)
	}
	return nil
}
