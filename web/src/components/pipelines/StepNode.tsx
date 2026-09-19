// One step in the graph. The card itself stays as quiet as a table row - the
// state lives in a 3px rail down its left edge, the same device the nav rail
// uses for the active page - and the one piece of drawing in the whole view
// is reserved for the thing a DAG cannot say in words: a fan-out step is
// drawn as a real stack of cards with its n/N counter, so "this step is
// running four times" is legible before anything is read.
import { Handle, Position, type Node, type NodeProps } from '@xyflow/react'
import clsx from 'clsx'
import type { PipelineNode } from '../../api/types'
import { NODE_HEIGHT, NODE_WIDTH } from './graphLayout'
import { STEP_LABEL, STEP_RAIL_CLASS, type NodeProgress } from './stepState'

export type StepNodeData = {
  node: PipelineNode
  /** Null in the editor's preview, where no run exists yet. */
  progress: NodeProgress | null
  active: boolean
  onSelect?: (stepId: string) => void
}

export type StepFlowNode = Node<StepNodeData, 'step'>

const HANDLE_STYLE = { width: 1, height: 1, minWidth: 0, minHeight: 0, opacity: 0, border: 0 }

export function StepNode({ data }: NodeProps<StepFlowNode>) {
  const { node, progress, active, onSelect } = data
  const isFanOut = !!node.foreach
  const state = progress?.state ?? 'pending'
  const label = progress ? STEP_LABEL[state] : 'Not started'

  return (
    <div
      data-testid="graph-node"
      data-step-id={node.id}
      data-state={progress ? state : 'preview'}
      className="relative"
      style={{ width: NODE_WIDTH, height: NODE_HEIGHT }}
    >
      <Handle type="target" position={Position.Top} style={HANDLE_STYLE} isConnectable={false} />

      {/* The stack behind a fan-out step: two thinner cards, offset, so the
          node reads as several runs of one step rather than one run. */}
      {isFanOut && (
        <>
          <span
            aria-hidden
            className="absolute left-2 top-2 h-full w-full rounded-[var(--radius-control)] border border-hairline bg-surface-1"
          />
          <span
            aria-hidden
            className="absolute left-1 top-1 h-full w-full rounded-[var(--radius-control)] border border-strong bg-surface-2"
          />
        </>
      )}

      <button
        type="button"
        // nodrag/nopan: React Flow otherwise treats a press on the card as
        // the start of a pan gesture and swallows the click.
        className={clsx(
          'nodrag nopan relative flex h-full w-full flex-col justify-center gap-0.5 overflow-hidden rounded-[var(--radius-control)]',
          'border bg-surface-2 pl-3.5 pr-2.5 text-left outline-none shadow-[var(--shadow-card)]',
          'transition-[border-color,background-color] duration-[var(--duration-fast)]',
          'hover:border-[var(--accent-border)] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-[var(--ring)]',
          active ? 'border-accent bg-surface-3' : 'border-strong',
        )}
        aria-label={`${node.id}, ${node.template || 'no template'}, ${label}`}
        aria-pressed={active}
        onClick={() => onSelect?.(node.id)}
      >
        <span
          aria-hidden
          className={clsx('absolute bottom-1.5 left-0 top-1.5 w-[3px] rounded-r-full', STEP_RAIL_CLASS[state])}
        />

        <span className="flex items-baseline gap-2">
          <span className="min-w-0 flex-1 truncate text-[13px] font-medium text-fg-primary">{node.id}</span>
          {progress && progress.attempt > 1 && (
            <span className="shrink-0 rounded-full border border-state-attention/40 px-1.5 text-[10px] font-medium leading-4 tabular-nums text-state-attention">
              Attempt {progress.attempt}
            </span>
          )}
        </span>

        <span className="min-w-0 truncate font-mono text-[11px] text-fg-secondary">{node.template || '—'}</span>

        <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-fg-muted">
          {isFanOut && (
            <span className="shrink-0 font-medium text-fg-secondary" title="Runs once per item of its foreach list">
              Fan-out
            </span>
          )}
          {isFanOut && progress && (
            <span className="shrink-0 font-mono tabular-nums text-fg-secondary">
              {progress.done}/{progress.total}
            </span>
          )}
          {node.worktree === 'shared' && <span className="shrink-0">shared worktree</span>}
          {/* Before a run there is no state worth naming, so a plain step's
              third line stays empty rather than saying "Not started" four
              times over. */}
          {!isFanOut && node.worktree !== 'shared' && progress && (
            <span className="min-w-0 truncate">{label}</span>
          )}
        </span>
      </button>

      <Handle type="source" position={Position.Bottom} style={HANDLE_STYLE} isConnectable={false} />
    </div>
  )
}
