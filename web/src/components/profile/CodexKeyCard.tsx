// Shown on Profile (the user's own key, PUT/DELETE /api/v1/me/codex-key) and
// on Settings for admins (the service-wide key, PUT /api/v1/settings/codex-key
// - which has no DELETE route in docs/openapi.yaml, so `onRemove` is omitted
// there). Mirrors ClaudeTokenCard: both endpoints verify before they store, so
// on 422 nothing changed server-side and this component never has to reconcile
// an optimistic write with a failed verify.
//
// It is a separate component rather than a prop on ClaudeTokenCard because the
// two credentials differ in what they are and where they come from: a Claude
// token is minted by a CLI command on a machine that is already signed in, a
// Codex key is an OpenAI API key copied from a dashboard, and only one of them
// has a verified_at to show.
import { useState, type FormEvent, type ReactNode } from 'react'
import { ApiError } from '../../api/client'
import type { CodexKeyInfo } from '../../api/types'
import { CODEX_SANDBOX_NOTE } from '../../lib/harness'
import { Button, Card, Input, Skeleton } from '../ui'

interface CodexKeyCardProps {
  /** Card heading, e.g. "Codex API key" or "Service Codex key". */
  title: string
  /** Accessible name for the password input; also distinguishes the two cards when both render on one page. */
  inputLabel: string
  keyInfo: CodexKeyInfo | undefined
  onSave: (key: string) => Promise<void>
  /** Omit where the API has no delete route for this key (the service key). */
  onRemove?: () => Promise<void>
  testId: string
  /** Drops the frame and the duplicate title where a heading and a panel
   * already surround this component. */
  nested?: boolean
}

export function CodexKeyCard({ title, inputLabel, keyInfo, onSave, onRemove, testId, nested }: CodexKeyCardProps) {
  const [editing, setEditing] = useState(false)
  const [value, setValue] = useState('')
  const [saving, setSaving] = useState(false)
  const [removing, setRemoving] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')

  const loading = keyInfo === undefined
  const showForm = loading ? false : !keyInfo.present || editing

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    if (!value) return
    setSaving(true)
    setErrorMessage('')
    try {
      // The real verify runs one read-only Codex turn and can take up to 60s,
      // so this deliberately has no client-side timeout shorter than that -
      // the spinner just stays up until the request settles.
      await onSave(value)
      setValue('')
      setEditing(false)
    } catch (err) {
      setErrorMessage(err instanceof ApiError ? err.message : 'Something went wrong saving the key.')
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
            Codex sessions run on an OpenAI API key. Paste one from your OpenAI account; Styr seals it and passes it to
            the Codex CLI as an environment variable, nothing else. {CODEX_SANDBOX_NOTE}
          </p>

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
                placeholder="sk-proj-…"
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

      {!loading && !showForm && keyInfo.present && (
        <div data-testid={`${testId}-present`} className="flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="truncate font-mono text-[13px] text-fg-primary">{keyInfo.label}</p>
            <p className="mt-0.5 text-[12px] text-fg-secondary">Verified when it was saved</p>
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
 * this component. */
function NestedFrame({ children, ...rest }: { children?: ReactNode; 'data-testid'?: string; title?: string }) {
  return <div {...rest}>{children}</div>
}
