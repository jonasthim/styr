// The definition editor: a plain textarea with a line-number gutter beside
// it, and the validator's complaints listed underneath. No code editor -
// a pipeline is twenty lines of YAML, and what actually helps is knowing
// which line the validator is unhappy about, so the gutter marks those lines
// and clicking a complaint puts the caret on the line it came from.
import { useMemo, useRef, type UIEvent } from 'react'
import clsx from 'clsx'
import type { PipelineError } from '../../api/types'
import { textareaClasses } from '../ui'

const LINE_HEIGHT = 20

export function YamlEditor({
  id,
  value,
  onChange,
  errors,
  describedBy,
  height = 420,
}: {
  id?: string
  value: string
  onChange: (value: string) => void
  errors: PipelineError[]
  describedBy?: string
  height?: number
}) {
  const textareaRef = useRef<HTMLTextAreaElement>(null)
  const gutterRef = useRef<HTMLDivElement>(null)

  const lineCount = useMemo(() => Math.max(value.split('\n').length, 1), [value])
  const badLines = useMemo(() => new Set(errors.map((e) => e.line)), [errors])

  function handleScroll(event: UIEvent<HTMLTextAreaElement>) {
    if (gutterRef.current) gutterRef.current.scrollTop = event.currentTarget.scrollTop
  }

  /** Puts the caret at the start of `line` and scrolls it into view, so a
   * complaint is one click away from the thing it is complaining about. */
  function goToLine(line: number) {
    const textarea = textareaRef.current
    if (!textarea) return
    const offset = value.split('\n').slice(0, line - 1).join('\n').length + (line > 1 ? 1 : 0)
    textarea.focus()
    textarea.setSelectionRange(offset, offset)
    textarea.scrollTop = Math.max(0, (line - 4) * LINE_HEIGHT)
  }

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div
        className="flex min-w-0 overflow-hidden rounded-[var(--radius-control)] border border-strong bg-surface-2 focus-within:border-accent"
        style={{ height }}
      >
        <div
          ref={gutterRef}
          aria-hidden
          className="shrink-0 select-none overflow-hidden border-r border-hairline bg-surface-1 py-2 pl-2.5 pr-2 text-right font-mono text-[12px] leading-5 text-[var(--diff-gutter-fg)]"
        >
          {Array.from({ length: lineCount }, (_, i) => i + 1).map((line) => (
            <div
              key={line}
              className={clsx('tabular-nums', badLines.has(line) && 'rounded-[2px] bg-state-failed/15 text-fg-danger')}
            >
              {line}
            </div>
          ))}
        </div>
        <textarea
          ref={textareaRef}
          id={id}
          aria-describedby={describedBy}
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onScroll={handleScroll}
          className={clsx(
            textareaClasses,
            'h-full min-w-0 flex-1 resize-none rounded-none border-0 bg-transparent py-2 font-mono text-[12px] leading-5 shadow-none',
            'focus-visible:border-0 focus-visible:outline-none',
          )}
        />
      </div>

      {errors.length > 0 && (
        <ul className="flex flex-col gap-1" aria-label="Definition errors">
          {errors.map((error, index) => (
            <li key={`${error.line}-${index}`}>
              <button
                type="button"
                data-testid="yaml-error"
                onClick={() => goToLine(error.line)}
                className="flex w-full items-baseline gap-2 rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-2.5 py-1.5 text-left text-[12px] text-fg-danger outline-none hover:bg-state-failed/15 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]"
              >
                <span className="shrink-0 font-mono tabular-nums">Line {error.line}</span>
                <span className="min-w-0">{error.message}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
