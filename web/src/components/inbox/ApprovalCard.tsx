// One pending approval, rows 1-4 per
// docs/superpowers/plans/2026-09-18-styr-v0.1.md, "### Task 18: Inbox page
// (approvals)". The parent (Inbox.tsx) owns focus, mutations and the
// optimistic collapse animation; this component is presentational plus the
// inline "edit and allow" JSON draft.
import { forwardRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import type { Approval } from '../../api/types'
import { ToolInput } from '../common/ToolInput'
import { PlanBody } from '../session/PlanCard'
import { planText, PLAN_TOOL } from '../session/planMarkdown'
import { Button, buttonClasses, Textarea } from '../ui'
import { RiskBadge } from './RiskBadge'
import { SnoozeMenu, type SnoozeOption } from './SnoozeMenu'
import { relativeTime, summarizeToolInput } from './format'

// A phone thumb needs 44px; a desktop row wants the standard 32. `!` because
// both rules set the same property and class order in the attribute is not
// what decides the winner.
const ACTION_HEIGHT = 'h-11! min-[640px]:h-8!'

export interface ApprovalCardProps {
  approval: Approval
  workspaceName: string
  focused: boolean
  collapsing: boolean
  editing: boolean
  pending: boolean
  snoozeMenuOpen: boolean
  onFocus: () => void
  onAllow: () => void
  onDeny: () => void
  onStartEdit: () => void
  onCancelEdit: () => void
  onSubmitEdit: (updatedInput: unknown) => void
  onSnoozeOpenChange: (open: boolean) => void
  onSnoozeSelect: (option: SnoozeOption) => void
}

export const ApprovalCard = forwardRef<HTMLDivElement, ApprovalCardProps>(function ApprovalCard(
  {
    approval,
    workspaceName,
    focused,
    collapsing,
    editing,
    pending,
    snoozeMenuOpen,
    onFocus,
    onAllow,
    onDeny,
    onStartEdit,
    onCancelEdit,
    onSubmitEdit,
    onSnoozeOpenChange,
    onSnoozeSelect,
  },
  ref,
) {
  const [draft, setDraft] = useState('')
  const [draftError, setDraftError] = useState<string | null>(null)
  // A plan-mode session asks to leave plan mode; what it is really asking is
  // "shall I do this?", so the inbox shows the plan rather than the tool call.
  const plan = approval.tool === PLAN_TOOL ? planText(approval.updated_input ?? approval.input) : null

  function startEdit() {
    setDraft(JSON.stringify(approval.updated_input ?? approval.input, null, 2))
    setDraftError(null)
    onStartEdit()
  }

  function submit() {
    try {
      const parsed: unknown = JSON.parse(draft)
      setDraftError(null)
      onSubmitEdit(parsed)
    } catch {
      setDraftError('Invalid JSON')
    }
  }

  return (
    <div
      ref={ref}
      tabIndex={-1}
      onClick={onFocus}
      onFocus={onFocus}
      className={clsx(
        'flex flex-col gap-3 rounded-[var(--radius-panel)] border border-hairline bg-surface-1 p-3 shadow-[var(--shadow-card)] outline-none',
        'transition-[opacity,transform] duration-[var(--duration-base)] min-[640px]:p-4',
        focused && 'border-[var(--accent-border)] ring-2 ring-accent ring-offset-2 ring-offset-canvas',
        collapsing && 'pointer-events-none -translate-y-1 opacity-0',
      )}
    >
      <div className="flex min-w-0 items-baseline gap-2">
        <Link
          to="/sessions/$id"
          params={{ id: approval.session_id }}
          className="min-w-0 flex-1 truncate text-[13px] font-medium text-fg-primary hover:underline"
        >
          {approval.session_title}
        </Link>
        <span className="shrink-0 font-mono text-[11px] text-fg-secondary">{workspaceName}</span>
        <span className="shrink-0 font-mono text-[11px] tabular-nums text-fg-muted">
          {relativeTime(approval.created_at)}
        </span>
      </div>

      <div className="flex min-w-0 items-center gap-2">
        <RiskBadge tier={approval.risk} />
        <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-fg-secondary">
          {plan ? 'Plan awaiting approval' : summarizeToolInput(approval.tool, approval.input)}
        </span>
      </div>

      {editing ? (
        <div>
          <Textarea
            mono
            aria-label={`Editable input for ${approval.tool}`}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            rows={6}
            aria-invalid={draftError ? true : undefined}
            className="resize-y bg-surface-3"
          />
          {draftError && <p className="mt-1.5 text-[12px] text-fg-danger">{draftError}</p>}
        </div>
      ) : plan ? (
        <div data-testid="plan-summary" className="max-h-56 overflow-y-auto rounded-[var(--radius-1)] bg-surface-2 px-3 py-2">
          <PlanBody markdown={plan} compact />
        </div>
      ) : (
        <ToolInput tool={approval.tool} input={approval.input} />
      )}

      <div className="grid grid-cols-2 gap-2 min-[640px]:flex min-[640px]:flex-wrap">
        {editing ? (
          <>
            <Button variant="primary" onClick={submit} disabled={pending} className={ACTION_HEIGHT}>
              Send
            </Button>
            <Button onClick={onCancelEdit} disabled={pending} className={ACTION_HEIGHT}>
              Cancel
            </Button>
          </>
        ) : (
          <>
            <Button variant="primary" onClick={onAllow} disabled={pending} className={ACTION_HEIGHT}>
              Allow
            </Button>
            <Button onClick={onDeny} disabled={pending} className={ACTION_HEIGHT}>
              Deny
            </Button>
            <Button onClick={startEdit} disabled={pending} className={ACTION_HEIGHT}>
              Edit and allow
            </Button>
            <SnoozeMenu
              open={snoozeMenuOpen}
              onOpenChange={onSnoozeOpenChange}
              onSelect={onSnoozeSelect}
              disabled={pending}
              buttonClassName={buttonClasses('secondary', 'md', ACTION_HEIGHT)}
            />
            <Link
              to="/sessions/$id"
              params={{ id: approval.session_id }}
              className={buttonClasses('secondary', 'md', clsx(ACTION_HEIGHT, 'col-span-2 min-[640px]:col-span-1'))}
            >
              Open session
            </Link>
          </>
        )}
      </div>
    </div>
  )
})
