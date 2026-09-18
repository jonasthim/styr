// Turns a FileDiff's hunks into the rows the renderer draws, in the two
// shapes the layout needs: one line per row (unified, below 1100px) and one
// old/new pair per row (side by side, at 1100px and up).
//
// Pure and dependency-free so the pairing rule — the only part of the diff
// renderer with a decision in it — can be unit tested without a DOM.
import type { DiffHunk, DiffLine } from '../../api/types'

export interface HunkRow {
  kind: 'hunk'
  key: string
  label: string
}

export interface LineRow {
  kind: 'line'
  key: string
  line: DiffLine
}

export interface PairRow {
  kind: 'pair'
  key: string
  left: DiffLine | null
  right: DiffLine | null
}

export type UnifiedRow = HunkRow | LineRow
export type SplitRow = HunkRow | PairRow

/** The `@@ -41,7 +41,9 @@` header git writes above a hunk. */
export function hunkLabel(hunk: DiffHunk): string {
  return `@@ -${hunk.old_start},${hunk.old_lines} +${hunk.new_start},${hunk.new_lines} @@`
}

export function totalLines(hunks: DiffHunk[]): number {
  return hunks.reduce((sum, hunk) => sum + hunk.lines.length, 0)
}

export function unifiedRows(hunks: DiffHunk[]): UnifiedRow[] {
  const rows: UnifiedRow[] = []
  hunks.forEach((hunk, h) => {
    rows.push({ kind: 'hunk', key: `h${h}`, label: hunkLabel(hunk) })
    hunk.lines.forEach((line, i) => {
      rows.push({ kind: 'line', key: `h${h}l${i}`, line })
    })
  })
  return rows
}

/** Pairs each run of deletions with the run of additions that follows it, so a
 * rewritten line sits opposite the line it replaced; a run that outlives its
 * partner pairs against a blank half. Context lines pair with themselves. */
export function splitRows(hunks: DiffHunk[]): SplitRow[] {
  const rows: SplitRow[] = []
  hunks.forEach((hunk, h) => {
    rows.push({ kind: 'hunk', key: `h${h}`, label: hunkLabel(hunk) })
    const lines = hunk.lines
    let i = 0
    let seq = 0
    while (i < lines.length) {
      const line = lines[i]
      if (line.type === 'ctx') {
        rows.push({ kind: 'pair', key: `h${h}p${seq++}`, left: line, right: line })
        i += 1
        continue
      }
      const dels: DiffLine[] = []
      while (i < lines.length && lines[i].type === 'del') dels.push(lines[i++])
      const adds: DiffLine[] = []
      while (i < lines.length && lines[i].type === 'add') adds.push(lines[i++])
      const height = Math.max(dels.length, adds.length)
      for (let j = 0; j < height; j += 1) {
        rows.push({ kind: 'pair', key: `h${h}p${seq++}`, left: dels[j] ?? null, right: adds[j] ?? null })
      }
    }
  })
  return rows
}
