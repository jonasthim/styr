// Shown on Profile (the user's own token, PUT/DELETE /api/v1/me/claude-token)
// and on Settings for admins (the service-wide token, PUT
// /api/v1/settings/service-token - which has no DELETE route in
// docs/openapi.yaml, so `onRemove` is omitted there). Both endpoints verify
// before they store: on 422 nothing changes server-side, so this component
// never has to reconcile an optimistic write with a failed verify.
import { useState, type FormEvent, type ReactNode } from 'react'
import { Check, Copy } from 'lucide-react'
import { ApiError } from '../../api/client'
import type { ClaudeTokenInfo } from '../../api/types'
import { Button, Card, Input, Skeleton } from '../ui'

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
  /** Inside onboarding this already sits in a card under its own heading, so
   * it drops the frame and the duplicate title rather than nesting a panel
   * of the same surface inside one. */
  nested?: boolean
}

export function ClaudeTokenCard({ title, inputLabel, tokenInfo, onSave, onRemove, testId, nested }: ClaudeTokenCardProps) {
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

  const Frame = nested ? NestedFrame : Card

  return (
    <Frame data-testid={testId} title={nested ? undefined : title}>
      {loading && (
        <div className="flex flex-col gap-2">
          <Skeleton className="h-3 w-56" />
          <Skeleton className="h-3 w-40" />
        </div>
      )}

      {!loading && showForm && (
        <div data-testid={`${testId}-absent`} className="flex flex-col gap-4">
          <p className="max-w-[58ch] text-[13px] text-fg-secondary">
            Styr runs your sessions under your own Claude Code login rather than a shared API key. Run the command below
            on any machine where Claude Code is already signed in, then paste the token it prints.
          </p>

          <div className="flex items-center gap-2 rounded-[var(--radius-control)] border border-hairline bg-surface-2 py-1 pl-3 pr-1">
            <code className="flex-1 truncate font-mono text-[12px] text-fg-primary">{SETUP_COMMAND}</code>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => void copyCommand()}
              aria-label="Copy command"
              className="w-7 px-0"
              icon={copied ? <Check size={13} aria-hidden /> : <Copy size={13} aria-hidden />}
            />
          </div>

          <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-2">
            <label htmlFor={`${testId}-input`} className="text-[12px] font-medium text-fg-secondary">
              {inputLabel}
            </label>
            <div className="flex flex-wrap items-center gap-2">
              <Input
                id={`${testId}-input`}
                data-testid={`${testId}-input`}
                mono
                type="password"
                autoComplete="off"
                spellCheck={false}
                value={value}
                onChange={(e) => setValue(e.target.value)}
                placeholder="sk-ant-oat01-…"
                disabled={saving}
                aria-invalid={errorMessage ? true : undefined}
                className="min-w-[200px] flex-1"
              />
              <Button variant="primary" type="submit" disabled={!value} loading={saving}>
                Save and verify
              </Button>
              {editing && !saving && (
                <Button
                  variant="ghost"
                  onClick={() => {
                    setEditing(false)
                    setValue('')
                    setErrorMessage('')
                  }}
                >
                  Cancel
                </Button>
              )}
            </div>
            {errorMessage && (
              <p
                data-testid={`${testId}-error`}
                role="alert"
                className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger"
              >
                {errorMessage}
              </p>
            )}
          </form>
        </div>
      )}

      {!loading && !showForm && tokenInfo.present && (
        <div data-testid={`${testId}-present`} className="flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="truncate font-mono text-[13px] text-fg-primary">{tokenInfo.label}</p>
            <p className="mt-0.5 text-[12px] text-fg-secondary">Verified {formatVerifiedAt(tokenInfo.verified_at)}</p>
          </div>
          <div className="flex shrink-0 gap-2">
            <Button onClick={() => setEditing(true)}>Replace</Button>
            {onRemove && (
              <Button variant="danger" onClick={() => void handleRemove()} loading={removing}>
                Remove
              </Button>
            )}
          </div>
        </div>
      )}
    </Frame>
  )
}

/** Card's shape, no chrome: used when a heading and a panel already surround
 * this component (the onboarding step). */
function NestedFrame({ children, ...rest }: { children?: ReactNode; 'data-testid'?: string; title?: string }) {
  return <div {...rest}>{children}</div>
}
