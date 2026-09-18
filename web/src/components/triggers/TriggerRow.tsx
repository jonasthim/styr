// One row in the Triggers table: kind chip, template name, an enabled
// switch that PATCHes immediately (same optimistic-write pattern as
// Workspaces.tsx's WorktreesSwitch), last delivery (relative time), and
// buttons that open this trigger's deliveries drawer / test-payload dialog.
import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Send } from 'lucide-react'
import { api } from '../../api/client'
import type { Trigger } from '../../api/types'
import { relativeTime } from '../inbox/format'
import { Button, Switch, Td, Tr } from '../ui'
import { KindChip } from './chips'

export function TriggerRow({
  trigger,
  templateName,
  onOpenDeliveries,
  onOpenTest,
}: {
  trigger: Trigger
  templateName: string
  onOpenDeliveries: (trigger: Trigger) => void
  onOpenTest: (trigger: Trigger) => void
}) {
  const queryClient = useQueryClient()
  const [pending, setPending] = useState(false)

  async function handleToggle(checked: boolean) {
    setPending(true)
    try {
      await api(`/api/v1/triggers/${trigger.id}`, { method: 'PATCH', json: { enabled: checked } })
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
      <Td className="min-w-0 truncate font-mono text-[12px] text-fg-secondary">{templateName}</Td>
      <Td className="whitespace-nowrap font-mono text-[12px] tabular-nums text-fg-muted">
        {trigger.last_delivery_at ? `${relativeTime(trigger.last_delivery_at)} ago` : 'never'}
      </Td>
      <Td>
        <Switch
          checked={trigger.enabled}
          disabled={pending}
          onCheckedChange={(checked) => void handleToggle(checked)}
          aria-label={`Enabled for ${trigger.name}`}
        />
      </Td>
      <Td className="text-right">
        <div className="flex justify-end gap-1.5">
          <Button size="sm" variant="ghost" onClick={() => onOpenDeliveries(trigger)}>
            Deliveries
          </Button>
          <Button size="sm" variant="ghost" icon={<Send size={12} aria-hidden />} onClick={() => onOpenTest(trigger)}>
            Send test
          </Button>
        </div>
      </Td>
    </Tr>
  )
}
