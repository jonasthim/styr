// A small standard-5-field cron parser for the mock's POST
// /schedules/preview. The real backend uses robfig/cron with a hand-written
// describer (internal/schedules/cron.go); this is the same contract - the
// next five firing times plus one human sentence - computed in the browser,
// and `describeCron` below is a port of that describer's phrasing so the
// mock says what the scheduler would say.
//
// Supported: `*`, `a`, `a-b`, `a,b`, `*/n` and `a-b/n` per field, plus the
// `@hourly`/`@daily`/`@weekly`/`@monthly` descriptors. Day-of-month and
// day-of-week are OR'd when both are restricted, matching cron's own rule.

export interface CronParseResult {
  next: string[]
  description: string
}

const DESCRIPTORS: Record<string, string> = {
  '@hourly': '0 * * * *',
  '@daily': '0 0 * * *',
  '@midnight': '0 0 * * *',
  '@weekly': '0 0 * * 0',
  '@monthly': '0 0 1 * *',
}

const FIELD_RANGES: Array<[number, number]> = [
  [0, 59], // minute
  [0, 23], // hour
  [1, 31], // day of month
  [1, 12], // month
  [0, 6], // day of week
]

// Sunday first: a cron day-of-week field counts 0 (and 7) as Sunday.
const DAY_NAMES = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

/** The set of values one field matches, or null when the field is invalid. */
function parseField(raw: string, min: number, max: number): Set<number> | null {
  const values = new Set<number>()
  for (const part of raw.split(',')) {
    const [range, stepRaw] = part.split('/')
    const step = stepRaw === undefined ? 1 : Number(stepRaw)
    if (!Number.isInteger(step) || step < 1) return null

    let start = min
    let end = max
    if (range !== '*' && range !== '') {
      const bounds = range.split('-')
      if (bounds.length > 2) return null
      start = Number(bounds[0])
      end = bounds.length === 2 ? Number(bounds[1]) : bounds[0] === undefined ? max : start
      // `a/n` (no upper bound) means "from a to the end of the range".
      if (bounds.length === 1 && stepRaw !== undefined) end = max
      if (!Number.isInteger(start) || !Number.isInteger(end)) return null
      if (start < min || end > max || start > end) return null
    } else if (range === '') {
      return null
    }
    for (let v = start; v <= end; v += step) values.add(v)
  }
  return values.size > 0 ? values : null
}

export interface ParsedCron {
  minute: Set<number>
  hour: Set<number>
  dom: Set<number>
  month: Set<number>
  dow: Set<number>
  /** Whether each field was written as a bare `*`, which cron's
   * day-of-month/day-of-week OR rule needs. */
  wildcards: boolean[]
}

export function parseCron(expression: string): ParsedCron | null {
  const normalised = DESCRIPTORS[expression.trim().toLowerCase()] ?? expression
  const fields = normalised.trim().split(/\s+/)
  if (fields.length !== 5) return null
  const sets: Array<Set<number>> = []
  for (let i = 0; i < 5; i += 1) {
    const [min, max] = FIELD_RANGES[i]!
    const parsed = parseField(fields[i]!, min, max)
    if (!parsed) return null
    sets.push(parsed)
  }
  return {
    minute: sets[0]!,
    hour: sets[1]!,
    dom: sets[2]!,
    month: sets[3]!,
    dow: sets[4]!,
    wildcards: fields.map((f) => f === '*'),
  }
}

function matches(cron: ParsedCron, at: Date): boolean {
  if (!cron.minute.has(at.getMinutes())) return false
  if (!cron.hour.has(at.getHours())) return false
  if (!cron.month.has(at.getMonth() + 1)) return false
  const domRestricted = !cron.wildcards[2]
  const dowRestricted = !cron.wildcards[4]
  const domHit = cron.dom.has(at.getDate())
  const dowHit = cron.dow.has(at.getDay())
  // cron's own rule: with both day fields restricted a tick matches when
  // either one does; otherwise the restricted one has to.
  if (domRestricted && dowRestricted) return domHit || dowHit
  if (domRestricted) return domHit
  if (dowRestricted) return dowHit
  return true
}

