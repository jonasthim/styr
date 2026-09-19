// Shared vocabulary for the two agentic CLIs Styr drives (docs/HARNESSES.md).
// The kinds themselves come from the generated schema, so this file only holds
// what the API cannot say: how to name each one to a person, and the one
// behavioural difference the UI has to act on.
import type { HarnessKind } from '../api/types'

export const HARNESS_LABEL: Record<HarnessKind, string> = {
  claude: 'Claude Code',
  codex: 'Codex',
}

/** The display name for a harness, falling back to the raw value for a kind a
 * newer server knows and this build does not. */
export function harnessLabel(kind: string): string {
  return HARNESS_LABEL[kind as HarnessKind] ?? kind
}

/** Whether this harness asks the operator before each tool use. Codex decides
 * tool use with the sandbox policy its process started under and never asks,
 * so its sessions have no approvals to show, answer or wait for. */
export function hasApprovals(kind: string): boolean {
  return kind !== 'codex'
}

/** The one-line explanation shown wherever Codex is chosen. */
export const CODEX_SANDBOX_NOTE =
  'Codex applies a sandbox policy instead of per-command approvals; read-only for investigate.'
