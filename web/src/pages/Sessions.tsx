// Sessions list (Task 19): attention-ordered rows grouped under "Needs you",
// "Running", "Idle" and "Closed" headers, plus the New session dialog. The
// route already validates ?new=1 (see router.tsx) - the shell's `n`
// shortcut and the command palette's "New session" action both navigate
// there, and this page just reacts to the param instead of owning it.
import { useMemo } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { q } from '../api/queries'
import type { Session, SessionState } from '../api/types'
import { SessionRow } from '../components/sessions/SessionRow'
import { NewSessionDialog } from '../components/sessions/NewSessionDialog'

type Group = 'needs-you' | 'running' | 'idle' | 'closed'

const GROUP_ORDER: Array<{ key: Group; label: string }> = [
  { key: 'needs-you', label: 'Needs you' },
  { key: 'running', label: 'Running' },
  { key: 'idle', label: 'Idle' },
  { key: 'closed', label: 'Closed' },
]

function groupFor(state: SessionState): Group {
  switch (state) {
    case 'waiting':
      return 'needs-you'
    case 'running':
      return 'running'
    case 'open':
      return 'idle'
    case 'closed':
    case 'failed':
      return 'closed'
  }
}

/** Splits the API-ordered list into groups, preserving relative order within
 * each group - "Order comes from the API (attention order); do not re-sort
 * client side." */
function groupSessions(sessions: Session[]): Map<Group, Session[]> {
  const groups = new Map<Group, Session[]>()
  for (const session of sessions) {
    const key = groupFor(session.state)
    const bucket = groups.get(key)
    if (bucket) bucket.push(session)
    else groups.set(key, [session])
  }
  return groups
}

function SkeletonRow() {
  return (
    <div className="flex h-[var(--row-h)] items-center gap-3 border-b border-hairline px-3">
      <span className="h-2 w-2 shrink-0 animate-pulse rounded-full bg-surface-3" />
      <span className="h-3 w-40 max-w-[40%] animate-pulse rounded-[var(--radius-1)] bg-surface-3" />
      <span className="ml-auto h-3 w-16 animate-pulse rounded-[var(--radius-1)] bg-surface-3" />
    </div>
  )
}

function EmptyState({ onNewSession }: { onNewSession: () => void }) {
  return (
    <div className="flex flex-col items-center gap-3 border-b border-hairline px-3 py-16 text-center">
      <p className="text-[13px] text-fg-secondary">No sessions yet</p>
      <button
        type="button"
        onClick={onNewSession}
        className="h-8 rounded-[var(--radius-1)] bg-accent px-3 text-[13px] font-medium text-[#0b0d10] transition-opacity duration-150 hover:opacity-90"
      >
        New session
      </button>
    </div>
  )
}

export function Sessions() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/_app/sessions' })
  const sessions = useQuery(q.sessions())
  const workspaces = useQuery(q.workspaces())

  const dialogOpen = search.new === 1

  function setDialogOpen(open: boolean) {
    void navigate({ to: '/sessions', search: open ? { new: 1 } : {}, replace: true })
  }

  const workspaceNames = useMemo(() => {
    const names = new Map<string, string>()
    for (const workspace of workspaces.data ?? []) names.set(workspace.id, workspace.name)
    return names
  }, [workspaces.data])

  const grouped = useMemo(() => groupSessions(sessions.data ?? []), [sessions.data])
  const isEmpty = sessions.isSuccess && (sessions.data?.length ?? 0) === 0

  return (
    <div className="mx-auto max-w-[1100px] px-4 py-6 sm:px-6">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-[18px] font-semibold tracking-[-0.01em] text-fg-primary">Sessions</h1>
        <button
          type="button"
          onClick={() => setDialogOpen(true)}
          className="h-8 rounded-[var(--radius-1)] bg-accent px-3 text-[13px] font-medium text-[#0b0d10] transition-opacity duration-150 hover:opacity-90"
        >
          New session
        </button>
      </div>

      <div className="overflow-hidden rounded-[var(--radius-2)] border border-hairline bg-surface-1">
        {sessions.isLoading && (
          <>
            <SkeletonRow />
            <SkeletonRow />
            <SkeletonRow />
          </>
        )}

        {isEmpty && <EmptyState onNewSession={() => setDialogOpen(true)} />}

        {!sessions.isLoading &&
          !isEmpty &&
          GROUP_ORDER.filter(({ key }) => (grouped.get(key)?.length ?? 0) > 0).map(({ key, label }) => (
            <div key={key} data-testid={`session-group-${key}`}>
              <h2 className="border-b border-hairline bg-surface-2 px-3 py-1.5 text-[12px] font-medium text-fg-secondary">
                {label}
              </h2>
              {grouped.get(key)!.map((session) => (
                <SessionRow
                  key={session.id}
                  session={session}
                  workspaceName={workspaceNames.get(session.workspace_id) ?? session.workspace_id}
                />
              ))}
            </div>
          ))}
      </div>

      <NewSessionDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </div>
  )
}
