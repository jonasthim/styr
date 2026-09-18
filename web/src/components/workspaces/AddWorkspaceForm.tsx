// Shared "Add workspace" form body: the segmented source control plus that
// source's fields, reused verbatim by the Workspaces page dialog and
// onboarding step 2 (docs/superpowers/plans/2026-09-18-styr-v0.1.md, T29 -
// this supersedes Task 22's admin-only path dialog). A member sees only
// "Git repository" and "Empty"; "Server path" is for an admin registering a
// checkout that already exists on the machine Styr runs on.
//
// Submitting posts once. A git source comes back `state: "cloning"`; this
// component then reads the same row out of the `['workspaces']` cache -
// patched live by useLiveEvents.ts on the `workspace.state` SSE frame - until
// it reaches "ready" (calls onCreated) or "failed" (shows the error inline
// with Retry, and stays put rather than closing).
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { api, ApiError } from '../../api/client'
import { q } from '../../api/queries'
import type { Workspace, WorkspaceSource } from '../../api/types'
import { Button, Field, Input, Select, Tabs, TabsContent, TabsList, TabsTrigger } from '../ui'
import { useCloningPoll } from '../../hooks/useCloningPoll'

/** Auto-fills Name from a git URL's last path segment - "https://host/a/repo.git" or "git@host:a/repo.git" both end in the part someone actually recognises. */
function nameFromUrl(url: string): string {
  const trimmed = url.trim().replace(/\/+$/, '')
  if (!trimmed) return ''
  const last = trimmed.split(/[/:]/).pop() ?? ''
  return last.replace(/\.git$/, '')
}

export interface AddWorkspaceFormProps {
  isAdmin: boolean
  onCreated: (workspace: Workspace) => void
  /** Omit to hide the Cancel action (onboarding has its own Skip). */
  onCancel?: () => void
}

