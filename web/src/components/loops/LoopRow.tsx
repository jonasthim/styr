// One row in the Loops table: where the loop is, how many iterations it has
// spent of its budget, and the session it keeps resuming. Stopping one is
// irreversible (it closes the session), so it asks first.
import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../api/client'
import type { Loop } from '../../api/types'
import { useToast } from '../../hooks/useToast'
import { relativeTime } from '../inbox/format'
import { Badge, Button, Dialog, DialogContent, Td, Tr } from '../ui'
import { LOOP_STATE_LABEL, LOOP_STATE_TONE, LoopGlyph } from './loopState'

export function LoopRow({
  loop,
  templateName,
  onOpenDetails,
}: {
  loop: Loop
  templateName: string
  onOpenDetails: (loop: Loop) => void
}) {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [confirming, setConfirming] = useState(false)
  const [stopping, setStopping] = useState(false)

  async function handleStop() {
    setStopping(true)
    try {
      await api(`/api/v1/loops/${loop.id}/stop`, { method: 'POST' })
      await queryClient.invalidateQueries({ queryKey: ['loops'] })
      void queryClient.invalidateQueries({ queryKey: ['sessions'] })
      setConfirming(false)
      toast({ title: 'Loop stopped', description: 'Its session is closed.', tone: 'success' })
    } catch (err) {
      toast({
        title: 'Could not stop the loop',
        description: err instanceof ApiError ? err.message : undefined,
        tone: 'danger',
      })
    } finally {
      setStopping(false)
    }
  }

  return (
    <Tr data-testid={`loop-row-${loop.id}`}>
      <Td>
        <span className="flex items-center gap-2">
          <LoopGlyph state={loop.state} />
          <Badge tone={LOOP_STATE_TONE[loop.state]}>{LOOP_STATE_LABEL[loop.state]}</Badge>
        </span>
      </Td>
      <Td className="min-w-0 truncate font-medium">{templateName}</Td>
      <Td className="whitespace-nowrap font-mono text-[12px] tabular-nums text-fg-secondary">
        {loop.iteration}/{loop.max_iterations}
      </Td>
      <Td className="font-mono text-[12px] text-fg-secondary">{loop.origin}</Td>
      <Td>
        {loop.session_id ? (
          <Link
            to="/sessions/$id"
            params={{ id: loop.session_id }}
            className="text-[12px] text-accent no-underline hover:underline"
          >
            Open session
          </Link>
        ) : (
          <span className="text-[12px] text-fg-muted">—</span>
        )}
      </Td>
      <Td className="whitespace-nowrap font-mono text-[12px] tabular-nums text-fg-muted">
        {relativeTime(loop.updated_at)} ago
      </Td>
      <Td className="text-right">
        <div className="flex justify-end gap-1.5">
          <Button size="sm" variant="ghost" onClick={() => onOpenDetails(loop)}>
            Details
          </Button>
          {loop.state === 'running' && (
            <Button size="sm" variant="ghost" onClick={() => setConfirming(true)}>
              Stop
            </Button>
          )}
        </div>

        <Dialog open={confirming} onOpenChange={setConfirming}>
          <DialogContent
            title="Stop this loop?"
            description="The current iteration finishes, nothing else starts, and the session it has been resuming is closed."
            width={420}
            footer={
              <>
                <Button onClick={() => setConfirming(false)}>Keep going</Button>
                <Button variant="danger" loading={stopping} onClick={() => void handleStop()}>
                  Stop loop
                </Button>
              </>
            }
          >
            <p className="text-[13px] text-fg-secondary">
              {templateName} is on iteration {loop.iteration} of {loop.max_iterations}.
            </p>
          </DialogContent>
        </Dialog>
      </Td>
    </Tr>
  )
}
