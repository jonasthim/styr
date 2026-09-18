// Information architecture #10 ("Settings", admin-only): service token,
// harness limits, profiles, users, OIDC providers, about.
import type { ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client'
import { q } from '../api/queries'
import { useMe } from '../hooks/useMe'
import type { ClaudeTokenInfo, Role, User } from '../api/types'
import { ClaudeTokenCard } from '../components/profile/ClaudeTokenCard'
import { NotificationsSection } from '../components/settings/NotificationsSection'
import { ProfilesTable, type ProfilePatch } from '../components/settings/ProfilesTable'
import { UsersTable } from '../components/settings/UsersTable'
import { Card, PageHeader } from '../components/ui'

// GET /api/v1/settings's shape (docs/openapi.yaml); not in api/types.ts,
// which this card leaves untouched, so it lives here instead.
interface SettingsInfo {
  max_open_sessions: number
  idle_timeout: string
  service_token: ClaudeTokenInfo
}

function Section({ title, note, children }: { title: string; note?: string; children: ReactNode }) {
  return (
    <section className="mt-8">
      <div className="mb-3">
        <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">{title}</h2>
        {note && <p className="mt-1 text-[13px] text-fg-secondary">{note}</p>}
      </div>
      {children}
    </section>
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
      <div className="mx-auto flex w-full max-w-[640px] flex-1 flex-col px-4 py-6 sm:px-6">
        <PageHeader title="Settings" description="Only admins can see server-wide settings." />
      </div>
    )
  }

  return (
    <div className="mx-auto flex w-full max-w-[880px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader title="Settings" description="Server-wide configuration, for admins only." />

      <div className="mt-6">
        <ClaudeTokenCard
          title="Service Claude token"
          inputLabel="Service Claude token"
          tokenInfo={settingsQuery.data?.service_token}
          onSave={handleSaveServiceToken}
          testId="service-token-card"
        />
      </div>

      <Section title="Limits" note="These are set in the server's config file, not here.">
        <Card>
          <dl className="flex flex-wrap gap-x-10 gap-y-4">
            <div>
              <dt className="text-[12px] font-medium text-fg-secondary">Max open sessions</dt>
              <dd className="mt-1 font-mono text-[15px] tabular-nums text-fg-primary">
                {settingsQuery.data?.max_open_sessions ?? '—'}
              </dd>
            </div>
            <div>
              <dt className="text-[12px] font-medium text-fg-secondary">Idle timeout</dt>
              <dd className="mt-1 font-mono text-[15px] tabular-nums text-fg-primary">
                {settingsQuery.data?.idle_timeout ?? '—'}
              </dd>
            </div>
          </dl>
        </Card>
      </Section>

      <Section title="Profiles" note="What a session may do on its own, and where it has to stop and ask.">
        {profiles.data && <ProfilesTable profiles={profiles.data} onUpdate={handleUpdateProfile} />}
      </Section>

      <Section title="Users" note="The first person to sign in became an admin.">
        {users.data && me && <UsersTable users={users.data} currentUserId={me.id} onUpdateRole={handleUpdateRole} />}
      </Section>

      <Section title="Notifications" note="Pushed when an unattended run finishes, needs a decision, or fails.">
        <NotificationsSection />
      </Section>

      <Section title="Sign-in providers" note="Configured in the server's config file.">
        <Card>
          <ul className="flex flex-col divide-y divide-hairline">
            {providers.data?.map((provider) => (
              <li key={provider.slug} className="flex items-center justify-between gap-3 py-2 first:pt-0 last:pb-0">
                <span className="text-[13px] text-fg-primary">{provider.name}</span>
                <span className="font-mono text-[12px] text-fg-secondary">{provider.slug}</span>
              </li>
            ))}
            {providers.data?.length === 0 && <li className="text-[13px] text-fg-secondary">No providers configured.</li>}
          </ul>
        </Card>
      </Section>

      <Section title="About">
        <Card>
          <div className="flex flex-wrap items-center gap-x-8 gap-y-3 text-[13px]">
            <span className="text-fg-secondary">
              Styr <span className="font-mono tabular-nums text-fg-primary">{status.data?.version ?? '—'}</span>
            </span>
            <span className="text-fg-secondary">
              Claude Code{' '}
              <span className="font-mono tabular-nums text-fg-primary">{status.data?.claude_version ?? '—'}</span>
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
        </Card>
      </Section>
    </div>
  )
}
