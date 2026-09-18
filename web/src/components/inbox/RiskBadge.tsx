// Risk tier pill: read muted, write accent, exec attention, destructive
// failed-colour with a filled icon. Colours come from tokens only (see
// docs/superpowers/plans/2026-09-18-styr-v0.1.md, "Frontend conventions").
import { AlertTriangle, Eye, Pencil, Terminal, type LucideIcon } from 'lucide-react'
import type { RiskTier } from '../../api/types'
import { Badge, type BadgeTone, type BadgeVariant } from '../ui'

const RISK_META: Record<RiskTier, { icon: LucideIcon; tone: BadgeTone; variant: BadgeVariant; filled: boolean }> = {
  read: { icon: Eye, tone: 'neutral', variant: 'outline', filled: false },
  write: { icon: Pencil, tone: 'accent', variant: 'outline', filled: false },
  exec: { icon: Terminal, tone: 'attention', variant: 'outline', filled: false },
  // The one tier that gets a fill: a destructive command should be the first
  // thing the eye lands on in a stack of cards.
  destructive: { icon: AlertTriangle, tone: 'failed', variant: 'soft', filled: true },
}

export function RiskBadge({ tier }: { tier: RiskTier }) {
  const meta = RISK_META[tier]
  const Icon = meta.icon
  return (
    <Badge data-testid="risk-badge" tone={meta.tone} variant={meta.variant}>
      <Icon size={11} aria-hidden fill={meta.filled ? 'currentColor' : 'none'} />
      {tier}
    </Badge>
  )
}
