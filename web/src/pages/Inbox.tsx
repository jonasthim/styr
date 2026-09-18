// Inbox: two sections, "Needs you" (pending approvals, oldest first) and
// "FYI" (sessions that reached closed/failed in the last 24h). Behaviour
// follows docs/superpowers/plans/2026-09-18-styr-v0.1.md, "### Task 18:
// Inbox page (approvals)" and docs/specs/2026-09-18-styr-design.md,
// "Information architecture" #1. Document title (N) Styr is handled by
// useLiveEvents.ts watching the ['approvals'] cache, which this page feeds
// simply by using q.approvals() through useQuery like everywhere else.
import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import * as Toast from '@radix-ui/react-toast'
import { q } from '../api/queries'
import { api, ApiError } from '../api/client'
import type { Approval, Session } from '../api/types'
import { useUiStore } from '../store/ui'
import { useMe } from '../hooks/useMe'
import { ApprovalCard } from '../components/inbox/ApprovalCard'
import { EmptyInbox } from '../components/inbox/EmptyInbox'
import { FyiRow } from '../components/inbox/FyiRow'
import { snoozeUntil, type SnoozeOption } from '../components/inbox/SnoozeMenu'
import { Badge } from '../components/ui'

const FYI_WINDOW_MS = 24 * 60 * 60 * 1000

// Same key components/onboarding/Onboarding.tsx writes via markWelcomed()
// once a user has reached /welcome and finished or skipped it. Duplicated
// (not imported) because that file is outside this card's own file list.
const WELCOMED_KEY = 'styr.welcomed'

function hasSeenOnboarding(): boolean {
  try {
    return sessionStorage.getItem(WELCOMED_KEY) != null
  } catch {
    // sessionStorage unavailable (private mode, disabled storage): treat as
    // not seen, the same fallback Onboarding.tsx's markWelcomed() takes.
    return false
  }
}

function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable) return true
  return target.tagName === 'INPUT' || target.tagName === 'TEXTAREA'
}

interface DecideVars {
  id: string
  decision: 'allow' | 'deny'
  updated_input?: unknown
}

interface SnoozeVars {
  id: string
  until: string
}

