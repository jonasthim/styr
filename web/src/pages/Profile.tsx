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
import { ApiTokens } from '../components/profile/ApiTokens'
import { ClaudeTokenCard } from '../components/profile/ClaudeTokenCard'
import { Badge, Button, Card, Kbd, PageHeader } from '../components/ui'

function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '?'
  return parts
    .slice(0, 2)
    .map((p) => p[0]?.toUpperCase())
    .join('')
}

// A segmented control rather than three loose buttons: the three options are
// one choice, so they share one frame and only the chosen one is filled.
function ThemeButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: string }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={clsx(
        'h-7 rounded-[var(--radius-1)] px-3 text-[13px] font-medium outline-none transition-colors duration-[var(--duration-fast)]',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]',
        active
          ? 'bg-surface-3 text-fg-primary shadow-[var(--shadow-card)]'
          : 'text-fg-secondary hover:text-fg-primary',
      )}
    >
      {children}
    </button>
  )
}

function ThemeSelector() {
  const { mode, setTheme, setMode } = useTheme()
  // "System" is the one mode Profile's selector can put the store into -
  // ThemeToggle, the `t` shortcut and the palette action only ever cycle
  // between the two explicit themes (see useTheme.ts). Picking it here
  // keeps tracking OS-level changes rather than applying the preference
  // once, so it does show its own pressed state.

  return (
    <div
      role="group"
      aria-label="Theme"
      className="inline-flex gap-1 rounded-[var(--radius-control)] border border-hairline bg-surface-2 p-1"
    >
      <ThemeButton active={mode === 'system'} onClick={() => setMode('system')}>
        System
      </ThemeButton>
      <ThemeButton active={mode === 'dark'} onClick={() => setTheme('dark')}>
        Dark
      </ThemeButton>
      <ThemeButton active={mode === 'light'} onClick={() => setTheme('light')}>
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

  async function handleSignOut() {
    setSigningOut(true)
    try {
      await api('/api/v1/auth/logout', { method: 'POST' })
    } catch {
      // Best-effort: navigate away regardless.
    }
    void navigate({ to: '/login' })
  }

  // "Sign out everywhere" ends every login session for this user
  // (docs/openapi.yaml POST /auth/logout-all -> db.LoginSessions.
  // DeleteAllForUser), not just the one behind this browser's own cookie.
  async function handleSignOutEverywhere() {
    setSigningOut(true)
    try {
      await api('/api/v1/auth/logout-all', { method: 'POST' })
    } catch {
      // Best-effort: navigate away regardless.
    }
    void navigate({ to: '/login' })
  }

  return (
    <div className="mx-auto flex w-full max-w-[640px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader title="Profile" />

      <Card className="mt-6">
        <div className="flex items-center gap-4">
          <div className="flex h-12 w-12 shrink-0 items-center justify-center overflow-hidden rounded-full bg-accent text-[15px] font-semibold text-accent-fg">
            {me?.avatar_url ? (
              <img src={me.avatar_url} alt="" className="h-full w-full object-cover" />
            ) : (
              initials(me?.display_name ?? '?')
            )}
          </div>
          <div className="min-w-0 flex-1">
            <p className="truncate text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">{me?.display_name}</p>
            <p className="truncate text-[13px] text-fg-secondary">{me?.email}</p>
          </div>
          {me && (
            <Badge
              data-testid="role-chip"
              tone={me.role === 'admin' ? 'accent' : 'neutral'}
              variant="outline"
              className="capitalize"
            >
              {me.role}
            </Badge>
          )}
        </div>
      </Card>

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

      <ApiTokens />

      <Card
        className="mt-4"
        title="Appearance"
        description="How Styr looks on this device."
        actions={<ThemeSelector />}
      />

      <Card
        className="mt-4"
        title="Keyboard shortcuts"
        description={
          <>
            Every shortcut, or press <Kbd>?</Kbd> anywhere.
          </>
        }
        actions={
          <Button onClick={() => setShortcutsOpen(true)}>Show shortcuts</Button>
        }
      />

      <Card className="mt-4" title="Session" description="End this browser's login, or every login you have.">
        <div className="flex flex-wrap gap-2">
          <Button onClick={() => void handleSignOut()} disabled={signingOut}>
            Sign out
          </Button>
          <Button variant="ghost" onClick={() => void handleSignOutEverywhere()} disabled={signingOut}>
            Sign out everywhere
          </Button>
        </div>
      </Card>
    </div>
  )
}
