// The command list behind the composer's `/` menu: Styr's own actions plus
// whatever the CLI reported on its last init message, minus the built-ins that
// do not survive headless mode (GET /status's hidden_commands, from
// internal/harness/claude/builtins.go).

export interface SlashCommand {
  /** Command name without the leading slash. */
  name: string
  description: string
  /** styr: handled here, never sent. cli: inserted and sent to the session as-is. */
  kind: 'styr' | 'cli'
}

/** Styr's own commands. `/interrupt` and `/close` are also understood when
 * typed by hand (Composer.tsx has mapped them to their mutations since Task 20). */
export const STYR_COMMANDS: SlashCommand[] = [
  { name: 'model', description: 'Switch the model this session runs on', kind: 'styr' },
  { name: 'effort', description: 'Change how much reasoning effort to spend', kind: 'styr' },
  { name: 'new', description: 'Start another session in this workspace', kind: 'styr' },
  { name: 'interrupt', description: 'Stop the current turn', kind: 'styr' },
  { name: 'close', description: 'End this session', kind: 'styr' },
  { name: 'help', description: 'List every command', kind: 'styr' },
]

/** Styr's commands first, then the session's own, with the hidden built-ins
 * and any CLI command Styr already owns (model, effort) filtered out. */
export function buildSlashCommands(sessionCommands: string[], hidden: string[]): SlashCommand[] {
  const hiddenSet = new Set(hidden)
  const styrNames = new Set(STYR_COMMANDS.map((c) => c.name))
  const cli = sessionCommands
    .filter((name) => !hiddenSet.has(name) && !styrNames.has(name))
    .map<SlashCommand>((name) => ({ name, description: 'Run this command in the session', kind: 'cli' }))
  return [...STYR_COMMANDS, ...cli]
}

/** The `/query` the input currently holds, or null when the menu should be
 * closed: only a value that starts with `/` and carries no whitespace yet. */
export function slashQuery(text: string): string | null {
  if (!text.startsWith('/')) return null
  const rest = text.slice(1)
  if (/\s/.test(rest)) return null
  return rest
}

export function filterSlashCommands(commands: SlashCommand[], query: string): SlashCommand[] {
  if (!query) return commands
  const needle = query.toLowerCase()
  return commands.filter((c) => c.name.toLowerCase().includes(needle))
}
