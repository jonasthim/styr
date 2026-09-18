// Model, profile, identity and stats for the session, plus a copyable
// `claude --resume <id>` block for continuing it from a terminal.
import { useState, type ReactNode } from 'react'
import { Copy, Check } from 'lucide-react'
import type { Origin, Profile, Session } from '../../api/types'

const ORIGIN_LABEL: Record<Origin, string> = {
  ui: 'UI',
  webhook: 'Webhook',
  schedule: 'Schedule',
  pipeline: 'Pipeline',
}

function formatDateTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-1.5 text-[12px]">
      <span className="shrink-0 text-fg-muted">{label}</span>
      <span className="min-w-0 truncate text-right font-mono tabular-nums text-fg-primary">{children}</span>
    </div>
  )
}

function ToolList({ label, tools }: { label: string; tools: string[] }) {
  return (
    <div className="py-1.5 text-[12px]">
      <div className="text-fg-muted">{label}</div>
      {tools.length === 0 ? (
        <div className="mt-1 text-fg-secondary">All tools</div>
      ) : (
        <div className="mt-1 flex flex-wrap gap-1">
          {tools.map((t) => (
            <span
              key={t}
              className="rounded-full border border-hairline px-1.5 py-0.5 font-mono text-[11px] text-fg-secondary"
            >
              {t}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false)
  return (
    <button
      type="button"
      aria-label={label}
      onClick={() => {
        void navigator.clipboard.writeText(text)
        setCopied(true)
        setTimeout(() => setCopied(false), 1200)
      }}
      className="flex h-6 w-6 shrink-0 items-center justify-center rounded-[var(--radius-1)] text-fg-secondary transition-colors duration-150 hover:bg-surface-3 hover:text-fg-primary"
    >
      {copied ? <Check size={12} className="text-state-running" /> : <Copy size={12} />}
    </button>
  )
}

export function InfoPanel({ session, profile }: { session: Session; profile?: Profile }) {
  const resumeCommand = `claude --resume ${session.id}`

  return (
    <div className="flex flex-col p-3" data-testid="info-panel">
      <Row label="Model">{session.model}</Row>
      <Row label="Turns">{session.num_turns}</Row>
      <Row label="Cost">${session.cost_usd.toFixed(2)}</Row>
      <Row label="Tokens in / out">
        {session.tokens_in.toLocaleString()} / {session.tokens_out.toLocaleString()}
      </Row>
      <Row label="Origin">{ORIGIN_LABEL[session.origin]}</Row>
      <Row label="Created">{formatDateTime(session.created_at)}</Row>
      <Row label="Last active">{formatDateTime(session.last_active_at)}</Row>

      <div className="my-2 h-px bg-hairline" />

      <div className="flex items-baseline justify-between gap-3 py-1.5 text-[12px]">
        <span className="text-fg-muted">Profile</span>
        <span className="font-mono text-fg-primary">{profile?.name ?? session.profile_id}</span>
      </div>
      {profile && (
        <>
          <Row label="Mode">{profile.mode}</Row>
          <ToolList label="Allowed tools" tools={profile.allowed_tools} />
          <ToolList label="Denied tools" tools={profile.disallowed_tools} />
        </>
      )}

      <div className="my-2 h-px bg-hairline" />

      <div className="flex items-center justify-between gap-2 py-1.5 text-[12px]">
        <span className="shrink-0 text-fg-muted">Session id</span>
        <div className="flex min-w-0 items-center gap-1">
          <span className="truncate font-mono text-[11px] text-fg-primary">{session.id}</span>
          <CopyButton text={session.id} label="Copy session id" />
        </div>
      </div>

      <div className="py-1.5 text-[12px]">
        <div className="mb-1 text-fg-muted">Resume in terminal</div>
        <div className="flex items-center gap-2 rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-2 py-1.5">
          <code data-testid="resume-command" className="min-w-0 flex-1 truncate font-mono text-[12px] text-fg-primary">
            {resumeCommand}
          </code>
          <CopyButton text={resumeCommand} label="Copy resume command" />
        </div>
      </div>
    </div>
  )
}
