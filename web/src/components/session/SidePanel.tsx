// Activity / Review / Info, tabbed with Radix Tabs. "Changes" became "Review"
// in v0.3: the real worktree diff, the comments on it and the Send review
// button live there now (components/review/ReviewPanel.tsx), with the old
// transcript-derived file list kept underneath it for sessions that have no
// worktree to diff. Mounted once by
// SessionDetail.tsx in a spot that lays out two ways with plain CSS
// breakpoints (no JS media-query check needed): on >= 1100px it becomes a
// fixed 320px column beside the transcript; below that it becomes a tab
// strip stacked above the composer that starts collapsed — on a phone the
// transcript is the page, and a permanently open panel left it a sliver —
// and opens to a bounded sheet when a tab is tapped (tapping the active tab
// again, or the chevron, collapses it). On desktop the panel is always open
// and the toggle state is inert.
import * as Tabs from '@radix-ui/react-tabs'
import { useQuery } from '@tanstack/react-query'
import clsx from 'clsx'
import { ChevronDown, ChevronUp } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { q } from '../../api/queries'
import { ActivityTimeline } from './ActivityTimeline'
import { InfoPanel } from './InfoPanel'
import { ReviewPanel } from '../review/ReviewPanel'
import type { Block } from '../../lib/blocks'
import type { Profile, Session } from '../../api/types'

function TabTrigger({ value, onClick, children }: { value: string; onClick: () => void; children: ReactNode }) {
  return (
    <Tabs.Trigger
      value={value}
      onClick={onClick}
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
  const [tab, setTab] = useState('activity')
  const [open, setOpen] = useState(false)

  function tapped(value: string) {
    if (value === tab) {
      setOpen((o) => !o)
    } else {
      setTab(value)
      setOpen(true)
    }
  }

  return (
    <div
      data-testid="side-panel"
      data-open={open || undefined}
      className={clsx(
        'flex shrink-0 flex-col border-t border-hairline bg-surface-1',
        open ? 'max-[1099px]:h-[42dvh]' : 'max-[1099px]:h-auto',
        'min-[1100px]:h-auto min-[1100px]:w-[320px] min-[1100px]:min-h-0 min-[1100px]:border-t-0 min-[1100px]:border-l',
        className,
      )}
    >
      <Tabs.Root value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col">
        <Tabs.List className="flex h-9 shrink-0 border-b border-hairline min-[1100px]:h-8">
          <TabTrigger value="activity" onClick={() => tapped('activity')}>
            Activity
          </TabTrigger>
          <TabTrigger value="review" onClick={() => tapped('review')}>
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
          <TabTrigger value="info" onClick={() => tapped('info')}>
            Info
          </TabTrigger>
          <button
            type="button"
            aria-label={open ? 'Collapse panel' : 'Expand panel'}
            aria-expanded={open}
            onClick={() => setOpen((o) => !o)}
            className="flex w-10 shrink-0 items-center justify-center text-fg-muted outline-none hover:text-fg-primary focus-visible:text-fg-primary min-[1100px]:hidden"
          >
            {open ? <ChevronDown size={14} aria-hidden /> : <ChevronUp size={14} aria-hidden />}
          </button>
        </Tabs.List>
        <div className={clsx('min-h-0 flex-1 flex-col', open ? 'flex' : 'hidden min-[1100px]:flex')}>
          <Tabs.Content value="activity" className="min-h-0 flex-1 overflow-y-auto p-3 outline-none">
            <ActivityTimeline blocks={blocks} running={session.state === 'running'} />
          </Tabs.Content>
          <Tabs.Content value="review" className="min-h-0 flex-1 overflow-y-auto outline-none">
            <ReviewPanel session={session} blocks={blocks} />
          </Tabs.Content>
          <Tabs.Content value="info" className="min-h-0 flex-1 overflow-y-auto outline-none">
            <InfoPanel session={session} profile={profile} />
          </Tabs.Content>
        </div>
      </Tabs.Root>
    </div>
  )
}