/** The next `count` times the expression fires after `from`, walking minute
 * by minute. Bounded at four years of ticks so a cron that can never match
 * (31 February) returns what it found instead of spinning. */
export function nextRuns(cron: ParsedCron, from: Date, count = 5): Date[] {
  const out: Date[] = []
  const cursor = new Date(from.getTime())
  cursor.setSeconds(0, 0)
  cursor.setMinutes(cursor.getMinutes() + 1)
  const limit = 4 * 366 * 24 * 60
  for (let i = 0; i < limit && out.length < count; i += 1) {
    if (matches(cron, cursor)) out.push(new Date(cursor.getTime()))
    cursor.setMinutes(cursor.getMinutes() + 1)
  }
  return out
}

function pad2(field: string): string {
  return String(Number(field)).padStart(2, '0')
}

/** The step of a star-slash-N field (`*` `/` `15`), if that is its exact
 * shape. */
function everyN(field: string): number | null {
  const match = /^\*\/(\d+)$/.exec(field)
  if (!match) return null
  const n = Number(match[1])
  return Number.isInteger(n) && n > 0 ? n : null
}

/** A single non-negative integer, with no range, list or step syntax. */
function isPlainNumber(field: string): boolean {
  return /^\d+$/.test(field)
}

const DESCRIPTOR_PHRASES: Record<string, string> = {
  '@hourly': 'every hour',
  '@daily': 'every day at 00:00',
  '@midnight': 'every day at 00:00',
  '@weekly': 'every Sunday at 00:00',
  '@monthly': 'on the 1st of the month at 00:00',
  '@yearly': 'once a year on Jan 1 at 00:00',
  '@annually': 'once a year on Jan 1 at 00:00',
}

/** One sentence for the expression, word for word what internal/schedules/
 * cron.go's describeCron answers: the common forms the Schedules UI cares
 * about, and the raw expression for anything else. Takes the raw text, not
 * the parsed form, because that is what the shape rules read. */
export function describeCron(expression: string): string {
  const trimmed = expression.trim()
  const descriptor = DESCRIPTOR_PHRASES[trimmed.toLowerCase()]
  if (descriptor) return descriptor
  if (trimmed.toLowerCase().startsWith('@every ')) return `every ${trimmed.slice('@every '.length).trim()}`

  const fields = trimmed.split(/\s+/)
  if (fields.length !== 5) return trimmed
  const [minute, hour, dom, month, dow] = fields as [string, string, string, string, string]
  const rest = dom === '*' && month === '*' && dow === '*'

  if (minute === '*' && hour === '*' && rest) return 'every minute'
  if (rest && hour === '*') {
    const step = everyN(minute)
    if (step !== null) return `every ${step} minutes`
    if (isPlainNumber(minute)) return `every hour at :${pad2(minute)}`
  }
  if (rest && isPlainNumber(minute) && isPlainNumber(hour)) return `every day at ${pad2(hour)}:${pad2(minute)}`
  if (dom === '*' && month === '*' && isPlainNumber(minute) && isPlainNumber(hour) && isPlainNumber(dow)) {
    return `every ${DAY_NAMES[Number(dow) % 7]} at ${pad2(hour)}:${pad2(minute)}`
  }
  return trimmed
}

/** The whole POST /schedules/preview answer for one expression, or null
 * when it does not parse - which the handler answers as a 422 with code
 * `invalid_cron`, not as a body field. */
export function previewCron(expression: string, from = new Date()): CronParseResult | null {
  const cron = parseCron(expression)
  if (!cron) return null
  return {
    next: nextRuns(cron, from).map((d) => d.toISOString()),
    description: describeCron(expression),
  }
}
