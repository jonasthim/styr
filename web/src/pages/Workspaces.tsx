// Information architecture #9 ("Workspaces"): registered checkouts, default
// profile, worktree setting. Everyone can see the table; only admins get
// the "Add workspace" dialog (POST /api/v1/workspaces, path errors inline).
import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { FolderKanban, Plus } from 'lucide-react'
import { api, ApiError } from '../api/client'
import { q } from '../api/queries'
import { useMe } from '../hooks/useMe'
import type { Workspace } from '../api/types'
import {
  Button,
  Dialog,
  DialogContent,
  EmptyState,
  Field,
  Input,
  PageHeader,
  Select,
  Switch,
  TableFrame,
  Td,
  Th,
  Tr,
} from '../components/ui'

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
    <Switch
      checked={workspace.worktrees}
      disabled={!editable || pending}
      onCheckedChange={(checked) => void handleChange(checked)}
      aria-label={`Worktrees for ${workspace.name}`}
    />
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
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title="Add workspace"
        description="Register a checkout Styr can start sessions against."
        width={460}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button variant="primary" type="submit" form="add-workspace-form" loading={submitting}>
              Add workspace
            </Button>
          </>
        }
      >
        <form id="add-workspace-form" onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-4">
          <Field label="Name">
            {({ id }) => <Input id={id} required value={name} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <Field label="Path" hint="An absolute path on the machine running Styr.">
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                mono
                required
                aria-describedby={describedBy}
                value={path}
                onChange={(e) => setPath(e.target.value)}
                placeholder="/home/dev/project"
              />
            )}
          </Field>
          <Field label="Default profile">
            {({ id }) => (
              <Select
                id={id}
                value={resolvedProfileId}
                onValueChange={setDefaultProfileId}
                placeholder="Choose a profile"
                options={(profiles.data ?? []).map((p) => ({ value: p.id, label: p.name }))}
              />
            )}
          </Field>

          <div className="flex items-center justify-between gap-4 rounded-[var(--radius-control)] border border-hairline bg-surface-1 px-3 py-2.5">
            <label htmlFor="add-workspace-worktrees" className="text-[13px] text-fg-primary">
              Worktrees
              <span className="mt-0.5 block text-[12px] text-fg-secondary">Give each session its own git worktree.</span>
            </label>
            <Switch id="add-workspace-worktrees" checked={worktrees} onCheckedChange={setWorktrees} />
          </div>

          {errorMessage && (
            <p
              role="alert"
              data-testid="add-workspace-error"
              className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger"
            >
              {errorMessage}
            </p>
          )}
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function Workspaces() {
  const { data: me } = useMe()
  const workspaces = useQuery(q.workspaces())
  const profiles = useQuery(q.profiles())
  const [dialogOpen, setDialogOpen] = useState(false)
  const isAdmin = me?.role === 'admin'
  const isEmpty = workspaces.isSuccess && workspaces.data.length === 0

  function profileName(id: string): string {
    return profiles.data?.find((p) => p.id === id)?.name ?? id
  }

  return (
    <div className="mx-auto flex w-full max-w-[880px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader
        title="Workspaces"
        description="Checkouts on this machine that a session can run inside."
        actions={
          isAdmin && !isEmpty ? (
            <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setDialogOpen(true)}>
              Add workspace
            </Button>
          ) : undefined
        }
      />

      {isEmpty && (
        <EmptyState
          icon={<FolderKanban size={18} aria-hidden />}
          title="No workspaces yet"
          description={
            isAdmin
              ? 'Register a checkout and Styr can start sessions inside it.'
              : 'Ask an admin to register a checkout, then you can start sessions in it.'
          }
          action={
            isAdmin ? (
              <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setDialogOpen(true)}>
                Add workspace
              </Button>
            ) : undefined
          }
        />
      )}

      {workspaces.data && workspaces.data.length > 0 && (
        <TableFrame className="mt-5">
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Path</Th>
              <Th>Default profile</Th>
              <Th className="text-right">Worktrees</Th>
            </tr>
          </thead>
          <tbody>
            {workspaces.data.map((workspace) => (
              <Tr key={workspace.id} data-testid={`workspace-row-${workspace.id}`}>
                <Td className="font-medium">{workspace.name}</Td>
                <Td className="font-mono text-[12px] text-fg-secondary">{workspace.path}</Td>
                <Td className="text-fg-secondary">{profileName(workspace.default_profile_id)}</Td>
                <Td className="text-right">
                  <div className="flex justify-end">
                    <WorktreesSwitch workspace={workspace} editable={isAdmin} />
                  </div>
                </Td>
              </Tr>
            ))}
          </tbody>
        </TableFrame>
      )}

      <AddWorkspaceDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </div>
  )
}
