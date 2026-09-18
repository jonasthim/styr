// Small labelled chips shared by the Triggers list and its deliveries
// drawer: a trigger's kind, and a delivery's pipeline status.
import { Badge, type BadgeTone } from '../ui'
import type { DeliveryStatus, TriggerKind } from '../../api/types'

const KIND_LABEL: Record<TriggerKind, string> = {
  generic: 'Generic',
  grafana: 'Grafana',
  github: 'GitHub',
}

export function KindChip({ kind }: { kind: TriggerKind }) {
  return (
    <Badge tone="accent" variant="outline">
      {KIND_LABEL[kind]}
    </Badge>
  )
}

const STATUS_LABEL: Record<DeliveryStatus, string> = {
  accepted: 'Accepted',
  deduped: 'Deduped',
  cooldown: 'Cooldown',
  storm: 'Storm',
  rejected: 'Rejected',
  failed: 'Failed',
  skipped: 'Skipped',
}

const STATUS_TONE: Record<DeliveryStatus, BadgeTone> = {
  accepted: 'running',
  deduped: 'neutral',
  cooldown: 'neutral',
  storm: 'attention',
  rejected: 'failed',
  failed: 'failed',
  skipped: 'neutral',
}

export function StatusChip({ status }: { status: DeliveryStatus }) {
  return <Badge tone={STATUS_TONE[status]}>{STATUS_LABEL[status]}</Badge>
}
