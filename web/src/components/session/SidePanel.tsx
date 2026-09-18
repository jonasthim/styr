// Activity / Review / Info, tabbed with Radix Tabs. "Changes" became "Review"
// in v0.3: the real worktree diff, the comments on it and the Send review
// button live there now (components/review/ReviewPanel.tsx), with the old
// transcript-derived file list kept underneath it for sessions that have no
// worktree to diff. Mounted once by
// SessionDetail.tsx in a spot that lays out two ways with plain CSS
// breakpoints (no JS media-query check needed): on >= 1100px it becomes a
// fixed 320px column beside the transcript; below that it becomes a bounded
// strip stacked above the composer, matching "right panel 320px on >= 1100
// px, otherwise a Radix Tabs strip above the composer" from the card.
import * as Tabs from '@radix-ui/react-tabs'
import { useQuery } from '@tanstack/react-query'
import clsx from 'clsx'
import type { ReactNode } from 'react'
import { q } from '../../api/queries'
import { ActivityTimeline } from './ActivityTimeline'
import { InfoPanel } from './InfoPanel'
import { ReviewPanel } from '../review/ReviewPanel'
import type { Block } from '../../lib/blocks'
import type { Profile, Session } from '../../api/types'

function TabTrigger({ value, children }: { value: string; children: ReactNode }) {
  return (
    <Tabs.Trigger
      value={value}
      className="flex-1 border-b-2 border-transparent text-[12px] font-medium text-fg-secondary outline-none transition-colors duration-150 hover:text-fg-primary focus-visible:text-fg-primary data-[state=active]:border-accent data-[state=active]:text-fg-primary"
    >
      {children}
    </Tabs.Trigger>
  )
}

export function SidePanel({
  blocks,
  session,
  profile,
  className,
}: {
  blocks: Block[]
  session: Session
  profile?: Profile
  className?: string
}) {
  const commentsQuery = useQuery(q.sessionComments(session.id))
  const unsent = (commentsQuery.data ?? []).filter((c) => !c.sent_at).length

  return (
    <div
      data-testid="side-panel"
      className={clsx(
        'flex h-60 shrink-0 flex-col border-t border-hairline bg-surface-1',
        'min-[1100px]:h-auto min-[1100px]:w-[320px] min-[1100px]:min-h-0 min-[1100px]:border-t-0 min-[1100px]:border-l',
        className,
      )}
    >
      <Tabs.Root defaultValue="activity" className="flex min-h-0 flex-1 flex-col">
        <Tabs.List className="flex h-8 shrink-0 border-b border-hairline">
          <TabTrigger value="activity">Activity</TabTrigger>
          <TabTrigger value="review">
            Review
            {unsent > 0 && (
              <span
                aria-label={`${unsent} unsent ${unsent === 1 ? 'comment' : 'comments'}`}
                className="ml-1.5 rounded-full bg-accent px-1.5 py-px text-[10px] font-semibold tabular-nums text-accent-fg"
              >
                {unsent}
              </span>
            )}
          </TabTrigger>
          <TabTrigger value="info">Info</TabTrigger>
        </Tabs.List>
        <Tabs.Content value="activity" className="min-h-0 flex-1 overflow-y-auto p-3 outline-none">
          <ActivityTimeline blocks={blocks} running={session.state === 'running'} />
        </Tabs.Content>
        <Tabs.Content value="review" className="min-h-0 flex-1 overflow-y-auto outline-none">
          <ReviewPanel session={session} blocks={blocks} />
        </Tabs.Content>
        <Tabs.Content value="info" className="min-h-0 flex-1 overflow-y-auto outline-none">
          <InfoPanel session={session} profile={profile} />
        </Tabs.Content>
      </Tabs.Root>
    </div>
  )
}
