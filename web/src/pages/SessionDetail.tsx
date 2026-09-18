// Session view: header, virtualised transcript, inline permission prompt and
// composer. Route: /sessions/$id (see router.tsx).
//
// The trailing `!` on padding/margin utilities in this file and under
// components/session/ works around a pre-existing base.css bug (not
// introduced here, and out of this card's file scope to fix): its
// `*, ::before, ::after { margin: 0; padding: 0 }` reset is an *unlayered*
// rule, and per the CSS cascade-layers spec unlayered rules always beat
// layered ones regardless of specificity — Tailwind wraps every utility in
// `@layer utilities`, so every plain p-*/m-* utility in the app (including
// Shell.tsx's own `pl-14`/`pb-14` on <main>) is silently zeroed. Marking a
// utility important (Tailwind v4's trailing `!`) is enough to win regardless
// of layering. The real fix is wrapping base.css's reset in `@layer base`.
import { useMemo } from 'react'
import { useParams } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { q } from '../api/queries'
import { foldEvents } from '../lib/blocks'
import { SessionHeader } from '../components/session/SessionHeader'
import { Transcript } from '../components/session/Transcript'
import { PermissionCard } from '../components/session/PermissionCard'
import { Composer } from '../components/session/Composer'

export function SessionDetail() {
  const { id } = useParams({ from: '/_app/sessions/$id' })
  const sessionQuery = useQuery(q.session(id))
  const eventsQuery = useQuery(q.sessionEvents(id))
  const workspacesQuery = useQuery(q.workspaces())
  const profilesQuery = useQuery(q.profiles())

  const blocks = useMemo(() => foldEvents(eventsQuery.data ?? []), [eventsQuery.data])

  if (sessionQuery.isLoading) {
    return (
      <div
        className="flex h-dvh items-center justify-center pb-14! text-[13px] text-fg-muted min-[900px]:pb-0!"
        data-testid="session-loading"
      >
        Loading…
      </div>
    )
  }

  if (!sessionQuery.data) {
    return (
      <div className="flex h-dvh items-center justify-center pb-14! text-[13px] text-fg-muted min-[900px]:pb-0!">
        Session not found.
      </div>
    )
  }

  const session = sessionQuery.data
  const workspaceName = workspacesQuery.data?.find((w) => w.id === session.workspace_id)?.name ?? session.workspace_id
  const profileName = profilesQuery.data?.find((p) => p.id === session.profile_id)?.name ?? session.profile_id

  return (
    // Shell.tsx's <main> is meant to reserve space for its own fixed chrome
    // (pl-14 for the desktop rail, pb-14 for the phone tab bar), but a
    // base.css bug (see block comment above the component) zeroes that out
    // along with every other padding/margin utility in the app, so this page
    // has to reserve the same space itself or its own content and footer
    // render underneath that fixed chrome instead of clear of it.
    <div
      data-testid="session-view"
      className="flex h-dvh min-h-0 flex-col pb-14! min-[900px]:pb-0! min-[900px]:pl-14!"
    >
      <SessionHeader session={session} workspaceName={workspaceName} profileName={profileName} />
      <Transcript blocks={blocks} sessionId={session.id} />
      <PermissionCard sessionId={session.id} />
      <Composer sessionId={session.id} sessionState={session.state} />
    </div>
  )
}
