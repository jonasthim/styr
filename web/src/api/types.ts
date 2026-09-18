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

export type Profile = Schemas['Profile']

export type SessionState = Schemas['Session']['state']
export type Origin = Schemas['Session']['origin']

export type Session = Schemas['Session']

export type SessionEvent = Omit<Schemas['Event'], 'payload'> & { payload: unknown }

export type RiskTier = Schemas['Approval']['risk']
export type ApprovalState = Schemas['Approval']['state']

export type Approval = Omit<Schemas['Approval'], 'input' | 'updated_input'> & {
  input: unknown
  updated_input?: unknown
}

export type StatusInfo = Schemas['StatusInfo']

export type Provider = Schemas['Provider']

export type ApiErrorBody = Schemas['ErrorBody']

// ApiToken/ApiTokenCreated (T36, personal API tokens: GET/POST
// /me/api-tokens, DELETE /me/api-tokens/{id}) are hand-written rather than
// derived from Schemas, like Workspace above: schema.d.ts is regenerated
// from docs/openapi.yaml by `npm run gen:api`, a step this card does not
// run (schema.d.ts is a generated file owned by the codegen step, not this
// card's allowed files), so Schemas['APIToken'] does not exist yet. Field
// names match docs/openapi.yaml's APIToken/APITokenCreated schemas exactly,
// so this becomes redundant rather than wrong once codegen catches up.
export interface ApiToken {
  id: string
  name: string
  prefix: string
  created_at: string
  last_used_at: string | null
  expires_at: string | null
}

/** POST /me/api-tokens's response: the same shape as ApiToken (destructure
 * to build one) plus `token`, the raw secret shown once and never returned
 * by any other response. */
export interface ApiTokenCreated {
  id: string
  name: string
  prefix: string
  token: string
}
