// Small formatting helpers shared by the inbox card and FYI rows.

/** "2m", "1h", "3d" — coarse relative time for card headers. */
export function relativeTime(iso: string): string {
  const diffMs = Date.now() - new Date(iso).getTime()
  const minutes = Math.max(0, Math.round(diffMs / 60_000))
  if (minutes < 1) return 'now'
  if (minutes < 60) return `${minutes}m`
  const hours = Math.round(minutes / 60)
  if (hours < 24) return `${hours}h`
  return `${Math.round(hours / 24)}d`
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

/** A short, one-line mono description of a tool call for the risk row. */
export function summarizeToolInput(tool: string, input: unknown): string {
  if (isRecord(input)) {
    for (const key of ['command', 'file_path', 'path', 'pattern', 'url']) {
      const value = input[key]
      if (typeof value === 'string' && value.length > 0) return `${tool}: ${value}`
    }
  }
  return tool
}