export function Inbox() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const paletteOpen = useUiStore((s) => s.paletteOpen)
  const { data: me } = useMe()

  // First-run onboarding redirect (Task 22's card, out of this file's own
  // scope, only owns what happens once a browser is already on /welcome —
  // this is the "when" half): a user with no Claude token yet who hasn't
  // been through /welcome this browser session gets sent there instead of
  // seeing an empty Inbox. Re-checks whenever `me` changes (e.g. a token
  // reset elsewhere), not just on the very first mount.
  useEffect(() => {
    if (!me || me.claude_token.present || hasSeenOnboarding()) return
    void navigate({ to: '/welcome', replace: true })
  }, [me, navigate])

  const approvalsQuery = useQuery(q.approvals())
  const sessionsQuery = useQuery(q.sessions())
  const workspacesQuery = useQuery(q.workspaces())

  const [focusIndex, setFocusIndex] = useState(0)
  const [collapsingIds, setCollapsingIds] = useState<Set<string>>(new Set())
  const [editingId, setEditingId] = useState<string | null>(null)
  const [snoozeMenuId, setSnoozeMenuId] = useState<string | null>(null)
  const [toastMessage, setToastMessage] = useState<string | null>(null)
  const cardRefs = useRef(new Map<string, HTMLDivElement>())

  const sortedApprovals = useMemo(() => {
    const data = approvalsQuery.data ?? []
    return [...data].sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
  }, [approvalsQuery.data])

  const sessionById = useMemo(() => {
    const map = new Map<string, Session>()
    for (const session of sessionsQuery.data ?? []) map.set(session.id, session)
    return map
  }, [sessionsQuery.data])

  const workspaceNameById = useMemo(() => {
    const map = new Map<string, string>()
    for (const workspace of workspacesQuery.data ?? []) map.set(workspace.id, workspace.name)
    return map
  }, [workspacesQuery.data])

  function workspaceNameForSession(sessionId: string): string {
    const session = sessionById.get(sessionId)
    if (!session) return ''
    return workspaceNameById.get(session.workspace_id) ?? ''
  }

  const fyiSessions = useMemo(() => {
    const now = Date.now()
    return (sessionsQuery.data ?? [])
      .filter((s) => (s.state === 'closed' || s.state === 'failed') && now - new Date(s.last_active_at).getTime() <= FYI_WINDOW_MS)
      .sort((a, b) => new Date(b.last_active_at).getTime() - new Date(a.last_active_at).getTime())
  }, [sessionsQuery.data])

  // Keep focus in range as the list shrinks (a decision resolves, a snooze
  // resolves, or the initial fetch returns fewer rows than the stale index).
  useEffect(() => {
    setFocusIndex((i) => Math.min(i, Math.max(0, sortedApprovals.length - 1)))
  }, [sortedApprovals.length])

  function errorMessage(err: unknown): string {
    return err instanceof ApiError ? err.message : 'Something went wrong. Try again.'
  }

  const decideMutation = useMutation({
    mutationFn: ({ id, decision, updated_input }: DecideVars) =>
      api<void>(`/api/v1/approvals/${id}`, { method: 'POST', json: { decision, updated_input } }),
    onMutate: ({ id }) => {
      setCollapsingIds((prev) => new Set(prev).add(id))
    },
    onSuccess: (_data, { id }) => {
      queryClient.setQueryData<Approval[]>(['approvals'], (prev) => prev?.filter((a) => a.id !== id))
      setEditingId((cur) => (cur === id ? null : cur))
    },
    onError: (err) => {
      setToastMessage(errorMessage(err))
    },
    onSettled: (_data, _err, { id }) => {
      setCollapsingIds((prev) => {
        const next = new Set(prev)
        next.delete(id)
        return next
      })
    },
  })

  const snoozeMutation = useMutation({
    mutationFn: ({ id, until }: SnoozeVars) => api<void>(`/api/v1/approvals/${id}/snooze`, { method: 'POST', json: { until } }),
    onMutate: ({ id }) => {
      setCollapsingIds((prev) => new Set(prev).add(id))
    },
    onSuccess: (_data, { id }) => {
      queryClient.setQueryData<Approval[]>(['approvals'], (prev) => prev?.filter((a) => a.id !== id))
    },
    onError: (err) => {
      setToastMessage(errorMessage(err))
    },
    onSettled: (_data, _err, { id }) => {
      setCollapsingIds((prev) => {
        const next = new Set(prev)
        next.delete(id)
        return next
      })
    },
  })

  function allow(approval: Approval) {
    decideMutation.mutate({ id: approval.id, decision: 'allow' })
  }
  function deny(approval: Approval) {
    decideMutation.mutate({ id: approval.id, decision: 'deny' })
  }
  function submitEdit(approval: Approval, updatedInput: unknown) {
    decideMutation.mutate({ id: approval.id, decision: 'allow', updated_input: updatedInput })
  }
  function snooze(approval: Approval, option: SnoozeOption) {
    setSnoozeMenuId(null)
    snoozeMutation.mutate({ id: approval.id, until: snoozeUntil(option) })
  }
  function openSession(approval: Approval) {
    void navigate({ to: '/sessions/$id', params: { id: approval.session_id } })
  }

  // Page-local keyboard triage: j/k move focus, a allow, d deny, e edit, s
  // snooze menu, o open. Ignored while typing, while the command palette is
  // open, or with a modifier held (those are the global shortcuts' turf).
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (paletteOpen || isTypingTarget(event.target) || event.metaKey || event.ctrlKey || event.altKey) return
      const current = sortedApprovals[focusIndex]

      switch (event.key) {
        case 'j':
          event.preventDefault()
          setFocusIndex((i) => Math.min(sortedApprovals.length - 1, i + 1))
          return
        case 'k':
          event.preventDefault()
          setFocusIndex((i) => Math.max(0, i - 1))
          return
        case 'a':
          if (current) allow(current)
          return
        case 'd':
          if (current) deny(current)
          return
        case 'e':
          if (current) setEditingId(current.id)
          return
        case 's':
          if (current) setSnoozeMenuId(current.id)
          return
        case 'o':
          if (current) openSession(current)
          return
      }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sortedApprovals, focusIndex, paletteOpen])

  useEffect(() => {
    const id = sortedApprovals[focusIndex]?.id
    if (id) cardRefs.current.get(id)?.focus()
  }, [focusIndex, sortedApprovals])

  return (
    <Toast.Provider swipeDirection="right">
      <div className="mx-auto flex w-full max-w-[720px] flex-1 flex-col">
        <header className="flex items-center gap-3 px-4 pb-1 pt-6 min-[640px]:px-6">
          <h1 className="text-[20px] font-semibold leading-7 tracking-[-0.02em] text-fg-primary">Inbox</h1>
          {sortedApprovals.length > 0 && (
            <Badge tone="attention" variant="solid" className="font-mono font-semibold">
              {sortedApprovals.length}
            </Badge>
          )}
        </header>

        <section aria-labelledby="needs-you-heading" className="px-4 pb-2 pt-5 min-[640px]:px-6">
          <div className="mb-3 flex items-center gap-2">
            <h2 id="needs-you-heading" className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">
              Needs you
            </h2>
            <span aria-live="polite" className="font-mono text-[12px] tabular-nums text-fg-secondary">
              {sortedApprovals.length}
            </span>
          </div>

          {sortedApprovals.length === 0 ? (
            <EmptyInbox />
          ) : (
            <ul data-testid="needs-you-list" role="list" className="flex list-none flex-col gap-2">
              {sortedApprovals.map((approval, index) => (
                <li key={approval.id} role="listitem">
                  <ApprovalCard
                    ref={(el) => {
                      if (el) cardRefs.current.set(approval.id, el)
                      else cardRefs.current.delete(approval.id)
                    }}
                    approval={approval}
                    workspaceName={workspaceNameForSession(approval.session_id)}
                    focused={index === focusIndex}
                    collapsing={collapsingIds.has(approval.id)}
                    editing={editingId === approval.id}
                    pending={decideMutation.isPending || snoozeMutation.isPending}
                    snoozeMenuOpen={snoozeMenuId === approval.id}
                    onFocus={() => setFocusIndex(index)}
                    onAllow={() => allow(approval)}
                    onDeny={() => deny(approval)}
                    onStartEdit={() => setEditingId(approval.id)}
                    onCancelEdit={() => setEditingId(null)}
                    onSubmitEdit={(updatedInput) => submitEdit(approval, updatedInput)}
                    onSnoozeOpenChange={(open) => setSnoozeMenuId(open ? approval.id : null)}
                    onSnoozeSelect={(option) => snooze(approval, option)}
                  />
                </li>
              ))}
            </ul>
          )}
        </section>

        <section aria-labelledby="fyi-heading" className="px-4 py-6 min-[640px]:px-6">
          <div className="mb-3 flex items-center gap-2">
            <h2 id="fyi-heading" className="text-[15px] font-semibold tracking-[-0.01em] text-fg-primary">
              FYI
            </h2>
            <span aria-live="polite" className="font-mono text-[12px] tabular-nums text-fg-secondary">
              {fyiSessions.length}
            </span>
          </div>

          {fyiSessions.length === 0 ? (
            <p className="text-[13px] text-fg-secondary">Nothing finished in the last 24 hours.</p>
          ) : (
            <ul data-testid="fyi-list" role="list" className="flex list-none flex-col gap-2">
              {fyiSessions.map((session) => (
                <li key={session.id} role="listitem">
                  <FyiRow session={session} workspaceName={workspaceNameById.get(session.workspace_id) ?? ''} />
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>

      <Toast.Root
        open={toastMessage !== null}
        onOpenChange={(open) => {
          if (!open) setToastMessage(null)
        }}
        duration={5000}
        className="styr-panel fixed bottom-4 right-4 z-50 rounded-[var(--radius-2)] border border-hairline bg-surface-3 px-4 py-3 text-[13px] text-fg-primary shadow-[var(--shadow-popover)]"
      >
        <Toast.Title>{toastMessage}</Toast.Title>
      </Toast.Root>
      <Toast.Viewport />
    </Toast.Provider>
  )
}
