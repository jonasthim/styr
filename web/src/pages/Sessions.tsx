// Sessions list (Task 19): attention-ordered rows grouped under "Needs you",
// "Running", "Idle" and "Closed" headers, plus the New session dialog. The
// route already validates ?new=1 (see router.tsx) - the shell's `n`
// shortcut and the command palette's "New session" action both navigate
// there, and this page just reacts to the param instead of owning it.
import { useMemo } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { MessagesSquare, Plus } from 'lucide-react'
import { q } from '../api/queries'
import type { Session, SessionState } from '../api/types'
import { SessionRow } from '../components/sessions/SessionRow'
import { NewSessionDialog } from '../components/sessions/NewSessionDialog'
import { Badge, Button, EmptyState, PageHeader, Skeleton } from '../components/ui'

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
    <div className="flex h-[var(--row-h)] items-center gap-3 border-b border-hairline px-3 last:border-b-0">
      <Skeleton className="h-2 w-2 shrink-0 rounded-full" />
      <Skeleton className="h-3 w-40 max-w-[40%]" />
      <Skeleton className="ml-auto h-3 w-16" />
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
  const needsYou = grouped.get('needs-you')?.length ?? 0

  function description(): string | undefined {
    if (isEmpty || sessions.isLoading) return undefined
    if (needsYou > 0) return `${needsYou} of ${sessions.data?.length ?? 0} are waiting on a decision from you.`
    return 'Everything Claude Code is running for you on this box.'
  }

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader
        title="Sessions"
        description={description()}
        actions={
          <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setDialogOpen(true)}>
            New session
          </Button>
        }
      />

      {sessions.isLoading && (
        <div className="mt-5 overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-1 shadow-[var(--shadow-card)]">
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </div>
      )}

      {/* Centred in the content area rather than boxed at the top of the
          page: an empty screen is the whole screen. */}
      {isEmpty && (
        <EmptyState
          icon={<MessagesSquare size={18} aria-hidden />}
          title="No sessions yet"
          description="Start one and Styr streams every tool call, cost and permission prompt here as it happens."
          action={
            <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setDialogOpen(true)}>
              New session
            </Button>
          }
        />
      )}

      {!sessions.isLoading && !isEmpty && (
        <div className="mt-5 overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-1 shadow-[var(--shadow-card)]">
          {GROUP_ORDER.filter(({ key }) => (grouped.get(key)?.length ?? 0) > 0).map(({ key, label }) => (
            // The last row in the card drops its divider so the hairline
            // doesn't double up with the card's own bottom edge.
            <div key={key} data-testid={`session-group-${key}`} className="last:[&_a:last-child]:border-b-0">
              <div className="flex h-8 items-center gap-2 border-b border-hairline bg-surface-2 px-3">
                <h2 className="text-[12px] font-medium tracking-[-0.005em] text-fg-secondary">{label}</h2>
                <Badge tone={key === 'needs-you' ? 'attention' : 'neutral'}>{grouped.get(key)!.length}</Badge>
              </div>
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
      )}

      <NewSessionDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </div>
  )
}
