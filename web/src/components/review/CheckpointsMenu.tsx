// Every turn leaves a checkpoint commit; this is the list of them and the way
// back to one. Rewinding throws away everything after the checkpoint, so it
// asks first, and it is refused outright while the session is mid-turn or
// waiting on a decision — the process would be writing into a worktree that
// moved under it.
import { useState } from 'react'
import * as DropdownMenu from '@radix-ui/react-dropdown-menu'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { History, Undo2 } from 'lucide-react'
import { api, ApiError } from '../../api/client'
import { invalidateReview, q } from '../../api/queries'
import type { Checkpoint, Session } from '../../api/types'
import { useToast } from '../../hooks/useToast'
import { Button, Dialog, DialogContent } from '../ui'
import { relativeTime } from '../inbox/format'

export function CheckpointsMenu({ session, busy }: { session: Session; busy: boolean }) {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [open, setOpen] = useState(false)
  const [pending, setPending] = useState<Checkpoint | null>(null)
  const checkpointsQuery = useQuery({ ...q.sessionCheckpoints(session.id), enabled: open })

  const rewind = useMutation({
    mutationFn: (checkpoint: Checkpoint) =>
      api<void>(`/api/v1/sessions/${session.id}/rewind`, { method: 'POST', json: { checkpoint_id: checkpoint.id } }),
    onSuccess: (_data, checkpoint) => {
      setPending(null)
      invalidateReview(queryClient, session.id)
      toast({ title: `Rewound to turn ${checkpoint.turn}`, description: checkpoint.commit_sha.slice(0, 7) })
    },
    onError: (err) =>
      toast({
        title: 'Rewind refused',
        description: err instanceof ApiError ? err.message : 'Try again once the session is idle.',
        tone: 'danger',
      }),
  })

  const checkpoints = checkpointsQuery.data ?? []

  return (
    <>
      <DropdownMenu.Root open={open} onOpenChange={setOpen} modal={false}>
        <DropdownMenu.Trigger asChild>
          <Button size="sm" icon={<History size={13} aria-hidden />}>
            Checkpoints
          </Button>
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content
            data-testid="checkpoints-menu"
            align="end"
            sideOffset={6}
            className="styr-panel z-50 w-[min(360px,calc(100vw-2rem))] overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-2 py-1 shadow-[var(--shadow-popover)]"
          >
            {checkpoints.length === 0 ? (
              <p className="px-3 py-2 text-[12px] text-fg-muted">
                {checkpointsQuery.isLoading ? 'Loading…' : 'No checkpoints yet. One lands after every turn.'}
              </p>
            ) : (
              checkpoints.map((checkpoint) => (
                <DropdownMenu.Item
                  key={checkpoint.id}
                  asChild
                  disabled={busy}
                  onSelect={(event) => {
                    event.preventDefault()
                    setOpen(false)
                    setPending(checkpoint)
                  }}
                >
                  <button
                    type="button"
                    data-testid="checkpoint-row"
                    className="flex w-full items-baseline gap-2 px-3 py-1.5 text-left outline-none data-[disabled]:opacity-45 data-[highlighted]:bg-surface-3"
                  >
                    <span className="w-12 shrink-0 font-mono text-[11px] tabular-nums text-fg-muted">
                      turn {checkpoint.turn}
                    </span>
                    <span className="min-w-0 flex-1 truncate text-[12px] text-fg-primary">
                      {checkpoint.summary || checkpoint.commit_sha.slice(0, 7)}
                    </span>
                    <span className="shrink-0 font-mono text-[11px] tabular-nums text-fg-muted">
                      {relativeTime(checkpoint.created_at)}
                    </span>
                  </button>
                </DropdownMenu.Item>
              ))
            )}
            {busy && (
              <p className="border-t border-hairline px-3 py-2 text-[11px] text-state-attention">
                Rewinding is off while the session is working.
              </p>
            )}
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>

      <Dialog open={pending !== null} onOpenChange={(next) => !next && setPending(null)}>
        {pending && (
          <DialogContent
            title={`Rewind to turn ${pending.turn}?`}
            description="The worktree goes back to this checkpoint. Everything the session changed after it is lost."
            width={440}
            footer={
              <>
                <Button onClick={() => setPending(null)}>Keep everything</Button>
                <Button
                  variant="danger"
                  loading={rewind.isPending}
                  onClick={() => rewind.mutate(pending)}
                  icon={<Undo2 size={13} aria-hidden />}
                >
                  Rewind to here
                </Button>
              </>
            }
          >
            <p className="font-mono text-[12px] text-fg-secondary">
              {pending.commit_sha.slice(0, 7)} · {pending.summary || 'no summary'}
            </p>
          </DialogContent>
        )}
      </Dialog>
    </>
  )
}
