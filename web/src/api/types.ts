// Hand-written API types matching the Task 13 route table
// (docs/superpowers/plans/2026-09-18-styr-v0.1.md, "### Task 13: HTTP API").
// T13's OpenAPI spec later replaces this with generated schema.d.ts.

export type Role = 'admin' | 'member'

export interface ClaudeTokenInfo {
  present: boolean
  label: string
  verified_at: string | null
}

export interface Me {
  id: string
  email: string
  display_name: string
  avatar_url: string
  role: Role
  prefs: Record<string, unknown>
  claude_token: ClaudeTokenInfo
}

export interface User {
  id: string
  email: string
  display_name: string
  avatar_url: string
  role: Role
  created_at: string
  last_login_at: string
}

export interface Workspace {
  id: string
  name: string
  path: string
  default_profile_id: string
  worktrees: boolean
  created_at: string
}

export type ProfileMode = 'default' | 'acceptEdits' | 'plan' | 'dontAsk' | 'auto'

export interface Profile {
  id: string
  name: string
  mode: ProfileMode
  allowed_tools: string[]
  disallowed_tools: string[]
  max_turns: number
  unattended: boolean
  approval_timeout: number // seconds
  builtin: boolean
}

export type SessionState = 'open' | 'running' | 'waiting' | 'closed' | 'failed'
export type Origin = 'ui' | 'webhook' | 'schedule' | 'pipeline'

export interface Session {
  id: string
  owner_id: string | null
  title: string
  workspace_id: string
  profile_id: string
  harness: string
  state: SessionState
  origin: Origin
  origin_ref: string
  worktree: string
  created_at: string
  last_active_at: string
  num_turns: number
  cost_usd: number
  tokens_in: number
  tokens_out: number
  now_line: string
  model: string
}

export interface SessionEvent {
  id: number
  session_id: string
  seq: number
  at: string
  type: string
  payload: unknown
}

export type RiskTier = 'read' | 'write' | 'exec' | 'destructive'
export type ApprovalState = 'pending' | 'allowed' | 'denied' | 'expired'

export interface Approval {
  id: string
  session_id: string
  request_id: string
  tool: string
  input: unknown
  risk: RiskTier
  state: ApprovalState
  created_at: string
  decided_by: string | null
  decided_at: string | null
  snoozed_until: string | null
  updated_input: unknown | null
  message: string
  // GET /api/v1/approvals augments each row for display.
  session_title: string
  now_line: string
}

export interface StatusInfo {
  version: string
  claude_version: string
  open_processes: number
  slots: number
  queue_depth: number
}

export interface Provider {
  name: string
  slug: string
}

export interface ApiErrorBody {
  error: {
    code: string
    message: string
  }
}
