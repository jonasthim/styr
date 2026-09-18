// Template editor (`/templates/$id`): fields on the left, a live preview
// (debounced POST /templates/{id}/render against a picked kind's sample) and
// the Grafana variables help on the right.
import { useEffect, useRef, useState } from 'react'
import { useParams, Link, useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft, Play } from 'lucide-react'
import { api, ApiError } from '../api/client'
import { q } from '../api/queries'
import type { RunStartedResult, Template, TemplateRenderResult, TriggerKind } from '../api/types'
import { useDebounced } from '../hooks/useDebounced'
import { useToast } from '../hooks/useToast'
import { Button, Card, Field, Input, Select, Textarea } from '../components/ui'

// The until-field picker offers the report schema's own boolean properties
// first - those are the fields a run can actually answer with - and a
// free-text escape hatch for a schema this editor cannot read.
// Radix Select refuses an empty item value, so "off" needs a sentinel.
const NO_LOOP = '__none__'
const CUSTOM_FIELD = '__custom__'

/** The boolean property names declared by a JSON Schema string, or [] when
 * it is empty, invalid or has no booleans. */
function booleanProperties(reportSchema: string): string[] {
  if (!reportSchema.trim()) return []
  try {
    const parsed: unknown = JSON.parse(reportSchema)
    const properties = (parsed as { properties?: Record<string, { type?: string }> } | null)?.properties
    if (!properties || typeof properties !== 'object') return []
    return Object.entries(properties)
      .filter(([, value]) => value?.type === 'boolean')
      .map(([name]) => name)
  } catch {
    return []
  }
}

const PREVIEW_KIND_OPTIONS: Array<{ value: TriggerKind; label: string }> = [
  { value: 'grafana', label: 'Grafana sample' },
  { value: 'generic', label: 'Generic sample' },
  { value: 'github', label: 'GitHub sample' },
]

const GRAFANA_VARIABLES: Array<{ name: string; hint: string }> = [
  { name: '.status', hint: 'firing | resolved' },
  { name: '.alerts', hint: 'list of {status, labels, annotations, startsAt, endsAt, fingerprint, generatorURL, silenceURL, dashboardURL, panelURL, values}' },
  { name: '.commonLabels', hint: 'labels shared by every alert in the group' },
  { name: '.commonAnnotations', hint: 'annotations shared by every alert in the group' },
  { name: '.groupLabels', hint: 'labels the alerts were grouped by' },
  { name: '.title', hint: "Grafana's own alert title" },
  { name: '.message', hint: "Grafana's own alert message" },
  { name: '.externalURL', hint: 'link back to Grafana' },
  { name: '.payload', hint: 'the raw delivered payload, any kind' },
]

const TEMPLATE_FUNCTIONS = ['lower', 'upper', 'join', 'default', 'truncate n', 'json', 'now']

