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
import { useMe } from '../hooks/useMe'
import type { Profile, Workspace } from '../api/types'
import { AddWorkspaceForm } from '../components/workspaces/AddWorkspaceForm'
import { DeleteWorkspaceDialog } from '../components/workspaces/DeleteWorkspaceDialog'
import { WorkspaceSourceChip, WorkspaceStateBadge } from '../components/workspaces/workspaceDisplay'
import { Button, Dialog, DialogContent, EmptyState, PageHeader, Select, Switch, TableFrame, Td, Th, Tr } from '../components/ui'

function WorktreesSwitch({ workspace }: { workspace: Workspace }) {
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
      disabled={pending}
      onCheckedChange={(checked) => void handleChange(checked)}
      aria-label={`Worktrees for ${workspace.name}`}
    />
  )
}

function DefaultProfileSelect({ workspace, profiles }: { workspace: Workspace; profiles: Profile[] }) {
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
      disabled={pending}
      onValueChange={(value) => void handleChange(value)}
      options={profiles.map((p) => ({ value: p.id, label: p.name }))}
      className="max-w-[170px]"
    />
  )
}

function RetryButton({ workspaceId }: { workspaceId: string }) {
  const queryClient = useQueryClient()
  const [pending, setPending] = useState(false)

  async function handleRetry() {
    setPending(true)
    try {
      await api(`/api/v1/workspaces/${workspaceId}/retry`, { method: 'POST' })
      queryClient.setQueryData<Workspace[]>(['workspaces'], (prev) =>
        prev?.map((w) => (w.id === workspaceId ? { ...w, state: 'cloning', error: null } : w)),
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <Button variant="secondary" size="sm" onClick={() => void handleRetry()} loading={pending}>
      Retry
    </Button>
  )
}

export function Workspaces() {
  const { data: me } = useMe()
  const workspaces = useQuery(q.workspaces())
  const profiles = useQuery(q.profiles())
  const [addOpen, setAddOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<Workspace | null>(null)
  const isAdmin = me?.role === 'admin'
  const isEmpty = workspaces.isSuccess && workspaces.data.length === 0

  const addButton = (
    <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setAddOpen(true)}>
      Add workspace
    </Button>
  )

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
                    {workspace.state === 'failed' && <RetryButton workspaceId={workspace.id} />}
                  </div>
                </Td>
                <Td>{profiles.data && <DefaultProfileSelect workspace={workspace} profiles={profiles.data} />}</Td>
                <Td className="text-right">
                  <div className="flex justify-end">
                    <WorktreesSwitch workspace={workspace} />
                  </div>
                </Td>
                <Td className="text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={`Delete ${workspace.name}`}
                    onClick={() => setDeleteTarget(workspace)}
                    icon={<Trash2 size={14} aria-hidden />}
                  />
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