export function AddWorkspaceForm({ isAdmin, onCreated, onCancel }: AddWorkspaceFormProps) {
  const queryClient = useQueryClient()
  const profiles = useQuery(q.profiles())
  const workspaces = useQuery(q.workspaces())
  useCloningPoll(workspaces.data)

  const [source, setSource] = useState<WorkspaceSource>('git')
  const [repoUrl, setRepoUrl] = useState('')
  const [branch, setBranch] = useState('')
  const [path, setPath] = useState('')
  const [name, setName] = useState('')
  const [nameTouched, setNameTouched] = useState(false)
  const [defaultProfileId, setDefaultProfileId] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const [createdId, setCreatedId] = useState<string | null>(null)
  const [retrying, setRetrying] = useState(false)
  const notified = useRef(new Set<string>())

  const interactiveProfile = profiles.data?.find((p) => p.name === 'interactive') ?? profiles.data?.[0]
  const resolvedProfileId = defaultProfileId || interactiveProfile?.id || ''
  const created = workspaces.data?.find((w) => w.id === createdId)

  useEffect(() => {
    if (!created || !createdId) return
    if (created.state === 'ready' && !notified.current.has(createdId)) {
      notified.current.add(createdId)
      onCreated(created)
    }
  }, [created, createdId, onCreated])

  function handleSourceChange(next: string) {
    setSource(next as WorkspaceSource)
    setName('')
    setNameTouched(false)
  }

  function handleRepoUrlChange(value: string) {
    setRepoUrl(value)
    if (!nameTouched) setName(nameFromUrl(value))
  }

  function handleNameChange(value: string) {
    setNameTouched(true)
    setName(value)
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setSubmitting(true)
    setSubmitError('')
    const body: Record<string, unknown> = { name, source }
    if (resolvedProfileId) body.default_profile_id = resolvedProfileId
    if (source === 'git') {
      body.repo_url = repoUrl
      if (branch.trim()) body.branch = branch.trim()
    }
    if (source === 'path') body.path = path
    try {
      const workspace = await api<Workspace>('/api/v1/workspaces', { method: 'POST', json: body })
      queryClient.setQueryData<Workspace[]>(['workspaces'], (prev) => (prev ? [workspace, ...prev] : [workspace]))
      setCreatedId(workspace.id)
    } catch (err) {
      setSubmitError(err instanceof ApiError ? err.message : 'Something went wrong adding the workspace.')
    } finally {
      setSubmitting(false)
    }
  }

  async function handleRetry() {
    if (!createdId) return
    setRetrying(true)
    try {
      await api(`/api/v1/workspaces/${createdId}/retry`, { method: 'POST' })
      queryClient.setQueryData<Workspace[]>(['workspaces'], (prev) =>
        prev?.map((w) => (w.id === createdId ? { ...w, state: 'cloning', error: null } : w)),
      )
    } finally {
      setRetrying(false)
    }
  }

  if (createdId && created) {
    return (
      <div className="flex flex-col gap-3">
        {created.state === 'cloning' && (
          <div
            data-testid="add-workspace-cloning"
            className="flex items-center gap-2 rounded-[var(--radius-control)] border border-hairline bg-surface-1 px-3 py-2.5 text-[13px] text-fg-secondary"
          >
            <Loader2 size={14} className="animate-spin text-accent" aria-hidden />
            Cloning {created.name}…
          </div>
        )}
        {created.state === 'failed' && (
          <div className="flex flex-col gap-2 rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2.5">
            <p role="alert" data-testid="add-workspace-error" className="text-[12px] text-fg-danger">
              {created.error || 'Could not create the workspace.'}
            </p>
            <Button variant="secondary" size="sm" className="self-start" onClick={() => void handleRetry()} loading={retrying}>
              Retry
            </Button>
          </div>
        )}
      </div>
    )
  }

  return (
    <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-4">
      <Tabs value={source} onValueChange={handleSourceChange}>
        <TabsList aria-label="Workspace source">
          <TabsTrigger value="git">Git repository</TabsTrigger>
          <TabsTrigger value="empty">Empty</TabsTrigger>
          {isAdmin && <TabsTrigger value="path">Server path</TabsTrigger>}
        </TabsList>

        <TabsContent value="git" className="mt-4 flex flex-col gap-4">
          <Field label="Repository URL">
            {({ id }) => (
              <Input
                id={id}
                required
                mono
                value={repoUrl}
                onChange={(e) => handleRepoUrlChange(e.target.value)}
                placeholder="https://github.com/you/repo.git"
              />
            )}
          </Field>
          <Field label="Branch" labelAside="optional">
            {({ id }) => <Input id={id} value={branch} onChange={(e) => setBranch(e.target.value)} placeholder="main" />}
          </Field>
          <Field label="Name">
            {({ id }) => <Input id={id} required value={name} onChange={(e) => handleNameChange(e.target.value)} />}
          </Field>
        </TabsContent>

        <TabsContent value="empty" className="mt-4">
          <Field label="Name">
            {({ id }) => <Input id={id} required value={name} onChange={(e) => handleNameChange(e.target.value)} />}
          </Field>
        </TabsContent>

        {isAdmin && (
          <TabsContent value="path" className="mt-4 flex flex-col gap-4">
            <Field label="Name">
              {({ id }) => <Input id={id} required value={name} onChange={(e) => handleNameChange(e.target.value)} />}
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
          </TabsContent>
        )}
      </Tabs>

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

      {submitError && (
        <p
          role="alert"
          data-testid="add-workspace-error"
          className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger"
        >
          {submitError}
        </p>
      )}

      <div className="mt-1 flex items-center justify-end gap-2">
        {onCancel && (
          <Button variant="ghost" type="button" onClick={onCancel}>
            Cancel
          </Button>
        )}
        <Button variant="primary" type="submit" loading={submitting}>
          Add workspace
        </Button>
      </div>
    </form>
  )
}
