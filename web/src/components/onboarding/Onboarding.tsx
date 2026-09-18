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
    <ol className="flex flex-col gap-1" aria-label="Onboarding steps">
      {STEPS.map((s) => {
        const done = s.index < step
        const active = s.index === step
        return (
          <li key={s.index} className="flex items-center gap-2.5 text-[13px]" data-testid={`onboarding-step-${s.index}`}>
            <span
              className={clsx(
                'flex h-5 w-5 shrink-0 items-center justify-center rounded-full border text-[11px] font-medium',
                done && 'border-accent bg-accent text-[#0b0d10]',
                active && !done && 'border-accent text-accent',
                !active && !done && 'border-hairline text-fg-muted',
              )}
            >
              {done ? <Check size={12} aria-hidden /> : s.index}
            </span>
            <span className={clsx(active ? 'text-fg-primary' : 'text-fg-secondary')}>{s.label}</span>
          </li>
        )
      })}
    </ol>
  )
}

function StepFooter({ onSkip, children }: { onSkip: () => void; children?: ReactNode }) {
  return (
    <div className="mt-5 flex items-center justify-between">
      <button
        type="button"
        onClick={onSkip}
        className="text-[13px] text-fg-muted transition-colors duration-150 hover:text-fg-secondary"
      >
        Skip
      </button>
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
      <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">Add your Claude token</h2>
      <p className="mt-1 text-[13px] text-fg-secondary">
        Styr starts sessions with your own Claude Code login, not a shared key.
      </p>
      <div className="mt-4">
        <ClaudeTokenCard
          title="Your Claude token"
          inputLabel="Claude token"
          tokenInfo={me?.claude_token}
          onSave={handleSave}
          testId="onboarding-token-card"
        />
      </div>
      <StepFooter onSkip={onSkip}>
        <button
          type="button"
          onClick={onContinue}
          disabled={!present}
          className={clsx(
            'h-8 rounded-[var(--radius-1)] bg-accent px-3 text-[13px] font-medium text-[#0b0d10]',
            !present && 'opacity-60',
          )}
        >
          Continue
        </button>
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
        <form onSubmit={(e) => void handleSubmit(e)} className="mt-3 flex flex-col gap-2">
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
          {errorMessage && (
            <p role="alert" className="text-[12px] text-state-failed">
              {errorMessage}
            </p>
          )}
          <button
            type="submit"
            disabled={submitting}
            className={clsx(
              'mt-1 h-8 self-start rounded-[var(--radius-1)] border border-hairline px-3 text-[13px] font-medium text-fg-primary transition-colors duration-150 hover:bg-surface-2',
              submitting && 'opacity-60',
            )}
          >
            Add workspace
          </button>
        </form>
      )}

      {isAdmin && added && <p className="mt-3 text-[13px] text-fg-secondary">Workspace added.</p>}

      <StepFooter onSkip={onSkip}>
        <button
          type="button"
          onClick={onContinue}
          className="h-8 rounded-[var(--radius-1)] bg-accent px-3 text-[13px] font-medium text-[#0b0d10]"
        >
          Continue
        </button>
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
      <textarea
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        rows={3}
        aria-label="First prompt"
        className="mt-3 w-full resize-none rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2.5 py-2 text-[13px] text-fg-primary outline-none focus-visible:ring-2 focus-visible:ring-accent"
      />
      {errorMessage && (
        <p role="alert" className="mt-2 text-[12px] text-state-failed">
          {errorMessage}
        </p>
      )}
      <StepFooter onSkip={onSkip}>
        <button
          type="button"
          onClick={() => void handleStart()}
          disabled={!canStart || starting}
          className={clsx(
            'h-8 rounded-[var(--radius-1)] bg-accent px-3 text-[13px] font-medium text-[#0b0d10]',
            (!canStart || starting) && 'opacity-60',
          )}
        >
          Start session
        </button>
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
    <main className="mx-auto max-w-[720px] px-4 py-8 sm:px-6">
      <h1 className="text-[18px] font-semibold tracking-[-0.01em] text-fg-primary">Welcome to Styr</h1>
      <p className="mt-1 text-[13px] text-fg-secondary">A few steps to get your first session running.</p>

      <div className="mt-6 grid grid-cols-1 gap-6 sm:grid-cols-[180px_1fr]">
        <ProgressRail step={step} />
        <div className="rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4">
          {step === 1 && <TokenStep onContinue={() => setStep(2)} onSkip={skip} />}
          {step === 2 && <WorkspaceStep onContinue={() => setStep(3)} onSkip={skip} />}
          {step === 3 && <FirstSessionStep onFinish={finish} onSkip={skip} />}
        </div>
      </div>
    </main>
  )
}
