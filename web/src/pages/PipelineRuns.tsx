// The Pipelines tab under Runs (`/runs/pipelines`): every pipeline run,
// newest first. A pipeline run is a run of runs, so the row counts its steps
// rather than naming a session, and leads with the pipeline it came from.
import { useMemo } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Workflow } from 'lucide-react'
import { q } from '../api/queries'
import { RunsTabs } from '../components/runs/RunsTabs'
import { RUN_STATE_LABEL, RUN_STATE_TONE } from '../components/pipelines/stepState'
import { relativeTime } from '../components/inbox/format'
import { Badge, EmptyState, PageHeader, Skeleton, TableFrame, Td, Th, Tr } from '../components/ui'

function duration(startedAt: string, finishedAt: string | null): string {
  const end = finishedAt ? Date.parse(finishedAt) : Date.now()
  const seconds = Math.max(0, Math.round((end - Date.parse(startedAt)) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  return `${Math.round(minutes / 60)}h`
}

export function PipelineRuns() {
  const navigate = useNavigate()
  const runs = useQuery({ ...q.pipelineRuns(), refetchInterval: 6000 })
  const pipelines = useQuery(q.pipelines())

  const names = useMemo(() => {
    const map = new Map<string, string>()
    for (const pipeline of pipelines.data ?? []) map.set(pipeline.id, pipeline.name)
    return map
  }, [pipelines.data])

  const isEmpty = runs.isSuccess && runs.data.length === 0

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader
        title="Runs"
        description="Every unattended session a trigger started, with its report once it finishes."
      />

      <RunsTabs className="mt-5" />

      {isEmpty && (
        <EmptyState
          icon={<Workflow size={18} aria-hidden />}
          title="No pipeline runs yet"
          description="Start a pipeline from its page, or point a trigger or a schedule at one, and its runs appear here."
        />
      )}

      {!isEmpty && (
        <TableFrame className="mt-4" minWidth={700}>
          <thead>
            <tr>
              <Th>Pipeline</Th>
              <Th className="w-28">State</Th>
              <Th className="w-24">Origin</Th>
              <Th className="w-24">Started</Th>
              <Th className="w-24">Duration</Th>
              <Th className="w-20">Cost</Th>
            </tr>
          </thead>
          <tbody>
            {runs.isLoading && (
              <Tr>
                <Td colSpan={6}>
                  <Skeleton className="h-3 w-48 max-w-[45%]" />
                </Td>
              </Tr>
            )}
            {runs.data?.map((run) => (
              <Tr
                key={run.id}
                data-testid="pipeline-run-row"
                className="cursor-pointer"
                onClick={() => void navigate({ to: '/pipeline-runs/$id', params: { id: run.id } })}
              >
                <Td className="font-medium">{names.get(run.pipeline_id) ?? run.pipeline_id}</Td>
                <Td>
                  <Badge tone={RUN_STATE_TONE[run.state]}>{RUN_STATE_LABEL[run.state]}</Badge>
                </Td>
                <Td className="font-mono text-[12px] text-fg-secondary">{run.origin}</Td>
                <Td className="font-mono text-[12px] tabular-nums text-fg-muted">
                  {relativeTime(run.started_at)} ago
                </Td>
                <Td className="font-mono text-[12px] tabular-nums text-fg-secondary">
                  {duration(run.started_at, run.finished_at)}
                </Td>
                <Td className="font-mono text-[12px] tabular-nums text-fg-secondary">${run.cost_usd.toFixed(2)}</Td>
              </Tr>
            ))}
          </tbody>
        </TableFrame>
      )}
    </div>
  )
}
