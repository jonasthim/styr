// "Add channel" dialog for Settings > Notifications: kind (ntfy/webhook),
// name, url, a token password field (ntfy only) and which run events it
// fires for.
import { useState, type FormEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../api/client'
import type { NotificationChannel, NotificationChannelKind, NotificationEvent } from '../../api/types'
import { Button, Dialog, DialogContent, Field, Input, Select } from '../ui'

const KIND_OPTIONS: Array<{ value: NotificationChannelKind; label: string }> = [
  { value: 'ntfy', label: 'ntfy' },
  { value: 'webhook', label: 'Generic webhook' },
]

const EVENT_OPTIONS: Array<{ value: NotificationEvent; label: string }> = [
  { value: 'run.finished', label: 'Run finished' },
  { value: 'run.needs_human', label: 'Run needs a decision' },
  { value: 'run.failed', label: 'Run failed' },
]

export function AddNotificationDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const queryClient = useQueryClient()
  const [kind, setKind] = useState<NotificationChannelKind>('ntfy')
  const [name, setName] = useState('')
  const [url, setUrl] = useState('')
  const [token, setToken] = useState('')
  const [events, setEvents] = useState<NotificationEvent[]>(['run.finished', 'run.needs_human', 'run.failed'])
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')

  function reset() {
    setKind('ntfy')
    setName('')
    setUrl('')
    setToken('')
    setEvents(['run.finished', 'run.needs_human', 'run.failed'])
    setSubmitError('')
  }

  function handleOpenChange(next: boolean) {
    if (!next) reset()
    onOpenChange(next)
  }

  function toggleEvent(event: NotificationEvent, checked: boolean) {
    setEvents((prev) => (checked ? [...prev, event] : prev.filter((e) => e !== event)))
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setSubmitting(true)
    setSubmitError('')
    try {
      const channel = await api<NotificationChannel>('/api/v1/notifications', {
        method: 'POST',
        json: { kind, name, url, token: kind === 'ntfy' ? token || undefined : undefined, events },
      })
      queryClient.setQueryData<NotificationChannel[]>(['notifications'], (prev) => (prev ? [...prev, channel] : [channel]))
      handleOpenChange(false)
    } catch (err) {
      setSubmitError(err instanceof ApiError ? err.message : 'Something went wrong adding the channel.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent title="Add notification channel" width={440}>
        <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-4">
          <Field label="Kind">{({ id }) => <Select id={id} value={kind} onValueChange={(v) => setKind(v as NotificationChannelKind)} options={KIND_OPTIONS} />}</Field>

          <Field label="Name">{({ id }) => <Input id={id} required value={name} onChange={(e) => setName(e.target.value)} placeholder="Ops phone" />}</Field>

          <Field label="URL">
            {({ id }) => (
              <Input id={id} required mono value={url} onChange={(e) => setUrl(e.target.value)} placeholder={kind === 'ntfy' ? 'https://ntfy.sh/styr-ops' : 'https://example.com/hook'} />
            )}
          </Field>

          {kind === 'ntfy' && (
            <Field label="Access token" labelAside="optional">
              {({ id }) => <Input id={id} type="password" mono value={token} onChange={(e) => setToken(e.target.value)} placeholder="tk_…" />}
            </Field>
          )}

          <fieldset className="flex flex-col gap-2">
            <legend className="mb-1 text-[12px] font-medium text-fg-secondary">Notify on</legend>
            {EVENT_OPTIONS.map((option) => (
              <label key={option.value} className="flex items-center gap-2 text-[13px] text-fg-primary">
                <input
                  type="checkbox"
                  className="h-3.5 w-3.5 accent-[var(--accent)]"
                  checked={events.includes(option.value)}
                  onChange={(e) => toggleEvent(option.value, e.target.checked)}
                />
                {option.label}
              </label>
            ))}
          </fieldset>

          {submitError && (
            <p role="alert" className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger">
              {submitError}
            </p>
          )}

          <div className="mt-1 flex items-center justify-end gap-2">
            <Button variant="ghost" type="button" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" loading={submitting}>
              Add channel
            </Button>
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}
