// Settings (admin) > Users. Every row gets a role select except the current
// admin's own row, which is disabled - changing your own role away from
// admin would lock you out of this page with no one else able to undo it.
import { useState } from 'react'
import clsx from 'clsx'
import type { Role, User } from '../../api/types'
import { Select, TableFrame, Td, Th, Tr } from '../ui'

const ROLE_OPTIONS = [
  { value: 'viewer', label: 'Viewer' },
  { value: 'member', label: 'Member' },
  { value: 'admin', label: 'Admin' },
]

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
    <Tr data-testid={`user-row-${user.id}`}>
      <Td>
        <p className="font-medium text-fg-primary">
          {user.display_name}
          {isSelf && <span className="ml-1.5 text-[12px] font-normal text-fg-muted">(you)</span>}
        </p>
        <p className="mt-0.5 text-[12px] text-fg-secondary">{user.email}</p>
      </Td>
      <Td>
        <Select
          aria-label={`Role for ${user.display_name}`}
          value={user.role}
          disabled={isSelf || saving}
          onValueChange={(value) => handleChange(value as Role)}
          options={ROLE_OPTIONS}
          className="max-w-[140px]"
        />
        {isSelf && <p className="mt-1 text-[11px] text-fg-muted">You can&apos;t change your own role.</p>}
      </Td>
      <Td className={clsx('text-[12px] text-fg-muted', saving && 'text-fg-secondary')}>{saving ? 'Saving…' : ''}</Td>
    </Tr>
  )
}

export function UsersTable({ users, currentUserId, onUpdateRole }: UsersTableProps) {
  return (
    <TableFrame minWidth={420}>
      <thead>
        <tr>
          <Th>User</Th>
          <Th>Role</Th>
          <Th />
        </tr>
      </thead>
      <tbody>
        {users.map((user) => (
          <UserRow key={user.id} user={user} isSelf={user.id === currentUserId} onUpdateRole={onUpdateRole} />
        ))}
      </tbody>
    </TableFrame>
  )
}
