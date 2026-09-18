// Pipelines (`/pipelines`): every chain of agents this box knows how to run,
// what each one is made of, and how its last run went. A pipeline is the one
// thing in Styr that starts several unattended sessions in a row, so the row
// leads with its shape (steps) and its last outcome, and Start is right there
// on the row.
import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Play, Plus, Workflow } from 'lucide-react'
import { api, ApiError } from '../api/client'
import { q } from '../api/queries'
import type { Pipeline, PipelineRun } from '../api/types'
import { StartPipelineDialog } from '../components/pipelines/StartPipelineDialog'
import { RUN_STATE_LABEL, RUN_STATE_TONE } from '../components/pipelines/stepState'
import { pipelineName, stepCount } from '../components/pipelines/yamlSummary'
import { relativeTime } from '../components/inbox/format'
import { useToast } from '../hooks/useToast'
import { Badge, Button, EmptyState, PageHeader, Skeleton, TableFrame, Td, Th, Tr } from '../components/ui'

/** The most recent run of each pipeline, so a row can say how the last one
 * went without a request per row. */
function useLastRuns(): Map<string, PipelineRun> {
  const runs = useQuery(q.pipelineRuns())
  return useMemo(() => {
    const latest = new Map<string, PipelineRun>()
    for (const run of runs.data ?? []) {
      const current = latest.get(run.pipeline_id)
      if (!current || run.started_at > current.started_at) latest.set(run.pipeline_id, run)
    }
    return latest
  }, [runs.data])
}

export function Pipelines() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const pipelines = useQuery(q.pipelines())
  const workspaces = useQuery(q.workspaces())
  const lastRuns = useLastRuns()
  const [startTarget, setStartTarget] = useState<Pipeline | null>(null)
  const [creating, setCreating] = useState(false)

  function workspaceName(id: string): string {
    return workspaces.data?.find((w) => w.id === id)?.name ?? id
  }

  // "New from template": a sequential two-step skeleton, created and opened
  // straight away, so the first thing on screen is a working definition to
  // edit rather than an empty box.
  async function handleNewFromTemplate() {
    setCreating(true)
    try {
      const pipeline = await api<Pipeline>('/api/v1/pipelines', {
        method: 'POST',
        json: { name: 'new-pipeline', workspace_id: workspaces.data?.[0]?.id ?? 'w1' },
      })
      queryClient.setQueryData<Pipeline[]>(['pipelines'], (prev) => (prev ? [pipeline, ...prev] : [pipeline]))
      void navigate({ to: '/pipelines/$id', params: { id: pipeline.id } })
    } catch (err) {
      toast({
        title: 'Could not create the pipeline',
        description: err instanceof ApiError ? err.message : undefined,
        tone: 'danger',
      })
    } finally {
      setCreating(false)
    }
  }

  const isEmpty = pipelines.isSuccess && pipelines.data.length === 0

  const newButton = (
    <Button variant="primary" icon={<Plus size={14} aria-hidden />} loading={creating} onClick={() => void handleNewFromTemplate()}>
      New from template
    </Button>
  )

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader
        title="Pipelines"
        description="A YAML graph of steps, each one a template run whose report feeds the steps after it."
        actions={newButton}
      />

      {isEmpty && (
        <EmptyState
          icon={<Workflow size={18} aria-hidden />}
          title="No pipelines yet"
          description="A pipeline chains agents: triage an alert, apply the fix in its own worktree, then verify it — each step reading the one before."
          action={newButton}
        />
      )}

      {!isEmpty && (
        <TableFrame className="mt-5" minWidth={760}>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Workspace</Th>
              <Th className="w-20">Steps</Th>
              <Th className="w-36">Last run</Th>
              <Th className="w-24">Updated</Th>
              <Th className="w-10">
                <span className="sr-only">Actions</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {pipelines.isLoading && (
              <Tr>
                <Td colSpan={6}>
                  <Skeleton className="h-3 w-48 max-w-[45%]" />
                </Td>
              </Tr>
            )}
            {pipelines.data?.map((pipeline) => {
              const last = lastRuns.get(pipeline.id)
              return (
                <Tr key={pipeline.id} data-testid={`pipeline-row-${pipeline.id}`}>
                  <Td>
                    <button
                      type="button"
                      onClick={() => void navigate({ to: '/pipelines/$id', params: { id: pipeline.id } })}
                      className="rounded-[2px] font-medium text-fg-primary outline-none hover:text-accent hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]"
                    >
                      {pipelineName(pipeline.yaml, pipeline.name)}
                    </button>
                  </Td>
                  <Td className="font-mono text-[12px] text-fg-secondary">{workspaceName(pipeline.workspace_id)}</Td>
                  <Td className="font-mono text-[12px] tabular-nums text-fg-secondary">{stepCount(pipeline.yaml)}</Td>
                  <Td>
                    {last ? (
                      <button
                        type="button"
                        onClick={() => void navigate({ to: '/pipeline-runs/$id', params: { id: last.id } })}
                        className="rounded-[2px] outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]"
                      >
                        <Badge tone={RUN_STATE_TONE[last.state]}>{RUN_STATE_LABEL[last.state]}</Badge>
                      </button>
                    ) : (
                      <span className="text-[12px] text-fg-muted">Never run</span>
                    )}
                  </Td>
                  <Td className="font-mono text-[12px] tabular-nums text-fg-muted">
                    {relativeTime(pipeline.updated_at)} ago
                  </Td>
                  <Td>
                    <Button size="sm" icon={<Play size={13} aria-hidden />} onClick={() => setStartTarget(pipeline)}>
                      Start
                    </Button>
                  </Td>
                </Tr>
              )
            })}
          </tbody>
        </TableFrame>
      )}

      <StartPipelineDialog
        pipeline={startTarget}
        open={startTarget !== null}
        onOpenChange={(open) => !open && setStartTarget(null)}
      />
    </div>
  )
}
