// The Review tab of the session side panel: what changed, what you have said
// about it, and the one button that turns the second into the session's next
// prompt.
//
// It is the whole right rail rather than a file list alone, because the
// comments have to be visible while you read the diff — that is the difference
// between reviewing and annotating.
import { useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { Send } from 'lucide-react'
import clsx from 'clsx'
import { api } from '../../api/client'
import { invalidateReview, q } from '../../api/queries'
import type { ReviewComment, Session } from '../../api/types'
import type { Block } from '../../lib/blocks'
import { Button, buttonClasses } from '../ui'
import { ChangesList } from '../session/ChangesList'
import { DiffCount, StatusChip } from './fileMeta'

function plural(n: number, one: string, many: string): string {
  return `${n} ${n === 1 ? one : many}`
}

export function ReviewPanel({ session, blocks }: { session: Session; blocks: Block[] }) {
  const sessionId = session.id
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { id } = useParams({ from: '/_app/sessions/$id' })
  const search = useSearch({ from: '/_app/sessions/$id' })
  const diffQuery = useQuery(q.sessionDiff(sessionId))
  const commentsQuery = useQuery(q.sessionComments(sessionId))

  const unsent = useMemo(
    () => (commentsQuery.data ?? []).filter((c: ReviewComment) => !c.sent_at),
    [commentsQuery.data],
  )

  const unsentByPath = useMemo(() => {
    const map = new Map<string, number>()
    for (const comment of unsent) map.set(comment.path, (map.get(comment.path) ?? 0) + 1)
    return map
  }, [unsent])

  const sendReview = useMutation({
    mutationFn: () => api<void>(`/api/v1/sessions/${sessionId}/review`, { method: 'POST' }),
    onSuccess: () => {
      invalidateReview(queryClient, sessionId)
      void queryClient.invalidateQueries({ queryKey: ['session-events', sessionId] })
    },
  })

  function selectFile(path: string) {
    void navigate({
      to: '/sessions/$id',
      params: { id },
      search: (prev) => ({ ...prev, diff: prev.diff === path ? undefined : path }),
      replace: true,
    })
  }

  // A session with no worktree (never had one, or it was discarded) answers
  // 422 here. TanStack Query keeps the last successful data on a failed
  // refetch, so the error - not an empty file list - is what says there is
  // nothing left to review.
  const diff = diffQuery.isError ? undefined : diffQuery.data
  const files = diff?.files ?? []

  return (
    <div className="flex flex-col">
      <section aria-labelledby="review-files-heading">
        <div className="flex items-baseline justify-between gap-2 px-3 py-2">
          <h3 id="review-files-heading" className="text-[12px] font-medium text-fg-secondary">
            Changed files
          </h3>
          {diff && files.length > 0 && <DiffCount add={diff.total_add} del={diff.total_del} />}
        </div>

        {files.length === 0 ? (
          <p className="px-3 pb-3 text-[12px] text-fg-muted">
            {diff?.branch ? 'Nothing changed on this branch yet.' : 'This session does not run in a worktree.'}
          </p>
        ) : (
          <ul data-testid="review-files" className="flex flex-col border-y border-hairline">
            {files.map((file) => {
              const count = unsentByPath.get(file.path) ?? 0
              const active = search.diff === file.path
              return (
                <li key={file.path}>
                  <button
                    type="button"
                    data-testid="review-file"
                    aria-pressed={active}
                    onClick={() => selectFile(file.path)}
                    className={clsx(
                      'flex w-full items-center gap-2 px-3 py-1.5 text-left outline-none',
                      'transition-colors duration-[var(--duration-fast)] hover:bg-surface-2',
                      'focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-[var(--ring)]',
                      active && 'bg-surface-2',
                    )}
                  >
                    <StatusChip status={file.status} />
                    <span title={file.path} className="min-w-0 flex-1 truncate font-mono text-[12px] text-fg-primary">
                      {file.path}
                    </span>
                    {count > 0 && (
                      <span
                        aria-label={plural(count, 'unsent comment', 'unsent comments')}
                        className="shrink-0 rounded-full bg-accent px-1.5 text-[10px] font-semibold tabular-nums text-accent-fg"
                      >
                        {count}
                      </span>
                    )}
                    <DiffCount add={file.add} del={file.del} />
                  </button>
                </li>
              )
            })}
          </ul>
        )}
      </section>

      <section aria-labelledby="review-rail-heading" data-testid="review-rail" className="border-b border-hairline">
        <div className="flex items-baseline justify-between gap-2 px-3 py-2">
          <h3 id="review-rail-heading" className="text-[12px] font-medium text-fg-secondary">
            Review
          </h3>
          <span className="font-mono text-[11px] tabular-nums text-fg-muted">
            {plural(unsent.length, 'comment', 'comments')}
          </span>
        </div>

        {unsent.length === 0 ? (
          <p className="px-3 pb-3 text-[12px] text-fg-muted">
            Click a line number in the diff to leave a comment. Sending them asks the session to address each one.
          </p>
        ) : (
          <ul className="flex flex-col gap-1.5 px-3 pb-2">
            {unsent.map((comment) => (
              <li key={comment.id} className="rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 py-1.5">
                <button
                  type="button"
                  onClick={() => selectFile(comment.path)}
                  className="block w-full truncate text-left font-mono text-[11px] text-accent outline-none hover:underline focus-visible:underline"
                >
                  {comment.path}:{comment.line}
                </button>
                <p className="mt-0.5 line-clamp-3 text-[12px] leading-5 text-fg-primary">{comment.body}</p>
              </li>
            ))}
          </ul>
        )}

        <div className="flex items-center gap-2 px-3 pb-3">
          <Button
            variant="primary"
            size="sm"
            disabled={unsent.length === 0}
            loading={sendReview.isPending}
            onClick={() => sendReview.mutate()}
            icon={<Send size={12} aria-hidden />}
          >
            Send review
          </Button>
          <a
            href={`/api/v1/sessions/${sessionId}/patch`}
            download={`${sessionId}.patch`}
            className={buttonClasses('secondary', 'sm')}
          >
            Download patch
          </a>
        </div>
      </section>

      <section aria-labelledby="review-touched-heading">
        <h3 id="review-touched-heading" className="px-3 py-2 text-[12px] font-medium text-fg-secondary">
          Touched by tools
        </h3>
        <ChangesList blocks={blocks} />
      </section>
    </div>
  )
}
