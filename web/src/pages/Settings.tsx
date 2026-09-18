// Information architecture #10 ("Settings", admin-only): service token,
// harness limits, profiles, users, OIDC providers, about.
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { q } from '../api/queries'
import { useMe } from '../hooks/useMe'
import type { ClaudeTokenInfo, Role, User } from '../api/types'
import { ClaudeTokenCard } from '../components/profile/ClaudeTokenCard'
import { ProfilesTable, type ProfilePatch } from '../components/settings/ProfilesTable'
import { UsersTable } from '../components/settings/UsersTable'

// GET /api/v1/settings's shape (docs/openapi.yaml); not in api/types.ts,
// which this card leaves untouched, so it lives here instead.
interface SettingsInfo {
  max_open_sessions: number
  idle_timeout: string
  service_token: ClaudeTokenInfo
}

function SectionHeading({ title, note }: { title: string; note?: string }) {
  return (
    <div className="mb-3">
      <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">{title}</h2>
      {note && <p className="mt-1 text-[13px] text-fg-secondary">{note}</p>}
    </div>
  )
}

export function Settings() {
  const { data: me } = useMe()
  const queryClient = useQueryClient()

  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: () => api<SettingsInfo>('/api/v1/settings'),
    enabled: me?.role === 'admin',
  })
  const users = useQuery({
    queryKey: ['users'],
    queryFn: () => api<User[]>('/api/v1/users'),
    enabled: me?.role === 'admin',
  })
  const profiles = useQuery({ ...q.profiles(), enabled: me?.role === 'admin' })
  const providers = useQuery({ ...q.providers(), enabled: me?.role === 'admin' })
  const status = useQuery({ ...q.status(), enabled: me?.role === 'admin' })

  async function handleSaveServiceToken(token: string) {
    await api('/api/v1/settings/service-token', { method: 'PUT', json: { token } })
    await queryClient.invalidateQueries({ queryKey: ['settings'] })
  }

  async function handleUpdateProfile(id: string, patch: ProfilePatch) {
    await api(`/api/v1/profiles/${id}`, { method: 'PATCH', json: patch })
    await queryClient.invalidateQueries({ queryKey: ['profiles'] })
  }

  async function handleUpdateRole(id: string, role: Role) {
    await api(`/api/v1/users/${id}`, { method: 'PATCH', json: { role } })
    await queryClient.invalidateQueries({ queryKey: ['users'] })
  }

  if (me && me.role !== 'admin') {
    return (
      <main className="mx-auto max-w-[640px] px-4 py-8 sm:px-6">
        <h1 className="text-[18px] font-semibold tracking-[-0.01em] text-fg-primary">Settings</h1>
        <p className="mt-2 text-[13px] text-fg-secondary">Only admins can see server-wide settings.</p>
      </main>
    )
  }

  return (
    <main className="mx-auto max-w-[880px] px-4 py-8 sm:px-6">
      <h1 className="text-[18px] font-semibold tracking-[-0.01em] text-fg-primary">Settings</h1>

      <div className="mt-5">
        <ClaudeTokenCard
          title="Service Claude token"
          inputLabel="Service Claude token"
          tokenInfo={settingsQuery.data?.service_token}
          onSave={handleSaveServiceToken}
          testId="service-token-card"
        />
      </div>

      <section className="mt-6">
        <SectionHeading title="Limits" note="These are set in the server's config file, not here." />
        <dl className="flex flex-wrap gap-6 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4">
          <div>
            <dt className="text-[12px] text-fg-muted">Max open sessions</dt>
            <dd className="mt-0.5 font-mono text-[13px] tabular-nums text-fg-primary">
              {settingsQuery.data?.max_open_sessions ?? '—'}
            </dd>
          </div>
          <div>
            <dt className="text-[12px] text-fg-muted">Idle timeout</dt>
            <dd className="mt-0.5 font-mono text-[13px] tabular-nums text-fg-primary">
              {settingsQuery.data?.idle_timeout ?? '—'}
            </dd>
          </div>
        </dl>
      </section>

      <section className="mt-6">
        <SectionHeading title="Profiles" />
        {profiles.data && <ProfilesTable profiles={profiles.data} onUpdate={handleUpdateProfile} />}
      </section>

      <section className="mt-6">
        <SectionHeading title="Users" />
        {users.data && me && (
          <UsersTable users={users.data} currentUserId={me.id} onUpdateRole={handleUpdateRole} />
        )}
      </section>

      <section className="mt-6">
        <SectionHeading title="Sign-in providers" note="Configured in the server's config file." />
        <ul className="flex flex-col gap-1 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4">
          {providers.data?.map((provider) => (
            <li key={provider.slug} className="flex items-center justify-between text-[13px]">
              <span className="text-fg-primary">{provider.name}</span>
              <span className="font-mono text-[12px] text-fg-muted">{provider.slug}</span>
            </li>
          ))}
          {providers.data?.length === 0 && <li className="text-[13px] text-fg-muted">No providers configured.</li>}
        </ul>
      </section>

      <section className="mt-6">
        <SectionHeading title="About" />
        <div className="flex flex-wrap items-center gap-x-6 gap-y-2 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4 text-[13px]">
          <span className="text-fg-secondary">
            Styr <span className="font-mono text-fg-primary">{status.data?.version ?? '—'}</span>
          </span>
          <span className="text-fg-secondary">
            Claude Code <span className="font-mono text-fg-primary">{status.data?.claude_version ?? '—'}</span>
          </span>
          <a
            href="https://github.com/jonasthim/styr/blob/main/LICENSE"
            target="_blank"
            rel="noreferrer"
            className="text-accent underline-offset-2 hover:underline"
          >
            Licence
          </a>
        </div>
      </section>
    </main>
  )
}