export function TemplateEditor() {
  const { id } = useParams({ from: '/_app/templates/$id' })
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const templateQuery = useQuery(q.template(id))
  const workspaces = useQuery(q.workspaces())
  const profiles = useQuery(q.profiles())

  const [form, setForm] = useState<Template | null>(null)
  const [schemaError, setSchemaError] = useState('')
  const [saving, setSaving] = useState(false)
  const [previewKind, setPreviewKind] = useState<TriggerKind>('grafana')
  const [customUntil, setCustomUntil] = useState(false)
  const [starting, setStarting] = useState(false)
  const navigate = useNavigate()
  const loadedId = useRef<string | null>(null)

  useEffect(() => {
    if (templateQuery.data && loadedId.current !== templateQuery.data.id) {
      setForm(templateQuery.data)
      setCustomUntil(
        !!templateQuery.data.loop_until && !booleanProperties(templateQuery.data.report_schema).includes(templateQuery.data.loop_until),
      )
      loadedId.current = templateQuery.data.id
    }
  }, [templateQuery.data])

  function update<K extends keyof Template>(key: K, value: Template[K]) {
    setForm((prev) => (prev ? { ...prev, [key]: value } : prev))
  }

  useEffect(() => {
    if (form === null) return
    if (!form.report_schema.trim()) {
      setSchemaError('')
      return
    }
    try {
      JSON.parse(form.report_schema)
      setSchemaError('')
    } catch {
      setSchemaError('This is not valid JSON.')
    }
  }, [form?.report_schema])

  const debouncedTitle = useDebounced(form?.title_template ?? '', 400)
  const debouncedPrompt = useDebounced(form?.prompt_template ?? '', 400)
  const sample = useQuery(q.triggerSample(previewKind))
  const preview = useQuery({
    queryKey: ['template-render', id, previewKind, debouncedTitle, debouncedPrompt],
    queryFn: () =>
      api<TemplateRenderResult>(`/api/v1/templates/${id}/render`, {
        method: 'POST',
        // title_template/prompt_template are a contract addition (see this
        // card's report) so the preview reflects unsaved edits.
        json: { payload: sample.data, kind: previewKind, title_template: debouncedTitle, prompt_template: debouncedPrompt },
      }),
    enabled: sample.isSuccess,
  })

  async function handleRunNow() {
    setStarting(true)
    try {
      const result = await api<RunStartedResult>(`/api/v1/templates/${id}/run`, { method: 'POST' })
      void queryClient.invalidateQueries({ queryKey: ['runs'] })
      void queryClient.invalidateQueries({ queryKey: ['loops'] })
      void navigate({ to: '/runs/$id', params: { id: result.run_id } })
    } catch (err) {
      toast({
        title: 'Could not start a run',
        description: err instanceof ApiError ? err.message : undefined,
        tone: 'danger',
      })
    } finally {
      setStarting(false)
    }
  }

  async function handleSave() {
    if (!form || schemaError) return
    setSaving(true)
    try {
      const saved = await api<Template>(`/api/v1/templates/${id}`, {
        method: 'PATCH',
        json: {
          name: form.name,
          workspace_id: form.workspace_id,
          profile_id: form.profile_id,
          title_template: form.title_template,
          prompt_template: form.prompt_template,
          system_prompt: form.system_prompt,
          report_schema: form.report_schema,
          loop_until: form.loop_until,
          loop_max: form.loop_max,
        },
      })
      queryClient.setQueryData(['template', id], saved)
      void queryClient.invalidateQueries({ queryKey: ['templates'] })
      toast({ title: 'Template saved', tone: 'success' })
    } catch (err) {
      toast({ title: 'Could not save the template', description: err instanceof ApiError ? err.message : undefined, tone: 'danger' })
    } finally {
      setSaving(false)
    }
  }

  if (templateQuery.isLoading || !form) {
    return <div className="mx-auto w-full max-w-[1100px] flex-1 px-4 py-6 text-[13px] text-fg-secondary sm:px-6">Loading…</div>
  }

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-1 flex-col px-4 py-6 sm:px-6">
      <Link to="/triggers" search={{ tab: 'templates' }} className="inline-flex w-fit items-center gap-1.5 text-[12px] text-fg-secondary no-underline hover:text-fg-primary">
        <ArrowLeft size={13} aria-hidden />
        Templates
      </Link>

      <div className="mt-3 flex items-start justify-between gap-4">
        <h1 className="min-w-0 truncate text-[20px] font-semibold leading-7 tracking-[-0.02em] text-fg-primary">{form.name || 'Untitled template'}</h1>
        <div className="flex shrink-0 items-center gap-2">
          <Button icon={<Play size={13} aria-hidden />} loading={starting} onClick={() => void handleRunNow()}>
            Run now
          </Button>
          <Button variant="primary" onClick={() => void handleSave()} loading={saving} disabled={!!schemaError}>
            Save
          </Button>
        </div>
      </div>

      <div className="mt-5 flex flex-col gap-5 lg:flex-row lg:items-start">
        <div className="flex min-w-0 flex-1 flex-col gap-4">
          <Field label="Name">{({ id: fieldId }) => <Input id={fieldId} value={form.name} onChange={(e) => update('name', e.target.value)} />}</Field>

          <div className="flex gap-3">
            <Field label="Workspace" className="flex-1">
              {({ id: fieldId }) => (
                <Select
                  id={fieldId}
                  value={form.workspace_id}
                  onValueChange={(v) => update('workspace_id', v)}
                  options={(workspaces.data ?? []).map((w) => ({ value: w.id, label: w.name }))}
                />
              )}
            </Field>
            <Field label="Profile" className="flex-1">
              {({ id: fieldId }) => (
                <Select
                  id={fieldId}
                  value={form.profile_id}
                  onValueChange={(v) => update('profile_id', v)}
                  options={(profiles.data ?? []).map((p) => ({ value: p.id, label: p.name }))}
                />
              )}
            </Field>
          </div>

          <Field label="Title template">
            {({ id: fieldId }) => <Input id={fieldId} mono value={form.title_template} onChange={(e) => update('title_template', e.target.value)} />}
          </Field>

          <Field label="Prompt template">
            {({ id: fieldId }) => <Textarea id={fieldId} mono rows={10} value={form.prompt_template} onChange={(e) => update('prompt_template', e.target.value)} />}
          </Field>

          <Field label="System prompt" labelAside="optional">
            {({ id: fieldId }) => <Textarea id={fieldId} rows={4} value={form.system_prompt} onChange={(e) => update('system_prompt', e.target.value)} />}
          </Field>

          <Field label="Report schema" hint="A JSON Schema object; the run's structured report is validated against it." error={schemaError}>
            {({ id: fieldId, 'aria-describedby': describedBy, 'aria-invalid': invalid }) => (
              <Textarea id={fieldId} mono rows={10} aria-describedby={describedBy} aria-invalid={invalid} value={form.report_schema} onChange={(e) => update('report_schema', e.target.value)} />
            )}
          </Field>

          <Card title="Loop" description="Runs again on the same session until the report's field is true.">
            <div className="flex flex-col gap-4 sm:flex-row">
              <Field label="Until field" className="flex-1">
                {({ id: fieldId }) => (
                  <Select
                    id={fieldId}
                    aria-label="Until field"
                    value={customUntil ? CUSTOM_FIELD : form.loop_until || NO_LOOP}
                    onValueChange={(value) => {
                      if (value === CUSTOM_FIELD) {
                        setCustomUntil(true)
                        return
                      }
                      setCustomUntil(false)
                      update('loop_until', value === NO_LOOP ? '' : value)
                      if (value !== NO_LOOP && !form.loop_max) update('loop_max', 5)
                    }}
                    options={[
                      { value: NO_LOOP, label: 'No loop' },
                      ...booleanProperties(form.report_schema).map((name) => ({ value: name, label: name })),
                      { value: CUSTOM_FIELD, label: 'Another field…' },
                    ]}
                  />
                )}
              </Field>

              <Field label="Max iterations" className="sm:w-[140px]">
                {({ id: fieldId }) => (
                  <Input
                    id={fieldId}
                    type="number"
                    min={1}
                    max={50}
                    disabled={!form.loop_until}
                    value={form.loop_max || ''}
                    onChange={(e) => update('loop_max', Number(e.target.value) || 0)}
                  />
                )}
              </Field>
            </div>

            {customUntil && (
              <Field label="Field name" className="mt-4">
                {({ id: fieldId }) => (
                  <Input
                    id={fieldId}
                    mono
                    placeholder="done"
                    value={form.loop_until}
                    onChange={(e) => update('loop_until', e.target.value)}
                  />
                )}
              </Field>
            )}
          </Card>
        </div>

        <div className="flex w-full flex-col gap-4 lg:w-[360px] lg:shrink-0">
          <Card title="Preview" actions={<Select aria-label="Preview sample" value={previewKind} onValueChange={(v) => setPreviewKind(v as TriggerKind)} options={PREVIEW_KIND_OPTIONS} className="w-[168px]" />}>
            <div className="flex flex-col gap-3" data-testid="template-preview">
              <div>
                <p className="text-[12px] font-medium text-fg-secondary">Title</p>
                <p className="mt-1 min-h-5 font-mono text-[12px] text-fg-primary" data-testid="template-preview-title">
                  {preview.data?.title || '—'}
                </p>
              </div>
              <div>
                <p className="text-[12px] font-medium text-fg-secondary">Prompt</p>
                <pre className="mt-1 min-h-10 overflow-x-auto whitespace-pre-wrap break-words font-mono text-[12px] leading-5 text-fg-primary" data-testid="template-preview-prompt">
                  {preview.data?.prompt || '—'}
                </pre>
              </div>
              {preview.data?.errors && preview.data.errors.length > 0 && (
                <p className="text-[12px] text-fg-danger">{preview.data.errors.join(' ')}</p>
              )}
            </div>
          </Card>

          <Card title="Grafana variables" description="Available when a delivery came from a Grafana trigger.">
            <dl className="flex flex-col gap-2.5">
              {GRAFANA_VARIABLES.map((v) => (
                <div key={v.name}>
                  <dt className="font-mono text-[12px] text-accent">{v.name}</dt>
                  <dd className="mt-0.5 text-[12px] text-fg-secondary">{v.hint}</dd>
                </div>
              ))}
            </dl>
            <p className="mt-3 text-[12px] text-fg-secondary">
              Functions: {TEMPLATE_FUNCTIONS.map((fn) => (
                <code key={fn} className="mr-1.5 font-mono text-[11px] text-fg-primary">
                  {fn}
                </code>
              ))}
            </p>
          </Card>
        </div>
      </div>
    </div>
  )
}
