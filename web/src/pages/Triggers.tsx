// Triggers (`/triggers`): a Tabs strip switches between the Triggers list
// (inbound webhooks) and the Templates list (what a trigger's delivery
// renders into a prompt) - see router.tsx's ?tab= search param, which makes
// the active tab a shareable link rather than local-only state.
import { useState } from 'react'
import { useNavigate, useSearch, Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Zap } from 'lucide-react'
import { api } from '../api/client'
import { q } from '../api/queries'
import type { Template, Trigger } from '../api/types'
import { CreateTriggerDialog } from '../components/triggers/CreateTriggerDialog'
import { DeliveriesDialog } from '../components/triggers/DeliveriesDialog'
import { TestPayloadDialog } from '../components/triggers/TestPayloadDialog'
import { TriggerRow } from '../components/triggers/TriggerRow'
import { relativeTime } from '../components/inbox/format'
import { Badge, Button, EmptyState, PageHeader, TableFrame, Tabs, TabsList, TabsTrigger, Td, Th, Tr } from '../components/ui'

function TriggersTab() {
  const triggers = useQuery(q.triggers())
  const templates = useQuery(q.templates())
  const pipelines = useQuery(q.pipelines())
  const [createOpen, setCreateOpen] = useState(false)
  const [deliveriesTarget, setDeliveriesTarget] = useState<Trigger | null>(null)
  const [testTarget, setTestTarget] = useState<Trigger | null>(null)

  /** What a trigger starts: a template, or - since T54 - a pipeline. */
  function targetName(trigger: Trigger): string {
    if (trigger.pipeline_id) {
      return pipelines.data?.find((p) => p.id === trigger.pipeline_id)?.name ?? trigger.pipeline_id
    }
    return templates.data?.find((t) => t.id === trigger.template_id)?.name ?? trigger.template_id
  }

  const isEmpty = triggers.isSuccess && triggers.data.length === 0

  return (
    <>
      <div className="mt-5 flex justify-end">
        <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setCreateOpen(true)}>
          New trigger
        </Button>
      </div>

      {isEmpty && (
        <EmptyState
          icon={<Zap size={18} aria-hidden />}
          title="No triggers yet"
          description="A trigger turns an inbound webhook — a Grafana alert, GitHub event or anything else — into an unattended investigation."
          action={
            <Button variant="primary" icon={<Plus size={14} aria-hidden />} onClick={() => setCreateOpen(true)}>
              New trigger
            </Button>
          }
        />
      )}

      {triggers.data && triggers.data.length > 0 && (
        <TableFrame className="mt-3" minWidth={720}>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Kind</Th>
              <Th>Runs</Th>
              <Th>Last delivery</Th>
              <Th>Enabled</Th>
              <Th className="w-10">
                <span className="sr-only">Actions</span>
              </Th>
            </tr>
          </thead>
          <tbody>
            {triggers.data.map((trigger) => (
              <TriggerRow
                key={trigger.id}
                trigger={trigger}
                targetName={targetName(trigger)}
                onOpenDeliveries={setDeliveriesTarget}
                onOpenTest={setTestTarget}
              />
            ))}
          </tbody>
        </TableFrame>
      )}

      <CreateTriggerDialog open={createOpen} onOpenChange={setCreateOpen} />
      <DeliveriesDialog trigger={deliveriesTarget} open={deliveriesTarget !== null} onOpenChange={(open) => !open && setDeliveriesTarget(null)} />
      <TestPayloadDialog trigger={testTarget} open={testTarget !== null} onOpenChange={(open) => !open && setTestTarget(null)} />
    </>
  )
}

