// API types derived from the generated OpenAPI schema (docs/openapi.yaml ->
// `npm run gen:api` -> schema.d.ts), so these can't drift from the spec the
// backend handlers are checked against (internal/api/*_handlers.go). Only
// re-exports/aliases live here - never hand-written field lists - plus a
// handful of field-level overrides for schema properties OpenAPI can only
// describe as a bare "object": those come back from openapi-typescript as
// `Record<string, never>` (an empty object), which is unusable for the
// actual free-form JSON these fields carry (session/user preferences, a
// tool call's input, an event's decoded payload).
import type { components } from './schema'

type Schemas = components['schemas']

export type Role = Schemas['User']['role']

export type ClaudeTokenInfo = Schemas['ClaudeToken']

export type Me = Omit<Schemas['Me'], 'prefs'> & { prefs: Record<string, unknown> }

export type User = Omit<Schemas['User'], 'prefs'> & { prefs: Record<string, unknown> }

// Workspace overrides the generated schema outright rather than narrowing it:
// the backend card implementing the per-user workspaces contract (T29) hasn't
// regenerated schema.d.ts yet, so Schemas['Workspace'] still describes the
// old admin-registered-path shape. Field names match the contract exactly so
// this becomes a no-op once codegen catches up.
export type WorkspaceSource = 'git' | 'path' | 'empty'
export type WorkspaceState = 'cloning' | 'ready' | 'failed'

export type Workspace = Omit<Schemas['Workspace'], 'path' | 'error' | 'repo_url' | 'branch'> & {
  owner_id: string | null
  path: string
  source: WorkspaceSource
  repo_url: string | null
  branch: string | null
  managed: boolean
  state: WorkspaceState
  error: string | null
  worktrees: boolean
  created_at: string
  updated_at: string
}

export type ProfileMode = Schemas['Profile']['mode']

// Effort, Profile, Session and StatusInfo below intersect the generated
// schema with the model/effort/slash-command contract (docs/openapi.yaml, card
// T38). schema.d.ts is regenerated in the integration wave; until then these
// intersections carry the fields the backend already returns, the same way
// Workspace above carries T29's. Field names match the spec exactly, so each
// intersection becomes a no-op once codegen catches up.

/** Reasoning effort levels; '' means the CLI's own default. */
export type Effort = '' | 'low' | 'medium' | 'high' | 'xhigh' | 'max'

/** One entry of GET /status's model list: the alias the CLI takes, and its label. */
export interface ModelOption {
  alias: string
  label: string
}

export type Profile = Schemas['Profile'] & {
  model: string
  effort: Effort
}

export type SessionState = Schemas['Session']['state']
export type Origin = Schemas['Session']['origin']

export type Session = Schemas['Session'] & {
  effort: Effort
  /** What the CLI reported on its last init message, without the leading slash. */
  slash_commands: string[]
}

export type SessionEvent = Omit<Schemas['Event'], 'payload'> & { payload: unknown }

export type RiskTier = Schemas['Approval']['risk']
export type ApprovalState = Schemas['Approval']['state']

export type Approval = Omit<Schemas['Approval'], 'input' | 'updated_input'> & {
  input: unknown
  updated_input?: unknown
}

export type StatusInfo = Schemas['StatusInfo'] & {
  models: ModelOption[]
  efforts: Exclude<Effort, ''>[]
  /** CLI built-ins the composer's slash menu must hide (internal/harness/claude/builtins.go). */
  hidden_commands: string[]
}

export type Provider = Schemas['Provider']

export type ApiErrorBody = Schemas['ErrorBody']
