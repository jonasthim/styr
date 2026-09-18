// Shown on Profile (the user's own token, PUT/DELETE /api/v1/me/claude-token)
// and on Settings for admins (the service-wide token, PUT
// /api/v1/settings/service-token - which has no DELETE route in
// docs/openapi.yaml, so `onRemove` is omitted there). Both endpoints verify
// before they store: on 422 nothing changes server-side, so this component
// never has to reconcile an optimistic write with a failed verify.
import { useState, type FormEvent } from 'react'
import { Check, Copy, Loader2 } from 'lucide-react'
import clsx from 'clsx'
import { ApiError } from '../../api/client'
import type { ClaudeTokenInfo } from '../../api/types'

const SETUP_COMMAND = 'claude setup-token'

function formatVerifiedAt(verifiedAt: string | null): string {
  if (!verifiedAt) return 'Unknown'
  return new Date(verifiedAt).toLocaleString(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short',
  })
}

interface ClaudeTokenCardProps {
  /** Card heading, e.g. "Your Claude token" or "Service Claude token". */
  title: string
  /** Accessible name for the password input; also distinguishes the two cards when both render on one page. */
  inputLabel: string
  tokenInfo: ClaudeTokenInfo | undefined
  onSave: (token: string) => Promise<void>
  /** Omit where the API has no delete route for this token (the service token). */
  onRemove?: () => Promise<void>
  testId: string
}

export function ClaudeTokenCard({ title, inputLabel, tokenInfo, onSave, onRemove, testId }: ClaudeTokenCardProps) {
  const [editing, setEditing] = useState(false)
  const [value, setValue] = useState('')
  const [saving, setSaving] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')
  const [copied, setCopied] = useState(false)

  const loading = tokenInfo === undefined
  const showForm = loading ? false : !tokenInfo.present || editing

  async function copyCommand() {
    try {
      await navigator.clipboard.writeText(SETUP_COMMAND)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard access can be denied; the command is still selectable text.
    }
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    if (!value) return
    setSaving(true)
    setErrorMessage('')
    try {
      // The real verify call can take up to 60s (it round-trips to
      // Claude), so this deliberately has no client-side timeout shorter
      // than that - the spinner just stays up until the request settles.
      await onSave(value)
      setValue('')
      setEditing(false)
    } catch (err) {
      setErrorMessage(err instanceof ApiError ? err.message : 'Something went wrong saving the token.')
    } finally {
      setSaving(false)
    }
  }

  async function handleRemove() {
    if (!onRemove) return
    setRemoving(true)
    try {
      await onRemove()
    } finally {
      setRemoving(false)
    }
  }

  return (
    <section
      data-testid={testId}
      className="rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-4"
    >
      <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">{title}</h2>

      {loading && <p className="mt-2 text-[13px] text-fg-muted">Loading…</p>}

      {!loading && showForm && (
        <div data-testid={`${testId}-absent`} className="mt-2 flex flex-col gap-3">
          <p className="max-w-[52ch] text-[13px] text-fg-secondary">
            Styr runs your sessions under your own Claude Code login rather than a shared API key. Run{' '}
            <code className="rounded-[var(--radius-1)] bg-surface-2 px-1 py-0.5 font-mono text-[12px] text-fg-primary">
              {SETUP_COMMAND}
            </code>{' '}
            on any machine where Claude Code is already logged in, then paste the token it prints below.
          </p>

          <div className="flex items-center gap-2">
            <code className="flex-1 truncate rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2.5 py-1.5 font-mono text-[12px] text-fg-primary">
              {SETUP_COMMAND}
            </code>
            <button
              type="button"
              onClick={() => void copyCommand()}
              aria-label="Copy command"
              className="flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-1)] border border-hairline text-fg-secondary transition-colors duration-150 hover:bg-surface-2 hover:text-fg-primary"
            >
              {copied ? <Check size={14} aria-hidden /> : <Copy size={14} aria-hidden />}
            </button>
          </div>

          <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-2">
            <label htmlFor={`${testId}-input`} className="text-[12px] font-medium text-fg-secondary">
              {inputLabel}
            </label>
            <div className="flex items-center gap-2">
              <input
                id={`${testId}-input`}
                data-testid={`${testId}-input`}
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder="sk-ant-oat01-…"
                disabled={saving}
                className="h-8 flex-1 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2.5 font-mono text-[12px] text-fg-primary outline-none placeholder:text-fg-muted focus-visible:ring-2 focus-visible:ring-accent focus-visible:ring-offset-2 focus-visible:ring-offset-surface-1"
              />
              <button
                type="submit"
                disabled={saving || !value}
                className={clsx(
                  'flex h-8 shrink-0 items-center gap-1.5 rounded-[var(--radius-1)] bg-accent px-3 text-[13px] font-medium text-[#0b0d10] transition-opacity duration-150',
                  (saving || !value) && 'opacity-60',
                )}
              >
                {saving && <Loader2 size={14} className="animate-spin" aria-hidden />}
                Save and verify
              </button>
              {editing && !saving && (
                <button
                  type="button"
                  onClick={() => {
                    setEditing(false)
                    setValue('')
                    setErrorMessage('')
                  }}
                  className="h-8 shrink-0 rounded-[var(--radius-1)] px-2 text-[13px] text-fg-secondary transition-colors duration-150 hover:text-fg-primary"
                >
                  Cancel
                </button>
              )}
            </div>
            {errorMessage && (
              <p data-testid={`${testId}-error`} role="alert" className="text-[12px] text-state-failed">
                {errorMessage}
              </p>
            )}
          </form>
        </div>
      )}

      {!loading && !showForm && tokenInfo.present && (
        <div data-testid={`${testId}-present`} className="mt-2 flex items-center justify-between gap-3">
          <div>
            <p className="font-mono text-[13px] text-fg-primary">{tokenInfo.label}</p>
            <p className="mt-0.5 text-[12px] text-fg-muted">Verified {formatVerifiedAt(tokenInfo.verified_at)}</p>
          </div>
          <div className="flex shrink-0 gap-2">
            <button
              type="button"
              onClick={() => setEditing(true)}
              className="h-8 rounded-[var(--radius-1)] border border-hairline px-3 text-[13px] font-medium text-fg-primary transition-colors duration-150 hover:bg-surface-2"
            >
              Replace
            </button>
            {onRemove && (
              <button
                type="button"
                onClick={() => void handleRemove()}
                disabled={removing}
                className={clsx(
                  'h-8 rounded-[var(--radius-1)] border border-hairline px-3 text-[13px] font-medium text-state-failed transition-colors duration-150 hover:bg-surface-2',
                  removing && 'opacity-60',
                )}
              >
                Remove
              </button>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
