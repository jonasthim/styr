// Information architecture #9 ("Workspaces"): a per-user list of checkouts a
// session can run inside. Styr owns the path for a git or empty workspace -
// nobody types one - so nothing on this page shows a filesystem path unless
// the row's source is "path" (an admin registering a repo that already
// exists on the machine), and even then only to an admin.
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { FolderKanban, Plus, Trash2 } from 'lucide-react'
import { api } from '../api/client'
import { q } from '../api/queries'
import { useCanWrite } from '../hooks/useCanWrite'
import { useMe } from '../hooks/useMe'
import type { Profile, Workspace } from '../api/types'
import { AddWorkspaceForm } from '../components/workspaces/AddWorkspaceForm'
import { DeleteWorkspaceDialog } from '../components/workspaces/DeleteWorkspaceDialog'
import { WorkspaceAccessDialog } from '../components/workspaces/WorkspaceAccessDialog'
import { WorkspaceSourceChip, WorkspaceStateBadge } from '../components/workspaces/workspaceDisplay'
import { Button, Dialog, DialogContent, EmptyState, PageHeader, Select, Switch, TableFrame, Td, Th, Tr } from '../components/ui'
import { useCloningPoll } from '../hooks/useCloningPoll'

function WorktreesSwitch({ workspace, canWrite }: { workspace: Workspace; canWrite: boolean }) {
  const queryClient = useQueryClient()
  const [pending, setPending] = useState(false)

  async function handleChange(checked: boolean) {
    setPending(true)
    try {
      await api(`/api/v1/workspaces/${workspace.id}`, { method: 'PATCH', json: { worktrees: checked } })
      queryClient.setQueryData<Workspace[]>(['workspaces'], (prev) =>
        prev?.map((w) => (w.id === workspace.id ? { ...w, worktrees: checked } : w)),
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <Switch
      checked={workspace.worktrees}
      disabled={pending || !canWrite}
      onCheckedChange={(checked) => void handleChange(checked)}
      aria-label={`Worktrees for ${workspace.name}`}
    />
  )
}

function DefaultProfileSelect({ workspace, profiles, canWrite }: { workspace: Workspace; profiles: Profile[]; canWrite: boolean }) {
  const queryClient = useQueryClient()
  const [pending, setPending] = useState(false)

  async function handleChange(value: string) {
    setPending(true)
    try {
      await api(`/api/v1/workspaces/${workspace.id}`, { method: 'PATCH', json: { default_profile_id: value } })
      queryClient.setQueryData<Workspace[]>(['workspaces'], (prev) =>
        prev?.map((w) => (w.id === workspace.id ? { ...w, default_profile_id: value } : w)),
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <Select
      aria-label={`Default profile for ${workspace.name}`}
      value={workspace.default_profile_id}
      disabled={pending || !canWrite}
      onValueChange={(value) => void handleChange(value)}
      options={profiles.map((p) => ({ value: p.id, label: p.name }))}
      className="max-w-[170px]"
    />
  )
}

function RetryButton({ workspaceId, canWrite }: { workspaceId: string; canWrite: boolean }) {
  const queryClient = useQueryClient()
  const [pending, setPending] = useState(false)

  async function handleRetry() {
    setPending(true)
    try {
      await api(`/api/v1/workspaces/${workspaceId}/retry`, { method: 'POST' })
      queryClient.setQueryData<Workspace[]>(['workspaces'], (prev) =>
        prev?.map((w) => (w.id === workspaceId ? { ...w, state: 'cloning', error: '' } : w)),
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <Button variant="secondary" size="sm" onClick={() => void handleRetry()} loading={pending} disabled={!canWrite}>
      Retry
    </Button>
  )
}

// AccessControl is the admin-only "Access" cell: a button showing the
// current mode that opens WorkspaceAccessDialog. Only meaningful for a
// shared workspace (owner_id null) - an owned one shows a plain dash.
function AccessControl({ workspace, isAdmin }: { workspace: Workspace; isAdmin: boolean }) {
  const [open, setOpen] = useState(false)
  if (workspace.owner_id !== null) return <span className="text-fg-muted">—</span>
  if (!isAdmin) return <span className="text-fg-secondary">{workspace.access === 'listed' ? 'Listed' : 'Everyone'}</span>
  return (
    <>
      <Button variant="secondary" size="sm" onClick={() => setOpen(true)} data-testid={`workspace-access-${workspace.id}`}>
        {workspace.access === 'listed' ? 'Listed' : 'Everyone'}
      </Button>
      <WorkspaceAccessDialog workspace={workspace} open={open} onOpenChange={setOpen} />
    </>
  )
}

export function Workspaces() {
  const { data: me } = useMe()
  const canWrite = useCanWrite()
  const workspaces = useQuery(q.workspaces())
  useCloningPoll(workspaces.data)
  const profiles = useQuery(q.profiles())
  const [addOpen, setAddOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<Workspace | null>(null)
  const isAdmin = me?.role === 'admin'
  const isEmpty = workspaces.isSuccess && workspaces.data.length === 0

  const addButton = canWrite ? (
    <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setAddOpen(true)}>
      Add workspace
    </Button>
  ) : undefined

  return (
    <div className="mx-auto flex w-full max-w-[960px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader
        title="Workspaces"
        description="Where your sessions run. Bring a repository, start from an empty folder, or point at a checkout already on this machine."
        actions={!isEmpty ? addButton : undefined}
      />

      {isEmpty && (
        <EmptyState
          icon={<FolderKanban size={18} aria-hidden />}
          title="No workspaces yet"
          description="Styr keeps the checkout; you keep the prompt. Add a git repository or start from an empty folder."
          action={addButton}
        />
      )}

      {workspaces.data && workspaces.data.length > 0 && (
        <TableFrame className="mt-5" minWidth={760}>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Source</Th>
              <Th>State</Th>
              <Th>Default profile</Th>
              <Th className="text-right">Worktrees</Th>
              {isAdmin && <Th>Access</Th>}
              <Th className="w-10">
                <span className="sr-only">Actions</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {workspaces.data.map((workspace) => (
              <Tr key={workspace.id} data-testid={`workspace-row-${workspace.id}`}>
                <Td className="font-medium">{workspace.name}</Td>
                <Td>
                  <WorkspaceSourceChip workspace={workspace} isAdmin={isAdmin} />
                </Td>
                <Td>
                  <div className="flex items-center gap-2">
                    <WorkspaceStateBadge workspace={workspace} />
                    {workspace.state === 'failed' && <RetryButton workspaceId={workspace.id} canWrite={canWrite} />}
                  </div>
                </Td>
                <Td>
                  {profiles.data && <DefaultProfileSelect workspace={workspace} profiles={profiles.data} canWrite={canWrite} />}
                </Td>
                <Td className="text-right">
                  <div className="flex justify-end">
                    <WorktreesSwitch workspace={workspace} canWrite={canWrite} />
                  </div>
                </Td>
                {isAdmin && (
                  <Td>
                    <AccessControl workspace={workspace} isAdmin={isAdmin} />
                  </Td>
                )}
                <Td className="text-right">
                  {canWrite && (
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-label={`Delete ${workspace.name}`}
                      onClick={() => setDeleteTarget(workspace)}
                      icon={<Trash2 size={14} aria-hidden />}
                    />
                  )}
                </Td>
              </Tr>
            ))}
          </tbody>
        </TableFrame>
      )}

      <Dialog open={addOpen} onOpenChange={setAddOpen}>
        <DialogContent
          title="Add workspace"
          description="You never need to type a server path unless you're registering a repository that only exists on this machine."
          width={480}
        >
          <AddWorkspaceForm isAdmin={isAdmin} onCreated={() => setAddOpen(false)} onCancel={() => setAddOpen(false)} />
        </DialogContent>
      </Dialog>

      <DeleteWorkspaceDialog
        workspace={deleteTarget}
        open={deleteTarget !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null)
        }}
      />
    </div>
  )
}
