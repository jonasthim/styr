// Session view header: title, workspace/profile chips, state, model, running
// stats, and the Interrupt/Close/copy-id actions.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Copy, Check, Square, XCircle } from 'lucide-react'
import { api } from '../../api/client'
import type { Session, SessionState } from '../../api/types'

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

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <span className="rounded-full border border-hairline bg-surface-2 px-2! py-0.5! text-[11px] text-fg-secondary">
      {children}
    </span>
  )
}

export function SessionHeader({
  session,
  workspaceName,
  profileName,
}: {
  session: Session
  workspaceName: string
  profileName: string
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
    <div className="flex flex-col gap-2 border-b border-hairline bg-surface-1 px-4! py-3!">
      <div className="flex flex-wrap items-center gap-3">
        <h1 className="min-w-0 truncate text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">
          {session.title || 'Untitled session'}
        </h1>
        <Chip>{workspaceName}</Chip>
        <Chip>{profileName}</Chip>
        <span className="flex items-center gap-1.5 text-[12px] text-fg-secondary">
          <span aria-hidden className={`h-1.5 w-1.5 rounded-full ${STATE_DOT[session.state]}`} />
          {STATE_LABEL[session.state]}
        </span>

        <div className="ml-auto! flex items-center gap-2">
          {session.state === 'running' && (
            <button
              type="button"
              onClick={() => interrupt.mutate()}
              disabled={interrupt.isPending}
              className="flex h-7 items-center gap-1.5 rounded-[var(--radius-1)] border border-hairline px-2.5! text-[12px] font-medium text-fg-secondary transition-colors duration-150 hover:bg-surface-3 hover:text-fg-primary disabled:opacity-50"
            >
              <Square size={12} />
              Interrupt
            </button>
          )}
          <button
            type="button"
            onClick={() => close.mutate()}
            disabled={close.isPending || session.state === 'closed'}
            className="flex h-7 items-center gap-1.5 rounded-[var(--radius-1)] border border-hairline px-2.5! text-[12px] font-medium text-fg-secondary transition-colors duration-150 hover:bg-surface-3 hover:text-fg-primary disabled:opacity-50"
          >
            <XCircle size={12} />
            Close
          </button>
          <button
            type="button"
            onClick={copyId}
            aria-label="Copy session id"
            className="flex h-7 w-7 items-center justify-center rounded-[var(--radius-1)] border border-hairline text-fg-secondary transition-colors duration-150 hover:bg-surface-3 hover:text-fg-primary"
          >
            {copied ? <Check size={13} className="text-state-running" /> : <Copy size={13} />}
          </button>
        </div>
      </div>

      <div className="flex items-center gap-3 font-mono text-[12px] tabular-nums text-fg-muted">
        <span>{session.model}</span>
        <span aria-hidden>·</span>
        <span>
          {session.num_turns} {session.num_turns === 1 ? 'turn' : 'turns'}
        </span>
        <span aria-hidden>·</span>
        <span>${session.cost_usd.toFixed(2)}</span>
        <span aria-hidden>·</span>
        <span>
          {session.tokens_in.toLocaleString()}/{session.tokens_out.toLocaleString()} tokens
        </span>
      </div>
    </div>
  )
}
