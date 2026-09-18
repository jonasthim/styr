// Settings > Notifications: outbound channels (ntfy, generic webhook) that
// fire on run.finished / run.needs_human / run.failed.
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api, ApiError } from '../../api/client'
import { q } from '../../api/queries'
import type { NotificationChannel } from '../../api/types'
import { useToast } from '../../hooks/useToast'
import { Badge, Button, Card, Switch, TableFrame, Td, Th, Tr } from '../ui'
import { AddNotificationDialog } from './AddNotificationDialog'

const EVENT_LABEL: Record<string, string> = {
  'run.finished': 'Finished',
  'run.needs_human': 'Needs you',
  'run.failed': 'Failed',
}

function TestButton({ channel }: { channel: NotificationChannel }) {
  const { toast } = useToast()
  const [pending, setPending] = useState(false)

  async function handleTest() {
    setPending(true)
    try {
      await api(`/api/v1/notifications/${channel.id}/test`, { method: 'POST' })
      toast({ title: 'Test notification sent', description: channel.name, tone: 'success' })
    } catch (err) {
      toast({ title: 'Test notification failed', description: err instanceof ApiError ? err.message : undefined, tone: 'danger' })
    } finally {
      setPending(false)
    }
  }

  return (
    <Button size="sm" variant="ghost" loading={pending} onClick={() => void handleTest()}>
      Test
    </Button>
  )
}

function EnabledSwitch({ channel }: { channel: NotificationChannel }) {
  const queryClient = useQueryClient()
  const [pending, setPending] = useState(false)

  async function handleChange(checked: boolean) {
    setPending(true)
    try {
      await api(`/api/v1/notifications/${channel.id}`, { method: 'PATCH', json: { enabled: checked } })
      queryClient.setQueryData<NotificationChannel[]>(['notifications'], (prev) =>
        prev?.map((c) => (c.id === channel.id ? { ...c, enabled: checked } : c)),
      )
    } finally {
      setPending(false)
    }
  }

  return <Switch checked={channel.enabled} disabled={pending} onCheckedChange={(checked) => void handleChange(checked)} aria-label={`Enabled for ${channel.name}`} />
}

export function NotificationsSection() {
  const notifications = useQuery(q.notifications())
  const [addOpen, setAddOpen] = useState(false)

  return (
    <>
      <Card
        title="Channels"
        description="Where a run's finish, need-a-decision or failure gets pushed."
        actions={
          <Button size="sm" variant="secondary" onClick={() => setAddOpen(true)}>
            Add channel
          </Button>
        }
        flush
      >
        {notifications.data && notifications.data.length > 0 ? (
          <TableFrame minWidth={640}>
            <thead>
              <tr>
                <Th>Kind</Th>
                <Th>Name</Th>
                <Th>URL</Th>
                <Th>Events</Th>
                <Th>Enabled</Th>
                <Th className="w-16">
                  <span className="sr-only">Test</span>
                </Th>
              </tr>
            </thead>
            <tbody>
              {notifications.data.map((channel) => (
                <Tr key={channel.id} data-testid={`notification-row-${channel.id}`}>
                  <Td>
                    <Badge tone="accent" variant="outline">
                      {channel.kind}
                    </Badge>
                  </Td>
                  <Td className="font-medium">{channel.name}</Td>
                  <Td className="max-w-[220px] truncate font-mono text-[12px] text-fg-secondary">{channel.url}</Td>
                  <Td>
                    <div className="flex flex-wrap gap-1">
                      {channel.events.map((event) => (
                        <Badge key={event}>{EVENT_LABEL[event] ?? event}</Badge>
                      ))}
                    </div>
                  </Td>
                  <Td>
                    <EnabledSwitch channel={channel} />
                  </Td>
                  <Td className="text-right">
                    <TestButton channel={channel} />
                  </Td>
                </Tr>
              ))}
            </tbody>
          </TableFrame>
        ) : (
          <p className="p-5 text-[13px] text-fg-secondary">No notification channels yet.</p>
        )}
      </Card>

      <AddNotificationDialog open={addOpen} onOpenChange={setAddOpen} />
    </>
  )
}
