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
import { useCanWrite } from '../hooks/useCanWrite'
import type { Session, SessionState } from '../api/types'
import { SessionRow } from '../components/sessions/SessionRow'
import { NewSessionDialog } from '../components/sessions/NewSessionDialog'
import { FleetGantt } from '../components/stats/FleetGantt'
import { Badge, Button, EmptyState, PageHeader, Skeleton, Tabs, TabsList, TabsTrigger } from '../components/ui'

// The Gantt's window, as the segmented control offers it.
const WINDOWS = [
  { value: '1h', label: '1 h', hours: 1 },
  { value: '6h', label: '6 h', hours: 6 },
  { value: '24h', label: '24 h', hours: 24 },
] as const

type WindowValue = (typeof WINDOWS)[number]['value']

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
  const canWrite = useCanWrite()
  const search = useSearch({ from: '/_app/sessions' })
  const sessions = useQuery(q.sessions())
  const workspaces = useQuery(q.workspaces())

  const view = search.view === 'gantt' ? 'gantt' : 'list'
  const windowValue: WindowValue = search.window ?? '1h'
  const hours = WINDOWS.find((w) => w.value === windowValue)?.hours ?? 1
  // Only asked for while the Gantt is on screen; it polls every 30 s
  // (api/queries.ts) so the lanes keep up with the fleet.
  const gantt = useQuery({ ...q.gantt(hours), enabled: view === 'gantt' })

  // A viewer never gets the dialog, even via a bookmarked ?new=1 link.
  const dialogOpen = canWrite && search.new === 1

  function setView(next: 'list' | 'gantt') {
    void navigate({
      to: '/sessions',
      search: next === 'gantt' ? { view: 'gantt', window: windowValue === '1h' ? undefined : windowValue } : {},
      replace: true,
    })
  }

  function setWindow(next: WindowValue) {
    void navigate({ to: '/sessions', search: { view: 'gantt', window: next === '1h' ? undefined : next }, replace: true })
  }

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
          canWrite ? (
            <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setDialogOpen(true)}>
              New session
            </Button>
          ) : undefined
        }
      />

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <Tabs value={view} onValueChange={(v) => setView(v as 'list' | 'gantt')}>
          <TabsList aria-label="Sessions view">
            <TabsTrigger value="list">List</TabsTrigger>
            <TabsTrigger value="gantt">Gantt</TabsTrigger>
          </TabsList>
        </Tabs>
        {view === 'gantt' && (
          <Tabs value={windowValue} onValueChange={(v) => setWindow(v as WindowValue)}>
            <TabsList aria-label="Gantt window">
              {WINDOWS.map((w) => (
                <TabsTrigger key={w.value} value={w.value}>
                  {w.label}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        )}
      </div>

      {view === 'gantt' && (
        <>
          {gantt.isLoading && <Skeleton className="mt-4 h-40 w-full" />}
          {gantt.data && gantt.data.lanes.length > 0 && <FleetGantt lanes={gantt.data.lanes} hours={hours} />}
          {gantt.isSuccess && gantt.data.lanes.length === 0 && (
            <EmptyState
              icon={<MessagesSquare size={18} aria-hidden />}
              title="Nothing ran in this window"
              description="Widen the window, or start a session — the fleet view fills in as sessions work."
              action={
                canWrite ? (
                  <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setDialogOpen(true)}>
                    New session
                  </Button>
                ) : undefined
              }
            />
          )}
        </>
      )}

      {view === 'list' && sessions.isLoading && (
        <div className="mt-4 overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-1 shadow-[var(--shadow-card)]">
          <SkeletonRow />
          <SkeletonRow />
          <SkeletonRow />
        </div>
      )}

      {/* Centred in the content area rather than boxed at the top of the
          page: an empty screen is the whole screen. */}
      {view === 'list' && isEmpty && (
        <EmptyState
          icon={<MessagesSquare size={18} aria-hidden />}
          title="No sessions yet"
          description="Start one and Styr streams every tool call, cost and permission prompt here as it happens."
          action={
            canWrite ? (
              <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setDialogOpen(true)}>
                New session
              </Button>
            ) : undefined
          }
        />
      )}

      {view === 'list' && !sessions.isLoading && !isEmpty && (
        <div className="mt-4 overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-1 shadow-[var(--shadow-card)]">
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
