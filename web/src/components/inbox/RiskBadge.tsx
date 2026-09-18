// Risk tier pill: read muted, write accent, exec attention, destructive
// failed-colour with a filled icon. Colours come from tokens only (see
// docs/superpowers/plans/2026-09-18-styr-v0.1.md, "Frontend conventions").
import { AlertTriangle, Eye, Pencil, Terminal, type LucideIcon } from 'lucide-react'
import clsx from 'clsx'
import type { RiskTier } from '../../api/types'

const RISK_META: Record<RiskTier, { icon: LucideIcon; className: string; filled: boolean }> = {
  read: { icon: Eye, className: 'text-fg-muted', filled: false },
  write: { icon: Pencil, className: 'text-accent', filled: false },
  exec: { icon: Terminal, className: 'text-state-attention', filled: false },
  destructive: { icon: AlertTriangle, className: 'text-state-failed bg-state-failed/10', filled: true },
}

export function RiskBadge({ tier }: { tier: RiskTier }) {
  const meta = RISK_META[tier]
  const Icon = meta.icon
  return (
    <span
      data-testid="risk-badge"
      className={clsx(
        'inline-flex items-center gap-1 rounded-full border border-hairline px-1.5 py-0.5 text-[11px] font-medium tabular-nums',
        meta.className,
      )}
    >
      <Icon size={11} aria-hidden fill={meta.filled ? 'currentColor' : 'none'} />
      {tier}
    </span>
  )
}
