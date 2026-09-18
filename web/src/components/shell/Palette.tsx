// Cmd/Ctrl+K command palette (cmdk). Three groups: "Go to" (pages), "Sessions"
// (fuzzy over titles, cmdk's default filter also matches the workspace name
// baked into each item's value), "Actions" (new session, toggle theme,
// shortcuts). Enter runs the highlighted item; Esc closes (both built into
// cmdk/Radix Dialog).
import { Command } from 'cmdk'
import { useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { q } from '../../api/queries'
import { useTheme } from '../../hooks/useTheme'
import { useUiStore } from '../../store/ui'
import type { Session } from '../../api/types'

const STATE_GLYPH: Record<Session['state'], string> = {
  open: '○',
  running: '●',
  waiting: '◐',
  closed: '□',
  failed: '✕',
}

const GO_TO_ITEMS = [
  { to: '/inbox', label: 'Inbox' },
  { to: '/sessions', label: 'Sessions' },
  { to: '/runs', label: 'Runs' },
  { to: '/triggers', label: 'Triggers' },
  { to: '/workspaces', label: 'Workspaces' },
  { to: '/profile', label: 'Profile' },
] as const

export function Palette({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const navigate = useNavigate()
  const { toggleTheme } = useTheme()
  const setShortcutsOpen = useUiStore((s) => s.setShortcutsOpen)
  const sessions = useQuery({ ...q.sessions(), enabled: open })
  const workspaces = useQuery({ ...q.workspaces(), enabled: open })

  function workspaceName(workspaceId: string): string {
    return workspaces.data?.find((w) => w.id === workspaceId)?.name ?? workspaceId
  }

  function close() {
    onOpenChange(false)
  }

  function goTo(to: string) {
    close()
    void navigate({ to })
  }

  return (
    <Command.Dialog
      open={open}
      onOpenChange={onOpenChange}
      label="Command palette"
      shouldFilter
      overlayClassName="styr-overlay fixed inset-0 z-50 bg-[var(--overlay)] backdrop-blur-[3px]"
      contentClassName="styr-panel fixed left-1/2 top-[14vh] z-50 w-[calc(100vw-2rem)] max-w-[560px] -translate-x-1/2 overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-surface-2 shadow-[var(--shadow-dialog)]"
    >
      <Command.Input
        autoFocus
        placeholder="Go to a page, session or action…"
        className="w-full border-b border-hairline bg-transparent px-4 py-3.5 text-[14px] text-fg-primary outline-none placeholder:text-fg-muted"
      />
      <Command.List className="max-h-[min(400px,60vh)] overflow-y-auto p-2">
        <Command.Empty className="px-3 py-8 text-center text-[13px] text-fg-secondary">No results</Command.Empty>

        <Command.Group
          heading="Go to"
          className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-fg-secondary [&_[cmdk-group-heading]]:tracking-[-0.005em]"
        >
          {GO_TO_ITEMS.map((item) => (
            <Command.Item
              key={item.to}
              value={item.label}
              onSelect={() => goTo(item.to)}
              className="flex h-8 cursor-pointer items-center rounded-[var(--radius-control)] px-2 text-[13px] text-fg-primary data-[selected=true]:bg-accent-subtle"
            >
              {item.label}
            </Command.Item>
          ))}
        </Command.Group>

        {sessions.data && sessions.data.length > 0 && (
          <Command.Group
            heading="Sessions"
            className="mt-1 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-fg-secondary [&_[cmdk-group-heading]]:tracking-[-0.005em]"
          >
            {sessions.data.map((session) => (
              <Command.Item
                key={session.id}
                value={`${session.title} ${workspaceName(session.workspace_id)}`}
                onSelect={() => goTo(`/sessions/${session.id}`)}
                className="flex h-8 cursor-pointer items-center gap-2 rounded-[var(--radius-control)] px-2 text-[13px] text-fg-primary data-[selected=true]:bg-accent-subtle"
              >
                <span className="text-fg-secondary" aria-hidden>
                  {STATE_GLYPH[session.state]}
                </span>
                <span className="min-w-0 flex-1 truncate">{session.title}</span>
                <span className="shrink-0 font-mono text-[11px] text-fg-muted">
                  {workspaceName(session.workspace_id)}
                </span>
              </Command.Item>
            ))}
          </Command.Group>
        )}

        <Command.Group
          heading="Actions"
          className="mt-1 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-fg-secondary [&_[cmdk-group-heading]]:tracking-[-0.005em]"
        >
          <Command.Item
            value="New session"
            onSelect={() => {
              close()
              void navigate({ to: '/sessions', search: { new: 1 } })
            }}
            className="flex h-8 cursor-pointer items-center rounded-[var(--radius-control)] px-2 text-[13px] text-fg-primary data-[selected=true]:bg-accent-subtle"
          >
            New session
          </Command.Item>
          <Command.Item
            value="Toggle theme"
            onSelect={() => {
              toggleTheme()
              close()
            }}
            className="flex h-8 cursor-pointer items-center rounded-[var(--radius-control)] px-2 text-[13px] text-fg-primary data-[selected=true]:bg-accent-subtle"
          >
            Toggle theme
          </Command.Item>
          <Command.Item
            value="Shortcuts"
            onSelect={() => {
              close()
              setShortcutsOpen(true)
            }}
            className="flex h-8 cursor-pointer items-center rounded-[var(--radius-control)] px-2 text-[13px] text-fg-primary data-[selected=true]:bg-accent-subtle"
          >
            Shortcuts
          </Command.Item>
        </Command.Group>
      </Command.List>
    </Command.Dialog>
  )
}
