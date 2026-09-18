// Settings (admin) > Profiles. Builtin rows (docs/openapi.yaml: "Builtin
// profiles only allow max_turns and approval_timeout to change") lock name
// and mode; custom rows are fully editable. Every mode select - locked or
// not - only ever renders these five values, never the skip-all-checks mode.
import { useState } from 'react'
import clsx from 'clsx'
import type { Profile, ProfileMode } from '../../api/types'
import { Badge, Input, Select, TableFrame, Td, Th, Tr } from '../ui'

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

// A locked field still has to read as the value it holds, not as a greyed-out
// box: drop the control chrome instead of dimming the text.
const LOCKED_INPUT = 'disabled:border-transparent disabled:bg-transparent disabled:px-0 disabled:opacity-100 disabled:shadow-none'

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
    <Tr data-testid={`profile-row-${profile.id}`}>
      <Td>
        <div className="flex items-center gap-2">
          <Input
            value={name}
            disabled={locked}
            aria-label={`Name for ${profile.name}`}
            onChange={(e) => setName(e.target.value)}
            onBlur={() => {
              if (!locked && name !== profile.name && name.trim()) void onUpdate(profile.id, { name })
            }}
            className={clsx('max-w-[180px] font-medium', LOCKED_INPUT)}
          />
          {locked && <Badge>builtin</Badge>}
        </div>
      </Td>
      <Td>
        <Select
          aria-label={`Mode for ${profile.name}`}
          value={profile.mode}
          disabled={locked}
          onValueChange={(value) => void onUpdate(profile.id, { mode: value as ProfileMode })}
          options={MODE_OPTIONS}
          className="max-w-[170px]"
        />
      </Td>
      <Td>
        <Input
          type="number"
          min={0}
          mono
          aria-label={`Max turns for ${profile.name}`}
          value={maxTurns}
          onChange={(e) => setMaxTurns(e.target.value)}
          onBlur={() => commitNumber('max_turns', maxTurns)}
          className="w-20! text-right tabular-nums"
        />
      </Td>
      <Td>
        <Input
          type="number"
          min={0}
          mono
          aria-label={`Approval timeout in seconds for ${profile.name}`}
          value={approvalTimeout}
          onChange={(e) => setApprovalTimeout(e.target.value)}
          onBlur={() => commitNumber('approval_timeout', approvalTimeout)}
          className="w-24! text-right tabular-nums"
        />
      </Td>
      <Td className={clsx('text-[12px] text-fg-muted', saving && 'text-fg-secondary')}>{saving ? 'Saving…' : ''}</Td>
    </Tr>
  )
}

export function ProfilesTable({ profiles, onUpdate }: ProfilesTableProps) {
  return (
    <TableFrame minWidth={640}>
      <thead>
        <tr>
          <Th>Name</Th>
          <Th>Mode</Th>
          <Th>Max turns</Th>
          <Th>Approval timeout (s)</Th>
          <Th />
        </tr>
      </thead>
      <tbody>
        {profiles.map((profile) => (
          <ProfileRow key={profile.id} profile={profile} onUpdate={onUpdate} />
        ))}
      </tbody>
    </TableFrame>
  )
}
