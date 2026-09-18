// "Send test payload" dialog: prefilled with the trigger's kind's sample
// payload (GET /triggers/samples/{kind} - a contract addition this card made,
// see the plan doc's frontend section), edit it if you like, POST
// /triggers/{id}/test, then a link to the run it started.
import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { api, ApiError } from '../../api/client'
import { q } from '../../api/queries'
import type { Trigger, TriggerTestResult } from '../../api/types'
import { Button, Dialog, DialogContent, Field, Textarea } from '../ui'
import { StatusChip } from './chips'

export function TestPayloadDialog({
  trigger,
  open,
  onOpenChange,
}: {
  trigger: Trigger | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const queryClient = useQueryClient()
  const sample = useQuery({ ...q.triggerSample(trigger?.kind ?? 'generic'), enabled: open && !!trigger })
  const [text, setText] = useState('')
  const [parseError, setParseError] = useState('')
  const [sending, setSending] = useState(false)
  const [result, setResult] = useState<TriggerTestResult | null>(null)
  const [sendError, setSendError] = useState('')

  useEffect(() => {
    if (open && sample.data !== undefined) {
      setText(JSON.stringify(sample.data, null, 2))
      setResult(null)
      setSendError('')
      setParseError('')
    }
  }, [open, sample.data])

  async function handleSend() {
    let payload: unknown
    try {
      payload = JSON.parse(text)
    } catch {
      setParseError('This is not valid JSON.')
      return
    }
    setParseError('')
    setSending(true)
    setSendError('')
    try {
      const response = await api<TriggerTestResult>(`/api/v1/triggers/${trigger!.id}/test`, {
        method: 'POST',
        json: { payload },
      })
      setResult(response)
      void queryClient.invalidateQueries({ queryKey: ['deliveries', trigger!.id] })
      void queryClient.invalidateQueries({ queryKey: ['runs'] })
      void queryClient.invalidateQueries({ queryKey: ['triggers'] })
    } catch (err) {
      setSendError(err instanceof ApiError ? err.message : 'Something went wrong sending the test payload.')
    } finally {
      setSending(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={trigger ? `Send test payload — ${trigger.name}` : 'Send test payload'}
        description="Runs the full pipeline as if this payload had just been delivered."
        width={560}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Close
            </Button>
            <Button variant="primary" loading={sending} onClick={() => void handleSend()}>
              Send
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-4">
          <Field label="Payload" error={parseError}>
            {({ id, 'aria-describedby': describedBy, 'aria-invalid': invalid }) => (
              <Textarea
                id={id}
                mono
                rows={12}
                aria-describedby={describedBy}
                aria-invalid={invalid}
                value={text}
                onChange={(e) => setText(e.target.value)}
              />
            )}
          </Field>

          {sendError && (
            <p role="alert" className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger">
              {sendError}
            </p>
          )}

          {result && (
            <div data-testid="test-payload-result" className="flex items-center gap-2 rounded-[var(--radius-control)] border border-hairline bg-surface-2 px-3 py-2.5">
              <StatusChip status={result.status} />
              {result.run_id ? (
                <Link to="/runs/$id" params={{ id: result.run_id }} className="text-[13px] text-accent no-underline hover:underline">
                  View run
                </Link>
              ) : (
                <span className="text-[12px] text-fg-secondary">No run started.</span>
              )}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