function TemplatesTab() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const templates = useQuery(q.templates())
  const workspaces = useQuery(q.workspaces())
  const profiles = useQuery(q.profiles())
  const [creating, setCreating] = useState(false)

  function workspaceName(id: string): string {
    return workspaces.data?.find((w) => w.id === id)?.name ?? id
  }
  function profileName(id: string): string {
    return profiles.data?.find((p) => p.id === id)?.name ?? id
  }

  async function handleNewTemplate() {
    setCreating(true)
    try {
      const template = await api<Template>('/api/v1/templates', {
        method: 'POST',
        json: {
          name: 'Untitled template',
          workspace_id: workspaces.data?.[0]?.id ?? '',
          profile_id: profiles.data?.find((p) => p.name === 'investigate')?.id ?? profiles.data?.[0]?.id ?? '',
        },
      })
      queryClient.setQueryData<Template[]>(['templates'], (prev) => (prev ? [template, ...prev] : [template]))
      void navigate({ to: '/templates/$id', params: { id: template.id } })
    } finally {
      setCreating(false)
    }
  }

  const isEmpty = templates.isSuccess && templates.data.length === 0

  return (
    <>
      <div className="mt-5 flex justify-end">
        <Button variant="primary" icon={<Plus size={14} aria-hidden />} loading={creating} onClick={() => void handleNewTemplate()}>
          New template
        </Button>
      </div>

      {isEmpty && (
        <EmptyState
          title="No templates yet"
          description="A template turns a delivery's payload into a title and prompt for the unattended session it starts."
          action={
            <Button variant="primary" icon={<Plus size={14} aria-hidden />} loading={creating} onClick={() => void handleNewTemplate()}>
              New template
            </Button>
          }
        />
      )}

      {templates.data && templates.data.length > 0 && (
        <TableFrame className="mt-3" minWidth={640}>
          <thead>
            <tr>
              <Th>Name</Th>
              <Th>Workspace</Th>
              <Th>Profile</Th>
              <Th>Updated</Th>
            </tr>
          </thead>
          <tbody>
            {templates.data.map((template) => (
              <Tr key={template.id} data-testid={`template-row-${template.id}`}>
                <Td>
                  <span className="flex items-center gap-2">
                    <Link to="/templates/$id" params={{ id: template.id }} className="font-medium text-fg-primary no-underline hover:text-accent hover:underline">
                      {template.name}
                    </Link>
                    {/* A looping template keeps going on its own, which is
                        worth knowing before you point a trigger at it. */}
                    {template.loop_until && (
                      <span title={`Repeats until the report's ${template.loop_until} is true`}>
                        <Badge tone="accent" variant="outline">
                          Loop
                        </Badge>
                      </span>
                    )}
                  </span>
                </Td>
                <Td className="font-mono text-[12px] text-fg-secondary">{workspaceName(template.workspace_id)}</Td>
                <Td className="font-mono text-[12px] text-fg-secondary">{profileName(template.profile_id)}</Td>
                <Td className="font-mono text-[12px] tabular-nums text-fg-muted">{relativeTime(template.updated_at)} ago</Td>
              </Tr>
            ))}
          </tbody>
        </TableFrame>
      )}
    </>
  )
}

export function Triggers() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/_app/triggers' })
  const tab = search.tab ?? 'triggers'

  function setTab(next: 'triggers' | 'templates') {
    void navigate({ to: '/triggers', search: next === 'triggers' ? {} : { tab: next }, replace: true })
  }

  return (
    <div className="mx-auto flex w-full max-w-[1100px] flex-1 flex-col px-4 py-6 sm:px-6">
      <PageHeader
        title="Triggers"
        description="Inbound webhooks that start an unattended session, and the templates they render into a prompt."
      />

      <Tabs value={tab} onValueChange={(v) => setTab(v as 'triggers' | 'templates')} className="mt-5">
        <TabsList aria-label="Triggers or templates">
          <TabsTrigger value="triggers">Triggers</TabsTrigger>
          <TabsTrigger value="templates">Templates</TabsTrigger>
        </TabsList>
      </Tabs>

      {tab === 'triggers' ? <TriggersTab /> : <TemplatesTab />}
    </div>
  )
}
