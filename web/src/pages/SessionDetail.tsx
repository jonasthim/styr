// Session view: header, virtualised transcript, inline permission prompt,
// composer and the right-hand Activity/Changes/Info panel. Route:
// /sessions/$id (see router.tsx).
import { useMemo } from 'react'
import { useNavigate, useParams, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import { q } from '../api/queries'
import { foldEvents, type Block } from '../lib/blocks'
import { SessionHeader } from '../components/session/SessionHeader'
import { Transcript } from '../components/session/Transcript'
import { PermissionCard } from '../components/session/PermissionCard'
import { Composer } from '../components/session/Composer'
import { SidePanel } from '../components/session/SidePanel'
import { PlanCard } from '../components/session/PlanCard'
import { DiffView } from '../components/review/DiffView'
import { Button } from '../components/ui'

// The session view is the one screen that does not scroll as a page: the
// transcript scrolls inside it. The shell reserves 56px for the tab bar below
// 900px, so the definite height it can fill is the viewport minus that.
const VIEW_HEIGHT = 'flex h-[calc(100dvh-3.5rem)] min-h-0 flex-col min-[900px]:h-dvh'

/** Blocks whose tool input targets the given file path (?file=, set by ChangesList.tsx). */
function touchesFile(block: Block, file: string): boolean {
  if (block.kind !== 'tool') return false
  const input = block.input
  if (input && typeof input === 'object' && 'file_path' in input) {
    return (input as Record<string, unknown>).file_path === file
  }
  return false
}

export function SessionDetail() {
  const { id } = useParams({ from: '/_app/sessions/$id' })
  const search = useSearch({ from: '/_app/sessions/$id' })
  const navigate = useNavigate()
  const sessionQuery = useQuery(q.session(id))
  const eventsQuery = useQuery(q.sessionEvents(id))
  const workspacesQuery = useQuery(q.workspaces())
  const profilesQuery = useQuery(q.profiles())

  const blocks = useMemo(() => foldEvents(eventsQuery.data ?? []), [eventsQuery.data])
  const visibleBlocks = useMemo(
    () => (search.file ? blocks.filter((b) => touchesFile(b, search.file!)) : blocks),
    [blocks, search.file],
  )
  // What the session last said, which is what a pull request body should
  // start as (components/review/PullRequestDialog.tsx).
  const lastText = useMemo(() => {
    for (let i = blocks.length - 1; i >= 0; i -= 1) {
      const block = blocks[i]
      if (block.kind === 'text') return block.text
    }
    return ''
  }, [blocks])

  function closeDiff() {
    void navigate({
      to: '/sessions/$id',
      params: { id },
      search: (prev) => ({ ...prev, diff: undefined }),
      replace: true,
    })
  }

  if (sessionQuery.isLoading) {
    return (
      <div
        className={`${VIEW_HEIGHT} items-center justify-center text-[13px] text-fg-secondary`}
        data-testid="session-loading"
      >
        Loading…
      </div>
    )
  }

  if (!sessionQuery.data) {
    return (
      <div className={`${VIEW_HEIGHT} items-center justify-center text-[13px] text-fg-secondary`}>
        Session not found.
      </div>
    )
  }

  const session = sessionQuery.data
  const workspaceName = workspacesQuery.data?.find((w) => w.id === session.workspace_id)?.name ?? session.workspace_id
  const profile = profilesQuery.data?.find((p) => p.id === session.profile_id)
  const profileName = profile?.name ?? session.profile_id

  return (
    // The shell's <main> already insets for the rail and the tab bar, so this
    // only needs a definite height to fill: the viewport, less the tab bar
    // below 900px where the shell reserves that space.
    <div data-testid="session-view" className={VIEW_HEIGHT}>
      <SessionHeader
        session={session}
        workspaceName={workspaceName}
        profileName={profileName}
        summary={lastText}
      />
      <div className="flex min-h-0 flex-1 flex-col min-[1100px]:flex-row">
        <div className="flex min-h-0 flex-1 flex-col">
          {search.diff ? (
            <>
              {/* Reviewing takes the main area over; the transcript stays one
                  click away rather than being pushed off-screen. */}
              <div className="flex shrink-0 items-center gap-2 border-b border-hairline bg-surface-1 px-3! py-1.5!">
                <Button size="sm" variant="ghost" icon={<ArrowLeft size={13} aria-hidden />} onClick={closeDiff}>
                  Back to transcript
                </Button>
                <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-fg-muted">{search.diff}</span>
              </div>
              <DiffView sessionId={session.id} path={search.diff} />
            </>
          ) : (
            <Transcript blocks={visibleBlocks} sessionId={session.id} />
          )}
          <PlanCard sessionId={session.id} />
          <PermissionCard sessionId={session.id} />
        </div>
        {/* Reading a diff on a phone needs the column: below the side-by-side
            breakpoint the full-height panel would leave the diff barely a
            hundred pixels tall, so it gives most of that back while a file is
            open — the file list and Send review are a scroll away, not a
            navigation away. */}
        <SidePanel
          blocks={blocks}
          session={session}
          profile={profile}
          className={search.diff ? 'max-[1099px]:h-32' : undefined}
        />
      </div>
      <Composer session={session} />
    </div>
  )
}
