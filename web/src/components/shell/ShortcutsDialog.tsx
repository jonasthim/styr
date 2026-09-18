import * as Dialog from '@radix-ui/react-dialog'

const SHORTCUTS: Array<{ keys: string[]; description: string }> = [
  { keys: ['Cmd/Ctrl', 'K'], description: 'Open the command palette' },
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
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/60" />
        <Dialog.Content
          data-testid="shortcuts-dialog"
          className="fixed left-1/2 top-1/2 z-50 w-full max-w-[400px] -translate-x-1/2 -translate-y-1/2 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4 shadow-2xl"
        >
          <Dialog.Title className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">
            Shortcuts
          </Dialog.Title>
          <Dialog.Description className="sr-only">Keyboard shortcuts available throughout Styr</Dialog.Description>
          <ul className="mt-3 flex flex-col gap-1.5">
            {SHORTCUTS.map((shortcut) => (
              <li key={shortcut.description} className="flex items-center justify-between text-[13px]">
                <span className="text-fg-secondary">{shortcut.description}</span>
                <span className="flex gap-1">
                  {shortcut.keys.map((key) => (
                    <kbd
                      key={key}
                      className="rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-1.5 py-0.5 font-mono text-[11px] text-fg-primary"
                    >
                      {key}
                    </kbd>
                  ))}
                </span>
              </li>
            ))}
          </ul>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
