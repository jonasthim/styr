// React Flow places nodes where it is told to, so a DAG needs a layout of
// its own. This is the smallest one that reads correctly for a pipeline:
// longest-path layering (a step sits one row below its deepest dependency),
// declaration order within a row, rows centred on each other. Steps are
// written top to bottom in the YAML, so the graph runs top to bottom too.
import type { PipelineGraph } from '../../api/types'

export const NODE_WIDTH = 190
export const NODE_HEIGHT = 78
const GAP_X = 28
const GAP_Y = 56

export interface NodePosition {
  x: number
  y: number
}

/** Row index per node: 0 for a step with no dependencies, otherwise one more
 * than its deepest dependency. Capped by the node count so a definition that
 * slipped past validation with a cycle still terminates. */
function depths(graph: PipelineGraph): Map<string, number> {
  const parents = new Map<string, string[]>()
  for (const node of graph.nodes) parents.set(node.id, [])
  for (const edge of graph.edges) parents.get(edge.to)?.push(edge.from)

  const depth = new Map<string, number>()
  for (const node of graph.nodes) depth.set(node.id, 0)
  for (let pass = 0; pass < graph.nodes.length; pass += 1) {
    let moved = false
    for (const node of graph.nodes) {
      const own = Math.max(0, ...(parents.get(node.id) ?? []).map((p) => (depth.get(p) ?? 0) + 1))
      if (own > (depth.get(node.id) ?? 0)) {
        depth.set(node.id, own)
        moved = true
      }
    }
    if (!moved) break
  }
  return depth
}

export function layoutGraph(graph: PipelineGraph): Map<string, NodePosition> {
  const depth = depths(graph)
  const rows = new Map<number, string[]>()
  for (const node of graph.nodes) {
    const row = depth.get(node.id) ?? 0
    rows.set(row, [...(rows.get(row) ?? []), node.id])
  }

  const positions = new Map<string, NodePosition>()
  const widest = Math.max(1, ...[...rows.values()].map((ids) => ids.length))
  const canvasWidth = widest * NODE_WIDTH + (widest - 1) * GAP_X

  for (const [row, ids] of rows) {
    const rowWidth = ids.length * NODE_WIDTH + (ids.length - 1) * GAP_X
    const left = (canvasWidth - rowWidth) / 2
    ids.forEach((id, index) => {
      positions.set(id, {
        x: left + index * (NODE_WIDTH + GAP_X),
        y: row * (NODE_HEIGHT + GAP_Y),
      })
    })
  }
  return positions
}
