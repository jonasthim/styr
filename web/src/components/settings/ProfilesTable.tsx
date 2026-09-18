// Settings (admin) > Profiles. Builtin rows (docs/openapi.yaml: "Builtin
// profiles only allow max_turns and approval_timeout to change") lock name
// and mode; custom rows are fully editable. Every mode select - locked or
// not - only ever renders these five values, never the skip-all-checks mode.
import { useState } from 'react'
import clsx from 'clsx'
import type { Profile, ProfileMode } from '../../api/types'

const MODE_OPTIONS: Array<{ value: ProfileMode; label: string }> = [
  { value: 'default', label: 'Default' },
  { value: 'acceptEdits', label: 'Accept edits' },
  { value: 'plan', label: 'Plan' },
  { value: 'dontAsk', label: "Don't ask" },
  { value: 'auto', label: 'Auto' },
]

export type ProfilePatch = Partial<Pick<Profile, 'name' | 'mode' | 'max_turns' | 'approval_timeout' | 'unattended'>>

interface ProfilesTableProps {
  profiles: Profile[]
  onUpdate: (id: string, patch: ProfilePatch) => Promise<void>
}

function ProfileRow({ profile, onUpdate }: { profile: Profile; onUpdate: ProfilesTableProps['onUpdate'] }) {
  const [name, setName] = useState(profile.name)
  const [maxTurns, setMaxTurns] = useState(String(profile.max_turns))
  const [approvalTimeout, setApprovalTimeout] = useState(String(profile.approval_timeout))
  const [saving, setSaving] = useState(false)
  const locked = profile.builtin

  function commitNumber(field: 'max_turns' | 'approval_timeout', raw: string) {
    const n = Number(raw)
    if (!Number.isFinite(n) || n === profile[field]) return
    setSaving(true)
    void onUpdate(profile.id, { [field]: n }).finally(() => setSaving(false))
  }

  return (
    <tr className="border-t border-hairline" data-testid={`profile-row-${profile.id}`}>
      <td className="px-3 py-2">
        <input
          value={name}
          disabled={locked}
          onChange={(e) => setName(e.target.value)}
          onBlur={() => {
            if (!locked && name !== profile.name && name.trim()) void onUpdate(profile.id, { name })
          }}
          className="h-8 w-full rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 text-[13px] text-fg-primary outline-none disabled:border-transparent disabled:bg-transparent disabled:text-fg-secondary focus-visible:ring-2 focus-visible:ring-accent"
        />
        {locked && <span className="ml-1 align-middle text-[11px] text-fg-muted">builtin</span>}
      </td>
      <td className="px-3 py-2">
        <select
          aria-label={`Mode for ${profile.name}`}
          value={profile.mode}
          disabled={locked}
          onChange={(e) => void onUpdate(profile.id, { mode: e.target.value as ProfileMode })}
          className="h-8 w-full rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 text-[13px] text-fg-primary outline-none disabled:border-transparent disabled:bg-transparent disabled:text-fg-secondary"
        >
          {MODE_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      </td>
      <td className="px-3 py-2">
        <input
          type="number"
          min={0}
          aria-label={`Max turns for ${profile.name}`}
          value={maxTurns}
          onChange={(e) => setMaxTurns(e.target.value)}
          onBlur={() => commitNumber('max_turns', maxTurns)}
          className="h-8 w-20 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 text-right font-mono text-[12px] tabular-nums text-fg-primary outline-none focus-visible:ring-2 focus-visible:ring-accent"
        />
      </td>
      <td className="px-3 py-2">
        <input
          type="number"
          min={0}
          aria-label={`Approval timeout in seconds for ${profile.name}`}
          value={approvalTimeout}
          onChange={(e) => setApprovalTimeout(e.target.value)}
          onBlur={() => commitNumber('approval_timeout', approvalTimeout)}
          className="h-8 w-24 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 text-right font-mono text-[12px] tabular-nums text-fg-primary outline-none focus-visible:ring-2 focus-visible:ring-accent"
        />
      </td>
      <td className={clsx('px-3 py-2 text-[12px] text-fg-muted', saving && 'text-fg-secondary')}>
        {saving ? 'Saving…' : ''}
      </td>
    </tr>
  )
}

export function ProfilesTable({ profiles, onUpdate }: ProfilesTableProps) {
  return (
    <div className="overflow-x-auto rounded-[var(--radius-2)] border border-hairline">
      <table className="w-full min-w-[560px] border-collapse text-left">
        <thead>
          <tr className="text-[12px] font-medium text-fg-muted">
            <th className="px-3 py-2 font-medium">Name</th>
            <th className="px-3 py-2 font-medium">Mode</th>
            <th className="px-3 py-2 font-medium">Max turns</th>
            <th className="px-3 py-2 font-medium">Approval timeout (s)</th>
            <th className="px-3 py-2 font-medium" />
          </tr>
        </thead>
        <tbody>
          {profiles.map((profile) => (
            <ProfileRow key={profile.id} profile={profile} onUpdate={onUpdate} />
          ))}
        </tbody>
      </table>
    </div>
  )
}
