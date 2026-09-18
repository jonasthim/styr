// Session view header: title, workspace/profile chips, state, the model and
// effort selects (ModelSwitcher.tsx), running stats, and the
// Interrupt/Close/copy-id actions.
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { Copy, Check, Square, XCircle, Zap } from 'lucide-react'
import { api } from '../../api/client'
import type { Session, SessionState } from '../../api/types'
import { Badge, Button, Tooltip } from '../ui'
import { ModelSwitcher } from './ModelSwitcher'

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
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <h1 className="min-w-0 truncate text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">
          {session.title || 'Untitled session'}
        </h1>
        <Badge>{workspaceName}</Badge>
        <Badge>{profileName}</Badge>
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
            Close
          </Button>
          <Tooltip label={copied ? 'Copied' : 'Copy session id'}>
            <Button
              size="sm"
              onClick={copyId}
              aria-label="Copy session id"
              className="w-7 px-0"
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
        <span aria-hidden className="text-fg-muted">
          ·
        </span>
        <span>
          {session.tokens_in.toLocaleString()}/{session.tokens_out.toLocaleString()} tokens
        </span>
      </div>
    </div>
  )
}
