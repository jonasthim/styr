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
