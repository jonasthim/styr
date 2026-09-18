// Loops (`/runs/loops`): every template that is repeating itself on one
// session until its report says done. A tab under Runs rather than its own
// rail item - a loop is a run that hasn't finished having opinions.
import { useState } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Repeat } from 'lucide-react'
import { q } from '../api/queries'
import type { Loop, LoopState } from '../api/types'
import { LoopDrawer } from '../components/loops/LoopDrawer'
import { LoopRow } from '../components/loops/LoopRow'
import { LOOP_STATE_LABEL } from '../components/loops/loopState'
import { RunsTabs } from '../components/runs/RunsTabs'
import { Button, EmptyState, PageHeader, TableFrame, Th } from '../components/ui'

const STATES: LoopState[] = ['running', 'done', 'exhausted', 'failed', 'stopped']

export function Loops() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/_app/runs/loops' })
  const state = search.state ?? ''
  const loops = useQuery(q.loops(state))
  const templates = useQuery(q.templates())
  const [details, setDetails] = useState<Loop | null>(null)

  function setState(next: LoopState | '') {
    void navigate({ to: '/runs/loops', search: next ? { state: next } : {}, replace: true })
  }

  function templateName(id: string): string {
    return templates.data?.find((t) => t.id === id)?.name ?? id
  }

  const isEmpty = loops.isSuccess && loops.data.length === 0

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader title="Runs" description="Loops repeat a template on one session until its report says it's done." />

      <RunsTabs className="mt-5" />

      <div role="group" aria-label="Filter by state" className="mt-4 flex flex-wrap gap-1.5">
        <Button size="sm" variant={state === '' ? 'primary' : 'secondary'} aria-pressed={state === ''} onClick={() => setState('')}>
          All
        </Button>
        {STATES.map((s) => (
          <Button key={s} size="sm" variant={state === s ? 'primary' : 'secondary'} aria-pressed={state === s} onClick={() => setState(s)}>
            {LOOP_STATE_LABEL[s]}
          </Button>
        ))}
      </div>

      {isEmpty && (
        <EmptyState
          icon={<Repeat size={18} aria-hidden />}
          title={state ? `No ${LOOP_STATE_LABEL[state].toLowerCase()} loops` : 'No loops yet'}
          description="Give a template an until field in its editor and every run it starts keeps going until the report says so."
        />
      )}

      {!isEmpty && (
        <TableFrame className="mt-4" minWidth={840}>
          <thead>
            <tr>
              <Th>State</Th>
              <Th>Template</Th>
              <Th>Iteration</Th>
              <Th>Origin</Th>
              <Th>Session</Th>
              <Th>Updated</Th>
              <Th className="w-10">
                <span className="sr-only">Actions</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {loops.data?.map((loop) => (
              <LoopRow key={loop.id} loop={loop} templateName={templateName(loop.template_id)} onOpenDetails={setDetails} />
            ))}
          </tbody>
        </TableFrame>
      )}

      <LoopDrawer loop={details} open={details !== null} onOpenChange={(open) => !open && setDetails(null)} />
    </div>
  )
}
