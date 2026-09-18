// A small standard-5-field cron parser for the mock's POST
// /schedules/preview. The real backend uses robfig/cron with a hand-written
// describer (plan, "API"); this is the same contract - the next five firing
// times plus one human sentence - computed in the browser so the create
// dialog's preview is exercisable before T50 lands.
//
// Supported: `*`, `a`, `a-b`, `a,b`, `*/n` and `a-b/n` per field, plus the
// `@hourly`/`@daily`/`@weekly`/`@monthly` descriptors. Day-of-month and
// day-of-week are OR'd when both are restricted, matching cron's own rule.

export interface CronParseResult {
  next: string[]
  description: string
  error?: string
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

const DAY_NAMES = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const MONTH_NAMES = [
  'January',
  'February',
  'March',
  'April',
  'May',
  'June',
  'July',
  'August',
  'September',
  'October',
  'November',
  'December',
]

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
  /** Whether each field was written as a bare `*`, which the describer needs. */
  wildcards: boolean[]
  fields: string[]
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
    fields,
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

function pad(n: number): string {
  return String(n).padStart(2, '0')
}

function listPhrase(values: number[], label: (v: number) => string): string {
  const names = values.map(label)
  if (names.length === 1) return names[0]!
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`
}

/** One sentence for the expression, in the shape the plan asks for ("At
 * 07:30, every day"). Falls back to naming the fields it recognises rather
 * than trying to be exhaustive. */
export function describeCron(cron: ParsedCron): string {
  const [minuteField, hourField, domField, monthField, dowField] = cron.fields as [string, string, string, string, string]

  const everyN = /^\*\/(\d+)$/
  const minuteStep = everyN.exec(minuteField)
  if (minuteStep && hourField === '*' && domField === '*' && monthField === '*' && dowField === '*') {
    const n = Number(minuteStep[1])
    return n === 1 ? 'Every minute' : `Every ${n} minutes`
  }
  if (minuteField === '*' && hourField === '*' && domField === '*' && monthField === '*' && dowField === '*') {
    return 'Every minute'
  }

  const minutes = [...cron.minute].sort((a, b) => a - b)
  const hours = [...cron.hour].sort((a, b) => a - b)

  let when: string
  if (hourField === '*' && minutes.length === 1) {
    when = minutes[0] === 0 ? 'On the hour' : `At ${minutes[0]} minutes past every hour`
  } else if (minutes.length === 1 && hours.length <= 3) {
    when = `At ${listPhrase(hours, (h) => `${pad(h)}:${pad(minutes[0]!)}`)}`
  } else {
    when = `At ${listPhrase(minutes, (m) => `${pad(m)} past`)} of ${hourField === '*' ? 'every hour' : listPhrase(hours, (h) => `${pad(h)}:00`)}`
  }

  const parts: string[] = [when]
  if (dowField !== '*') {
    parts.push(`every ${listPhrase([...cron.dow].sort((a, b) => a - b), (d) => DAY_NAMES[d] ?? String(d))}`)
  }
  if (domField !== '*') {
    const days = [...cron.dom].sort((a, b) => a - b)
    parts.push(`on day ${listPhrase(days, String)} of the month`)
  }
  if (monthField !== '*') {
    parts.push(`in ${listPhrase([...cron.month].sort((a, b) => a - b), (m) => MONTH_NAMES[m - 1] ?? String(m))}`)
  }
  // "every day" only earns its place when the time itself is a fixed one -
  // "On the hour, every day" says the same thing twice.
  if (hourField !== '*' && dowField === '*' && domField === '*' && monthField === '*') parts.push('every day')

  return parts.join(', ')
}

/** The whole POST /schedules/preview answer for one expression. */
export function previewCron(expression: string, from = new Date()): CronParseResult {
  const cron = parseCron(expression)
  if (!cron) {
    return { next: [], description: '', error: 'That is not a 5-field cron expression.' }
  }
  return {
    next: nextRuns(cron, from).map((d) => d.toISOString()),
    description: describeCron(cron),
  }
}
