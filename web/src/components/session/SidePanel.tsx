// Activity / Changes / Info, tabbed with Radix Tabs. Mounted once by
// SessionDetail.tsx in a spot that lays out two ways with plain CSS
// breakpoints (no JS media-query check needed): on >= 1100px it becomes a
// fixed 320px column beside the transcript; below that it becomes a bounded
// strip stacked above the composer, matching "right panel 320px on >= 1100
// px, otherwise a Radix Tabs strip above the composer" from the card.
import * as Tabs from '@radix-ui/react-tabs'
import type { ReactNode } from 'react'
import { ActivityTimeline } from './ActivityTimeline'
import { ChangesList } from './ChangesList'
import { InfoPanel } from './InfoPanel'
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

export function SidePanel({ blocks, session, profile }: { blocks: Block[]; session: Session; profile?: Profile }) {
  return (
    <div
      data-testid="side-panel"
      className="flex h-60 shrink-0 flex-col border-t border-hairline bg-surface-1 min-[1100px]:h-auto min-[1100px]:w-[320px] min-[1100px]:min-h-0 min-[1100px]:border-t-0 min-[1100px]:border-l"
    >
      <Tabs.Root defaultValue="activity" className="flex min-h-0 flex-1 flex-col">
        <Tabs.List className="flex h-8 shrink-0 border-b border-hairline">
          <TabTrigger value="activity">Activity</TabTrigger>
          <TabTrigger value="changes">Changes</TabTrigger>
          <TabTrigger value="info">Info</TabTrigger>
        </Tabs.List>
        <Tabs.Content value="activity" className="min-h-0 flex-1 overflow-y-auto p-3 outline-none">
          <ActivityTimeline blocks={blocks} running={session.state === 'running'} />
        </Tabs.Content>
        <Tabs.Content value="changes" className="min-h-0 flex-1 overflow-y-auto outline-none">
          <ChangesList blocks={blocks} />
        </Tabs.Content>
        <Tabs.Content value="info" className="min-h-0 flex-1 overflow-y-auto outline-none">
          <InfoPanel session={session} profile={profile} />
        </Tabs.Content>
      </Tabs.Root>
    </div>
  )
}
