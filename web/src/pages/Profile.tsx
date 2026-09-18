// Information architecture #11 ("Profile"): name and avatar from OIDC
// (read-only), own Claude token, theme, shortcuts, sign out.
import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import clsx from 'clsx'
import { api } from '../api/client'
import { useMe } from '../hooks/useMe'
import { useTheme } from '../hooks/useTheme'
import { useUiStore } from '../store/ui'
import { ClaudeTokenCard } from '../components/profile/ClaudeTokenCard'

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  return parts
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase())
    .join('')
}

function ThemeButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: string }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={clsx(
        'h-8 rounded-[var(--radius-1)] border px-3 text-[13px] font-medium transition-colors duration-150',
        active
          ? 'border-accent bg-surface-2 text-fg-primary'
          : 'border-hairline text-fg-secondary hover:bg-surface-2 hover:text-fg-primary',
      )}
    >
      {children}
    </button>
  )
}

function ThemeSelector() {
  const { theme, setTheme } = useTheme()
  // The theme store only persists "dark" or "light" (see useTheme.ts); there
  // is no third stored value for "system". Choosing System here just applies
  // the OS preference once, the same as Dark/Light would - it doesn't keep
  // tracking future OS changes, so it isn't shown as its own pressed state.
  function chooseSystem() {
    const prefersDark = window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? true
    setTheme(prefersDark ? 'dark' : 'light')
  }

  return (
    <div role="group" aria-label="Theme" className="flex gap-2">
      <ThemeButton active={false} onClick={chooseSystem}>
        System
      </ThemeButton>
      <ThemeButton active={theme === 'dark'} onClick={() => setTheme('dark')}>
        Dark
      </ThemeButton>
      <ThemeButton active={theme === 'light'} onClick={() => setTheme('light')}>
        Light
      </ThemeButton>
    </div>
  )
}

export function Profile() {
  const { data: me } = useMe()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const setShortcutsOpen = useUiStore((s) => s.setShortcutsOpen)
  const [signingOut, setSigningOut] = useState(false)

  async function handleSaveToken(token: string) {
    await api('/api/v1/me/claude-token', { method: 'PUT', json: { token } })
    await queryClient.invalidateQueries({ queryKey: ['me'] })
  }

  async function handleRemoveToken() {
    await api('/api/v1/me/claude-token', { method: 'DELETE' })
    await queryClient.invalidateQueries({ queryKey: ['me'] })
  }

  // "Sign out everywhere" also posts to /api/v1/auth/logout: v0.1's API
  // (docs/openapi.yaml) has one logout route and no per-device session
  // list to revoke individually, so both actions end the one session a
  // browser can have. The two buttons stay in case a later API version
  // adds real multi-device revocation.
  async function handleSignOut() {
    setSigningOut(true)
    try {
      await api('/api/v1/auth/logout', { method: 'POST' })
    } catch {
      // Best-effort: navigate away regardless.
    }
    void navigate({ to: '/login' })
  }

  return (
    <main className="mx-auto max-w-[640px] px-4 py-8 sm:px-6">
      <h1 className="text-[18px] font-semibold tracking-[-0.01em] text-fg-primary">Profile</h1>

      <section className="mt-5 flex items-center gap-4 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4">
        <div className="flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-full bg-accent text-[15px] font-semibold text-[#0b0d10]">
          {me?.avatar_url ? (
            <img src={me.avatar_url} alt="" className="h-full w-full object-cover" />
          ) : (
            initials(me?.display_name ?? '?')
          )}
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate text-[15px] font-semibold text-fg-primary">{me?.display_name}</p>
          <p className="truncate text-[13px] text-fg-secondary">{me?.email}</p>
        </div>
        {me && (
          <span
            data-testid="role-chip"
            className={clsx(
              'shrink-0 rounded-full border px-2 py-0.5 text-[11px] font-medium capitalize',
              me.role === 'admin' ? 'border-accent text-accent' : 'border-hairline text-fg-secondary',
            )}
          >
            {me.role}
          </span>
        )}
      </section>

      <div className="mt-4">
        <ClaudeTokenCard
          title="Your Claude token"
          inputLabel="Claude token"
          tokenInfo={me?.claude_token}
          onSave={handleSaveToken}
          onRemove={handleRemoveToken}
          testId="claude-token-card"
        />
      </div>

      <section className="mt-4 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4">
        <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">Appearance</h2>
        <p className="mt-1 text-[13px] text-fg-secondary">Choose how Styr looks on this device.</p>
        <div className="mt-3">
          <ThemeSelector />
        </div>
      </section>

      <section className="mt-4 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4">
        <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">Keyboard shortcuts</h2>
        <p className="mt-1 text-[13px] text-fg-secondary">See every shortcut, or press ? anywhere.</p>
        <button
          type="button"
          onClick={() => setShortcutsOpen(true)}
          className="mt-3 h-8 rounded-[var(--radius-1)] border border-hairline px-3 text-[13px] font-medium text-fg-primary transition-colors duration-150 hover:bg-surface-2"
        >
          Show shortcuts
        </button>
      </section>

      <section className="mt-4 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4">
        <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">Session</h2>
        <div className="mt-3 flex gap-2">
          <button
            type="button"
            onClick={() => void handleSignOut()}
            disabled={signingOut}
            className={clsx(
              'h-8 rounded-[var(--radius-1)] border border-hairline px-3 text-[13px] font-medium text-fg-primary transition-colors duration-150 hover:bg-surface-2',
              signingOut && 'opacity-60',
            )}
          >
            Sign out
          </button>
          <button
            type="button"
            onClick={() => void handleSignOut()}
            disabled={signingOut}
            className={clsx(
              'h-8 rounded-[var(--radius-1)] border border-hairline px-3 text-[13px] font-medium text-fg-primary transition-colors duration-150 hover:bg-surface-2',
              signingOut && 'opacity-60',
            )}
          >
            Sign out everywhere
          </button>
        </div>
      </section>
    </main>
  )
}
