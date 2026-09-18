// Pipeline editor (`/pipelines/$id`): the definition on the left, the graph
// it describes on the right, redrawn from POST /pipelines/validate a beat
// after typing stops. The graph is the point of the page - a DAG written as
// indented text is hard to hold in your head, and seeing the fan-out appear
// as you type the `foreach` line is the whole reason this screen exists.
import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Play } from 'lucide-react'
import { api, ApiError } from '../api/client'
import { q } from '../api/queries'
import type { Pipeline, PipelineValidation } from '../api/types'
import { PipelineGraph } from '../components/pipelines/PipelineGraph'
import { StartPipelineDialog } from '../components/pipelines/StartPipelineDialog'
import { YamlEditor } from '../components/pipelines/YamlEditor'
import { pipelineName } from '../components/pipelines/yamlSummary'
import { useDebounced } from '../hooks/useDebounced'
import { useToast } from '../hooks/useToast'
import { Button, Field, Select } from '../components/ui'

const EMPTY_VALIDATION: PipelineValidation = { ok: true, errors: [], graph: { nodes: [], edges: [] } }

export function PipelineEditor() {
  const { id } = useParams({ from: '/_app/pipelines/$id' })
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const pipelineQuery = useQuery(q.pipeline(id))
  const workspaces = useQuery(q.workspaces())

  const [yaml, setYaml] = useState('')
  const [workspaceId, setWorkspaceId] = useState('')
  const [saving, setSaving] = useState(false)
  const [startOpen, setStartOpen] = useState(false)
  const loadedId = useRef<string | null>(null)

  useEffect(() => {
    if (pipelineQuery.data && loadedId.current !== pipelineQuery.data.id) {
      setYaml(pipelineQuery.data.yaml)
      setWorkspaceId(pipelineQuery.data.workspace_id)
      loadedId.current = pipelineQuery.data.id
    }
  }, [pipelineQuery.data])

  const debouncedYaml = useDebounced(yaml, 300)
  const validation = useQuery({
    queryKey: ['pipeline-validate', debouncedYaml, workspaceId],
    queryFn: () =>
      api<PipelineValidation>('/api/v1/pipelines/validate', {
        method: 'POST',
        json: { yaml: debouncedYaml, workspace_id: workspaceId },
      }),
    enabled: debouncedYaml.length > 0,
    // The definition is the only input, so a result for a given text never
    // goes stale - keeping it means flipping between two edits is instant.
    staleTime: Infinity,
  })

  const result = validation.data ?? EMPTY_VALIDATION

  async function handleSave() {
    if (!result.ok) return
    setSaving(true)
    try {
      const saved = await api<Pipeline>(`/api/v1/pipelines/${id}`, {
        method: 'PATCH',
        json: { name: pipelineName(yaml, pipelineQuery.data?.name ?? ''), workspace_id: workspaceId, yaml },
      })
      queryClient.setQueryData(['pipeline', id], saved)
      void queryClient.invalidateQueries({ queryKey: ['pipelines'] })
      toast({ title: 'Pipeline saved', tone: 'success' })
    } catch (err) {
      toast({
        title: 'Could not save the pipeline',
        description: err instanceof ApiError ? err.message : undefined,
        tone: 'danger',
      })
    } finally {
      setSaving(false)
    }
  }

  if (pipelineQuery.isLoading || !pipelineQuery.data) {
    return (
      <div className="mx-auto w-full max-w-[1180px] flex-1 px-4 py-6 text-[13px] text-fg-secondary sm:px-6">
        {pipelineQuery.isLoading ? 'Loading…' : 'Pipeline not found.'}
      </div>
    )
  }

  const name = pipelineName(yaml, pipelineQuery.data.name)

  return (
    <div className="mx-auto flex w-full max-w-[1180px] flex-1 flex-col px-4 py-6 sm:px-6">
      <Link
        to="/pipelines"
        className="inline-flex w-fit items-center gap-1.5 text-[12px] text-fg-secondary no-underline hover:text-fg-primary"
      >
        <ArrowLeft size={13} aria-hidden />
        Pipelines
      </Link>

      <div className="mt-3 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="truncate text-[20px] font-semibold leading-7 tracking-[-0.02em] text-fg-primary">{name}</h1>
          <p className="mt-1 text-[13px] text-fg-secondary">
            {result.ok
              ? `${result.graph.nodes.length} steps, ${result.graph.edges.length} dependencies.`
              : `${result.errors.length} problem${result.errors.length === 1 ? '' : 's'} to fix before this can run.`}
          </p>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button icon={<Play size={13} aria-hidden />} disabled={!result.ok} onClick={() => setStartOpen(true)}>
            Start
          </Button>
          <Button variant="primary" loading={saving} disabled={!result.ok} onClick={() => void handleSave()}>
            Save
          </Button>
        </div>
      </div>

      <div className="mt-5 flex flex-col gap-5 lg:flex-row lg:items-start">
        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <Field label="Definition" hint="Steps run in dependency order; `needs` may only name a step defined above it.">
            {({ id: fieldId, 'aria-describedby': describedBy }) => (
              <YamlEditor id={fieldId} describedBy={describedBy} value={yaml} onChange={setYaml} errors={result.errors} />
            )}
          </Field>

          <Field label="Workspace" hint="Every step's template has to use this workspace." className="max-w-[280px]">
            {({ id: fieldId }) => (
              <Select
                id={fieldId}
                value={workspaceId}
                onValueChange={setWorkspaceId}
                options={(workspaces.data ?? []).map((w) => ({ value: w.id, label: w.name }))}
              />
            )}
          </Field>
        </div>

        <div className="flex w-full flex-col gap-2 lg:w-[440px] lg:shrink-0">
          <PipelineGraph graph={result.graph} height={460} />
          <p className="text-[12px] text-fg-muted">
            Drag to pan. A stacked node runs once per item of its foreach list.
          </p>
        </div>
      </div>

      <StartPipelineDialog pipeline={pipelineQuery.data} open={startOpen} onOpenChange={setStartOpen} />
    </div>
  )
}
