// A plan arrives as markdown in an ExitPlanMode permission request's input
// (docs/superpowers/plans/2026-09-18-styr-v0.3-review.md, "Plan approval").
// Most of it is prose, but the steps are what someone actually approves, so
// they are lifted out of the markdown and rendered as a checklist instead of
// bullets — the rest is handed to the normal markdown renderer.
export const PLAN_TOOL = 'ExitPlanMode'

export interface PlanStep {
  text: string
  done: boolean
}

export type PlanChunk = { kind: 'prose'; text: string } | { kind: 'steps'; items: PlanStep[] }

const CHECKBOX = /^\s*[-*]\s+\[([ xX])\]\s+(.*)$/

/** The plan markdown carried by an ExitPlanMode request, or null for anything
 * that does not look like one. */
export function planText(input: unknown): string | null {
  if (typeof input !== 'object' || input === null) return null
  const plan = (input as Record<string, unknown>).plan
  return typeof plan === 'string' && plan.trim() !== '' ? plan : null
}

export function parsePlan(markdown: string): PlanChunk[] {
  const chunks: PlanChunk[] = []
  let prose: string[] = []
  let steps: PlanStep[] = []

  function flushProse() {
    const text = prose.join('\n').trim()
    if (text !== '') chunks.push({ kind: 'prose', text })
    prose = []
  }
  function flushSteps() {
    if (steps.length > 0) chunks.push({ kind: 'steps', items: steps })
    steps = []
  }

  for (const line of markdown.split('\n')) {
    const match = CHECKBOX.exec(line)
    if (match) {
      flushProse()
      steps.push({ text: match[2].trim(), done: match[1].toLowerCase() === 'x' })
      continue
    }
    // A blank line inside a checklist keeps the list together; anything else
    // ends it.
    if (steps.length > 0 && line.trim() === '') continue
    flushSteps()
    prose.push(line)
  }
  flushProse()
  flushSteps()
  return chunks
}

/** The plan's first heading or sentence, for a one-line summary. */
export function planTitle(markdown: string): string {
  for (const line of markdown.split('\n')) {
    const text = line.replace(/^#+\s*/, '').trim()
    if (text !== '') return text
  }
  return 'Plan'
}
