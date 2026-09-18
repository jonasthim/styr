// The `/` menu that floats above the composer. Styled after the command
// palette's cmdk list (components/shell/Palette.tsx) and the Select popover,
// but driven by the composer's own textarea: the keys arrive there, so this is
// a presentational listbox with a roving `activeIndex` rather than a second
// focusable input.
import { useEffect, useRef } from 'react'
import clsx from 'clsx'
import type { SlashCommand } from './slashCommands'

export const SLASH_MENU_ID = 'composer-slash-menu'

export function slashOptionId(index: number): string {
  return `${SLASH_MENU_ID}-option-${index}`
}

export function SlashMenu({
  items,
  activeIndex,
  onSelect,
  onHover,
}: {
  items: SlashCommand[]
  activeIndex: number
  onSelect: (command: SlashCommand) => void
  onHover: (index: number) => void
}) {
  const activeRef = useRef<HTMLLIElement>(null)

  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: 'nearest' })
  }, [activeIndex])

  if (items.length === 0) return null

  return (
    <div
      data-testid="slash-menu"
      className="styr-panel absolute bottom-full left-0 z-40 mb-2! w-[min(420px,calc(100%-1rem))] overflow-hidden rounded-[var(--radius-2)] border border-hairline bg-surface-3 shadow-[var(--shadow-popover)]"
    >
      <ul id={SLASH_MENU_ID} role="listbox" aria-label="Commands" className="max-h-[260px] overflow-y-auto p-1">
        {items.map((item, index) => (
          <li
            key={`${item.kind}-${item.name}`}
            ref={index === activeIndex ? activeRef : undefined}
            id={slashOptionId(index)}
            role="option"
            aria-selected={index === activeIndex}
            onMouseEnter={() => onHover(index)}
            // Keep the textarea focused: the menu is driven from there.
            onMouseDown={(event) => {
              event.preventDefault()
              onSelect(item)
            }}
            className={clsx(
              'flex cursor-pointer items-baseline gap-2 rounded-[var(--radius-control)] px-2 py-1.5 text-[13px] text-fg-primary',
              index === activeIndex && 'bg-accent-subtle',
            )}
          >
            <span className="shrink-0 font-mono text-[12px]">/{item.name}</span>
            <span className="min-w-0 flex-1 truncate text-[12px] text-fg-secondary">{item.description}</span>
            {item.kind === 'styr' && (
              <span className="shrink-0 text-[11px] text-fg-muted" aria-hidden>
                styr
              </span>
            )}
          </li>
        ))}
      </ul>
    </div>
  )
}
