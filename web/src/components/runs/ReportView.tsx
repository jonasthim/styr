// Renders a run's structured report against the plan's seeded Grafana
// schema (severity, diagnosis, evidence[], proposed_action, confidence,
// resolved_itself) - other templates may define a different report_schema,
// so every field is optional here and only rendered when present, per the
// plan's "report rendered from the schema" phrasing.
import { Badge } from '../ui'
import type { StructuredReport } from '../../api/types'

const SEVERITY_TONE: Record<NonNullable<StructuredReport['severity']>, 'neutral' | 'attention' | 'failed'> = {
  info: 'neutral',
  warning: 'attention',
  critical: 'failed',
}

function parseReport(raw: string): StructuredReport | null {
  if (!raw.trim()) return null
  try {
    const parsed: unknown = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? (parsed as StructuredReport) : null
  } catch {
    return null
  }
}

/** 4px confidence meter - a plain filled track rather than a numeric gauge,
 * since the report is meant to be skimmed at a glance from the runs list's
 * eventual drill-in as much as read closely here. */
function ConfidenceMeter({ confidence }: { confidence: number }) {
  const pct = Math.max(0, Math.min(100, Math.round(confidence * 100)))
  return (
    <div className="flex items-center gap-2">
      <div
        role="progressbar"
        aria-label="Confidence"
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
        className="h-1 w-32 overflow-hidden rounded-full bg-surface-3"
      >
        <div className="h-full rounded-full bg-accent transition-[width] duration-[var(--duration-base)]" style={{ width: `${pct}%` }} />
      </div>
      <span className="font-mono text-[12px] tabular-nums text-fg-secondary">{pct}%</span>
    </div>
  )
}

export function ReportView({ report }: { report: string }) {
  const parsed = parseReport(report)

  if (!parsed) {
    return <p className="text-[13px] text-fg-secondary">No report yet.</p>
  }

  return (
    <div data-testid="run-report" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        {parsed.severity && <Badge tone={SEVERITY_TONE[parsed.severity]}>{parsed.severity}</Badge>}
        {parsed.resolved_itself && <Badge tone="neutral">Resolved itself</Badge>}
        {typeof parsed.confidence === 'number' && <ConfidenceMeter confidence={parsed.confidence} />}
      </div>

      {parsed.diagnosis && <p className="max-w-[68ch] text-[13px] leading-5 text-fg-primary">{parsed.diagnosis}</p>}

      {parsed.evidence && parsed.evidence.length > 0 && (
        <div>
          <h3 className="text-[12px] font-medium text-fg-secondary">Evidence</h3>
          <ul className="mt-1.5 flex flex-col gap-1">
            {parsed.evidence.map((line, i) => (
              <li key={i} className="flex gap-2 font-mono text-[12px] leading-5 text-fg-secondary">
                <span aria-hidden className="text-fg-muted">
                  ·
                </span>
                <span className="min-w-0 break-words">{line}</span>
              </li>
            ))}
          </ul>
        </div>
      )}

      {parsed.proposed_action && (
        <div>
          <h3 className="text-[12px] font-medium text-fg-secondary">Proposed action</h3>
          <p className="mt-1 max-w-[68ch] text-[13px] leading-5 text-fg-primary">{parsed.proposed_action}</p>
        </div>
      )}
    </div>
  )
}
