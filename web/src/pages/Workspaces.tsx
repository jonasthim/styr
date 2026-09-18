// Information architecture #9 ("Workspaces"): registered checkouts, default
// profile, worktree setting. Everyone can see the table; only admins get
// the "Add workspace" dialog (POST /api/v1/workspaces, path errors inline).
import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import * as Dialog from '@radix-ui/react-dialog'
import * as Switch from '@radix-ui/react-switch'
import clsx from 'clsx'
import { api, ApiError } from '../api/client'
import { q } from '../api/queries'
import { useMe } from '../hooks/useMe'
import type { Workspace } from '../api/types'

function WorktreesSwitch({ workspace, editable }: { workspace: Workspace; editable: boolean }) {
  const queryClient = useQueryClient()
  const [pending, setPending] = useState(false)

  async function handleChange(checked: boolean) {
    if (!editable) return
    setPending(true)
    try {
      await api(`/api/v1/workspaces/${workspace.id}`, { method: 'PATCH', json: { worktrees: checked } })
      await queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    } finally {
      setPending(false)
    }
  }

  return (
    <Switch.Root
      checked={workspace.worktrees}
      disabled={!editable || pending}
      onCheckedChange={(checked) => void handleChange(checked)}
      aria-label={`Worktrees for ${workspace.name}`}
      className="relative h-5 w-9 rounded-full bg-surface-3 outline-none transition-colors duration-150 focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-surface-1 data-[state=checked]:bg-accent disabled:opacity-50"
    >
      <Switch.Thumb className="block h-4 w-4 translate-x-0.5 rounded-full bg-[#0b0d10] transition-transform duration-150 will-change-transform data-[state=checked]:translate-x-[18px]" />
    </Switch.Root>
  )
}

function AddWorkspaceDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const queryClient = useQueryClient()
  const profiles = useQuery({ ...q.profiles(), enabled: open })
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [defaultProfileId, setDefaultProfileId] = useState('')
  const [worktrees, setWorktrees] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')

  const resolvedProfileId = defaultProfileId || profiles.data?.[0]?.id || ''

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setSubmitting(true)
    setErrorMessage('')
    try {
      await api('/api/v1/workspaces', {
        method: 'POST',
        json: { name, path, default_profile_id: resolvedProfileId, worktrees },
      })
      await queryClient.invalidateQueries({ queryKey: ['workspaces'] })
      setName('')
      setPath('')
      setWorktrees(false)
      onOpenChange(false)
    } catch (err) {
      setErrorMessage(err instanceof ApiError ? err.message : 'Something went wrong adding the workspace.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/60" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-full max-w-[420px] -translate-x-1/2 -translate-y-1/2 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4 shadow-2xl">
          <Dialog.Title className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">
            Add workspace
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-[13px] text-fg-secondary">
            Register a checkout Styr can start sessions against.
          </Dialog.Description>

          <form onSubmit={(e) => void handleSubmit(e)} className="mt-4 flex flex-col gap-3">
            <label className="flex flex-col gap-1 text-[12px] font-medium text-fg-secondary">
              Name
              <input
                required
                value={name}
                onChange={(e) => setName(e.target.value)}
                className="h-8 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 text-[13px] text-fg-primary outline-none focus-visible:ring-2 focus-visible:ring-accent"
              />
            </label>
            <label className="flex flex-col gap-1 text-[12px] font-medium text-fg-secondary">
              Path
              <input
                required
                value={path}
                onChange={(e) => setPath(e.target.value)}
                placeholder="/home/dev/project"
                className="h-8 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 font-mono text-[12px] text-fg-primary outline-none focus-visible:ring-2 focus-visible:ring-accent"
              />
            </label>
            <label className="flex flex-col gap-1 text-[12px] font-medium text-fg-secondary">
              Default profile
              <select
                value={resolvedProfileId}
                onChange={(e) => setDefaultProfileId(e.target.value)}
                className="h-8 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 text-[13px] text-fg-primary outline-none"
              >
                {profiles.data?.map((profile) => (
                  <option key={profile.id} value={profile.id}>
                    {profile.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex items-center justify-between text-[13px] text-fg-primary">
              Worktrees
              <Switch.Root
                checked={worktrees}
                onCheckedChange={setWorktrees}
                className="relative h-5 w-9 rounded-full bg-surface-3 outline-none transition-colors duration-150 focus-visible:ring-2 focus-visible:ring-accent data-[state=checked]:bg-accent"
              >
                <Switch.Thumb className="block h-4 w-4 translate-x-0.5 rounded-full bg-[#0b0d10] transition-transform duration-150 will-change-transform data-[state=checked]:translate-x-[18px]" />
              </Switch.Root>
            </label>

            {errorMessage && (
              <p role="alert" data-testid="add-workspace-error" className="text-[12px] text-state-failed">
                {errorMessage}
              </p>
            )}

            <div className="mt-1 flex justify-end gap-2">
              <Dialog.Close asChild>
                <button
                  type="button"
                  className="h-8 rounded-[var(--radius-1)] px-3 text-[13px] font-medium text-fg-secondary transition-colors duration-150 hover:text-fg-primary"
                >
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="submit"
                disabled={submitting}
                className={clsx(
                  'h-8 rounded-[var(--radius-1)] bg-accent px-3 text-[13px] font-medium text-[#0b0d10]',
                  submitting && 'opacity-60',
                )}
              >
                Add workspace
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}

export function Workspaces() {
  const { data: me } = useMe()
  const workspaces = useQuery(q.workspaces())
  const profiles = useQuery(q.profiles())
  const [dialogOpen, setDialogOpen] = useState(false)
  const isAdmin = me?.role === 'admin'

  function profileName(id: string): string {
    return profiles.data?.find((p) => p.id === id)?.name ?? id
  }

  return (
    <main className="mx-auto max-w-[880px] px-4 py-8 sm:px-6">
      <div className="flex items-center justify-between">
        <h1 className="text-[18px] font-semibold tracking-[-0.01em] text-fg-primary">Workspaces</h1>
        {isAdmin && (
          <button
            type="button"
            onClick={() => setDialogOpen(true)}
            className="h-8 rounded-[var(--radius-1)] bg-accent px-3 text-[13px] font-medium text-[#0b0d10]"
          >
            Add workspace
          </button>
        )}
      </div>

      {workspaces.data && workspaces.data.length === 0 && (
        <p className="mt-6 text-[13px] text-fg-muted">
          No workspaces yet. {isAdmin ? 'Add one to start a session.' : 'Ask an admin to add one.'}
        </p>
      )}

      {workspaces.data && workspaces.data.length > 0 && (
        <div className="mt-5 overflow-x-auto rounded-[var(--radius-2)] border border-hairline">
          <table className="w-full min-w-[560px] border-collapse text-left">
            <thead>
              <tr className="text-[12px] font-medium text-fg-muted">
                <th className="px-3 py-2 font-medium">Name</th>
                <th className="px-3 py-2 font-medium">Path</th>
                <th className="px-3 py-2 font-medium">Default profile</th>
                <th className="px-3 py-2 font-medium">Worktrees</th>
              </tr>
            </thead>
            <tbody>
              {workspaces.data.map((workspace) => (
                <tr key={workspace.id} className="border-t border-hairline" data-testid={`workspace-row-${workspace.id}`}>
                  <td className="px-3 py-2 text-[13px] text-fg-primary">{workspace.name}</td>
                  <td className="px-3 py-2 font-mono text-[12px] text-fg-secondary">{workspace.path}</td>
                  <td className="px-3 py-2 text-[13px] text-fg-secondary">
                    {profileName(workspace.default_profile_id)}
                  </td>
                  <td className="px-3 py-2">
                    <WorktreesSwitch workspace={workspace} editable={isAdmin} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <AddWorkspaceDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </main>
  )
}
