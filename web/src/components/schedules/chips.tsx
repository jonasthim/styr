// Chips and time formatting shared by the schedules table and its firings
// drawer. A firing has its own three-value status, separate from a run's
// outcome: the scheduler either started a run, declined to (the previous one
// was still going), or could not.
import { Badge, type BadgeTone } from '../ui'
import type { ScheduleFiringStatus } from '../../api/types'

const FIRING_LABEL: Record<ScheduleFiringStatus, string> = {
  started: 'Started',
  skipped_overlap: 'Skipped',
  failed: 'Failed',
}

const FIRING_TONE: Record<ScheduleFiringStatus, BadgeTone> = {
  started: 'running',
  skipped_overlap: 'neutral',
  failed: 'failed',
}

export function FiringStatusChip({ status }: { status: ScheduleFiringStatus }) {
  return <Badge tone={FIRING_TONE[status]}>{FIRING_LABEL[status]}</Badge>
}

/** "in 12m", "in 7h", "in 3d" - the mirror of inbox/format.ts's
 * relativeTime, for a time that has not happened yet. */
export function untilTime(iso: string): string {
  const diffMs = new Date(iso).getTime() - Date.now()
  const minutes = Math.round(diffMs / 60_000)
  if (minutes <= 0) return 'any moment'
  if (minutes < 60) return `in ${minutes}m`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `in ${hours}h`
  return `in ${Math.round(hours / 24)}d`
}

/** The server's own clock, spelled out for the title attribute so a relative
 * time is never the only thing on offer. */
export function absoluteTime(iso: string): string {
  return new Date(iso).toLocaleString()
}

/** A firing time in the preview list: day and clock, no seconds and no year
 * - five of these stacked have to scan as a rhythm, not as timestamps. */
export function previewTime(iso: string): string {
  return new Date(iso).toLocaleString([], {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}
