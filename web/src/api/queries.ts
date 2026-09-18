// TanStack Query options factories. useLiveEvents() patches these caches
// directly on SSE frames; components should read through `q`, never call
// api() ad hoc, so query keys stay consistent.
import { queryOptions, type QueryClient } from '@tanstack/react-query'
import { api } from './client'
import type {
  Approval,
  Checkpoint,
  CostStats,
  Delivery,
  DiffSummary,
  FileDiff,
  GanttStats,
  Loop,
  LoopState,
  LoopView,
  Me,
  NotificationChannel,
  Profile,
  Provider,
  ReviewComment,
  RunOutcome,
  RunView,
  Schedule,
  ScheduleFiring,
  Session,
  SessionEvent,
  StatusInfo,
  Template,
  Trigger,
  TriggerKind,
  Workspace,
} from './types'

export const q = {
  me: () => queryOptions({ queryKey: ['me'], queryFn: () => api<Me>('/api/v1/me') }),

  providers: () =>
    queryOptions({ queryKey: ['auth-providers'], queryFn: () => api<Provider[]>('/api/v1/auth/providers') }),

  sessions: () => queryOptions({ queryKey: ['sessions'], queryFn: () => api<Session[]>('/api/v1/sessions') }),

  session: (id: string) =>
    queryOptions({ queryKey: ['session', id], queryFn: () => api<Session>(`/api/v1/sessions/${id}`) }),

  sessionEvents: (id: string, after = 0) =>
    queryOptions({
      queryKey: ['session-events', id],
      queryFn: () => api<SessionEvent[]>(`/api/v1/sessions/${id}/events?after=${after}&limit=500`),
    }),

  approvals: () =>
    queryOptions({
      queryKey: ['approvals'],
      queryFn: () => api<Approval[]>('/api/v1/approvals?state=pending'),
    }),

  workspaces: () => queryOptions({ queryKey: ['workspaces'], queryFn: () => api<Workspace[]>('/api/v1/workspaces') }),

  profiles: () => queryOptions({ queryKey: ['profiles'], queryFn: () => api<Profile[]>('/api/v1/profiles') }),

  status: () => queryOptions({ queryKey: ['status'], queryFn: () => api<StatusInfo>('/api/v1/status') }),

  templates: () => queryOptions({ queryKey: ['templates'], queryFn: () => api<Template[]>('/api/v1/templates') }),

  template: (id: string) =>
    queryOptions({ queryKey: ['template', id], queryFn: () => api<Template>(`/api/v1/templates/${id}`) }),

  triggers: () => queryOptions({ queryKey: ['triggers'], queryFn: () => api<Trigger[]>('/api/v1/triggers') }),

  trigger: (id: string) =>
    queryOptions({ queryKey: ['trigger', id], queryFn: () => api<Trigger>(`/api/v1/triggers/${id}`) }),

  deliveries: (triggerId: string) =>
    queryOptions({
      queryKey: ['deliveries', triggerId],
      queryFn: () => api<Delivery[]>(`/api/v1/triggers/${triggerId}/deliveries?limit=50`),
    }),

  triggerSample: (kind: TriggerKind) =>
    queryOptions({
      queryKey: ['trigger-sample', kind],
      queryFn: () => api<unknown>(`/api/v1/triggers/samples/${kind}`),
      staleTime: Infinity,
    }),

  runs: (filters: { outcome?: RunOutcome | ''; trigger?: string; limit?: number } = {}) => {
    const params = new URLSearchParams()
    if (filters.outcome) params.set('outcome', filters.outcome)
    if (filters.trigger) params.set('trigger', filters.trigger)
    params.set('limit', String(filters.limit ?? 100))
    return queryOptions({
      queryKey: ['runs', filters.outcome ?? '', filters.trigger ?? ''],
      queryFn: () => api<RunView[]>(`/api/v1/runs?${params.toString()}`),
    })
  },

  run: (id: string) =>
    queryOptions({
      queryKey: ['run', id],
      queryFn: () => api<RunView>(`/api/v1/runs/${id}`),
      // A running run has no push channel wired up yet (T33 has no SSE
      // frames for run state) - poll while it's still running so the page
      // notices the moment it finishes.
      refetchInterval: (query) => (query.state.data?.run.outcome === 'running' ? 2000 : false),
    }),

  notifications: () =>
    queryOptions({ queryKey: ['notifications'], queryFn: () => api<NotificationChannel[]>('/api/v1/notifications') }),

  // --- review (T43) --------------------------------------------------------
  // The whole worktree diff for a session. Cheap enough to keep fresh: a turn
  // that edits files moves these numbers, and `session.stats` only patches the
  // totals onto the session row, not the file list.
  sessionDiff: (id: string) =>
    queryOptions({ queryKey: ['session-diff', id], queryFn: () => api<DiffSummary>(`/api/v1/sessions/${id}/diff`) }),

  sessionFileDiff: (id: string, path: string) =>
    queryOptions({
      queryKey: ['session-file-diff', id, path],
      queryFn: () => api<FileDiff>(`/api/v1/sessions/${id}/diff/file?path=${encodeURIComponent(path)}`),
    }),

  sessionComments: (id: string) =>
    queryOptions({
      queryKey: ['session-comments', id],
      queryFn: () => api<ReviewComment[]>(`/api/v1/sessions/${id}/comments`),
    }),

  sessionCheckpoints: (id: string) =>
    queryOptions({
      queryKey: ['session-checkpoints', id],
      queryFn: () => api<Checkpoint[]>(`/api/v1/sessions/${id}/checkpoints`),
    }),

  // --- schedules, loops and stats (T49) ------------------------------------
  schedules: () => queryOptions({ queryKey: ['schedules'], queryFn: () => api<Schedule[]>('/api/v1/schedules') }),

  scheduleFirings: (id: string) =>
    queryOptions({
      queryKey: ['schedule-firings', id],
      queryFn: () => api<ScheduleFiring[]>(`/api/v1/schedules/${id}/firings?limit=50`),
    }),

  loops: (state: LoopState | '' = '') =>
    queryOptions({
      queryKey: ['loops', state],
      queryFn: () => api<Loop[]>(`/api/v1/loops${state ? `?state=${state}` : ''}`),
      // A running loop chains a new run every few minutes and nothing pushes
      // that over SSE yet, so the tab keeps itself honest.
      refetchInterval: 10_000,
    }),

  loop: (id: string) => queryOptions({ queryKey: ['loop', id], queryFn: () => api<LoopView>(`/api/v1/loops/${id}`) }),

  /** The fleet Gantt for the `hours` ending now, refetched on the plan's
   * 30 s cadence. The window bounds are part of the key so switching
   * 1 h/6 h/24 h refetches, but `to` is rounded to the minute: an unrounded
   * one would mint a new key - and a new request - every render. Rounded
   * *up*, not down: GET /stats/gantt only returns sessions with created_at
   * <= to (internal/stats/gantt.go), so a `to` in the past would leave a
   * session started in this minute out of the fleet for up to a minute. */
  gantt: (hours: number) => {
    const to = new Date(Math.ceil(Date.now() / 60_000) * 60_000)
    const from = new Date(to.getTime() - hours * 3_600_000)
    return queryOptions({
      queryKey: ['gantt', hours, to.toISOString()],
      queryFn: () =>
        api<GanttStats>(
          `/api/v1/stats/gantt?from=${encodeURIComponent(from.toISOString())}&to=${encodeURIComponent(to.toISOString())}`,
        ),
      refetchInterval: 30_000,
    })
  },

  costs: (days: number) =>
    queryOptions({ queryKey: ['costs', days], queryFn: () => api<CostStats>(`/api/v1/stats/costs?days=${days}`) }),
}

/** Everything the review surface reads that a review action can change.
 * Send review, commit, rewind and discard all move the diff (and the comment
 * list with it), so they invalidate the lot rather than guessing which. */
export function invalidateReview(client: QueryClient, sessionId: string): void {
  for (const key of ['session-diff', 'session-file-diff', 'session-comments', 'session-checkpoints']) {
    void client.invalidateQueries({ queryKey: [key, sessionId] })
  }
  void client.invalidateQueries({ queryKey: ['session', sessionId] })
}
