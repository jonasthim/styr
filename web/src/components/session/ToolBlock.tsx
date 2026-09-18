// A collapsed-by-default 32px log line for one tool_use + tool_result pair.
// Bash shows its command instead of the tool name (see lib/blocks.ts's
// summarize(): for Bash, `summary` already *is* the command).
import { useState, type ComponentType } from 'react'
import { FilePen, FilePlus, FileText, Globe, Search, Terminal, Wrench } from 'lucide-react'
import type { Block } from '../../lib/blocks'

type ToolBlockData = Extract<Block, { kind: 'tool' }>

const TOOL_ICONS: Record<string, ComponentType<{ size?: number; className?: string }>> = {
  Bash: Terminal,
  Read: FileText,
  Edit: FilePen,
  Write: FilePlus,
  Grep: Search,
  Glob: Search,
  WebFetch: Globe,
  WebSearch: Globe,
}

function iconFor(tool: string): ComponentType<{ size?: number; className?: string }> {
  return TOOL_ICONS[tool] ?? Wrench
}

function formatDuration(startedAt: string, endedAt?: string): string | null {
  if (!endedAt) return null
  const ms = new Date(endedAt).getTime() - new Date(startedAt).getTime()
  if (!Number.isFinite(ms) || ms < 0) return null
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function prettyInput(input: unknown): string {
  try {
    return JSON.stringify(input, null, 2)
  } catch {
    return String(input)
  }
}

export function ToolBlock({ block, highlighted = false }: { block: ToolBlockData; highlighted?: boolean }) {
  const [open, setOpen] = useState(false)
  const Icon = iconFor(block.tool)
  const duration = formatDuration(block.startedAt, block.endedAt)
  const running = !block.result
  const failed = block.result?.isError === true
  const anchorId = block.id

  function toggle() {
    setOpen((o) => !o)
  }

  function onKeyDown(e: React.KeyboardEvent) {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      toggle()
    }
  }

  return (
    <div
      id={anchorId}
      className={`rounded-[var(--radius-1)] border bg-surface-1 ${highlighted ? 'border-accent' : 'border-hairline'}`}
    >
      <div
        role="button"
        tabIndex={0}
        aria-expanded={open}
        onClick={toggle}
        onKeyDown={onKeyDown}
        className="flex h-8 min-w-0 items-center gap-2 px-2.5! text-[13px] text-fg-primary"
      >
        <Icon size={14} className="shrink-0 text-fg-muted" />
        {block.tool !== 'Bash' && <span className="shrink-0 text-fg-secondary">{block.tool}</span>}
        <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-fg-primary">{block.summary}</span>
        {duration && <span className="shrink-0 font-mono text-[11px] tabular-nums text-fg-muted">{duration}</span>}
        <span
          aria-label={running ? 'Running' : failed ? 'Failed' : 'Succeeded'}
          className={`h-1.5 w-1.5 shrink-0 rounded-full ${
            running ? 'bg-state-attention' : failed ? 'bg-state-failed' : 'bg-state-running'
          }`}
        />
      </div>
      {open && (
        <div className="space-y-2 border-t border-hairline px-2.5! py-2!">
          <div>
            <div className="mb-1! text-[11px] font-medium text-fg-muted">Input</div>
            <pre className="max-h-[400px] overflow-auto rounded-[var(--radius-1)] bg-surface-2 p-2! font-mono text-[12px] leading-5 text-fg-primary">
              {prettyInput(block.input)}
            </pre>
          </div>
          {block.result && (
            <div>
              <div className="mb-1! text-[11px] font-medium text-fg-muted">Result</div>
              <pre
                data-testid="tool-result"
                className={`max-h-[400px] overflow-auto rounded-[var(--radius-1)] p-2! font-mono text-[12px] leading-5 ${
                  block.result.isError ? 'bg-surface-2 text-state-failed' : 'bg-surface-2 text-fg-primary'
                }`}
              >
                {block.result.content}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
