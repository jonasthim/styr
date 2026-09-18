// Radix Dialog for starting a session (Task 19). Opened by the `n` shortcut
// and the palette's "New session" action, both of which navigate to
// /sessions?new=1 - Sessions.tsx owns that search param and passes `open`
// down; onOpenChange(false) here is how this dialog asks the page to clear
// it (Escape, backdrop click, Cancel and a successful submit all go through
// the same path).
import { useEffect, useRef, useState } from 'react'
import * as Dialog from '@radix-ui/react-dialog'
import { Link, useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { q } from '../../api/queries'
import { api, ApiError } from '../../api/client'
import type { Session } from '../../api/types'

const MAX_PROMPT_ROWS = 8

interface CreateSessionBody {
  workspace_id: string
  profile_id: string
  title: string
  prompt: string
}

export function NewSessionDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const workspaces = useQuery({ ...q.workspaces(), enabled: open })
  const profiles = useQuery({ ...q.profiles(), enabled: open })

  const [workspaceId, setWorkspaceId] = useState('')
  const [profileId, setProfileId] = useState('')
  const [title, setTitle] = useState('')
  const [prompt, setPrompt] = useState('')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Default the workspace to the first one once the list loads, and the
  // profile to that workspace's default whenever the workspace changes -
  // "profile select (defaults to workspace default)".
  useEffect(() => {
    if (!open) return
    if (workspaceId || !workspaces.data || workspaces.data.length === 0) return
    setWorkspaceId(workspaces.data[0].id)
  }, [open, workspaces.data, workspaceId])

  useEffect(() => {
    const workspace = workspaces.data?.find((w) => w.id === workspaceId)
    if (workspace) setProfileId(workspace.default_profile_id)
  }, [workspaceId, workspaces.data])

  useEffect(() => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    const lineHeight = 20
    const maxHeight = lineHeight * MAX_PROMPT_ROWS
    el.style.height = `${Math.min(el.scrollHeight, maxHeight)}px`
  }, [prompt, open])

  function reset() {
    setTitle('')
    setPrompt('')
  }

  const mutation = useMutation({
    mutationFn: () =>
      api<Session>('/api/v1/sessions', {
        method: 'POST',
        json: { workspace_id: workspaceId, profile_id: profileId, title, prompt } satisfies CreateSessionBody,
      }),
    onSuccess: (session) => {
      queryClient.setQueryData<Session[]>(['sessions'], (prev) => (prev ? [session, ...prev] : [session]))
      reset()
      onOpenChange(false)
      void navigate({ to: '/sessions/$id', params: { id: session.id } })
    },
  })

  const tokenMissing = mutation.error instanceof ApiError && mutation.error.status === 422

  function submit() {
    if (!workspaceId || !profileId || prompt.trim().length === 0 || mutation.isPending) return
    mutation.mutate()
  }

  function handleOpenChange(next: boolean) {
    if (!next) {
      mutation.reset()
    }
    onOpenChange(next)
  }

  return (
    <Dialog.Root open={open} onOpenChange={handleOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/60" />
        <Dialog.Content
          className="fixed left-1/2 top-[12vh] z-50 w-full max-w-[440px] -translate-x-1/2 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4 shadow-2xl"
          onKeyDown={(event) => {
            if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
              event.preventDefault()
              submit()
            }
          }}
        >
          <Dialog.Title className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">
            New session
          </Dialog.Title>
          <Dialog.Description className="sr-only">Start a new Claude Code session</Dialog.Description>

          <form
            className="mt-3 flex flex-col gap-3"
            onSubmit={(event) => {
              event.preventDefault()
              submit()
            }}
          >
            <div className="flex gap-3">
              <label className="flex flex-1 flex-col gap-1 text-[12px] font-medium text-fg-secondary">
                Workspace
                <select
                  value={workspaceId}
                  onChange={(event) => setWorkspaceId(event.target.value)}
                  className="h-8 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 text-[13px] text-fg-primary outline-none"
                >
                  {workspaces.data?.map((workspace) => (
                    <option key={workspace.id} value={workspace.id}>
                      {workspace.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="flex flex-1 flex-col gap-1 text-[12px] font-medium text-fg-secondary">
                Profile
                <select
                  value={profileId}
                  onChange={(event) => setProfileId(event.target.value)}
                  className="h-8 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 text-[13px] text-fg-primary outline-none"
                >
                  {profiles.data?.map((profile) => (
                    <option key={profile.id} value={profile.id}>
                      {profile.name}
                    </option>
                  ))}
                </select>
              </label>
            </div>

            <label className="flex flex-col gap-1 text-[12px] font-medium text-fg-secondary">
              Title
              <input
                type="text"
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                placeholder="Untitled session"
                className="h-8 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 text-[13px] text-fg-primary outline-none placeholder:text-fg-muted"
              />
            </label>

            <label className="flex flex-col gap-1 text-[12px] font-medium text-fg-secondary">
              Prompt
              <textarea
                ref={textareaRef}
                value={prompt}
                onChange={(event) => setPrompt(event.target.value)}
                rows={3}
                placeholder="What should Claude do?"
                className="resize-none rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 py-1.5 text-[13px] leading-5 text-fg-primary outline-none placeholder:text-fg-muted"
              />
            </label>

            {tokenMissing && (
              <p className="text-[12px] text-state-failed">
                {mutation.error?.message}{' '}
                <Link to="/profile" className="underline">
                  Add a Claude token
                </Link>
              </p>
            )}
            {mutation.isError && !tokenMissing && mutation.error && (
              <p className="text-[12px] text-state-failed">{mutation.error.message}</p>
            )}

            <div className="mt-1 flex items-center justify-end gap-2">
              <Dialog.Close asChild>
                <button
                  type="button"
                  className="h-8 rounded-[var(--radius-1)] px-3 text-[13px] font-medium text-fg-secondary transition-colors duration-150 hover:bg-surface-2 hover:text-fg-primary"
                >
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="submit"
                disabled={!workspaceId || !profileId || prompt.trim().length === 0 || mutation.isPending}
                className="h-8 rounded-[var(--radius-1)] bg-accent px-3 text-[13px] font-medium text-[#0b0d10] transition-opacity duration-150 disabled:opacity-50"
              >
                {mutation.isPending ? 'Starting…' : 'Start session'}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
