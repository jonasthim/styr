// Message input for a session. Cmd/Ctrl+Enter sends. `/interrupt` and
// `/close` map to the same mutations SessionHeader's Interrupt/Close buttons
// use, rather than being sent as transcript text.
import { useState, type KeyboardEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CornerDownLeft } from 'lucide-react'
import { api } from '../../api/client'
import type { SessionState } from '../../api/types'
import { Button, Kbd, MOD_KEY, Textarea } from '../ui'

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
        <Textarea
          data-testid="composer-input"
          aria-label="Message the session"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={onKeyDown}
          disabled={disabled}
          rows={2}
          placeholder={disabled ? '' : 'Message the session, or /interrupt, /close'}
          className="min-h-[44px] flex-1"
        />
        <Button
          variant="primary"
          data-testid="composer-send"
          onClick={submit}
          disabled={disabled || !text.trim()}
          loading={send.isPending && !reopening}
          iconRight={<CornerDownLeft size={13} aria-hidden />}
          className="h-9"
        >
          Send
        </Button>
      </div>
      {disabled ? (
        <div data-testid="composer-hint" className="mt-2! text-[12px] text-state-attention">
          Answer the permission request first
        </div>
      ) : (
        <div className="mt-2! flex items-center gap-1 text-[11px] text-fg-muted">
          <Kbd>{MOD_KEY}</Kbd>
          <Kbd>↵</Kbd>
          <span className="ml-0.5">to send</span>
        </div>
      )}
    </div>
  )
}
