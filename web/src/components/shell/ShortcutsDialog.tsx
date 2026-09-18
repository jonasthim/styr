import { Dialog, DialogContent, Kbd, MOD_KEY } from '../ui'

const SHORTCUTS: Array<{ keys: string[]; description: string }> = [
  { keys: [MOD_KEY, 'K'], description: 'Open the command palette' },
  { keys: ['G', 'I'], description: 'Go to Inbox' },
  { keys: ['G', 'S'], description: 'Go to Sessions' },
  { keys: ['G', 'W'], description: 'Go to Workspaces' },
  { keys: ['N'], description: 'New session' },
  { keys: ['['], description: 'Pin or unpin the rail' },
  { keys: ['T'], description: 'Toggle theme' },
  { keys: ['?'], description: 'Show this cheat sheet' },
]

export function ShortcutsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        data-testid="shortcuts-dialog"
        title="Shortcuts"
        srOnlyDescription="Keyboard shortcuts available throughout Styr"
        width={420}
      >
        <ul className="flex flex-col divide-y divide-hairline">
          {SHORTCUTS.map((shortcut) => (
            <li
              key={shortcut.description}
              className="flex items-center justify-between gap-4 py-2 first:pt-0 last:pb-0"
            >
              <span className="text-[13px] text-fg-secondary">{shortcut.description}</span>
              <span className="flex shrink-0 gap-1">
                {shortcut.keys.map((key) => (
                  <Kbd key={key}>{key}</Kbd>
                ))}
              </span>
            </li>
          ))}
        </ul>
      </DialogContent>
    </Dialog>
  )
}
