// Renders a tool call's input for a human deciding whether to allow it.
// Bash shows the command alone in a mono block; Edit shows the file path and
// a truncated diff of old_string/new_string; anything else falls back to
// pretty-printed JSON. See docs/superpowers/plans/2026-09-18-styr-v0.1.md,
// "### Task 18: Inbox page (approvals)".
const DIFF_LINE_MAX = 240

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function truncate(text: string, max: number): string {
  return text.length > max ? `${text.slice(0, max)}…` : text
}

function stringField(input: Record<string, unknown>, key: string): string | undefined {
  const value = input[key]
  return typeof value === 'string' ? value : undefined
}

export function ToolInput({ tool, input }: { tool: string; input: unknown }) {
  if (tool === 'Bash' && isRecord(input)) {
    const command = stringField(input, 'command')
    if (command !== undefined) {
      return (
        <pre
          data-testid="tool-input-bash"
          className="overflow-x-auto rounded-[var(--radius-1)] bg-surface-3 px-3 py-2 font-mono text-[12px] text-fg-primary"
        >
          <code>{command}</code>
        </pre>
      )
    }
  }

  if (tool === 'Edit' && isRecord(input)) {
    const filePath = stringField(input, 'file_path')
    if (filePath !== undefined) {
      const oldString = stringField(input, 'old_string') ?? ''
      const newString = stringField(input, 'new_string') ?? ''
      return (
        <div data-testid="tool-input-edit" className="min-w-0">
          <p className="truncate font-mono text-[12px] text-fg-secondary">{filePath}</p>
          <pre className="mt-1 overflow-x-auto rounded-[var(--radius-1)] bg-surface-3 px-3 py-2 font-mono text-[12px] leading-5">
            <code className="block text-state-failed">- {truncate(oldString, DIFF_LINE_MAX)}</code>
            <code className="block text-state-running">+ {truncate(newString, DIFF_LINE_MAX)}</code>
          </pre>
        </div>
      )
    }
  }

  return (
    <pre
      data-testid="tool-input-json"
      className="overflow-x-auto rounded-[var(--radius-1)] bg-surface-3 px-3 py-2 font-mono text-[12px] text-fg-primary"
    >
      <code>{JSON.stringify(input, null, 2)}</code>
    </pre>
  )
}
