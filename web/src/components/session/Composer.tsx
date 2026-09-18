// Message input for a session. Cmd/Ctrl+Enter sends. `/interrupt` and
// `/close` map to the same mutations SessionHeader's Interrupt/Close buttons
// use, rather than being sent as transcript text.
import { useState, type KeyboardEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CornerDownLeft } from 'lucide-react'
import { api } from '../../api/client'
import type { SessionState } from '../../api/types'

export function Composer({ sessionId, sessionState }: { sessionId: string; sessionState: SessionState }) {
  const [text, setText] = useState('')
  const queryClient = useQueryClient()

  function invalidateSession() {
    void queryClient.invalidateQueries({ queryKey: ['session', sessionId] })
  }

  const send = useMutation({
    mutationFn: (body: string) => api(`/api/v1/sessions/${sessionId}/messages`, { method: 'POST', json: { text: body } }),
    onSuccess: () => {
      invalidateSession()
      void queryClient.invalidateQueries({ queryKey: ['session-events', sessionId] })
    },
  })
  const interrupt = useMutation({
    mutationFn: () => api(`/api/v1/sessions/${sessionId}/interrupt`, { method: 'POST' }),
    onSuccess: invalidateSession,
  })
  const close = useMutation({
    mutationFn: () => api(`/api/v1/sessions/${sessionId}/close`, { method: 'POST' }),
    onSuccess: invalidateSession,
  })

  const disabled = sessionState === 'waiting'
  const reopening = sessionState === 'closed' && send.isPending

  function submit() {
    const trimmed = text.trim()
    if (!trimmed || disabled) return
    if (trimmed === '/interrupt') {
      interrupt.mutate()
    } else if (trimmed === '/close') {
      close.mutate()
    } else {
      send.mutate(trimmed)
    }
    setText('')
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault()
      submit()
    }
  }

  return (
    <div className="border-t border-hairline bg-surface-1 px-4! py-3!">
      {reopening && (
        <div data-testid="composer-reopening" className="mb-2! text-[12px] text-fg-muted">
          Reopening session…
        </div>
      )}
      <div className="flex items-end gap-2">
        <textarea
          data-testid="composer-input"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKeyDown}
          disabled={disabled}
          rows={2}
          placeholder={disabled ? '' : 'Message the session, or /interrupt, /close'}
          className="min-h-[44px] flex-1 resize-none rounded-[var(--radius-1)] border border-hairline bg-surface-2 px-3! py-2! text-[13px] text-fg-primary placeholder:text-fg-muted focus-visible:border-accent disabled:opacity-50"
        />
        <button
          type="button"
          data-testid="composer-send"
          onClick={submit}
          disabled={disabled || !text.trim()}
          className="flex h-9 shrink-0 items-center gap-1.5 rounded-[var(--radius-1)] bg-accent px-3! text-[13px] font-medium text-[#0b0d10] transition-opacity duration-150 hover:opacity-90 disabled:opacity-40"
        >
          Send
          <CornerDownLeft size={13} />
        </button>
      </div>
      {disabled && (
        <div data-testid="composer-hint" className="mt-1.5! text-[12px] text-state-attention">
          Answer the permission request first
        </div>
      )}
    </div>
  )
}
