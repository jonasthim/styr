// "New trigger" dialog: name, kind, template, an advanced disclosure
// (dedupe key template, cooldown, storm cap, run on resolved), then - once
// created - the WebhookReadyPanel showing the URL and the one-time secret.
import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown } from 'lucide-react'
import clsx from 'clsx'
import { api, ApiError } from '../../api/client'
import { q } from '../../api/queries'
import type { Trigger, TriggerCreateResult, TriggerKind } from '../../api/types'
import { Button, Dialog, DialogContent, Field, Input, Select, Switch } from '../ui'
import { WebhookReadyPanel } from './WebhookReadyPanel'

const KIND_OPTIONS: Array<{ value: TriggerKind; label: string }> = [
  { value: 'generic', label: 'Generic' },
  { value: 'grafana', label: 'Grafana' },
  { value: 'github', label: 'GitHub' },
]

export function CreateTriggerDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const queryClient = useQueryClient()
  const templates = useQuery({ ...q.templates(), enabled: open })

  const [name, setName] = useState('')
  const [kind, setKind] = useState<TriggerKind>('grafana')
  const [templateId, setTemplateId] = useState('')
  const [advancedOpen, setAdvancedOpen] = useState(false)
  const [dedupeKeyTemplate, setDedupeKeyTemplate] = useState('')
  const [cooldownS, setCooldownS] = useState('600')
  const [stormCap, setStormCap] = useState('10')
  const [runOnResolved, setRunOnResolved] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const [created, setCreated] = useState<TriggerCreateResult | null>(null)

  function reset() {
    setName('')
    setKind('grafana')
    setTemplateId('')
    setAdvancedOpen(false)
    setDedupeKeyTemplate('')
    setCooldownS('600')
    setStormCap('10')
    setRunOnResolved(false)
    setSubmitError('')
    setCreated(null)
  }

  function handleOpenChange(next: boolean) {
    if (!next) reset()
    onOpenChange(next)
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setSubmitting(true)
    setSubmitError('')
    try {
      const result = await api<TriggerCreateResult>('/api/v1/triggers', {
        method: 'POST',
        json: {
          name,
          kind,
          template_id: templateId,
          dedupe_key_template: dedupeKeyTemplate,
          cooldown_s: Number(cooldownS) || 0,
          storm_cap_per_hour: Number(stormCap) || 0,
          run_on_resolved: runOnResolved,
        },
      })
      queryClient.setQueryData<Trigger[]>(['triggers'], (prev) => (prev ? [result.trigger, ...prev] : [result.trigger]))
      setCreated(result)
    } catch (err) {
      setSubmitError(err instanceof ApiError ? err.message : 'Something went wrong creating the trigger.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        title={created ? 'Webhook ready' : 'New trigger'}
        description={created ? undefined : 'Turn an inbound webhook into an unattended investigation.'}
        width={520}
      >
        {created ? (
          <div className="flex flex-col gap-4">
            <WebhookReadyPanel trigger={created.trigger} secret={created.secret} />
            <div className="flex justify-end">
              <Button variant="primary" onClick={() => handleOpenChange(false)}>
                Done
              </Button>
            </div>
          </div>
        ) : (
          <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-4">
            <Field label="Name">{({ id }) => <Input id={id} required value={name} onChange={(e) => setName(e.target.value)} placeholder="Prod Grafana alerts" />}</Field>

            <Field label="Kind">
              {({ id }) => (
                <Select id={id} value={kind} onValueChange={(v) => setKind(v as TriggerKind)} options={KIND_OPTIONS} />
              )}
            </Field>

            <Field label="Template" hint={templates.data?.length === 0 ? 'Create a template first.' : undefined}>
              {({ id, 'aria-describedby': describedBy }) => (
                <Select
                  id={id}
                  aria-describedby={describedBy}
                  value={templateId}
                  onValueChange={setTemplateId}
                  placeholder="Choose a template"
                  options={(templates.data ?? []).map((t) => ({ value: t.id, label: t.name }))}
                />
              )}
            </Field>

            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="w-fit"
              onClick={() => setAdvancedOpen((o) => !o)}
              aria-expanded={advancedOpen}
              iconRight={<ChevronDown size={13} aria-hidden className={clsx('transition-transform duration-150', advancedOpen && 'rotate-180')} />}
            >
              Advanced
            </Button>

            {advancedOpen && (
              <div className="flex flex-col gap-4 rounded-[var(--radius-control)] border border-hairline bg-surface-1 p-3">
                <Field label="Dedupe key template" hint="A Go template; leave blank to never dedupe.">
                  {({ id, 'aria-describedby': describedBy }) => (
                    <Input id={id} mono aria-describedby={describedBy} value={dedupeKeyTemplate} onChange={(e) => setDedupeKeyTemplate(e.target.value)} placeholder="{{ .status }}:{{ .commonLabels.alertname }}" />
                  )}
                </Field>
                <div className="flex gap-3">
                  <Field label="Cooldown (seconds)" className="flex-1">
                    {({ id }) => <Input id={id} type="number" min={0} value={cooldownS} onChange={(e) => setCooldownS(e.target.value)} />}
                  </Field>
                  <Field label="Storm cap (per hour)" className="flex-1">
                    {({ id }) => <Input id={id} type="number" min={0} value={stormCap} onChange={(e) => setStormCap(e.target.value)} />}
                  </Field>
                </div>
                <label className="flex items-center justify-between gap-3 text-[12px] font-medium text-fg-secondary">
                  Run again when the alert resolves
                  <Switch checked={runOnResolved} onCheckedChange={setRunOnResolved} aria-label="Run on resolved" />
                </label>
              </div>
            )}

            {submitError && (
              <p role="alert" className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger">
                {submitError}
              </p>
            )}

            <div className="mt-1 flex items-center justify-end gap-2">
              <Button variant="ghost" type="button" onClick={() => handleOpenChange(false)}>
                Cancel
              </Button>
              <Button variant="primary" type="submit" loading={submitting} disabled={!templateId}>
                Create trigger
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
