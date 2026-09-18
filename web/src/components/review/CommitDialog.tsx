// Commit the worktree. The message starts as the session's title because that
// is what the operator already named this piece of work, and the dialog shows
// what is about to go in so "commit" is never a blind action.
import { useEffect, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { GitCommitVertical } from 'lucide-react'
import { api, ApiError } from '../../api/client'
import { invalidateReview } from '../../api/queries'
import type { CommitResult, DiffSummary, Session } from '../../api/types'
import { useToast } from '../../hooks/useToast'
import { Button, Dialog, DialogContent, Field, Textarea } from '../ui'
import { DiffCount } from './fileMeta'

export function CommitDialog({ session, diff }: { session: Session; diff: DiffSummary }) {
  const queryClient = useQueryClient()
  const { toast } = useToast()
  const [open, setOpen] = useState(false)
  const [message, setMessage] = useState(session.title)
  const [error, setError] = useState<string | null>(null)

  // The title can change under the dialog (the CLI renames a session as it
  // learns what it is doing); refill while the dialog is closed, never while
  // someone is typing in it.
  useEffect(() => {
    if (!open) setMessage(session.title)
  }, [open, session.title])

  const commit = useMutation({
    mutationFn: () => api<CommitResult>(`/api/v1/sessions/${session.id}/commit`, { method: 'POST', json: { message } }),
    onSuccess: (result) => {
      setOpen(false)
      invalidateReview(queryClient, session.id)
      toast({ title: `Committed ${result.sha.slice(0, 7)}`, description: message, tone: 'success' })
    },
    onError: (err) => setError(err instanceof ApiError ? err.message : 'The commit failed. Try again.'),
  })

  const fileCount = diff.files.length

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        setError(null)
      }}
    >
      <Button size="sm" icon={<GitCommitVertical size={13} aria-hidden />} onClick={() => setOpen(true)}>
        Commit
      </Button>
      <DialogContent
        title="Commit changes"
        description={`On ${diff.branch}, ${fileCount === 1 ? '1 file' : `${fileCount} files`} against ${diff.base_ref}.`}
        width={520}
        footer={
          <>
            <Button onClick={() => setOpen(false)}>Cancel</Button>
            <Button variant="primary" loading={commit.isPending} disabled={!message.trim()} onClick={() => commit.mutate()}>
              Commit changes
            </Button>
          </>
        }
        footerLeft={<DiffCount add={diff.total_add} del={diff.total_del} />}
      >
        <Field label="Message" error={error ?? undefined}>
          {(args) => (
            <Textarea
              {...args}
              rows={3}
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              placeholder="What this commit does"
            />
          )}
        </Field>
      </DialogContent>
    </Dialog>
  )
}
