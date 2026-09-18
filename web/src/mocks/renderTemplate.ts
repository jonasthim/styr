// A small subset of Go's text/template, just enough to preview the seeded
// Grafana template (and anything else built from the same primitives) in the
// mock: {{ .a.b }} interpolation with missingkey=zero (renders ''),
// {{ range .list }}...{{ end }} (one level, no nesting) and
// {{ default "x" .a }}. The real renderer (internal/templates, T32) is Go
// text/template itself; this only has to be plausible enough for the
// Preview card's "Render with sample" to feel real against mock data.
function getPath(obj: unknown, path: string): unknown {
  return path.split('.').reduce<unknown>((acc, key) => {
    if (acc && typeof acc === 'object') return (acc as Record<string, unknown>)[key]
    return undefined
  }, obj)
}

function stringify(value: unknown): string {
  if (value === undefined || value === null) return ''
  if (typeof value === 'string') return value
  if (typeof value === 'number' || typeof value === 'boolean') return String(value)
  return JSON.stringify(value)
}

const RANGE_RE = /\{\{\s*range\s+\.([\w.]+)\s*\}\}([\s\S]*?)\{\{\s*end\s*\}\}/g
const DEFAULT_RE = /\{\{\s*default\s+"([^"]*)"\s+\.([\w.]+)\s*\}\}/g
const VAR_RE = /\{\{\s*\.([\w.]+)\s*\}\}/g

function renderScalarPass(tpl: string, ctx: unknown): string {
  let out = tpl.replace(DEFAULT_RE, (_m, def: string, path: string) => {
    const value = getPath(ctx, path)
    const s = stringify(value)
    return s === '' ? def : s
  })
  out = out.replace(VAR_RE, (_m, path: string) => stringify(getPath(ctx, path)))
  return out
}

export function renderMiniTemplate(tpl: string, ctx: Record<string, unknown>): string {
  const withRanges = tpl.replace(RANGE_RE, (_match, path: string, inner: string) => {
    const list = getPath(ctx, path)
    if (!Array.isArray(list)) return ''
    return list.map((item) => renderScalarPass(inner, item)).join('')
  })
  return renderScalarPass(withRanges, ctx)
}
