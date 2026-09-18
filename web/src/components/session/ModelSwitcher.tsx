// Model and effort selects in the session header. The CLI takes both as
// start-up flags only, so switching either one ends the process and resumes
// the same session id under the new flags (POST /sessions/{id}/model). Until
// the CLI's next init message lands, the header says so rather than pretending
// the change already took.
import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { q } from '../../api/queries'
import { api } from '../../api/client'
import type { Effort, Session, SessionEvent } from '../../api/types'
import { Select } from '../ui'

/** Ids the composer's /model and /effort commands focus (SlashMenu.tsx). */
export const MODEL_SELECT_ID = 'session-model-select'
export const EFFORT_SELECT_ID = 'session-effort-select'

const EFFORT_LABEL: Record<Exclude<Effort, ''>, string> = {
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  xhigh: 'Extra high',
  max: 'Max',
}

/** The value the effort select shows for "whatever the CLI defaults to". */
const CLI_DEFAULT = '__default'

/** Counts init events in a transcript: one more than before means the resumed
 * process has reported in, so the switch is done. */
function initCount(events: SessionEvent[] | undefined): number {
  if (!events) return 0
  return events.filter((e) => {
    const payload = e.payload as { Type?: string } | null
    return payload?.Type === 'init'
  }).length
}

export function ModelSwitcher({ session }: { session: Session }) {
  const queryClient = useQueryClient()
  const status = useQuery(q.status())
  const events = useQuery(q.sessionEvents(session.id))

  // The label shown while resuming, and the init count at the moment the
  // switch was accepted; the banner clears when a later init arrives.
  const [resumingTo, setResumingTo] = useState<string | null>(null)
  const initsAtSwitch = useRef(0)

  // The resumed process's init is also when the session row's model becomes
  // the one the CLI actually resolved, and no session.state frame carries
  // that, so refetch the session as well as dropping the banner.
  const seenInits = initCount(events.data)
  useEffect(() => {
    if (resumingTo === null || seenInits <= initsAtSwitch.current) return
    setResumingTo(null)
    void queryClient.invalidateQueries({ queryKey: ['session', session.id] })
  }, [seenInits, resumingTo, queryClient, session.id])

  const models = status.data?.models ?? []
  const efforts = status.data?.efforts ?? []

  // The CLI reports the full model name it resolved ("claude-fable-5-1"),
  // which is not one of the aliases: offer it as its own option so the select
  // shows what is actually running instead of falling back to a placeholder.
  const isAlias = models.some((m) => m.alias === session.model)
  const modelOptions = [
    ...models.map((m) => ({ value: m.alias, label: m.label })),
    ...(session.model && !isAlias ? [{ value: session.model, label: session.model }] : []),
  ]

  const effortOptions = [
    { value: CLI_DEFAULT, label: 'Default effort' },
    ...efforts.map((e) => ({ value: e, label: EFFORT_LABEL[e] })),
  ]

  const switchTo = useMutation({
    mutationFn: (next: { model: string; effort: Effort }) =>
      api(`/api/v1/sessions/${session.id}/model`, { method: 'POST', json: next }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['session', session.id] })
      void queryClient.invalidateQueries({ queryKey: ['session-events', session.id] })
    },
    onError: () => setResumingTo(null),
  })

  function labelFor(model: string): string {
    return models.find((m) => m.alias === model)?.label ?? model
  }

  function apply(model: string, effort: Effort) {
    initsAtSwitch.current = seenInits
    setResumingTo(labelFor(model) || 'the CLI default')
    switchTo.mutate({ model, effort })
  }

  const busy = switchTo.isPending || resumingTo !== null

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select
        id={MODEL_SELECT_ID}
        aria-label="Model"
        value={session.model}
        disabled={busy}
        onValueChange={(value) => apply(value, session.effort)}
        options={modelOptions}
        placeholder="Model"
        className="w-[150px]"
      />
      <Select
        id={EFFORT_SELECT_ID}
        aria-label="Reasoning effort"
        value={session.effort || CLI_DEFAULT}
        disabled={busy}
        onValueChange={(value) => apply(session.model, value === CLI_DEFAULT ? '' : (value as Effort))}
        options={effortOptions}
        className="w-[140px]"
      />
      {resumingTo && (
        <span data-testid="model-resuming" className="text-[12px] text-fg-secondary">
          Resuming with {resumingTo}…
        </span>
      )}
    </div>
  )
}
