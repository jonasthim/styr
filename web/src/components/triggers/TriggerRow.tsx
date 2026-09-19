// One row in the Triggers table: kind chip, template name, an enabled
// switch that PATCHes immediately (same optimistic-write pattern as
// Workspaces.tsx's WorktreesSwitch), last delivery (relative time), and
// buttons that open this trigger's deliveries drawer / test-payload dialog.
import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Send } from 'lucide-react'
import { api } from '../../api/client'
import type { Trigger } from '../../api/types'
import { useCanWrite } from '../../hooks/useCanWrite'
import { relativeTime } from '../inbox/format'
import { Button, Switch, Td, Tr } from '../ui'
import { KindChip } from './chips'

export function TriggerRow({
  trigger,
  targetName,
  onOpenDeliveries,
  onOpenTest,
}: {
  trigger: Trigger
  /** The template or pipeline this trigger starts, resolved by the page. */
  targetName: string
  onOpenDeliveries: (trigger: Trigger) => void
  onOpenTest: (trigger: Trigger) => void
}) {
  const queryClient = useQueryClient()
  const canWrite = useCanWrite()
  const [pending, setPending] = useState(false)

  async function handleToggle(checked: boolean) {
    setPending(true)
    try {
      // PATCH /triggers/{id} replaces the trigger's mutable fields rather
      // than merging (docs/openapi.yaml's TriggerInput requires name, kind
      // and template_id; an omitted kind would silently fall back to
      // "generic"), so the whole row is sent back with only `enabled`
      // changed - the same full-body PATCH the template editor does.
      await api(`/api/v1/triggers/${trigger.id}`, {
        method: 'PATCH',
        json: {
          name: trigger.name,
          kind: trigger.kind,
          template_id: trigger.template_id,
          pipeline_id: trigger.pipeline_id,
          dedupe_key_template: trigger.dedupe_key_template,
          cooldown_s: trigger.cooldown_s,
          storm_cap_per_hour: trigger.storm_cap_per_hour,
          run_on_resolved: trigger.run_on_resolved,
          enabled: checked,
        },
      })
      queryClient.setQueryData<Trigger[]>(['triggers'], (prev) =>
        prev?.map((t) => (t.id === trigger.id ? { ...t, enabled: checked } : t)),
      )
    } finally {
      setPending(false)
    }
  }

  return (
    <Tr data-testid={`trigger-row-${trigger.id}`}>
      <Td className="font-medium">{trigger.name}</Td>
      <Td>
        <KindChip kind={trigger.kind} />
      </Td>
      <Td className="min-w-0 truncate font-mono text-[12px] text-fg-secondary">{targetName}</Td>
      <Td className="whitespace-nowrap font-mono text-[12px] tabular-nums text-fg-muted">
        {trigger.last_delivery_at ? `${relativeTime(trigger.last_delivery_at)} ago` : 'never'}
      </Td>
      <Td>
        <Switch
          checked={trigger.enabled}
          disabled={pending || !canWrite}
          onCheckedChange={(checked) => void handleToggle(checked)}
          aria-label={`Enabled for ${trigger.name}`}
        />
      </Td>
      <Td className="text-right">
        <div className="flex justify-end gap-1.5">
          <Button size="sm" variant="ghost" onClick={() => onOpenDeliveries(trigger)}>
            Deliveries
          </Button>
          {canWrite && (
            <Button size="sm" variant="ghost" icon={<Send size={12} aria-hidden />} onClick={() => onOpenTest(trigger)}>
              Send test
            </Button>
          )}
        </div>
      </Td>
    </Tr>
  )
}
