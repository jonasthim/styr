// T64: admin-only "Access" control for a shared (owner_id null) workspace -
// "everyone" (default, pre-T64 behaviour) or "listed" with a multi-select of
// users from GET /api/v1/users. Members left off a "listed" workspace no
// longer see it (Workspaces.tsx's list, the new-session dialog's workspace
// picker) and cannot start a session on it by hand.
import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../api/client'
import type { User, Workspace, WorkspaceAccess, WorkspaceAccessMode } from '../../api/types'
import { Button, Dialog, DialogContent, Select, Skeleton } from '../ui'

const MODE_OPTIONS = [
  { value: 'everyone', label: 'Everyone' },
  { value: 'listed', label: 'Listed users' },
]

export function WorkspaceAccessDialog({
  workspace,
  open,
  onOpenChange,
}: {
  workspace: Workspace | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const queryClient = useQueryClient()
  const id = workspace?.id ?? ''

  const access = useQuery({
    queryKey: ['workspace-access', id],
    queryFn: () => api<WorkspaceAccess>(`/api/v1/workspaces/${id}/access`),
    enabled: open && !!id,
  })
  const users = useQuery({
    queryKey: ['users'],
    queryFn: () => api<User[]>('/api/v1/users'),
    enabled: open,
  })

  const [mode, setMode] = useState<WorkspaceAccessMode>('everyone')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [saving, setSaving] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')

  // Seed local edit state from the fetched access row each time the dialog
  // opens for a (possibly different) workspace, not on every render.
  useEffect(() => {
    if (!open || !access.data) return
    setMode(access.data.access)
    setSelected(new Set(access.data.users.map((u) => u.id)))
    setErrorMessage('')
  }, [open, access.data])

  function toggleUser(userId: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(userId)) next.delete(userId)
      else next.add(userId)
      return next
    })
  }

  async function handleSave() {
    if (!workspace) return
    setSaving(true)
    setErrorMessage('')
    try {
      await api(`/api/v1/workspaces/${workspace.id}/access`, {
        method: 'PUT',
        json: { access: mode, user_ids: mode === 'listed' ? Array.from(selected) : [] },
      })
      queryClient.setQueryData<Workspace[]>(['workspaces'], (prev) =>
        prev?.map((w) => (w.id === workspace.id ? { ...w, access: mode } : w)),
      )
      await queryClient.invalidateQueries({ queryKey: ['workspace-access', workspace.id] })
      onOpenChange(false)
    } catch (err) {
      setErrorMessage(err instanceof ApiError ? err.message : 'Something went wrong saving access.')
    } finally {
      setSaving(false)
    }
  }

  const loading = access.isLoading || users.isLoading

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={workspace ? `Access to ${workspace.name}` : 'Workspace access'}
        description="Everyone can see and use a shared workspace by default. Switch to listed users to restrict it."
        width={440}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button variant="primary" onClick={() => void handleSave()} loading={saving} disabled={loading}>
              Save
            </Button>
          </>
        }
      >
        {loading ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-24 w-full" />
          </div>
        ) : (
          <div className="flex flex-col gap-4">
            <Select
              aria-label="Access"
              value={mode}
              onValueChange={(v) => setMode(v as WorkspaceAccessMode)}
              options={MODE_OPTIONS}
            />

            {mode === 'listed' && (
              <fieldset className="flex flex-col gap-1">
                <legend className="mb-1 text-[12px] font-medium text-fg-secondary">Users</legend>
                {(users.data ?? []).length === 0 ? (
                  <p className="text-[13px] text-fg-secondary">No other users yet.</p>
                ) : (
                  <ul
                    data-testid="workspace-access-users"
                    role="list"
                    className="flex max-h-56 flex-col gap-0.5 overflow-y-auto rounded-[var(--radius-control)] border border-hairline bg-surface-1 p-1"
                  >
                    {(users.data ?? []).map((u) => (
                      <li key={u.id}>
                        <label className="flex cursor-pointer items-center gap-2 rounded-[var(--radius-1)] px-2 py-1.5 text-[13px] text-fg-primary hover:bg-surface-2">
                          <input
                            type="checkbox"
                            checked={selected.has(u.id)}
                            onChange={() => toggleUser(u.id)}
                            className="h-3.5 w-3.5 accent-[var(--accent)]"
                          />
                          <span className="min-w-0 flex-1 truncate">{u.display_name}</span>
                          <span className="shrink-0 text-[11px] text-fg-muted">{u.email}</span>
                        </label>
                      </li>
                    ))}
                  </ul>
                )}
              </fieldset>
            )}

            {errorMessage && (
              <p role="alert" className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger">
                {errorMessage}
              </p>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
