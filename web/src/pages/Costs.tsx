// Cost (`/runs/costs`): what the whole box spent, per day and broken down
// by who, what started it and which template. A tab under Runs, because the
// question it answers - "what did all this cost?" - is the last one you ask
// about a run.
import { useQuery } from '@tanstack/react-query'
import { q } from '../api/queries'
import type { CostBreakdown, CostStats } from '../api/types'
import { CostChart } from '../components/stats/CostChart'
import { RunsTabs } from '../components/runs/RunsTabs'
import { Card, PageHeader, Skeleton, TableFrame, Td, Th, Tr } from '../components/ui'

function money(usd: number): string {
  return `$${usd.toFixed(2)}`
}

/** The last `n` days of the window, summed. The API sends the daily series,
 * so the tiles are slices of it rather than three more round trips. */
function sumLastDays(stats: CostStats, n: number): number {
  return stats.days.slice(-n).reduce((total, day) => total + day.usd, 0)
}

function Tile({ label, value, note, testId }: { label: string; value: string; note: string; testId?: string }) {
  return (
    <div
      data-testid="cost-tile"
      className="flex-1 rounded-[var(--radius-panel)] border border-hairline bg-surface-1 px-4 py-3.5 shadow-[var(--shadow-card)]"
    >
      <p className="text-[12px] font-medium text-fg-secondary">{label}</p>
      <p data-testid={testId} className="mt-1 text-[24px] font-semibold leading-8 tabular-nums tracking-[-0.02em] text-fg-primary">
        {value}
      </p>
      <p className="text-[11px] text-fg-muted">{note}</p>
    </div>
  )
}

function BreakdownTable({ title, rows, unit }: { title: string; rows: CostBreakdown[]; unit: string }) {
  return (
    <div className="min-w-0 flex-1">
      <h2 className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">{title}</h2>
      <TableFrame className="mt-2" minWidth={260}>
        <thead>
          <tr>
            <Th>Name</Th>
            <Th className="text-right">Cost</Th>
            <Th className="text-right">{unit}</Th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <Tr>
              <Td colSpan={3} className="text-fg-muted">
                Nothing in this window.
              </Td>
            </Tr>
          )}
          {rows.map((row) => (
            <Tr key={row.name}>
              <Td className="min-w-0 truncate">{row.name}</Td>
              <Td className="text-right font-mono text-[12px] tabular-nums text-fg-secondary">{money(row.usd)}</Td>
              <Td className="text-right font-mono text-[12px] tabular-nums text-fg-muted">{row.count}</Td>
            </Tr>
          ))}
        </tbody>
      </TableFrame>
    </div>
  )
}

export function Costs() {
  const costs = useQuery(q.costs(30))

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader title="Runs" description="What Styr has spent, and where it went." />

      <RunsTabs className="mt-5" />

      {costs.isLoading && (
        <div className="mt-4 flex flex-col gap-3">
          <Skeleton className="h-20 w-full" />
          <Skeleton className="h-48 w-full" />
        </div>
      )}

      {costs.data && (
        <>
          <div className="mt-4 flex flex-col gap-3 sm:flex-row">
            <Tile label="Today" value={money(sumLastDays(costs.data, 1))} note="so far" />
            <Tile label="Last 7 days" value={money(sumLastDays(costs.data, 7))} note="rolling" />
            <Tile
              label="Last 30 days"
              value={money(costs.data.total_usd)}
              note="the whole window"
              testId="cost-total"
            />
          </div>

          <Card className="mt-4" title="Daily spend">
            <CostChart days={costs.data.days} />
          </Card>

          <div className="mt-5 flex flex-col gap-5 lg:flex-row">
            <BreakdownTable title="By user" rows={costs.data.by_user} unit="Sessions" />
            <BreakdownTable title="By origin" rows={costs.data.by_origin} unit="Runs" />
            <BreakdownTable title="Top templates" rows={costs.data.top_templates} unit="Runs" />
          </div>

          <p className="mt-5 max-w-[68ch] text-[12px] text-fg-muted">
            API-equivalent cost as reported by Claude Code; your sessions run on your plan, so this is what the same
            work would have cost through the API rather than a bill you will be sent.
          </p>
        </>
      )}
    </div>
  )
}
