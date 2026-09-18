// One row in the Schedules table. The enabled switch PATCHes the whole row
// back (the same full-body PATCH TriggerRow.tsx does, for the same reason:
// the endpoint replaces the mutable fields rather than merging), and "Run
// now" fires immediately and follows the run it started.
import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { Play } from 'lucide-react'
import { api, ApiError } from '../../api/client'
import type { RunStartedResult, Schedule } from '../../api/types'
import { useToast } from '../../hooks/useToast'
import { relativeTime } from '../inbox/format'
import { OutcomeGlyph } from '../runs/OutcomeGlyph'
import { OUTCOME_LABEL } from '../runs/outcome'
import { Button, Switch, Td, Tr } from '../ui'
import { absoluteTime, untilTime } from './chips'

export function ScheduleRow({
  schedule,
  templateName,
  description,
  onEdit,
  onOpenFirings,
}: {
  schedule: Schedule
  templateName: string
  /** The human sentence for this row's cron, resolved by the page. */
  description: string
  onEdit: (schedule: Schedule) => void
  onOpenFirings: (schedule: Schedule) => void
}) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const { toast } = useToast()
  const [pending, setPending] = useState(false)
  const [running, setRunning] = useState(false)

  async function handleToggle(checked: boolean) {
    setPending(true)
    try {
      const saved = await api<Schedule>(`/api/v1/schedules/${schedule.id}`, {
        method: 'PATCH',
        json: {
          name: schedule.name,
          template_id: schedule.template_id,
          cron: schedule.cron,
          vars: schedule.vars,
          enabled: checked,
        },
      })
      queryClient.setQueryData<Schedule[]>(['schedules'], (prev) =>
        prev?.map((s) => (s.id === schedule.id ? saved : s)),
      )
    } catch (err) {
      toast({
        title: 'Could not change the schedule',
        description: err instanceof ApiError ? err.message : undefined,
        tone: 'danger',
      })
    } finally {
      setPending(false)
    }
  }

  async function handleRunNow() {
    setRunning(true)
    try {
      const result = await api<RunStartedResult>(`/api/v1/schedules/${schedule.id}/run`, { method: 'POST' })
      void queryClient.invalidateQueries({ queryKey: ['schedules'] })
      void queryClient.invalidateQueries({ queryKey: ['runs'] })
      toast({ title: 'Run started', description: 'Opening it now.', tone: 'success' })
      void navigate({ to: '/runs/$id', params: { id: result.run_id } })
    } catch (err) {
      toast({
        title: 'Could not start a run',
        description: err instanceof ApiError ? err.message : undefined,
        tone: 'danger',
      })
    } finally {
      setRunning(false)
    }
  }

  return (
    <Tr data-testid={`schedule-row-${schedule.id}`}>
      <Td className="font-medium">
        <button
          type="button"
          onClick={() => onEdit(schedule)}
          className="rounded-[var(--radius-1)] text-left text-fg-primary outline-none hover:text-accent hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]"
        >
          {schedule.name}
        </button>
      </Td>
      <Td className="min-w-0 truncate font-mono text-[12px] text-fg-secondary">{templateName}</Td>
      <Td>
        <span className="font-mono text-[12px] text-fg-primary">{schedule.cron}</span>
        {description && <span className="mt-0.5 block text-[11px] text-fg-muted">{description}</span>}
      </Td>
      <Td className="whitespace-nowrap font-mono text-[12px] tabular-nums text-fg-secondary">
        {schedule.next_run_at ? (
          <span title={absoluteTime(schedule.next_run_at)}>{untilTime(schedule.next_run_at)}</span>
        ) : (
          <span className="text-fg-muted">paused</span>
        )}
      </Td>
      <Td className="whitespace-nowrap">
        {schedule.last_run_at ? (
          <span className="flex items-center gap-2">
            {schedule.last_outcome && <OutcomeGlyph outcome={schedule.last_outcome} />}
            <span className="font-mono text-[12px] tabular-nums text-fg-secondary" title={absoluteTime(schedule.last_run_at)}>
              {relativeTime(schedule.last_run_at)} ago
            </span>
            <span className="sr-only">{schedule.last_outcome ? OUTCOME_LABEL[schedule.last_outcome] : ''}</span>
          </span>
        ) : (
          <span className="font-mono text-[12px] text-fg-muted">never</span>
        )}
      </Td>
      <Td>
        <Switch
          checked={schedule.enabled}
          disabled={pending}
          onCheckedChange={(checked) => void handleToggle(checked)}
          aria-label={`Enabled for ${schedule.name}`}
        />
      </Td>
      <Td className="text-right">
        <div className="flex justify-end gap-1.5">
          <Button
            size="sm"
            variant="ghost"
            icon={<Play size={12} aria-hidden />}
            loading={running}
            onClick={() => void handleRunNow()}
          >
            Run now
          </Button>
          <Button size="sm" variant="ghost" onClick={() => onOpenFirings(schedule)}>
            Firings
          </Button>
        </div>
      </Td>
    </Tr>
  )
}
