// Create/edit a schedule. One dialog for both: `schedule` null means new.
// The cron input carries the live five-next-times preview, and the variables
// box is validated as JSON before the save button will do anything - an
// unattended run is the worst place to discover a typo.
import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../api/client'
import { q } from '../../api/queries'
import type { Schedule } from '../../api/types'
import { useToast } from '../../hooks/useToast'
import { TargetSelector, type RunTarget } from '../pipelines/TargetSelector'
import { Button, Dialog, DialogContent, Field, Input, Select, Switch, Textarea } from '../ui'
import { CronPreview } from './CronPreview'

interface Draft {
  name: string
  target: RunTarget
  templateId: string
  pipelineId: string
  cron: string
  vars: string
  enabled: boolean
}

const EMPTY: Draft = {
  name: '',
  target: 'template',
  templateId: '',
  pipelineId: '',
  cron: '0 9 * * *',
  vars: '{}',
  enabled: true,
}

function draftFrom(schedule: Schedule): Draft {
  return {
    name: schedule.name,
    target: schedule.pipeline_id ? 'pipeline' : 'template',
    templateId: schedule.template_id,
    pipelineId: schedule.pipeline_id ?? '',
    cron: schedule.cron,
    vars: JSON.stringify(schedule.vars ?? {}, null, 2),
    enabled: schedule.enabled,
  }
}

export function ScheduleDialog({
  schedule,
  open,
  onOpenChange,
}: {
  schedule: Schedule | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const templates = useQuery({ ...q.templates(), enabled: open })
  const pipelines = useQuery({ ...q.pipelines(), enabled: open })
  const [draft, setDraft] = useState<Draft>(EMPTY)
  const [saving, setSaving] = useState(false)

  // Reset every time the dialog opens, so editing one schedule and then
  // creating another never starts from the previous row's values.
  useEffect(() => {
    if (open) setDraft(schedule ? draftFrom(schedule) : EMPTY)
  }, [open, schedule])

  function set<K extends keyof Draft>(key: K, value: Draft[K]) {
    setDraft((prev) => ({ ...prev, [key]: value }))
  }

  let varsError = ''
  let parsedVars: Record<string, unknown> = {}
  if (draft.vars.trim()) {
    try {
      const parsed: unknown = JSON.parse(draft.vars)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        varsError = 'Variables must be a JSON object.'
      } else {
        parsedVars = parsed as Record<string, unknown>
      }
    } catch {
      varsError = 'This is not valid JSON.'
    }
  }

  const templateOptions = (templates.data ?? []).map((t) => ({ value: t.id, label: t.name }))
  const pipelineOptions = (pipelines.data ?? []).map((p) => ({ value: p.id, label: p.name }))
  // Falls back to the first entry so the dialog is savable the moment a name
  // is typed, rather than making the picker a required extra step.
  const templateId = draft.templateId || templateOptions[0]?.value || ''
  const pipelineId = draft.pipelineId || pipelineOptions[0]?.value || ''
  const hasTarget = draft.target === 'template' ? !!templateId : !!pipelineId
  const canSave = !!draft.name.trim() && hasTarget && !!draft.cron.trim() && !varsError

  async function handleSave() {
    if (!canSave) return
    setSaving(true)
    const body = {
      name: draft.name.trim(),
      // A schedule starts a template or a pipeline, never both (T54).
      template_id: draft.target === 'template' ? templateId : '',
      pipeline_id: draft.target === 'pipeline' ? pipelineId : null,
      cron: draft.cron.trim(),
      vars: parsedVars,
      enabled: draft.enabled,
    }
    try {
      if (schedule) {
        await api<Schedule>(`/api/v1/schedules/${schedule.id}`, { method: 'PATCH', json: body })
        toast({ title: 'Schedule saved', tone: 'success' })
      } else {
        await api<Schedule>('/api/v1/schedules', { method: 'POST', json: body })
        toast({ title: 'Schedule created', tone: 'success' })
      }
      await queryClient.invalidateQueries({ queryKey: ['schedules'] })
      onOpenChange(false)
    } catch (err) {
      toast({
        title: schedule ? 'Could not save the schedule' : 'Could not create the schedule',
        description: err instanceof ApiError ? err.message : undefined,
        tone: 'danger',
      })
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={schedule ? 'Edit schedule' : 'New schedule'}
        description="Styr starts this template on its own, on the cadence you set here."
        width={560}
        footer={
          <>
            <Button onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button variant="primary" loading={saving} disabled={!canSave} onClick={() => void handleSave()}>
              {schedule ? 'Save schedule' : 'Create schedule'}
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-4">
          <Field label="Name">
            {({ id }) => <Input id={id} value={draft.name} onChange={(e) => set('name', e.target.value)} />}
          </Field>

          <TargetSelector value={draft.target} onChange={(value) => set('target', value)} />

          {draft.target === 'template' ? (
            <Field label="Template">
              {({ id }) => (
                <Select
                  id={id}
                  aria-label="Template"
                  value={templateId}
                  onValueChange={(v) => set('templateId', v)}
                  options={templateOptions}
                  placeholder="Pick a template"
                />
              )}
            </Field>
          ) : (
            <Field label="Pipeline">
              {({ id }) => (
                <Select
                  id={id}
                  aria-label="Pipeline"
                  value={pipelineId}
                  onValueChange={(v) => set('pipelineId', v)}
                  options={pipelineOptions}
                  placeholder="Pick a pipeline"
                />
              )}
            </Field>
          )}

          <Field label="Cron" hint="Runs in the server's timezone.">
            {({ id }) => <Input id={id} mono value={draft.cron} onChange={(e) => set('cron', e.target.value)} />}
          </Field>

          <CronPreview cron={draft.cron} />

          <Field
            label="Variables"
            labelAside="optional"
            hint="A JSON object merged into the template's variables."
            error={varsError}
          >
            {({ id, 'aria-describedby': describedBy, 'aria-invalid': invalid }) => (
              <Textarea
                id={id}
                mono
                rows={4}
                aria-describedby={describedBy}
                aria-invalid={invalid}
                value={draft.vars}
                onChange={(e) => set('vars', e.target.value)}
              />
            )}
          </Field>

          <div className="flex items-center justify-between gap-3 rounded-[var(--radius-control)] border border-hairline bg-surface-1 px-3 py-2.5">
            <div className="min-w-0">
              <p className="text-[13px] text-fg-primary">Enabled</p>
              <p className="mt-0.5 text-[12px] text-fg-secondary">A disabled schedule keeps its cron but never fires.</p>
            </div>
            <Switch
              checked={draft.enabled}
              onCheckedChange={(checked) => set('enabled', checked)}
              aria-label="Enabled"
            />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
