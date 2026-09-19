// Session view header: title, workspace/profile chips, state, the model and
// effort selects (ModelSwitcher.tsx), running stats, the
// Interrupt/Close/copy-id actions, and — for a session running in a worktree —
// the v0.3 changes cluster (commit, PR, checkpoints, discard) on the stats
// row, where the numbers it acts on already are.
//
// Below 900px (the shell's tab-bar breakpoint) the header keeps only what a
// phone needs above the transcript: a back link, the title, the state and
// Close on one row, the model and effort selects on a second. The chips, the
// token counts and the copy-id button are on the Info tab.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronLeft, Copy, Check, Square, XCircle, Zap } from 'lucide-react'
import { api } from '../../api/client'
import type { Session, SessionState } from '../../api/types'
import { Badge, Button, Tooltip } from '../ui'
import { CODEX_SANDBOX_NOTE, harnessLabel, hasApprovals } from '../../lib/harness'
import { ModelSwitcher } from './ModelSwitcher'
import { ReviewActions } from '../review/ReviewActions'

const STATE_LABEL: Record<SessionState, string> = {
  open: 'Open',
  running: 'Running',
  waiting: 'Waiting for you',
  closed: 'Closed',
  failed: 'Failed',
}

const STATE_DOT: Record<SessionState, string> = {
  open: 'bg-state-idle',
  running: 'bg-state-running',
  waiting: 'bg-state-attention',
  closed: 'bg-state-idle',
  failed: 'bg-state-failed',
}

export function SessionHeader({
  session,
  workspaceName,
  profileName,
  summary,
}: {
  session: Session
  workspaceName: string
  profileName: string
  /** The session's last reply, which the PR dialog prefills its body with. */
  summary: string
}) {
  const queryClient = useQueryClient()
  const [copied, setCopied] = useState(false)

  function invalidateSession() {
    void queryClient.invalidateQueries({ queryKey: ['session', session.id] })
  }

  const interrupt = useMutation({
    mutationFn: () => api(`/api/v1/sessions/${session.id}/interrupt`, { method: 'POST' }),
    onSuccess: invalidateSession,
  })
  const close = useMutation({
    mutationFn: () => api(`/api/v1/sessions/${session.id}/close`, { method: 'POST' }),
    onSuccess: invalidateSession,
  })

  function copyId() {
    void navigator.clipboard.writeText(session.id)
    setCopied(true)
    setTimeout(() => setCopied(false), 1200)
  }

  return (
    <div className="flex flex-col gap-1.5 border-b border-hairline bg-surface-1 px-3! py-2! min-[900px]:gap-2 min-[900px]:px-4! min-[900px]:py-3!">
      <div className="flex flex-nowrap items-center gap-x-2 gap-y-2 min-[900px]:flex-wrap min-[900px]:gap-x-3">
        <Link
          to="/sessions"
          aria-label="Back to sessions"
          className="-ml-1 flex h-8 w-8 shrink-0 items-center justify-center text-fg-secondary no-underline hover:text-fg-primary min-[900px]:hidden"
        >
          <ChevronLeft size={18} aria-hidden />
        </Link>
        <h1 className="min-w-0 flex-1 truncate text-[15px] font-semibold tracking-[-0.01em] text-fg-primary min-[900px]:flex-none">
          {session.title || 'Untitled session'}
        </h1>
        <Badge className="max-[899px]:hidden">{workspaceName}</Badge>
        <Badge className="max-[899px]:hidden">{profileName}</Badge>
        {/* Which CLI is actually running matters most on a Codex session,
            where the approval affordances are absent by design - the chip is
            what says why. */}
        <Tooltip label={hasApprovals(session.harness) ? 'Harness' : CODEX_SANDBOX_NOTE}>
          <Badge data-testid="harness-chip" variant="outline" className="max-[899px]:hidden">
            {harnessLabel(session.harness)}
          </Badge>
        </Tooltip>
        <span className="flex items-center gap-1.5 text-[12px] text-fg-secondary">
          <span aria-hidden className={`h-1.5 w-1.5 rounded-full ${STATE_DOT[session.state]}`} />
          {STATE_LABEL[session.state]}
        </span>
        {session.origin === 'webhook' && session.origin_ref && (
          <Link to="/runs/$id" params={{ id: session.origin_ref }} className="no-underline">
            <Badge tone="accent" variant="outline" className="gap-1">
              <Zap size={11} aria-hidden />
              Unattended run
            </Badge>
          </Link>
        )}

        <div className="ml-auto! flex items-center gap-2">
          {session.state === 'running' && (
            <Button
              size="sm"
              onClick={() => interrupt.mutate()}
              loading={interrupt.isPending}
              icon={<Square size={12} aria-hidden />}
            >
              Interrupt
            </Button>
          )}
          <Button
            size="sm"
            onClick={() => close.mutate()}
            disabled={session.state === 'closed'}
            loading={close.isPending}
            icon={<XCircle size={12} aria-hidden />}
          >
            <span className="hidden min-[900px]:inline">Close</span>
          </Button>
          <Tooltip label={copied ? 'Copied' : 'Copy session id'}>
            <Button
              size="sm"
              onClick={copyId}
              aria-label="Copy session id"
              className="max-[899px]:hidden w-7 px-0"
              icon={
                copied ? (
                  <Check size={13} aria-hidden className="text-state-running" />
                ) : (
                  <Copy size={13} aria-hidden />
                )
              }
            />
          </Tooltip>
        </div>
      </div>

      <ModelSwitcher session={session} />

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[12px] tabular-nums text-fg-secondary">
        <span>
          {session.num_turns} {session.num_turns === 1 ? 'turn' : 'turns'}
        </span>
        <span aria-hidden className="text-fg-muted">
          ·
        </span>
        <span>${session.cost_usd.toFixed(2)}</span>
        <span aria-hidden className="hidden text-fg-muted min-[900px]:inline">
          ·
        </span>
        <span className="hidden min-[900px]:inline">
          {session.tokens_in.toLocaleString()}/{session.tokens_out.toLocaleString()} tokens
        </span>
        {/* On a phone the four review buttons do not fit beside the numbers:
            they take a full-width strip that scrolls sideways instead of
            wrapping into a third and fourth header row. */}
        <div className="ml-auto! font-sans max-[899px]:w-full max-[899px]:overflow-x-auto max-[899px]:[scrollbar-width:none]">
          <ReviewActions session={session} summary={summary} />
        </div>
      </div>
    </div>
  )
}
