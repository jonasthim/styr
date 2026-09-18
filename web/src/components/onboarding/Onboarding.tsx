// /welcome. Shown after first login when the user has no Claude token yet
// (docs/superpowers/plans/2026-09-18-styr-v0.1.md, Task 22). Three steps in
// one card with a progress rail; every step is skippable. This component
// cannot itself decide *when* to redirect here - that lives on the Inbox
// route per the card, which is outside this card's file list - so it only
// owns what happens once a browser is already on /welcome.
import { useState, type FormEvent, type ReactNode } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import clsx from 'clsx'
import { api, ApiError } from '../../api/client'
import { q } from '../../api/queries'
import { useMe } from '../../hooks/useMe'
import { ClaudeTokenCard } from '../profile/ClaudeTokenCard'
import { Button, Card, Field, Input, PageHeader, Textarea } from '../ui'

const WELCOMED_KEY = 'styr.welcomed'
const FIRST_PROMPT = 'Summarise this repository in five bullet points.'

function markWelcomed() {
  try {
    sessionStorage.setItem(WELCOMED_KEY, '1')
  } catch {
    // Best-effort only; worst case onboarding shows again next visit.
  }
}

type StepIndex = 1 | 2 | 3

const STEPS: Array<{ index: StepIndex; label: string }> = [
  { index: 1, label: 'Add your Claude token' },
  { index: 2, label: 'Add a workspace' },
  { index: 3, label: 'Start your first session' },
]

function ProgressRail({ step }: { step: StepIndex }) {
  return (
    <ol className="flex gap-4 sm:flex-col sm:gap-1" aria-label="Onboarding steps">
      {STEPS.map((s) => {
        const done = s.index < step
        const active = s.index === step
        return (
          <li
            key={s.index}
            className="flex min-w-0 items-center gap-2.5 text-[13px]"
            data-testid={`onboarding-step-${s.index}`}
          >
            <span
              className={clsx(
                'flex h-5 w-5 shrink-0 items-center justify-center rounded-full border text-[11px] font-medium tabular-nums',
                done && 'border-transparent bg-accent text-accent-fg',
                active && !done && 'border-accent text-accent',
                !active && !done && 'border-hairline text-fg-muted',
              )}
            >
              {done ? <Check size={12} aria-hidden /> : s.index}
            </span>
            {/* Laid out in a row on a phone, where three full labels would
                run off the edge, so only the step being worked on is named. */}
            <span
              className={clsx(
                'truncate',
                active ? 'font-medium text-fg-primary' : 'hidden text-fg-secondary sm:inline',
              )}
            >
              {s.label}
            </span>
          </li>
        )
      })}
    </ol>
  )
}

function StepFooter({ onSkip, children }: { onSkip: () => void; children?: ReactNode }) {
  return (
    <div className="mt-6 flex items-center justify-between gap-3 border-t border-hairline pt-4">
      <Button variant="ghost" size="sm" onClick={onSkip}>
        Skip
      </Button>
      <div className="flex gap-2">{children}</div>
    </div>
  )
}

function TokenStep({ onContinue, onSkip }: { onContinue: () => void; onSkip: () => void }) {
  const { data: me } = useMe()
  const queryClient = useQueryClient()

  async function handleSave(token: string) {
    await api('/api/v1/me/claude-token', { method: 'PUT', json: { token } })
    await queryClient.invalidateQueries({ queryKey: ['me'] })
  }

  const present = me?.claude_token.present ?? false

  return (
    <div>
      {/* No subtitle here: the card below already explains why Styr wants a
          token, and saying it twice in four lines reads as filler. */}
      <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">Add your Claude token</h2>
      <div className="mt-4">
        <ClaudeTokenCard
          nested
          title="Your Claude token"
          inputLabel="Claude token"
          tokenInfo={me?.claude_token}
          onSave={handleSave}
          testId="onboarding-token-card"
        />
      </div>
      <StepFooter onSkip={onSkip}>
        <Button variant="primary" onClick={onContinue} disabled={!present}>
          Continue
        </Button>
      </StepFooter>
    </div>
  )
}

