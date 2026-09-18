// Files touched by the session (lib/spans.ts's filesTouched), mono paths
// with op chips. Clicking a row toggles `?file=` on the route (router.tsx),
// which SessionDetail.tsx uses to filter the transcript to blocks touching
// that file; clicking the active row again clears the filter.
import { useMemo } from 'react'
import { useNavigate, useParams, useSearch } from '@tanstack/react-router'
import clsx from 'clsx'
import { filesTouched } from '../../lib/spans'
import type { Block } from '../../lib/blocks'

const OP_META: Record<'read' | 'edit' | 'write', { label: string; className: string }> = {
  read: { label: 'read', className: 'text-fg-muted' },
  edit: { label: 'edit', className: 'text-accent' },
  write: { label: 'write', className: 'text-state-running' },
}

function OpChip({ op }: { op: 'read' | 'edit' | 'write' }) {
  const meta = OP_META[op]
  return (
    <span
      className={clsx(
        'rounded-full border border-hairline px-1.5 py-0.5 text-[10px] font-medium tabular-nums',
        meta.className,
      )}
    >
      {meta.label}
    </span>
  )
}

export function ChangesList({ blocks }: { blocks: Block[] }) {
  const files = useMemo(() => filesTouched(blocks), [blocks])
  const { id } = useParams({ from: '/_app/sessions/$id' })
  const search = useSearch({ from: '/_app/sessions/$id' })
  const navigate = useNavigate()

  function selectFile(path: string) {
    void navigate({
      to: '/sessions/$id',
      params: { id },
      search: (prev) => ({ ...prev, file: prev.file === path ? undefined : path }),
      replace: true,
    })
  }

  if (files.length === 0) {
    return <div className="py-6 text-center text-[13px] text-fg-muted">No files touched yet.</div>
  }

  return (
    <ul className="flex flex-col divide-y divide-hairline" data-testid="changes-list">
      {files.map((f) => (
        <li key={f.path}>
          <button
            type="button"
            onClick={() => selectFile(f.path)}
            aria-pressed={search.file === f.path}
            className={clsx(
              'flex w-full items-center gap-2 px-3 py-2 text-left transition-colors duration-150 hover:bg-surface-2',
              search.file === f.path && 'bg-surface-2',
            )}
          >
            <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-fg-primary">{f.path}</span>
            <span className="flex shrink-0 gap-1">
              {f.ops.map((op) => (
                <OpChip key={op} op={op} />
              ))}
            </span>
          </button>
        </li>
      ))}
    </ul>
  )
}
