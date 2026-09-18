// Schedules (`/schedules`): the cron table, its create/edit dialog and the
// firings drawer. A schedule is the one thing in Styr that starts work with
// nobody watching, so the row leads with when it will next do that, and what
// happened the last time it did.
import { useMemo, useState } from 'react'
import { useQueries, useQuery } from '@tanstack/react-query'
import { Clock, Plus } from 'lucide-react'
import { api } from '../api/client'
import { q } from '../api/queries'
import type { CronPreview, Schedule } from '../api/types'
import { FiringsDialog } from '../components/schedules/FiringsDialog'
import { ScheduleDialog } from '../components/schedules/ScheduleDialog'
import { ScheduleRow } from '../components/schedules/ScheduleRow'
import { Button, EmptyState, PageHeader, Skeleton, TableFrame, Th, Tr, Td } from '../components/ui'

/** One POST /schedules/preview per distinct expression, so the table can
 * show what each cron means without every row asking separately. Cached by
 * expression, so two schedules on the same cadence cost one request. */
function useCronDescriptions(crons: string[]): Map<string, string> {
  const unique = useMemo(() => [...new Set(crons)], [crons])
  const results = useQueries({
    queries: unique.map((cron) => ({
      queryKey: ['cron-preview', cron],
      queryFn: () => api<CronPreview>('/api/v1/schedules/preview', { method: 'POST', json: { cron } }),
      staleTime: 5 * 60_000,
    })),
  })
  return useMemo(() => {
    const map = new Map<string, string>()
    unique.forEach((cron, i) => {
      const description = results[i]?.data?.description
      if (description) map.set(cron, description)
    })
    return map
  }, [unique, results])
}

function SkeletonRow() {
  return (
    <Tr>
      <Td colSpan={7}>
        <Skeleton className="h-3 w-48 max-w-[45%]" />
      </Td>
    </Tr>
  )
}

export function Schedules() {
  const schedules = useQuery(q.schedules())
  const templates = useQuery(q.templates())
  const pipelines = useQuery(q.pipelines())
  const [editing, setEditing] = useState<Schedule | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [firingsTarget, setFiringsTarget] = useState<Schedule | null>(null)

  const descriptions = useCronDescriptions((schedules.data ?? []).map((s) => s.cron))

  /** What a schedule starts: a template, or - since T54 - a pipeline. */
  function targetName(schedule: Schedule): string {
    if (schedule.pipeline_id) {
      return pipelines.data?.find((p) => p.id === schedule.pipeline_id)?.name ?? schedule.pipeline_id
    }
    return templates.data?.find((t) => t.id === schedule.template_id)?.name ?? schedule.template_id
  }

  function openNew() {
    setEditing(null)
    setDialogOpen(true)
  }

  function openEdit(schedule: Schedule) {
    setEditing(schedule)
    setDialogOpen(true)
  }

  const isEmpty = schedules.isSuccess && schedules.data.length === 0

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader
        title="Schedules"
        description="Cron entries that start a template on their own, in the server's timezone."
        actions={
          <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={openNew}>
            New schedule
          </Button>
        }
      />

      {isEmpty && (
        <EmptyState
          icon={<Clock size={18} aria-hidden />}
          title="No schedules yet"
          description="A schedule runs one of your templates on a cadence — a nightly sweep, a Monday digest — without anyone starting it."
          action={
            <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={openNew}>
              New schedule
            </Button>
          }
        />
      )}

      {!isEmpty && (
        <TableFrame className="mt-5" minWidth={880}>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Runs</Th>
              <Th>Cron</Th>
              <Th>Next run</Th>
              <Th>Last run</Th>
              <Th>Enabled</Th>
              <Th className="w-10">
                <span className="sr-only">Actions</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {schedules.isLoading && (
              <>
                <SkeletonRow />
                <SkeletonRow />
              </>
            )}
            {schedules.data?.map((schedule) => (
              <ScheduleRow
                key={schedule.id}
                schedule={schedule}
                targetName={targetName(schedule)}
                description={descriptions.get(schedule.cron) ?? ''}
                onEdit={openEdit}
                onOpenFirings={setFiringsTarget}
              />
            ))}
          </tbody>
        </TableFrame>
      )}

      <ScheduleDialog schedule={editing} open={dialogOpen} onOpenChange={setDialogOpen} />
      <FiringsDialog
        schedule={firingsTarget}
        open={firingsTarget !== null}
        onOpenChange={(open) => !open && setFiringsTarget(null)}
      />
    </div>
  )
}
