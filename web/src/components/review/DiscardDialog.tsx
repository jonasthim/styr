// Throw the whole worktree away: the branch goes, the changes go, the session
// closes. The confirm names what is lost rather than asking "are you sure".
import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Trash2 } from 'lucide-react'
import { api, ApiError } from '../../api/client'
import { invalidateReview } from '../../api/queries'
import type { DiffSummary, Session } from '../../api/types'
import { useToast } from '../../hooks/useToast'
import { Button, Dialog, DialogContent } from '../ui'

export function DiscardDialog({ session, diff }: { session: Session; diff: DiffSummary }) {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [open, setOpen] = useState(false)

  const discard = useMutation({
    mutationFn: () => api<void>(`/api/v1/sessions/${session.id}/discard`, { method: 'POST' }),
    onSuccess: () => {
      setOpen(false)
      invalidateReview(queryClient, session.id)
      void queryClient.invalidateQueries({ queryKey: ['sessions'] })
      toast({ title: 'Changes discarded', description: `${diff.branch} removed`, tone: 'danger' })
    },
    onError: (err) =>
      toast({
        title: 'Discard refused',
        description: err instanceof ApiError ? err.message : 'Try again once the session is idle.',
        tone: 'danger',
      }),
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button size="sm" variant="danger" icon={<Trash2 size={13} aria-hidden />} onClick={() => setOpen(true)}>
        Discard changes
      </Button>
      <DialogContent
        title="Discard every change?"
        description="The worktree and its branch are deleted and the session closes. There is no undo."
        width={440}
        footer={
          <>
            <Button onClick={() => setOpen(false)}>Keep them</Button>
            <Button variant="danger" loading={discard.isPending} onClick={() => discard.mutate()}>
              Discard changes
            </Button>
          </>
        }
      >
        <p className="font-mono text-[12px] text-fg-secondary">
          {diff.branch} · {diff.files.length === 1 ? '1 file' : `${diff.files.length} files`}
        </p>
      </DialogContent>
    </Dialog>
  )
}
