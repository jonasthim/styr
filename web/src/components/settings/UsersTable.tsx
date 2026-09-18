// Settings (admin) > Users. Every row gets a role select except the current
// admin's own row, which is disabled - changing your own role away from
// admin would lock you out of this page with no one else able to undo it.
import { useState } from 'react'
import clsx from 'clsx'
import type { Role, User } from '../../api/types'

interface UsersTableProps {
  users: User[]
  currentUserId: string
  onUpdateRole: (id: string, role: Role) => Promise<void>
}

function UserRow({
  user,
  isSelf,
  onUpdateRole,
}: {
  user: User
  isSelf: boolean
  onUpdateRole: UsersTableProps['onUpdateRole']
}) {
  const [saving, setSaving] = useState(false)

  function handleChange(role: Role) {
    if (isSelf || role === user.role) return
    setSaving(true)
    void onUpdateRole(user.id, role).finally(() => setSaving(false))
  }

  return (
    <tr className="border-t border-hairline" data-testid={`user-row-${user.id}`}>
      <td className="px-3 py-2">
        <p className="text-[13px] text-fg-primary">
          {user.display_name}
          {isSelf && <span className="ml-1.5 text-[12px] text-fg-muted">(you)</span>}
        </p>
        <p className="text-[12px] text-fg-muted">{user.email}</p>
      </td>
      <td className="px-3 py-2">
        <select
          aria-label={`Role for ${user.display_name}`}
          value={user.role}
          disabled={isSelf || saving}
          title={isSelf ? "You can't change your own role." : undefined}
          onChange={(e) => handleChange(e.target.value as Role)}
          className="h-8 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 text-[13px] text-fg-primary outline-none disabled:border-transparent disabled:bg-transparent disabled:text-fg-secondary"
        >
          <option value="member">Member</option>
          <option value="admin">Admin</option>
        </select>
      </td>
      <td className={clsx('px-3 py-2 text-[12px] text-fg-muted', saving && 'text-fg-secondary')}>
        {saving ? 'Saving…' : ''}
      </td>
    </tr>
  )
}

export function UsersTable({ users, currentUserId, onUpdateRole }: UsersTableProps) {
  return (
    <div className="overflow-x-auto rounded-[var(--radius-2)] border border-hairline">
      <table className="w-full min-w-[420px] border-collapse text-left">
        <thead>
          <tr className="text-[12px] font-medium text-fg-muted">
            <th className="px-3 py-2 font-medium">User</th>
            <th className="px-3 py-2 font-medium">Role</th>
            <th className="px-3 py-2 font-medium" />
          </tr>
        </thead>
        <tbody>
          {users.map((user) => (
            <UserRow key={user.id} user={user} isSelf={user.id === currentUserId} onUpdateRole={onUpdateRole} />
          ))}
        </tbody>
      </table>
    </div>
  )
}
