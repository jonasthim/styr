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
import { RiskBadge } from './RiskBadge'
import { SnoozeMenu, type SnoozeOption } from './SnoozeMenu'
import { relativeTime, summarizeToolInput } from './format'

const ACTION_BUTTON =
  'flex h-11 min-[640px]:h-8 items-center justify-center rounded-[var(--radius-1)] border border-hairline px-3 text-[13px] font-medium transition-colors duration-150 disabled:cursor-not-allowed disabled:opacity-50'

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
        'flex flex-col gap-2 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-3 outline-none transition-[opacity,transform] duration-150 min-[640px]:p-4',
        focused && 'ring-2 ring-accent ring-offset-2 ring-offset-canvas',
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
          {summarizeToolInput(approval.tool, approval.input)}
        </span>
      </div>

      {editing ? (
        <div>
          <textarea
            aria-label={`Editable input for ${approval.tool}`}
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            rows={6}
            className="w-full resize-y rounded-[var(--radius-1)] border border-hairline bg-surface-3 px-3 py-2 font-mono text-[12px] text-fg-primary outline-none focus-visible:border-accent"
          />
          {draftError && <p className="mt-1 text-[12px] text-state-failed">{draftError}</p>}
        </div>
      ) : (
        <ToolInput tool={approval.tool} input={approval.input} />
      )}

      <div className="grid grid-cols-2 gap-2 min-[640px]:flex min-[640px]:flex-wrap">
        {editing ? (
          <>
            <button
              type="button"
              onClick={submit}
              disabled={pending}
              className={clsx(ACTION_BUTTON, 'border-accent bg-accent text-[#0b0d10]')}
            >
              Send
            </button>
            <button type="button" onClick={onCancelEdit} disabled={pending} className={ACTION_BUTTON}>
              Cancel
            </button>
          </>
        ) : (
          <>
            <button
              type="button"
              onClick={onAllow}
              disabled={pending}
              className={clsx(ACTION_BUTTON, 'border-accent bg-accent text-[#0b0d10]')}
            >
              Allow
            </button>
            <button type="button" onClick={onDeny} disabled={pending} className={ACTION_BUTTON}>
              Deny
            </button>
            <button type="button" onClick={startEdit} disabled={pending} className={ACTION_BUTTON}>
              Edit and allow
            </button>
            <SnoozeMenu
              open={snoozeMenuOpen}
              onOpenChange={onSnoozeOpenChange}
              onSelect={onSnoozeSelect}
              disabled={pending}
              buttonClassName={ACTION_BUTTON}
            />
            <Link
              to="/sessions/$id"
              params={{ id: approval.session_id }}
              className={clsx(ACTION_BUTTON, 'col-span-2 min-[640px]:col-span-1')}
            >
              Open session
            </Link>
          </>
        )}
      </div>
    </div>
  )
})
