// The header's changes cluster: commit, open a PR, walk back through the
// checkpoints, or throw the lot away. Absent entirely for a session that is
// not running in a worktree, which is what an empty `branch` means.
import { useQuery } from '@tanstack/react-query'
import { q } from '../../api/queries'
import type { Session } from '../../api/types'
import { CommitDialog } from './CommitDialog'
import { PullRequestDialog } from './PullRequestDialog'
import { CheckpointsMenu } from './CheckpointsMenu'
import { DiscardDialog } from './DiscardDialog'

export function ReviewActions({ session, summary }: { session: Session; summary: string }) {
  const diffQuery = useQuery(q.sessionDiff(session.id))
  // isError, not just an empty branch: once the worktree is discarded the
  // diff endpoint answers 422 forever, and the cache still holds the last
  // successful summary - these actions have to go with the worktree.
  const diff = diffQuery.isError ? undefined : diffQuery.data
  if (!diff?.branch) return null

  const busy = session.state === 'running' || session.state === 'waiting'

  return (
    <div data-testid="review-actions" className="flex flex-wrap items-center gap-2">
      <CommitDialog session={session} diff={diff} />
      <PullRequestDialog session={session} diff={diff} summary={summary} />
      <CheckpointsMenu session={session} busy={busy} />
      <DiscardDialog session={session} diff={diff} />
    </div>
  )
}
