// Message input for a session. Cmd/Ctrl+Enter sends.
//
// Typing `/` at the start of the input opens the command menu (SlashMenu.tsx):
// Styr's own commands act locally and are never sent, while a command the CLI
// reported for this session is inserted and then sent verbatim — the CLI runs
// custom commands and skills in `-p` mode, so the session executes it. Hidden
// built-ins (GET /status's hidden_commands) never reach the list.
//
// `/interrupt` and `/close` typed by hand still map to the same mutations
// SessionHeader's Interrupt/Close buttons use, rather than being sent as
// transcript text.
import { useEffect, useState, type KeyboardEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CornerDownLeft } from 'lucide-react'
import { q } from '../../api/queries'
import { api } from '../../api/client'
import type { Session } from '../../api/types'
import { Button, Kbd, MOD_KEY, Textarea } from '../ui'
import { NewSessionDialog } from '../sessions/NewSessionDialog'
import { SlashMenu, SLASH_MENU_ID, slashOptionId } from './SlashMenu'
import { SlashHelpDialog } from './SlashHelpDialog'
import { buildSlashCommands, filterSlashCommands, slashQuery, type SlashCommand } from './slashCommands'
import { EFFORT_SELECT_ID, MODEL_SELECT_ID } from './ModelSwitcher'

/** `/model` and `/effort` hand focus to the header's own selects rather than
 * duplicating the switcher in the composer. */
function focusById(id: string) {
  const el = document.getElementById(id)
  if (el instanceof HTMLElement) el.focus()
}

export function Composer({ session }: { session: Session }) {
  const sessionId = session.id
  const [text, setText] = useState('')
  const [menuOpen, setMenuOpen] = useState(false)
  const [activeIndex, setActiveIndex] = useState(0)
  const [helpOpen, setHelpOpen] = useState(false)
  const [newSessionOpen, setNewSessionOpen] = useState(false)
  const queryClient = useQueryClient()
  const status = useQuery(q.status())

  const commands = buildSlashCommands(session.slash_commands ?? [], status.data?.hidden_commands ?? [])
  const query = slashQuery(text)
  const matches = query === null ? [] : filterSlashCommands(commands, query)
  const open = menuOpen && query !== null && matches.length > 0

  // A narrowing filter can leave the highlight past the end of the list.
  useEffect(() => {
    setActiveIndex((i) => (i < matches.length ? i : 0))
  }, [matches.length])

  function invalidateSession() {
    void queryClient.invalidateQueries({ queryKey: ['session', sessionId] })
  }

  const send = useMutation({
    mutationFn: (body: string) =>
      api(`/api/v1/sessions/${sessionId}/messages`, { method: 'POST', json: { text: body } }),
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

  const disabled = session.state === 'waiting'
  const reopening = session.state === 'closed' && send.isPending

  function updateText(next: string) {
    setText(next)
    setMenuOpen(slashQuery(next) !== null)
  }

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
    setMenuOpen(false)
  }

  function runStyrCommand(name: string) {
    setText('')
    setMenuOpen(false)
    switch (name) {
      case 'model':
        focusById(MODEL_SELECT_ID)
        return
      case 'effort':
        focusById(EFFORT_SELECT_ID)
        return
      case 'new':
        setNewSessionOpen(true)
        return
      case 'interrupt':
        interrupt.mutate()
        return
      case 'close':
        close.mutate()
        return
      case 'help':
        setHelpOpen(true)
        return
    }
  }

  function choose(command: SlashCommand) {
    if (command.kind === 'styr') {
      runStyrCommand(command.name)
      return
    }
    // Inserted, not sent: the operator can add arguments before submitting,
    // and the trailing space closes the menu.
    setText(`/${command.name} `)
    setMenuOpen(false)
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (open) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setActiveIndex((i) => (i + 1) % matches.length)
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setActiveIndex((i) => (i - 1 + matches.length) % matches.length)
        return
      }
      if (e.key === 'Escape') {
        e.preventDefault()
        setMenuOpen(false)
        return
      }
      if (e.key === 'Enter' && !e.metaKey && !e.ctrlKey && !e.shiftKey) {
        e.preventDefault()
        choose(matches[activeIndex])
        return
      }
    }
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault()
      submit()
    }
  }

  return (
    <div className="relative border-t border-hairline bg-surface-1 px-4! py-3!">
      {reopening && (
        <div data-testid="composer-reopening" className="mb-2! text-[12px] text-fg-muted">
          Reopening session…
        </div>
      )}
      {open && <SlashMenu items={matches} activeIndex={activeIndex} onSelect={choose} onHover={setActiveIndex} />}
      <div className="flex items-end gap-2">
        <Textarea
          data-testid="composer-input"
          aria-label="Message the session"
          role="combobox"
          aria-expanded={open}
          aria-controls={open ? SLASH_MENU_ID : undefined}
          aria-activedescendant={open ? slashOptionId(activeIndex) : undefined}
          value={text}
          onChange={(e) => updateText(e.target.value)}
          onKeyDown={onKeyDown}
          disabled={disabled}
          rows={2}
          placeholder={disabled ? '' : 'Message the session, or / for commands'}
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
          <span aria-hidden className="mx-1 text-fg-muted">
            ·
          </span>
          <Kbd>/</Kbd>
          <span className="ml-0.5">for commands</span>
        </div>
      )}

      <SlashHelpDialog open={helpOpen} onOpenChange={setHelpOpen} commands={commands} />
      <NewSessionDialog open={newSessionOpen} onOpenChange={setNewSessionOpen} workspaceId={session.workspace_id} />
    </div>
  )
}
