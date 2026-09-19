// Two facts the pipelines list needs about a definition it has not sent to
// the validator: what it is called and how many steps it has. Reading them
// off the text keeps the YAML the single source of truth for both (the name
// in the `pipelines` row is a copy of the one in the definition), and keeps
// the list from firing a validate request per row to find out.
const NAME_LINE = /^name:\s*(.+?)\s*$/m
const STEP_LINE = /^\s+-\s+(?:id|template):/gm

function unquote(value: string): string {
  if (value.length >= 2 && ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'")))) {
    return value.slice(1, -1)
  }
  return value
}

export function pipelineName(yaml: string, fallback = ''): string {
  const match = NAME_LINE.exec(yaml)
  return match ? unquote(match[1]) : fallback
}

export function stepCount(yaml: string): number {
  return yaml.match(STEP_LINE)?.length ?? 0
}

/** The definition "New from template" creates: the plan's sequential
 * two-step skeleton, with the second step reading the first one's report.
 *
 * It is built here rather than being a constant because POST /pipelines
 * validates before it stores (internal/pipelines' Executor.CreatePipeline):
 * `workspace` has to be the chosen workspace's own name and every step's
 * `template` has to be one that exists in it, so a hard-coded skeleton
 * naming imaginary templates would be rejected 422 instead of opening an
 * editor. With no template to name yet, the skeleton is the header alone -
 * still valid, and still something to type into.
 */
export function starterYaml(workspaceName: string, templateName?: string): string {
  const header = `name: new-pipeline\nworkspace: ${workspaceName}\ntimeout: 1h\n`
  if (!templateName) return `${header}steps: []\n`
  const template = JSON.stringify(templateName)
  return (
    `${header}steps:\n` +
    `  - id: investigate\n` +
    `    template: ${template}\n` +
    `  - id: act\n` +
    `    needs: [investigate]\n` +
    `    template: ${template}\n` +
    `    with: { plan: "{{ .steps.investigate.report.proposed_action }}" }\n` +
    `    worktree: own\n`
  )
}
