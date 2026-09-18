// Small presentation helpers shared by the Workspaces page and
// NewSessionDialog. Nothing here shows a filesystem path for a git or empty
// workspace - only a "path" source does, and only to an admin, per the
// product decision that Styr owns every path except one an admin
// deliberately registers from the box it runs on.
import { Loader2 } from 'lucide-react'
import type { Workspace } from '../../api/types'
import { Badge, Tooltip } from '../ui'

function repoHost(url: string | null): string {
  if (!url) return 'git'
  const trimmed = url.trim()
  if (trimmed.startsWith('git@')) {
    const afterAt = trimmed.slice('git@'.length)
    const host = afterAt.split(':')[0]
    return host || 'git'
  }
  try {
    return new URL(trimmed).hostname || 'git'
  } catch {
    return 'git'
  }
}

/** Compact text used inside Select options, where only a string label fits. */
export function workspaceSourceText(workspace: Workspace): string {
  if (workspace.source === 'git') {
    return workspace.branch ? `git · ${workspace.branch}` : 'git'
  }
  if (workspace.source === 'empty') return 'empty'
  return 'server path'
}

export function workspaceOptionLabel(workspace: Workspace): string {
  return `${workspace.name} (${workspaceSourceText(workspace)})`
}

export function WorkspaceSourceChip({ workspace, isAdmin }: { workspace: Workspace; isAdmin: boolean }) {
  if (workspace.source === 'git') {
    return (
      <Badge variant="outline" pill={false}>
        {repoHost(workspace.repo_url)}
        {workspace.branch ? ` · ${workspace.branch}` : ''}
      </Badge>
    )
  }
  if (workspace.source === 'empty') {
    return (
      <Badge variant="outline" pill={false}>
        Empty
      </Badge>
    )
  }
  return (
    <Badge variant="outline" pill={false} className={isAdmin ? 'font-mono text-[11px]' : undefined}>
      {isAdmin ? workspace.path : 'Server path'}
    </Badge>
  )
}

export function WorkspaceStateBadge({ workspace }: { workspace: Workspace }) {
  if (workspace.state === 'cloning') {
    return (
      <Badge tone="attention" data-testid={`workspace-state-${workspace.id}`}>
        <Loader2 size={11} className="animate-spin" aria-hidden />
        Cloning
      </Badge>
    )
  }
  if (workspace.state === 'ready') {
    return (
      <Badge tone="running" data-testid={`workspace-state-${workspace.id}`}>
        Ready
      </Badge>
    )
  }
  return (
    <Tooltip label={workspace.error || 'Something went wrong cloning this repository.'}>
      <span tabIndex={0} className="inline-flex">
        <Badge tone="failed" data-testid={`workspace-state-${workspace.id}`}>
          Failed
        </Badge>
      </span>
    </Tooltip>
  )
}
