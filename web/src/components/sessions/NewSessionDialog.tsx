// Dialog for starting a session (Task 19). Opened by the `n` shortcut and the
// palette's "New session" action, both of which navigate to /sessions?new=1 -
// Sessions.tsx owns that search param and passes `open` down;
// onOpenChange(false) here is how this dialog asks the page to clear it
// (Escape, backdrop click, Cancel and a successful submit all go through the
// same path).
//
// First run: a server with no workspaces cannot start anything, so instead of
// two empty selects and a Start button that 400s, the dialog says so and
// points at the fix the viewer can actually apply.
import { useEffect, useRef, useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { FolderPlus } from 'lucide-react'
import { q } from '../../api/queries'
import { api, ApiError } from '../../api/client'
import type { Effort, HarnessKind, Session } from '../../api/types'
import { CODEX_SANDBOX_NOTE, HARNESS_LABEL } from '../../lib/harness'
import { workspaceOptionLabel } from '../workspaces/workspaceDisplay'
import { Button, Dialog, DialogContent, Field, Input, Kbd, MOD_KEY, Select, Textarea } from '../ui'

const MAX_PROMPT_ROWS = 8

interface CreateSessionBody {
  workspace_id: string
  profile_id: string
  title: string
  prompt: string
  model: string
  effort: Effort
  harness: HarnessKind
}

// The value the model and effort selects show for "whatever the CLI defaults
// to" — Radix Select has no empty-string item value.
const CLI_DEFAULT = '__default'

const EFFORT_LABEL: Record<Exclude<Effort, ''>, string> = {
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  xhigh: 'Extra high',
  max: 'Max',
}

export function NewSessionDialog({
  open,
  onOpenChange,
  workspaceId: initialWorkspaceId,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Preselects a workspace — the composer's `/new` starts the next session in
   * the one the current session is already running in. */
  workspaceId?: string
}) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const workspacesQuery = useQuery({ ...q.workspaces(), enabled: open })
  const profiles = useQuery({ ...q.profiles(), enabled: open })
  const status = useQuery({ ...q.status(), enabled: open })

  const [workspaceId, setWorkspaceId] = useState(initialWorkspaceId ?? '')
  const [profileId, setProfileId] = useState('')
  const [title, setTitle] = useState('')
  const [prompt, setPrompt] = useState('')
  const [model, setModel] = useState(CLI_DEFAULT)
  const [effort, setEffort] = useState<string>(CLI_DEFAULT)
  const [harness, setHarness] = useState<HarnessKind>('claude')
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  // Only harnesses whose binary actually answered at startup can run a
  // session, so an unavailable one is never offered; the roster is stable, so
  // this falls back to the current selection alone rather than an empty
  // select while /status is still loading.
  const harnessOptions = (status.data?.harnesses ?? [])
    .filter((h) => h.available)
    .map((h) => ({ value: h.kind, label: HARNESS_LABEL[h.kind] }))
  if (harnessOptions.length === 0) harnessOptions.push({ value: harness, label: HARNESS_LABEL[harness] })

  // A workspace still cloning (or one that failed) can't run a session yet -
  // only "ready" ones are offered here.
  const readyWorkspaces = workspacesQuery.data?.filter((w) => w.state === 'ready') ?? []
  const noWorkspaces = workspacesQuery.isSuccess && readyWorkspaces.length === 0

  // Default the workspace to the first ready one once the list loads, and the
  // profile to that workspace's default whenever the workspace changes -
  // "profile select (defaults to workspace default)".
  useEffect(() => {
    if (!open) return
    if (workspaceId || readyWorkspaces.length === 0) return
    setWorkspaceId(readyWorkspaces[0].id)
  }, [open, readyWorkspaces, workspaceId])

  useEffect(() => {
    const workspace = readyWorkspaces.find((w) => w.id === workspaceId)
    if (workspace) setProfileId(workspace.default_profile_id)
  }, [workspaceId, readyWorkspaces])

  // Model and effort default to the chosen profile's own defaults; an empty
  // profile default means the CLI's.
  useEffect(() => {
    const profile = profiles.data?.find((p) => p.id === profileId)
    if (!profile) return
    setModel(profile.model || CLI_DEFAULT)
    setEffort(profile.effort || CLI_DEFAULT)
    setHarness(profile.harness)
  }, [profileId, profiles.data])

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
        json: {
          workspace_id: workspaceId,
          profile_id: profileId,
          title,
          prompt,
          model: model === CLI_DEFAULT ? '' : model,
          effort: effort === CLI_DEFAULT ? '' : (effort as Effort),
          harness,
        } satisfies CreateSessionBody,
      }),
    onSuccess: (session) => {
      queryClient.setQueryData<Session[]>(['sessions'], (prev) => (prev ? [session, ...prev] : [session]))
      reset()
      onOpenChange(false)
      void navigate({ to: '/sessions/$id', params: { id: session.id } })
    },
  })

  const tokenMissing = mutation.error instanceof ApiError && mutation.error.status === 422
  const canSubmit = Boolean(workspaceId) && Boolean(profileId) && prompt.trim().length > 0 && !mutation.isPending

  function submit() {
    if (!canSubmit || noWorkspaces) return
    mutation.mutate()
  }

  function handleOpenChange(next: boolean) {
    if (!next) {
      mutation.reset()
    }
    onOpenChange(next)
  }

  const errorMessage = tokenMissing ? (
    <>
      {mutation.error?.message}{' '}
      <Link to="/profile" className="underline underline-offset-2">
        Add a Claude token
      </Link>
    </>
  ) : mutation.isError ? (
    mutation.error?.message
  ) : undefined

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        title="New session"
        srOnlyDescription="Start a new Claude Code session"
        width={480}
        // Radix would focus the first tabbable control (the workspace
        // select); the prompt is the only field anyone has to fill in.
        onOpenAutoFocus={(event) => {
          event.preventDefault()
          textareaRef.current?.focus()
        }}
        onKeyDown={(event) => {
          if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') {
            event.preventDefault()
            submit()
          }
        }}
        footerLeft={
          noWorkspaces ? null : (
            <span className="inline-flex items-center gap-1">
              <Kbd>{MOD_KEY}</Kbd>
              <Kbd>↵</Kbd>
              <span className="ml-0.5">to start</span>
            </span>
          )
        }
        footer={
          <>
            <Button variant="ghost" onClick={() => handleOpenChange(false)}>
              Cancel
            </Button>
            <Button
              variant="primary"
              type="submit"
              form="new-session-form"
              disabled={!canSubmit || noWorkspaces}
              loading={mutation.isPending}
            >
              {mutation.isPending ? 'Starting…' : 'Start session'}
            </Button>
          </>
        }
      >
        {noWorkspaces ? (
          <div
            data-testid="no-workspaces-notice"
            className="flex gap-3 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4"
          >
            <span className="flex h-8 w-8 shrink-0 items-center justify-center rounded-[var(--radius-control)] border border-hairline bg-surface-3 text-fg-secondary">
              <FolderPlus size={15} aria-hidden />
            </span>
            <div className="min-w-0">
              <p className="text-[13px] font-medium text-fg-primary">No workspaces yet</p>
              <p className="mt-1 text-[13px] text-fg-secondary">
                A session runs inside a workspace.{' '}
                <Link
                  to="/workspaces"
                  onClick={() => handleOpenChange(false)}
                  className="text-accent underline-offset-2 hover:underline"
                >
                  Add a workspace
                </Link>{' '}
                to start one.
              </p>
            </div>
          </div>
        ) : (
          <form
            id="new-session-form"
            className="flex flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault()
              submit()
            }}
          >
            <div className="flex flex-col gap-4 sm:flex-row">
              <Field label="Workspace" className="flex-1">
                {({ id }) => (
                  <Select
                    id={id}
                    value={workspaceId}
                    onValueChange={setWorkspaceId}
                    placeholder="Choose a workspace"
                    options={readyWorkspaces.map((w) => ({ value: w.id, label: workspaceOptionLabel(w) }))}
                  />
                )}
              </Field>
              <Field label="Profile" className="flex-1">
                {({ id }) => (
                  <Select
                    id={id}
                    value={profileId}
                    onValueChange={setProfileId}
                    placeholder="Choose a profile"
                    options={(profiles.data ?? []).map((p) => ({ value: p.id, label: p.name }))}
                  />
                )}
              </Field>
            </div>

            <Field label="Harness">
              {({ id }) => (
                <Select
                  id={id}
                  value={harness}
                  onValueChange={(value) => setHarness(value as HarnessKind)}
                  options={harnessOptions}
                />
              )}
            </Field>

            {/* Only under Codex: the difference is worth a line exactly where
                someone is choosing it, and noise everywhere else. */}
            {harness === 'codex' && (
              <p data-testid="codex-harness-note" className="-mt-2 text-[12px] text-fg-secondary">
                {CODEX_SANDBOX_NOTE}
              </p>
            )}

            <div className="flex flex-col gap-4 sm:flex-row">
              <Field label="Model" className="flex-1">
                {({ id }) => (
                  <Select
                    id={id}
                    value={model}
                    onValueChange={setModel}
                    options={[
                      { value: CLI_DEFAULT, label: 'Default model' },
                      ...(status.data?.models ?? []).map((m) => ({ value: m.alias, label: m.label })),
                    ]}
                  />
                )}
              </Field>
              <Field label="Effort" className="flex-1">
                {({ id }) => (
                  <Select
                    id={id}
                    value={effort}
                    onValueChange={setEffort}
                    options={[
                      { value: CLI_DEFAULT, label: 'Default effort' },
                      ...(status.data?.efforts ?? []).map((e) => ({ value: e, label: EFFORT_LABEL[e] })),
                    ]}
                  />
                )}
              </Field>
            </div>

            <Field label="Title" labelAside="optional">
              {({ id }) => (
                <Input
                  id={id}
                  type="text"
                  value={title}
                  onChange={(event) => setTitle(event.target.value)}
                  placeholder="Untitled session"
                />
              )}
            </Field>

            <Field label="Prompt">
              {({ id }) => (
                <Textarea
                  id={id}
                  ref={textareaRef}
                  value={prompt}
                  onChange={(event) => setPrompt(event.target.value)}
                  rows={3}
                  placeholder="What should Claude do?"
                />
              )}
            </Field>

            {/* The request failed, not one field: this belongs to the form,
                not under the prompt. */}
            {errorMessage && (
              <p
                role="alert"
                className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger"
              >
                {errorMessage}
              </p>
            )}
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