function WorkspaceStep({ onContinue, onSkip }: { onContinue: () => void; onSkip: () => void }) {
  const { data: me } = useMe()
  const isAdmin = me?.role === 'admin'
  const queryClient = useQueryClient()
  const profiles = useQuery(q.profiles())
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [added, setAdded] = useState(false)

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setSubmitting(true)
    setErrorMessage('')
    try {
      await api('/api/v1/workspaces', {
        method: 'POST',
        json: { name, path, default_profile_id: profiles.data?.[0]?.id ?? '', worktrees: false },
      })
      await queryClient.invalidateQueries({ queryKey: ['workspaces'] })
      setAdded(true)
    } catch (err) {
      setErrorMessage(err instanceof ApiError ? err.message : 'Something went wrong adding the workspace.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div>
      <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">Add a workspace</h2>

      {!isAdmin && (
        <p className="mt-1 text-[13px] text-fg-secondary" data-testid="onboarding-ask-admin">
          Only admins can register a workspace. Ask an admin to add one, then come back here.
        </p>
      )}

      {isAdmin && !added && (
        <form onSubmit={(e) => void handleSubmit(e)} className="mt-4 flex flex-col gap-4">
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
          {errorMessage && (
            <p
              role="alert"
              className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger"
            >
              {errorMessage}
            </p>
          )}
          <Button type="submit" className="self-start" loading={submitting}>
            Add workspace
          </Button>
        </form>
      )}

      {isAdmin && added && <p className="mt-3 text-[13px] text-fg-secondary">Workspace added.</p>}

      <StepFooter onSkip={onSkip}>
        <Button variant="primary" onClick={onContinue}>
          Continue
        </Button>
      </StepFooter>
    </div>
  )
}

function FirstSessionStep({ onFinish, onSkip }: { onFinish: (sessionId: string | null) => void; onSkip: () => void }) {
  const workspaces = useQuery(q.workspaces())
  const profiles = useQuery(q.profiles())
  const [prompt, setPrompt] = useState(FIRST_PROMPT)
  const [starting, setStarting] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')

  const workspace = workspaces.data?.[0]
  const profile = profiles.data?.find((p) => p.id === workspace?.default_profile_id) ?? profiles.data?.[0]
  const canStart = Boolean(workspace && profile)

  async function handleStart() {
    if (!workspace || !profile) return
    setStarting(true)
    setErrorMessage('')
    try {
      const session = await api<{ id: string }>('/api/v1/sessions', {
        method: 'POST',
        json: { workspace_id: workspace.id, profile_id: profile.id, title: prompt, prompt },
      })
      onFinish(session.id)
    } catch (err) {
      setErrorMessage(err instanceof ApiError ? err.message : 'Something went wrong starting the session.')
      setStarting(false)
    }
  }

  return (
    <div>
      <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">Start your first session</h2>
      <p className="mt-1 text-[13px] text-fg-secondary">
        {workspace ? `Runs in ${workspace.name}.` : 'Add a workspace first, or skip for now.'}
      </p>
      <Textarea
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        rows={3}
        aria-label="First prompt"
        className="mt-4"
      />
      {errorMessage && (
        <p
          role="alert"
          className="mt-3 rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger"
        >
          {errorMessage}
        </p>
      )}
      <StepFooter onSkip={onSkip}>
        <Button variant="primary" onClick={() => void handleStart()} disabled={!canStart} loading={starting}>
          Start session
        </Button>
      </StepFooter>
    </div>
  )
}

export function Onboarding() {
  const navigate = useNavigate()
  const [step, setStep] = useState<StepIndex>(1)

  function finish(sessionId: string | null) {
    markWelcomed()
    if (sessionId) {
      void navigate({ to: '/sessions/$id', params: { id: sessionId } })
    } else {
      void navigate({ to: '/inbox' })
    }
  }

  function skip() {
    finish(null)
  }

  return (
    <div className="mx-auto flex w-full max-w-[760px] flex-1 flex-col px-4 py-8 sm:px-6">
      <PageHeader title="Welcome to Styr" description="Three steps to your first session." />

      <div className="mt-8 grid grid-cols-1 gap-6 sm:grid-cols-[190px_1fr]">
        <ProgressRail step={step} />
        <Card>
          {step === 1 && <TokenStep onContinue={() => setStep(2)} onSkip={skip} />}
          {step === 2 && <WorkspaceStep onContinue={() => setStep(3)} onSkip={skip} />}
          {step === 3 && <FirstSessionStep onFinish={finish} onSkip={skip} />}
        </Card>
      </div>
    </div>
  )
}
