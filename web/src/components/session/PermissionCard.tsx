// Compact inline permission prompt shown above the Composer when this session
// has a pending approval. Posts straight to POST /api/v1/approvals/{id} —
// this is Task 20's own component, not components/inbox/ApprovalCard.tsx
// (Task 18, built in parallel): a later integration card may unify them.
import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ShieldAlert } from 'lucide-react'
import { q } from '../../api/queries'
import { api } from '../../api/client'
import type { Approval } from '../../api/types'

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  return target.isContentEditable || target.tagName === 'INPUT' || target.tagName === 'TEXTAREA'
}

function previewInput(input: unknown): string {
  if (input && typeof input === 'object') {
    const obj = input as Record<string, unknown>
    if (typeof obj.command === 'string') return obj.command
    if (typeof obj.file_path === 'string') return obj.file_path
  }
  try {
    return JSON.stringify(input)
  } catch {
    return String(input)
  }
}

export function PermissionCard({ sessionId }: { sessionId: string }) {
  const queryClient = useQueryClient()
  const approvalsQuery = useQuery(q.approvals())
  const [pending, setPending] = useState<'allow' | 'deny' | null>(null)
  const approval = approvalsQuery.data?.find((a) => a.session_id === sessionId && a.state === 'pending')

  const decide = useMutation({
    mutationFn: (input: { id: string; decision: 'allow' | 'deny' }) =>
      api(`/api/v1/approvals/${input.id}`, { method: 'POST', json: { decision: input.decision } }),
    onMutate: (input) => setPending(input.decision),
    onSettled: () => {
      setPending(null)
      void queryClient.invalidateQueries({ queryKey: ['approvals'] })
      void queryClient.invalidateQueries({ queryKey: ['session', sessionId] })
      void queryClient.invalidateQueries({ queryKey: ['session-events', sessionId] })
    },
  })

  useEffect(() => {
    if (!approval) return
    function onKeyDown(e: KeyboardEvent) {
      if (isTypingTarget(e.target) || e.metaKey || e.ctrlKey || e.altKey) return
      if (e.key === 'a') decide.mutate({ id: approval!.id, decision: 'allow' })
      else if (e.key === 'd') decide.mutate({ id: approval!.id, decision: 'deny' })
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [approval?.id])

  if (!approval) return null

  return <PermissionCardView approval={approval} pending={pending} onDecide={(d) => decide.mutate({ id: approval.id, decision: d })} />
}

function PermissionCardView({
  approval,
  pending,
  onDecide,
}: {
  approval: Approval
  pending: 'allow' | 'deny' | null
  onDecide: (decision: 'allow' | 'deny') => void
}) {
  return (
    <div
      data-testid="permission-card"
      className="mx-4! mb-2! flex items-center gap-3 rounded-[var(--radius-2)] border border-state-attention/40 bg-surface-2 px-3! py-2!"
    >
      <ShieldAlert size={16} className="shrink-0 text-state-attention" />
      <div className="min-w-0 flex-1">
        <div className="text-[12px] font-medium text-fg-primary">{approval.tool} wants to run</div>
        <div className="truncate font-mono text-[12px] text-fg-secondary">{previewInput(approval.input)}</div>
      </div>
      <button
        type="button"
        disabled={pending !== null}
        onClick={() => onDecide('deny')}
        className="rounded-[var(--radius-1)] border border-hairline px-3! py-1.5! text-[12px] font-medium text-fg-secondary transition-colors duration-150 hover:bg-surface-3 hover:text-fg-primary disabled:opacity-50"
      >
        Deny <span className="text-fg-muted">(d)</span>
      </button>
      <button
        type="button"
        disabled={pending !== null}
        onClick={() => onDecide('allow')}
        className="rounded-[var(--radius-1)] bg-accent px-3! py-1.5! text-[12px] font-medium text-[#0b0d10] transition-opacity duration-150 hover:opacity-90 disabled:opacity-50"
      >
        Allow <span className="opacity-70">(a)</span>
      </button>
    </div>
  )
}
