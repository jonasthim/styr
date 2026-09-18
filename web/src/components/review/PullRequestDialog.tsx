// Open a pull request from the session's branch. The body starts as the last
// thing the session said, because that is almost always the summary a reviewer
// wants; the base branch starts as the ref the worktree was cut from.
//
// `gh` is optional (docs/superpowers/plans/2026-09-18-styr-v0.3-review.md,
// "Constraints") and a checkout need not have a remote at all, so both 409s
// the handler can answer with - codes "gh_unavailable" and "no_remote"
// (internal/api/review_handlers.go's writeReviewError) - are first-class
// states here, not error toasts: they tell the operator what is missing and
// where the setup is written down.
import { useEffect, useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { GitPullRequest } from 'lucide-react'
import { api, ApiError } from '../../api/client'
import type { DiffSummary, PullRequestResult, Session } from '../../api/types'
import { useToast } from '../../hooks/useToast'
import { Button, Dialog, DialogContent, Field, Input, Textarea } from '../ui'

const REVIEW_DOCS = 'https://github.com/jonasthim/styr/blob/main/docs/REVIEW.md'

/** The two 409 codes that mean "your environment cannot publish this branch
 * yet", which the dialog explains in place instead of failing the form. */
const SETUP_CODES = ['gh_unavailable', 'no_remote']

/** First line of the last assistant message, trimmed to a title's length. */
function titleFrom(session: Session, summary: string): string {
  return session.title || summary.split('\n')[0]?.slice(0, 72) || 'Changes from a Styr session'
}

export function PullRequestDialog({
  session,
  diff,
  summary,
}: {
  session: Session
  diff: DiffSummary
  summary: string
}) {
  const { toast } = useToast()
  const [open, setOpen] = useState(false)
  const [title, setTitle] = useState('')
  const [body, setBody] = useState('')
  const [base, setBase] = useState('')
  const [unavailable, setUnavailable] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (open) return
    setTitle(titleFrom(session, summary))
    setBody(summary)
    setBase(diff.base_ref)
    setUnavailable(null)
    setError(null)
  }, [open, session, summary, diff.base_ref])

  const create = useMutation({
    mutationFn: () =>
      api<PullRequestResult>(`/api/v1/sessions/${session.id}/pr`, { method: 'POST', json: { title, body, base } }),
    onSuccess: (result) => {
      setOpen(false)
      toast({ title: 'Pull request opened', description: result.url, tone: 'success' })
    },
    onError: (err) => {
      if (err instanceof ApiError && SETUP_CODES.includes(err.code)) {
        setUnavailable(err.message)
        return
      }
      setError(err instanceof ApiError ? err.message : 'Opening the pull request failed. Try again.')
    },
  })

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button size="sm" icon={<GitPullRequest size={13} aria-hidden />} onClick={() => setOpen(true)}>
        Open PR
      </Button>
      <DialogContent
        title="Open a pull request"
        description={`From ${diff.branch} into the base branch below.`}
        width={560}
        footer={
          <>
            <Button onClick={() => setOpen(false)}>Cancel</Button>
            <Button variant="primary" loading={create.isPending} disabled={!title.trim()} onClick={() => create.mutate()}>
              Create pull request
            </Button>
          </>
        }
      >
        <div className="flex flex-col gap-4">
          <Field label="Title" error={error ?? undefined}>
            {(args) => <Input {...args} value={title} onChange={(e) => setTitle(e.target.value)} />}
          </Field>
          <Field label="Body" hint="Prefilled from the session's last reply.">
            {(args) => <Textarea {...args} rows={6} value={body} onChange={(e) => setBody(e.target.value)} />}
          </Field>
          <Field label="Base branch">
            {(args) => <Input {...args} mono value={base} onChange={(e) => setBase(e.target.value)} />}
          </Field>

          {unavailable && (
            <div
              data-testid="pr-error"
              role="alert"
              className="rounded-[var(--radius-control)] border border-state-attention/40 bg-state-attention/10 px-3 py-2 text-[12px] leading-5 text-fg-primary"
            >
              {unavailable}{' '}
              <a href={REVIEW_DOCS} target="_blank" rel="noreferrer" className="text-accent underline">
                docs/REVIEW.md
              </a>{' '}
              covers adding a remote and authenticating gh.
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
