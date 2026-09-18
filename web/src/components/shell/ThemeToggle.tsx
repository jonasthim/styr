import { Moon, Sun } from 'lucide-react'
import { useTheme } from '../../hooks/useTheme'

/** Small icon button that flips `data-theme`; also reachable via the `t` shortcut. */
export function ThemeToggle() {
  const { theme, toggleTheme } = useTheme()
  const Icon = theme === 'dark' ? Sun : Moon

  return (
    <button
      type="button"
      onClick={toggleTheme}
      data-testid="theme-toggle"
      aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
      className="flex h-8 w-8 shrink-0 items-center justify-center rounded-[var(--radius-1)] text-fg-secondary transition-colors duration-150 hover:bg-surface-2 hover:text-fg-primary"
    >
      <Icon size={16} aria-hidden />
    </button>
  )
}
