// A deliberately small YAML reader for the pipeline definitions the mock has
// to understand (docs/superpowers/plans/2026-09-19-styr-v0.5-pipelines.md,
// "YAML definition"): a few scalar keys and a `steps:` list of maps whose
// values are scalars, inline lists or inline maps. It is not a YAML parser -
// the real one is Go's, in T53 - it exists so POST /pipelines/validate in the
// mock can answer with a real graph and real line numbers instead of a canned
// response, which is what makes the editor's preview worth looking at.
import type { PipelineError, PipelineGraph, PipelineValidation } from '../api/types'

/** The plan's limits, repeated here so the mock rejects what the real
 * validator rejects. */
export const MAX_NODES = 20
export const MAX_RETRIES = 3

export interface ParsedStep {
  id: string
  template: string
  needs: string[]
  worktree: 'own' | 'shared'
  foreach: string
  retries: number
  /** 1-based line each key was read from, for error messages. */
  lines: Record<string, number>
}

export interface ParsedPipeline {
  name: string
  workspace: string
  timeout: string
  steps: ParsedStep[]
  /** 1-based line each top-level key was read from. */
  lines: Record<string, number>
}

function unquote(raw: string): string {
  const value = raw.trim()
  if (value.length >= 2 && ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'")))) {
    return value.slice(1, -1)
  }
  return value
}

/** `[a, "b"]` -> ['a', 'b']; anything else -> []. */
function parseList(raw: string): string[] {
  const value = raw.trim()
  if (!value.startsWith('[') || !value.endsWith(']')) return []
  const inner = value.slice(1, -1).trim()
  if (!inner) return []
  return inner.split(',').map(unquote).filter(Boolean)
}

function emptyStep(): ParsedStep {
  return { id: '', template: '', needs: [], worktree: 'own', foreach: '', retries: 0, lines: {} }
}

function assign(step: ParsedStep, key: string, raw: string, line: number): void {
  step.lines[key] = line
  switch (key) {
    case 'id':
      step.id = unquote(raw)
      return
    case 'template':
    case 'template_id':
      step.template = unquote(raw)
      return
    case 'needs':
      step.needs = parseList(raw)
      return
    case 'worktree':
      step.worktree = unquote(raw) === 'shared' ? 'shared' : 'own'
      return
    case 'foreach':
      step.foreach = unquote(raw)
      return
    case 'retries':
      step.retries = Number(unquote(raw)) || 0
      return
    default:
      // `with:` and anything else the executor cares about but the graph
      // does not; the line is recorded, the value ignored.
      return
  }
}

const KEY = /^(\s*)([A-Za-z_][\w-]*):\s*(.*)$/
const ITEM = /^(\s*)-\s*(.*)$/

export function parsePipelineYaml(yaml: string): ParsedPipeline {
  const pipeline: ParsedPipeline = { name: '', workspace: '', timeout: '', steps: [], lines: {} }
  let inSteps = false
  let current: ParsedStep | null = null

  yaml.split('\n').forEach((rawLine, index) => {
    const line = index + 1
    const text = rawLine.replace(/\s+$/, '')
    if (!text.trim() || text.trim().startsWith('#')) return

    const item = ITEM.exec(text)
    if (inSteps && item) {
      current = emptyStep()
      pipeline.steps.push(current)
      const first = KEY.exec(item[2])
      if (first) assign(current, first[2], first[3], line)
      return
    }

    const key = KEY.exec(text)
    if (!key) return
    const [, indent, name, value] = key

    if (indent.length === 0) {
      inSteps = name === 'steps'
      current = null
      pipeline.lines[name] = line
      if (name === 'name') pipeline.name = unquote(value)
      if (name === 'workspace') pipeline.workspace = unquote(value)
      if (name === 'timeout') pipeline.timeout = unquote(value)
      return
    }

    if (current) assign(current, name, value, line)
  })

  return pipeline
}

export function graphOf(parsed: ParsedPipeline): PipelineGraph {
  const ids = new Set(parsed.steps.map((s) => s.id).filter(Boolean))
  return {
    nodes: parsed.steps
      .filter((step) => step.id)
      .map((step) => ({ id: step.id, template: step.template, worktree: step.worktree, foreach: step.foreach })),
    // An edge to a step that does not exist would draw a node the definition
    // never declared, so unknown `needs` are reported as errors and dropped.
    edges: parsed.steps.flatMap((step) =>
      step.needs.filter((need) => ids.has(need) && need !== step.id).map((need) => ({ from: need, to: step.id })),
    ),
  }
}

/** The plan's validation rules: unique ids, `needs` referring to earlier ids,
 * acyclic (which "earlier ids" already guarantees), templates present, and
 * the node and retry limits. */
export function validatePipeline(yaml: string): PipelineValidation {
  const parsed = parsePipelineYaml(yaml)
  const errors: PipelineError[] = []
  const seen = new Set<string>()

  if (!parsed.name.trim()) errors.push({ line: parsed.lines.name ?? 1, message: 'A pipeline needs a name.' })
  if (!parsed.workspace.trim()) {
    errors.push({ line: parsed.lines.workspace ?? 1, message: 'A pipeline needs a workspace.' })
  }
  if (parsed.steps.length === 0) {
    errors.push({ line: parsed.lines.steps ?? 1, message: 'A pipeline needs at least one step.' })
  }
  if (parsed.steps.length > MAX_NODES) {
    errors.push({
      line: parsed.lines.steps ?? 1,
      message: `A pipeline runs at most ${MAX_NODES} steps; this one has ${parsed.steps.length}.`,
    })
  }

  for (const step of parsed.steps) {
    const at = step.lines.id ?? parsed.lines.steps ?? 1
    if (!step.id) {
      errors.push({ line: at, message: 'Every step needs an id.' })
      continue
    }
    if (seen.has(step.id)) {
      errors.push({ line: at, message: `Two steps are called "${step.id}"; ids have to be unique.` })
    }
    if (!step.template.trim()) {
      errors.push({ line: at, message: `Step "${step.id}" does not say which template to run.` })
    }
    if (step.retries > MAX_RETRIES) {
      errors.push({
        line: step.lines.retries ?? at,
        message: `Step "${step.id}" asks for ${step.retries} retries; ${MAX_RETRIES} is the most a step can have.`,
      })
    }
    for (const need of step.needs) {
      if (need === step.id) {
        errors.push({ line: step.lines.needs ?? at, message: `Step "${step.id}" needs itself.` })
      } else if (!seen.has(need)) {
        errors.push({
          line: step.lines.needs ?? at,
          message: `Step "${step.id}" needs "${need}", which is not defined above it.`,
        })
      }
    }
    seen.add(step.id)
  }

  return { ok: errors.length === 0, errors, graph: graphOf(parsed) }
}
