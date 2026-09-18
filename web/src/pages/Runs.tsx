// Runs list (`/runs`): every unattended session a trigger (or a replay/test)
// started, filterable by outcome. Route: /runs (see router.tsx for the
// ?outcome= search param this reads and writes, so a filter is a shareable
// link, not just local state).
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Activity } from 'lucide-react'
import { q } from '../api/queries'
import type { RunOutcome } from '../api/types'
import { RunRow } from '../components/runs/RunRow'
import { OUTCOME_LABEL } from '../components/runs/outcome'
import { Button, EmptyState, PageHeader, Skeleton } from '../components/ui'

const OUTCOMES: RunOutcome[] = ['running', 'needs_human', 'failed', 'success', 'timeout']

function SkeletonRow() {
  return (
    <div className="flex h-[var(--row-h)] items-center gap-3 border-b border-hairline px-3 last:border-b-0">
      <Skeleton className="h-2 w-2 shrink-0 rounded-full" />
      <Skeleton className="h-3 w-48 max-w-[45%]" />
      <Skeleton className="ml-auto h-3 w-12" />
    </div>
  )
}

export function Runs() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/_app/runs' })
  const outcome = search.outcome ?? ''
  const runs = useQuery({ ...q.runs({ outcome }), refetchInterval: 4000 })
  const triggers = useQuery(q.triggers())

  function setOutcome(next: RunOutcome | '') {
    void navigate({ to: '/runs', search: next ? { outcome: next } : {}, replace: true })
  }

  function triggerName(triggerId: string | null): string {
    if (!triggerId) return '—'
    return triggers.data?.find((t) => t.id === triggerId)?.name ?? triggerId
  }

  const isEmpty = runs.isSuccess && runs.data.length === 0

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader
        title="Runs"
        description="Every unattended session a trigger started, with its report once it finishes."
      />

      <div role="group" aria-label="Filter by outcome" className="mt-5 flex flex-wrap gap-1.5">
        <Button
          size="sm"
          variant={outcome === '' ? 'primary' : 'secondary'}
          aria-pressed={outcome === ''}
          onClick={() => setOutcome('')}
        >
          All
        </Button>
        {OUTCOMES.map((o) => (
          <Button
            key={o}
            size="sm"
            variant={outcome === o ? 'primary' : 'secondary'}
            aria-pressed={outcome === o}
            onClick={() => setOutcome(o)}
          >
            {OUTCOME_LABEL[o]}
          </Button>
        ))}
      </div>

      {runs.isLoading && (
        <div className="mt-4 overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-1 shadow-[var(--shadow-card)]">
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </div>
      )}

      {isEmpty && (
        <EmptyState
          icon={<Activity size={18} aria-hidden />}
          title={outcome ? `No ${OUTCOME_LABEL[outcome as RunOutcome].toLowerCase()} runs` : 'No runs yet'}
          description="Runs appear here the moment a trigger, a replay or a test payload starts an unattended session."
        />
      )}

      {!runs.isLoading && !isEmpty && (
        <div className="mt-4 overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-1 shadow-[var(--shadow-card)]">
          {runs.data?.map((run) => <RunRow key={run.id} run={run} triggerName={triggerName(run.trigger_id)} />)}
        </div>
      )}
    </div>
  )
}
