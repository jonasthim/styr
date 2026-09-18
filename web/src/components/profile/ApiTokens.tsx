// Personal API tokens (T36): GET/POST /api/v1/me/api-tokens list and
// create, DELETE /api/v1/me/api-tokens/{id} revoke. Mounted on Profile
// below ClaudeTokenCard. A created token's raw secret is shown exactly
// once, in the create dialog's own success state - list rows only ever
// carry its short, non-secret prefix (docs/openapi.yaml's APIToken
// schema), matching the same "never echo the secret back" rule
// ClaudeTokenCard follows for the Claude token.
import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Check, Copy, KeyRound, Plus } from 'lucide-react'
import { api, ApiError } from '../../api/client'
import type { ApiToken, ApiTokenCreated } from '../../api/types'
import { Button, Card, Dialog, DialogContent, EmptyState, Field, Input, Select, TableFrame, Td, Th, Tr } from '../ui'

const EXPIRY_OPTIONS = [
  { value: '0', label: 'Never' },
  { value: '30', label: '30 days' },
  { value: '90', label: '90 days' },
  { value: '365', label: '365 days' },
]

function formatDate(value: string | null): string {
  if (!value) return 'Never'
  return new Date(value).toLocaleDateString(undefined, { dateStyle: 'medium' })
}

function daysFromNowIso(days: number): string {
  return new Date(Date.now() + days * 24 * 60 * 60 * 1000).toISOString()
}

function CreateTokenDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (token: ApiToken) => void
}) {
  const [name, setName] = useState('')
  const [expiresInDays, setExpiresInDays] = useState('0')
  const [submitting, setSubmitting] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const [created, setCreated] = useState<ApiTokenCreated | null>(null)
  const [copied, setCopied] = useState(false)

  function reset() {
    setName('')
    setExpiresInDays('0')
    setSubmitError('')
    setCreated(null)
    setCopied(false)
  }

  function handleOpenChange(next: boolean) {
    if (!next) reset()
    onOpenChange(next)
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault()
    setSubmitting(true)
    setSubmitError('')
    try {
      const days = Number(expiresInDays)
      const body: Record<string, unknown> = { name }
      if (days > 0) body.expires_in_days = days
      const token = await api<ApiTokenCreated>('/api/v1/me/api-tokens', { method: 'POST', json: body })
      setCreated(token)
      onCreated({
        id: token.id,
        name: token.name,
        prefix: token.prefix,
        created_at: new Date().toISOString(),
        last_used_at: null,
        expires_at: days > 0 ? daysFromNowIso(days) : null,
      })
    } catch (err) {
      setSubmitError(err instanceof ApiError ? err.message : 'Something went wrong creating the token.')
    } finally {
      setSubmitting(false)
    }
  }

  async function copyToken() {
    if (!created) return
    try {
      await navigator.clipboard.writeText(created.token)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard access can be denied; the token is still selectable text.
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        title={created ? 'Token created' : 'Create token'}
        description={created ? undefined : 'Personal API tokens authenticate as you, without a browser login.'}
        srOnlyDescription={created ? 'The new token, shown once' : undefined}
        width={440}
      >
        {created ? (
          <div data-testid="api-token-created" className="flex flex-col gap-4">
            <p
              role="alert"
              data-testid="api-token-warning"
              className="rounded-[var(--radius-control)] border border-state-attention/30 bg-state-attention/10 px-3 py-2 text-[12px] text-fg-primary"
            >
              Copy this token now. It will not be shown again.
            </p>
            <div className="flex items-center gap-2 rounded-[var(--radius-control)] border border-hairline bg-surface-2 py-1 pl-3 pr-1">
              <code data-testid="api-token-value" className="flex-1 truncate font-mono text-[12px] text-fg-primary">
                {created.token}
              </code>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => void copyToken()}
                aria-label="Copy token"
                className="w-7 px-0"
                icon={copied ? <Check size={13} aria-hidden /> : <Copy size={13} aria-hidden />}
              />
            </div>
            <div className="flex justify-end">
              <Button variant="primary" onClick={() => handleOpenChange(false)}>
                Done
              </Button>
            </div>
          </div>
        ) : (
          <form onSubmit={(e) => void handleSubmit(e)} className="flex flex-col gap-4">
            <Field label="Name">
              {({ id }) => (
                <Input
                  id={id}
                  data-testid="api-token-name-input"
                  required
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="e.g. laptop script"
                  disabled={submitting}
                />
              )}
            </Field>
            <Field label="Expires">
              {({ id }) => (
                <Select
                  id={id}
                  value={expiresInDays}
                  onValueChange={setExpiresInDays}
                  options={EXPIRY_OPTIONS}
                  disabled={submitting}
                />
              )}
            </Field>
            {submitError && (
              <p
                role="alert"
                data-testid="api-token-error"
                className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger"
              >
                {submitError}
              </p>
            )}
            <div className="mt-1 flex items-center justify-end gap-2">
              <Button variant="ghost" type="button" onClick={() => handleOpenChange(false)}>
                Cancel
              </Button>
              <Button variant="primary" type="submit" disabled={!name} loading={submitting}>
                Create token
              </Button>
            </div>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

function RevokeTokenDialog({
  token,
  open,
  onOpenChange,
  onRevoked,
}: {
  token: ApiToken | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onRevoked: (id: string) => void
}) {
  const [revoking, setRevoking] = useState(false)
  const [errorMessage, setErrorMessage] = useState('')

  async function handleRevoke() {
    if (!token) return
    setRevoking(true)
    setErrorMessage('')
    try {
      await api(`/api/v1/me/api-tokens/${token.id}`, { method: 'DELETE' })
      onRevoked(token.id)
      onOpenChange(false)
    } catch (err) {
      setErrorMessage(err instanceof ApiError ? err.message : 'Something went wrong revoking the token.')
    } finally {
      setRevoking(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setErrorMessage('')
        onOpenChange(next)
      }}
    >
      <DialogContent
        title={token ? `Revoke ${token.name}?` : 'Revoke token?'}
        description="Anything using this token stops working immediately. This can't be undone."
        width={400}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button variant="danger" onClick={() => void handleRevoke()} loading={revoking}>
              Revoke
            </Button>
          </>
        }
      >
        {errorMessage && (
          <p
            role="alert"
            data-testid="revoke-token-error"
            className="rounded-[var(--radius-control)] border border-state-failed/30 bg-state-failed/10 px-3 py-2 text-[12px] text-fg-danger"
          >
            {errorMessage}
          </p>
        )}
      </DialogContent>
    </Dialog>
  )
}

export function ApiTokens() {
  const queryClient = useQueryClient()
  const tokens = useQuery({ queryKey: ['api-tokens'], queryFn: () => api<ApiToken[]>('/api/v1/me/api-tokens') })
  const [createOpen, setCreateOpen] = useState(false)
  const [revokeTarget, setRevokeTarget] = useState<ApiToken | null>(null)

  function handleCreated(token: ApiToken) {
    queryClient.setQueryData<ApiToken[]>(['api-tokens'], (prev) => (prev ? [token, ...prev] : [token]))
  }

  function handleRevoked(id: string) {
    queryClient.setQueryData<ApiToken[]>(['api-tokens'], (prev) => prev?.filter((t) => t.id !== id))
  }

  const isEmpty = tokens.isSuccess && tokens.data.length === 0

  return (
    <Card
      className="mt-4"
      title="API tokens"
      description="Personal tokens that authenticate as you, for scripts and automation."
      actions={
        <Button variant="primary" size="sm" icon={<Plus size={14} aria-hidden />} onClick={() => setCreateOpen(true)}>
          Create token
        </Button>
      }
      data-testid="api-tokens-card"
    >
      {isEmpty && (
        <EmptyState
          icon={<KeyRound size={18} aria-hidden />}
          title="No API tokens yet"
          description="Create one to authenticate scripts and automation as you."
        />
      )}

      {tokens.data && tokens.data.length > 0 && (
        <TableFrame minWidth={560}>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Token</Th>
              <Th>Created</Th>
              <Th>Last used</Th>
              <Th>Expires</Th>
              <Th className="w-10">
                <span className="sr-only">Actions</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {tokens.data.map((token) => (
              <Tr key={token.id} data-testid={`api-token-row-${token.id}`}>
                <Td className="font-medium">{token.name}</Td>
                <Td className="font-mono text-[12px] text-fg-secondary">{token.prefix}…</Td>
                <Td>{formatDate(token.created_at)}</Td>
                <Td>{formatDate(token.last_used_at)}</Td>
                <Td>{formatDate(token.expires_at)}</Td>
                <Td className="text-right">
                  <Button
                    variant="ghost"
                    size="sm"
                    aria-label={`Revoke ${token.name}`}
                    onClick={() => setRevokeTarget(token)}
                  >
                    Revoke
                  </Button>
                </Td>
              </Tr>
            ))}
          </tbody>
        </TableFrame>
      )}

      <CreateTokenDialog open={createOpen} onOpenChange={setCreateOpen} onCreated={handleCreated} />
      <RevokeTokenDialog
        token={revokeTarget}
        open={revokeTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRevokeTarget(null)
        }}
        onRevoked={handleRevoked}
      />
    </Card>
  )
}
