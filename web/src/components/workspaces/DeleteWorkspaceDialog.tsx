// Confirm-delete dialog for a workspace row. The only special-cased failure
// is 409 sessions_open - anything else falls back to the server's own
// message (ApiError.message), same pattern as every other inline API error
// in Styr.
import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../api/client'
import type { Workspace } from '../../api/types'
import { Button, Dialog, DialogContent } from '../ui'

const SESSIONS_OPEN_MESSAGE = 'This workspace has open sessions. Close them before deleting it.'

export function DeleteWorkspaceDialog({
  workspace,
  open,
  onOpenChange,
}: {
  workspace: Workspace | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const queryClient = useQueryClient()
  const [deleting, setDeleting] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')

  useEffect(() => {
    if (open) setErrorMessage('')
  }, [open, workspace?.id])

  async function handleDelete() {
    if (!workspace) return
    setDeleting(true)
    setErrorMessage('')
    try {
      await api(`/api/v1/workspaces/${workspace.id}`, { method: 'DELETE' })
      queryClient.setQueryData<Workspace[]>(['workspaces'], (prev) => prev?.filter((w) => w.id !== workspace.id))
      onOpenChange(false)
    } catch (err) {
      if (err instanceof ApiError && err.code === 'sessions_open') {
        setErrorMessage(SESSIONS_OPEN_MESSAGE)
      } else {
        setErrorMessage(err instanceof ApiError ? err.message : 'Something went wrong deleting the workspace.')
      }
    } finally {
      setDeleting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={workspace ? `Delete ${workspace.name}?` : 'Delete workspace?'}
        description="This removes it from the workspace list. Sessions that already ran here keep their history."
        width={420}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button variant="danger" onClick={() => void handleDelete()} loading={deleting}>
              Delete
            </Button>
          </>
        }
      >
        {errorMessage && (
          <p
            role="alert"
            data-testid="delete-workspace-error"
            className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger"
          >
            {errorMessage}
          </p>
        )}
      </DialogContent>
    </Dialog>
  )
}
