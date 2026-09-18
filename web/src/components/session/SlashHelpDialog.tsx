// `/help`: the full command list, split into Styr's own actions and the ones
// the CLI reported for this session. The composer's menu filters as you type;
// this is the place to read the whole list.
import type { SlashCommand } from './slashCommands'
import { Dialog, DialogContent } from '../ui'

function Group({ title, note, items }: { title: string; note: string; items: SlashCommand[] }) {
  return (
    <section>
      <h3 className="text-[12px] font-medium text-fg-secondary">{title}</h3>
      <p className="mt-1 text-[12px] text-fg-muted">{note}</p>
      {items.length === 0 ? (
        <p className="mt-2 text-[13px] text-fg-secondary">None reported yet.</p>
      ) : (
        <dl className="mt-2 flex flex-col gap-1.5">
          {items.map((item) => (
            <div key={item.name} className="flex items-baseline gap-3">
              <dt className="w-[150px] shrink-0 truncate font-mono text-[12px] text-fg-primary">/{item.name}</dt>
              <dd className="min-w-0 flex-1 text-[13px] text-fg-secondary">{item.description}</dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  )
}

export function SlashHelpDialog({
  open,
  onOpenChange,
  commands,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  commands: SlashCommand[]
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Commands"
        description="Type / in the composer to filter this list."
        width={560}
        data-testid="slash-help"
      >
        <div className="flex max-h-[min(60vh,440px)] flex-col gap-5 overflow-y-auto">
          <Group
            title="Styr"
            note="Handled here; never sent to the session."
            items={commands.filter((c) => c.kind === 'styr')}
          />
          <Group
            title="Session"
            note="Sent to the CLI as-is, exactly as typed."
            items={commands.filter((c) => c.kind === 'cli')}
          />
        </div>
      </DialogContent>
    </Dialog>
  )
}
