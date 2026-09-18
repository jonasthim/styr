// Every step run of one pipeline run, in the order they started: the flat
// record behind the graph, where a retry is a second row rather than a badge
// and a fan-out is one row per item. Clicking a row selects its node in the
// graph, so the two views stay in step with each other.
import { Link } from '@tanstack/react-router'
import type { StepRun } from '../../api/types'
import { relativeTime } from '../inbox/format'
import { Badge, TableFrame, Td, Th, Tr } from '../ui'
import { STEP_LABEL, STEP_TONE } from './stepState'

function duration(step: StepRun): string {
  if (!step.started_at) return '—'
  const end = step.finished_at ? Date.parse(step.finished_at) : Date.now()
  const seconds = Math.max(0, Math.round((end - Date.parse(step.started_at)) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${seconds % 60}s`
}

export function StepLog({ steps, onSelect }: { steps: StepRun[]; onSelect: (stepId: string) => void }) {
  const ordered = [...steps].sort((a, b) => {
    if (!a.started_at || !b.started_at) return Number(!a.started_at) - Number(!b.started_at)
    return a.started_at.localeCompare(b.started_at)
  })

  return (
    <TableFrame minWidth={640}>
      <thead>
        <tr>
          <Th>Step</Th>
          <Th>Item</Th>
          <Th className="w-20">Attempt</Th>
          <Th className="w-24">Started</Th>
          <Th className="w-20">Duration</Th>
          <Th className="w-28">State</Th>
          <Th className="w-16">
            <span className="sr-only">Run</span>
          </Th>
        </tr>
      </thead>
      <tbody>
        {ordered.map((step) => (
          <Tr key={step.id} data-testid="step-log-row" className="cursor-pointer" onClick={() => onSelect(step.step_id)}>
            <Td className="font-medium">{step.step_id}</Td>
            <Td className="max-w-[220px] truncate font-mono text-[12px] text-fg-secondary">{step.item || '—'}</Td>
            <Td className="font-mono text-[12px] tabular-nums text-fg-secondary">{step.attempt}</Td>
            <Td className="font-mono text-[12px] tabular-nums text-fg-muted">
              {step.started_at ? `${relativeTime(step.started_at)} ago` : '—'}
            </Td>
            <Td className="font-mono text-[12px] tabular-nums text-fg-secondary">{duration(step)}</Td>
            <Td>
              <Badge tone={STEP_TONE[step.state]}>{STEP_LABEL[step.state]}</Badge>
            </Td>
            <Td>
              {step.run_id ? (
                <Link
                  to="/runs/$id"
                  params={{ id: step.run_id }}
                  onClick={(e) => e.stopPropagation()}
                  className="text-[12px] text-accent no-underline hover:underline"
                >
                  Run
                </Link>
              ) : (
                <span className="text-[12px] text-fg-muted">—</span>
              )}
            </Td>
          </Tr>
        ))}
      </tbody>
    </TableFrame>
  )
}
