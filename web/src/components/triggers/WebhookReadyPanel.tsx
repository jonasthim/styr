// Shown once, right after a trigger is created: the webhook URL, the secret
// (this is the only response that ever carries it - the server only stores
// its hash from here on) and, for a Grafana trigger, a copyable contact-point
// snippet. Copy-to-clipboard mirrors ClaudeTokenCard's pattern (a 1.5s
// "Copied" flip, silently no-op if the clipboard API is denied).
import { useState } from 'react'
import { Check, Copy } from 'lucide-react'
import type { Trigger } from '../../api/types'
import { Button } from '../ui'

function CopyRow({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard access can be denied; the value is still selectable text.
    }
  }

  return (
    <div className="flex items-center gap-2 rounded-[var(--radius-control)] border border-hairline bg-surface-2 py-1 pl-3 pr-1">
      <code className="min-w-0 flex-1 truncate font-mono text-[12px] text-fg-primary">{value}</code>
      <Button
        size="sm"
        variant="ghost"
        onClick={() => void copy()}
        aria-label={`Copy ${label}`}
        className="w-7 shrink-0 px-0"
        icon={copied ? <Check size={13} aria-hidden className="text-state-running" /> : <Copy size={13} aria-hidden />}
      />
    </div>
  )
}

export function WebhookReadyPanel({ trigger, secret }: { trigger: Trigger; secret: string }) {
  const url = `${window.location.origin}/hooks/${trigger.slug}`
  const contactPointSnippet = `URL: ${url}\nAuthorization: Bearer ${secret}`

  return (
    <div data-testid="webhook-ready" className="flex flex-col gap-4">
      <div className="rounded-[var(--radius-control)] border border-state-attention/30 bg-state-attention/10 px-3 py-2.5">
        <p className="text-[12px] text-fg-primary">
          This is the only time the secret is shown. Copy it now — Styr only keeps its hash from here on.
        </p>
      </div>

      <div>
        <p className="mb-1.5 text-[12px] font-medium text-fg-secondary">Webhook URL</p>
        <CopyRow value={url} label="webhook URL" />
      </div>

      <div>
        <p className="mb-1.5 text-[12px] font-medium text-fg-secondary">Secret</p>
        <CopyRow value={secret} label="secret" />
      </div>

      {trigger.kind === 'grafana' && (
        <div>
          <p className="mb-1.5 text-[12px] font-medium text-fg-secondary">Grafana contact point</p>
          <p className="mb-1.5 text-[12px] text-fg-secondary">
            Add a webhook contact point in Grafana with this URL and header.
          </p>
          <div className="flex items-start gap-2 rounded-[var(--radius-control)] border border-hairline bg-surface-2 py-2 pl-3 pr-1">
            <pre className="min-w-0 flex-1 overflow-x-auto whitespace-pre-wrap break-all font-mono text-[12px] text-fg-primary">
              {contactPointSnippet}
            </pre>
            <CopySnippetButton value={contactPointSnippet} />
          </div>
        </div>
      )}
    </div>
  )
}

function CopySnippetButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)
  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      // Clipboard access can be denied; the value is still selectable text.
    }
  }
  return (
    <Button
      size="sm"
      variant="ghost"
      onClick={() => void copy()}
      aria-label="Copy Grafana contact point"
      className="w-7 shrink-0 px-0"
      icon={copied ? <Check size={13} aria-hidden className="text-state-running" /> : <Copy size={13} aria-hidden />}
    />
  )
}
