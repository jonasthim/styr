// TanStack Query options factories. useLiveEvents() patches these caches
// directly on SSE frames; components should read through `q`, never call
// api() ad hoc, so query keys stay consistent.
import { queryOptions } from '@tanstack/react-query'
import { api } from './client'
import type { Approval, Me, Profile, Provider, Session, SessionEvent, StatusInfo, Workspace } from './types'

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
}
