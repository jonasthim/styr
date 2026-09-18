// A plan-mode session ends its turn by asking permission to leave plan mode,
// and that request carries the plan itself. Approving it is the most
// consequential button in the product, so the plan is shown as what it is — a
// checklist of what the session is about to do — rather than as a tool call
// with a JSON blob.
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ClipboardCheck, Square, SquareCheckBig } from 'lucide-react'
import clsx from 'clsx'
import { q } from '../../api/queries'
import { api } from '../../api/client'
import type { Approval } from '../../api/types'
import { Button, Textarea } from '../ui'
import { TextBlock } from './TextBlock'
import { parsePlan, planText, PLAN_TOOL } from './planMarkdown'

/** The plan's own body: prose as markdown, steps as a checklist. Shared with
 * the inbox card, which renders the same thing at a smaller size. */
export function PlanBody({ markdown, compact }: { markdown: string; compact?: boolean }) {
  const chunks = parsePlan(markdown)
  return (
    <div className={clsx('flex flex-col gap-2', compact && 'text-[12px]')}>
      {chunks.map((chunk, i) =>
        chunk.kind === 'prose' ? (
          <TextBlock key={i} text={chunk.text} />
        ) : (
          <ul key={i} className="flex list-none flex-col gap-1">
            {chunk.items.map((step, j) => (
              <li key={j} data-testid="plan-step" className="flex items-start gap-2">
                {step.done ? (
                  <SquareCheckBig size={13} aria-hidden className="mt-0.5 shrink-0 text-state-running" />
                ) : (
                  <Square size={13} aria-hidden className="mt-0.5 shrink-0 text-fg-muted" />
                )}
                <span className={clsx('text-[13px] leading-5 text-fg-primary', compact && 'text-[12px]')}>
                  {step.text}
                </span>
              </li>
            ))}
          </ul>
        ),
      )}
    </div>
  )
}

/** The pending ExitPlanMode approval for a session, if there is one. */
export function usePlanApproval(sessionId: string): { approval: Approval; plan: string } | null {
  const approvalsQuery = useQuery(q.approvals())
  for (const approval of approvalsQuery.data ?? []) {
    if (approval.session_id !== sessionId || approval.state !== 'pending' || approval.tool !== PLAN_TOOL) continue
    const plan = planText(approval.updated_input ?? approval.input)
    if (plan) return { approval, plan }
  }
  return null
}

export function PlanCard({ sessionId }: { sessionId: string }) {
  const queryClient = useQueryClient()
  const pending = usePlanApproval(sessionId)
  const [changes, setChanges] = useState<string | null>(null)

  const decide = useMutation({
    mutationFn: (input: { id: string; decision: 'allow' | 'deny'; message?: string }) =>
      api<void>(`/api/v1/approvals/${input.id}`, {
        method: 'POST',
        json: { decision: input.decision, message: input.message },
      }),
    onSettled: () => {
      setChanges(null)
      void queryClient.invalidateQueries({ queryKey: ['approvals'] })
      void queryClient.invalidateQueries({ queryKey: ['session', sessionId] })
      void queryClient.invalidateQueries({ queryKey: ['session-events', sessionId] })
    },
  })

  if (!pending) return null
  const { approval, plan } = pending

  return (
    // The card shares the main column with the transcript, and on a phone that
    // column is short: the plan body shrinks (never below a couple of lines) so
    // the two actions stay reachable without scrolling the page.
    <div
      data-testid="plan-card"
      className="mx-4! mb-2! flex min-h-0 flex-col gap-3 rounded-[var(--radius-panel)] border border-[var(--accent-border)] bg-surface-1 p-3! shadow-[var(--shadow-card)]"
    >
      <div className="flex shrink-0 items-center gap-2">
        <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-control)] bg-accent-subtle text-accent">
          <ClipboardCheck size={15} aria-hidden />
        </span>
        <h2 className="text-[13px] font-semibold tracking-[-0.01em] text-fg-primary">Plan ready for your approval</h2>
      </div>

      <div className="min-h-12 max-h-64 flex-1 overflow-y-auto pr-1">
        <PlanBody markdown={plan} />
      </div>

      {changes === null ? (
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <Button
            variant="primary"
            size="sm"
            loading={decide.isPending && decide.variables?.decision === 'allow'}
            disabled={decide.isPending}
            onClick={() => decide.mutate({ id: approval.id, decision: 'allow' })}
          >
            Approve plan
          </Button>
          <Button size="sm" disabled={decide.isPending} onClick={() => setChanges('')}>
            Request changes
          </Button>
        </div>
      ) : (
        <div className="flex shrink-0 flex-col gap-2">
          <Textarea
            autoFocus
            aria-label="What should change in the plan?"
            placeholder="Say what to do differently — the session gets this as its answer."
            rows={3}
            value={changes}
            onChange={(e) => setChanges(e.target.value)}
          />
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="primary"
              size="sm"
              disabled={!changes.trim() || decide.isPending}
              loading={decide.isPending && decide.variables?.decision === 'deny'}
              onClick={() => decide.mutate({ id: approval.id, decision: 'deny', message: changes.trim() })}
            >
              Send changes
            </Button>
            <Button size="sm" disabled={decide.isPending} onClick={() => setChanges(null)}>
              Cancel
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
