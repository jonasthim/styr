// The pipeline graph, on React Flow (@xyflow/react). React Flow supplies the
// canvas, the panning and the edge routing; everything visible is ours -
// ./StepNode for the nodes, tokens for the edges - so the drawing belongs to
// the same design system as the rest of the app rather than arriving with a
// library's default look.
import { useCallback, useMemo, useEffect, type CSSProperties } from 'react'
import { ReactFlow, useReactFlow, useStore, type Edge, type NodeTypes } from '@xyflow/react'
import '@xyflow/react/dist/base.css'
import clsx from 'clsx'
import type { PipelineGraph as Graph, StepRun } from '../../api/types'
import { layoutGraph } from './graphLayout'
import { StepNode, type StepFlowNode } from './StepNode'
import { nodeProgress, type NodeProgress } from './stepState'

const NODE_TYPES: NodeTypes = { step: StepNode }

// React Flow's own colours come from these variables; pointing them at the
// design tokens is what keeps the canvas inside the theme, in both themes.
const FLOW_VARS = {
  '--xy-edge-stroke': 'var(--fg-muted)',
  '--xy-edge-stroke-selected': 'var(--accent)',
  '--xy-background-color': 'transparent',
  '--xy-attribution-background-color': 'transparent',
} as CSSProperties

/** Refits the view whenever the shape of the graph changes or the canvas is
 * resized - the editor redraws on every keystroke, and opening the step panel
 * takes a third of the width away. A graph that drifts off-canvas as a step
 * is added is worse than no preview. Lives inside <ReactFlow> so it can reach
 * the instance without a provider of its own. */
function FitOnChange({ signature }: { signature: string }) {
  const flow = useReactFlow()
  const width = useStore((state) => Math.round(state.width))
  useEffect(() => {
    const timer = setTimeout(() => void flow.fitView({ padding: 0.14, duration: 0 }), 0)
    return () => clearTimeout(timer)
  }, [flow, signature, width])
  return null
}

export function PipelineGraph({
  graph,
  steps,
  activeStepId,
  onSelect,
  className,
  height,
}: {
  graph: Graph
  /** The run's step runs; omitted in the editor, where nothing has run yet. */
  steps?: StepRun[]
  activeStepId?: string | null
  onSelect?: (stepId: string) => void
  className?: string
  height: number
}) {
  const positions = useMemo(() => layoutGraph(graph), [graph])

  // React Flow is a controlled component: handing it a new `nodes` array
  // makes it re-measure every node, which is visible as a flicker on a page
  // that re-renders on a timer (the run page's elapsed clock) or on a poll.
  // So the array is rebuilt only when something in it actually changed -
  // keyed on the states and attempts, not on the identity of the step rows a
  // refetch hands back.
  const stepsKey = (steps ?? []).map((s) => `${s.id}:${s.state}:${s.attempt}`).join('|')
  const progress = useMemo(() => {
    const map = new Map<string, NodeProgress | null>()
    for (const node of graph.nodes) map.set(node.id, steps ? nodeProgress(node.id, steps) : null)
    return map
    // eslint-disable-next-line react-hooks/exhaustive-deps -- stepsKey stands in for `steps`
  }, [graph, stepsKey])

  const nodes = useMemo<StepFlowNode[]>(
    () =>
      graph.nodes.map((node) => ({
        id: node.id,
        type: 'step' as const,
        position: positions.get(node.id) ?? { x: 0, y: 0 },
        draggable: false,
        // Selectable only so React Flow leaves the node's own pointer events
        // alone: a node it considers inert gets `pointer-events: none` and
        // the card's button never sees the click. Nothing renders off the
        // selection - the panel is driven by `activeStepId`.
        selectable: true,
        data: {
          node,
          progress: progress.get(node.id) ?? null,
          active: activeStepId === node.id,
          onSelect,
        },
      })),
    [graph, positions, progress, activeStepId, onSelect],
  )

  const edges = useMemo<Edge[]>(
    () =>
      graph.edges.map((edge) => {
        // A dependency that has been satisfied is drawn in the accent: the
        // completed spine of a running pipeline is then readable without
        // looking at a single node.
        const done = progress.get(edge.from)?.state === 'success'
        return {
          id: `${edge.from}->${edge.to}`,
          source: edge.from,
          target: edge.to,
          type: 'smoothstep',
          style: { stroke: done ? 'var(--accent)' : 'var(--fg-muted)', strokeWidth: 1.25 },
        }
      }),
    [graph, progress],
  )

  const signature = useMemo(
    () => `${graph.nodes.map((n) => n.id).join(',')}|${graph.edges.map((e) => `${e.from}>${e.to}`).join(',')}`,
    [graph],
  )

  const noop = useCallback(() => undefined, [])

  return (
    <div
      data-testid="pipeline-graph"
      style={{ ...FLOW_VARS, height }}
      className={clsx(
        'w-full overflow-hidden rounded-[var(--radius-panel)] border border-hairline bg-canvas',
        className,
      )}
    >
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={NODE_TYPES}
        onNodesChange={noop}
        fitView
        fitViewOptions={{ padding: 0.14 }}
        minZoom={0.4}
        maxZoom={1.4}
        nodesDraggable={false}
        nodesConnectable={false}
        nodesFocusable={false}
        edgesFocusable={false}
        zoomOnScroll={false}
        zoomOnDoubleClick={false}
        panOnScroll={false}
        // Leaves the wheel to the page: on a phone the graph sits in the
        // middle of a scrolling column, and a canvas that eats the scroll
        // traps the reader in it.
        preventScrolling={false}
        aria-label="Pipeline graph"
      >
        <FitOnChange signature={signature} />
      </ReactFlow>
    </div>
  )
}
