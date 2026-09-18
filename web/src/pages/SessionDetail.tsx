// Session view: header, virtualised transcript, inline permission prompt,
// composer and the right-hand Activity/Changes/Info panel. Route:
// /sessions/$id (see router.tsx).
import { useMemo } from 'react'
import { useParams, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { q } from '../api/queries'
import { foldEvents, type Block } from '../lib/blocks'
import { SessionHeader } from '../components/session/SessionHeader'
import { Transcript } from '../components/session/Transcript'
import { PermissionCard } from '../components/session/PermissionCard'
import { Composer } from '../components/session/Composer'
import { SidePanel } from '../components/session/SidePanel'

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
  const sessionQuery = useQuery(q.session(id))
  const eventsQuery = useQuery(q.sessionEvents(id))
  const workspacesQuery = useQuery(q.workspaces())
  const profilesQuery = useQuery(q.profiles())

  const blocks = useMemo(() => foldEvents(eventsQuery.data ?? []), [eventsQuery.data])
  const visibleBlocks = useMemo(
    () => (search.file ? blocks.filter((b) => touchesFile(b, search.file!)) : blocks),
    [blocks, search.file],
  )

  if (sessionQuery.isLoading) {
    return (
      <div
        className="flex h-dvh items-center justify-center pb-14 text-[13px] text-fg-muted min-[900px]:pb-0"
        data-testid="session-loading"
      >
        Loading…
      </div>
    )
  }

  if (!sessionQuery.data) {
    return (
      <div className="flex h-dvh items-center justify-center pb-14 text-[13px] text-fg-muted min-[900px]:pb-0">
        Session not found.
      </div>
    )
  }

  const session = sessionQuery.data
  const workspaceName = workspacesQuery.data?.find((w) => w.id === session.workspace_id)?.name ?? session.workspace_id
  const profile = profilesQuery.data?.find((p) => p.id === session.profile_id)
  const profileName = profile?.name ?? session.profile_id

  return (
    <div
      data-testid="session-view"
      className="flex h-dvh min-h-0 flex-col pb-14 min-[900px]:pb-0 min-[900px]:pl-14"
    >
      <SessionHeader session={session} workspaceName={workspaceName} profileName={profileName} />
      <div className="flex min-h-0 flex-1 flex-col min-[1100px]:flex-row">
        <div className="flex min-h-0 flex-1 flex-col">
          <Transcript blocks={visibleBlocks} sessionId={session.id} />
          <PermissionCard sessionId={session.id} />
        </div>
        <SidePanel blocks={blocks} session={session} profile={profile} />
      </div>
      <Composer sessionId={session.id} sessionState={session.state} />
    </div>
  )
}
