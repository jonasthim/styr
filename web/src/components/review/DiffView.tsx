// The diff for one file, hand-rolled over the FileDiff hunks the API returns
// (docs/superpowers/plans/2026-09-18-styr-v0.3-review.md, "API"): side by side
// from 1100px, unified below it, line numbers on every row, and a comment box
// behind every line number.
//
// No diff library. The server already did the hard part — git produced the
// hunks — so all that is left is pairing and painting, and diffRows.ts keeps
// the one decision in that (how a deletion pairs with the addition that
// replaced it) in a tested pure function.
//
// Long lines soft-wrap rather than scrolling the file sideways: a review that
// has to be read on a phone cannot afford a horizontal scrollbar, and wrapping
// keeps the line numbers in the gutter next to the line they belong to.
import { useMemo, useRef, useState, type ReactNode } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useVirtualizer } from '@tanstack/react-virtual'
import { FileWarning, Trash2 } from 'lucide-react'
import clsx from 'clsx'
import { q } from '../../api/queries'
import { api } from '../../api/client'
import { useMe } from '../../hooks/useMe'
import { useMediaQuery } from '../../hooks/useMediaQuery'
import type { DiffLine, FileDiff, ReviewComment } from '../../api/types'
import { Button, Textarea } from '../ui'
import { relativeTime } from '../inbox/format'
import { splitRows, totalLines, unifiedRows, type PairRow, type SplitRow, type UnifiedRow } from './diffRows'
import { langFor, tokenize, type Token } from './tokenize'
import { StatusChip } from './fileMeta'

/** Above this many diff lines the rows are virtualised; below it the browser
 * handles a few hundred rows faster than a measured virtual list does. */
const VIRTUALIZE_ABOVE = 1500

const SPLIT_QUERY = '(min-width: 1100px)'

const TOKEN_CLASS: Record<Token['kind'], string> = {
  plain: '',
  keyword: 'text-[var(--code-keyword)]',
  string: 'text-[var(--code-string)]',
  number: 'text-[var(--code-number)]',
  comment: 'text-[var(--code-comment)] italic',
}

const ROW_BG: Record<DiffLine['type'], string> = {
  ctx: '',
  add: 'bg-[var(--diff-add-bg)]',
  del: 'bg-[var(--diff-del-bg)]',
}

const GUTTER_BG: Record<DiffLine['type'], string> = {
  ctx: '',
  add: 'bg-[var(--diff-add-gutter)]',
  del: 'bg-[var(--diff-del-gutter)]',
}

const SIGN: Record<DiffLine['type'], string> = { ctx: ' ', add: '+', del: '−' }

type Side = 'old' | 'new'

interface Anchor {
  side: Side
  line: number
}

function anchorKey(side: Side, line: number): string {
  return `${side}:${line}`
}

function Code({ text, lang }: { text: string; lang: ReturnType<typeof langFor> }) {
  const tokens = useMemo(() => tokenize(text, lang), [text, lang])
  return (
    <code className="block whitespace-pre-wrap break-words px-2 text-fg-primary">
      {tokens.map((token, i) => (
        <span key={i} className={TOKEN_CLASS[token.kind]}>
          {token.text}
        </span>
      ))}
      {text === '' && ' '}
    </code>
  )
}

function LineNo({
  no,
  side,
  type,
  onComment,
}: {
  no: number | null
  side: Side
  type: DiffLine['type']
  onComment: (anchor: Anchor) => void
}) {
  if (no === null) {
    return <span aria-hidden className={clsx('block h-full w-10 shrink-0', GUTTER_BG[type])} />
  }
  return (
    <button
      type="button"
      aria-label={`Comment on ${side} line ${no}`}
      onClick={() => onComment({ side, line: no })}
      className={clsx(
        'group block h-full w-10 shrink-0 select-none px-1 text-right text-[11px] tabular-nums',
        'text-[var(--diff-gutter-fg)] outline-none',
        'hover:bg-accent-subtle hover:text-accent focus-visible:bg-accent-subtle focus-visible:text-accent',
        GUTTER_BG[type],
      )}
    >
      {no}
    </button>
  )
}

function CommentList({
  comments,
  meId,
  onDelete,
  deleting,
}: {
  comments: ReviewComment[]
  meId: string | undefined
  onDelete: (id: string) => void
  deleting: string | null
}) {
  return (
    <>
      {comments.map((comment) => (
        <div
          key={comment.id}
          data-testid="review-comment"
          className="flex items-start gap-2 border-l-2 border-accent bg-surface-2 px-3 py-2"
        >
          <div className="min-w-0 flex-1 font-sans">
            <div className="flex items-baseline gap-2 text-[11px] text-fg-secondary">
              <span className="font-medium text-fg-primary">{comment.author_name || comment.author_id}</span>
              <span className="tabular-nums">{relativeTime(comment.created_at)}</span>
              {comment.sent_at && <span className="text-fg-muted">sent</span>}
            </div>
            <p className="mt-0.5 whitespace-pre-wrap text-[13px] leading-5 text-fg-primary">{comment.body}</p>
          </div>
          {comment.author_id === meId && !comment.sent_at && (
            <Button
              size="sm"
              variant="ghost"
              aria-label="Delete comment"
              loading={deleting === comment.id}
              onClick={() => onDelete(comment.id)}
              icon={<Trash2 size={12} aria-hidden />}
              className="w-7 px-0"
            />
          )}
        </div>
      ))}
    </>
  )
}

function DraftBox({
  anchor,
  pending,
  onCancel,
  onSubmit,
}: {
  anchor: Anchor
  pending: boolean
  onCancel: () => void
  onSubmit: (body: string) => void
}) {
  const [body, setBody] = useState('')
  return (
    <div className="border-l-2 border-accent bg-surface-2 px-3 py-2 font-sans">
      <Textarea
        data-testid="comment-draft"
        autoFocus
        aria-label={`Comment on ${anchor.side} line ${anchor.line}`}
        placeholder="What should change here?"
        rows={2}
        value={body}
        onChange={(e) => setBody(e.target.value)}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && body.trim()) onSubmit(body.trim())
          if (e.key === 'Escape') onCancel()
        }}
      />
      <div className="mt-2 flex items-center gap-2">
        <Button variant="primary" size="sm" disabled={!body.trim()} loading={pending} onClick={() => onSubmit(body.trim())}>
          Add comment
        </Button>
        <Button size="sm" onClick={onCancel}>
          Cancel
        </Button>
      </div>
    </div>
  )
}

export function DiffView({ sessionId, path }: { sessionId: string; path: string }) {
  const queryClient = useQueryClient()
  const { data: me } = useMe()
  const split = useMediaQuery(SPLIT_QUERY)
  const fileQuery = useQuery(q.sessionFileDiff(sessionId, path))
  const commentsQuery = useQuery(q.sessionComments(sessionId))
  const [draft, setDraft] = useState<Anchor | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)

  const file: FileDiff | undefined = fileQuery.data
  const lang = useMemo(() => langFor(path), [path])

  const rows = useMemo<Array<UnifiedRow | SplitRow>>(() => {
    if (!file || file.binary) return []
    return split ? splitRows(file.hunks) : unifiedRows(file.hunks)
  }, [file, split])

  const byAnchor = useMemo(() => {
    const map = new Map<string, ReviewComment[]>()
    for (const comment of commentsQuery.data ?? []) {
      if (comment.path !== path) continue
      const key = anchorKey(comment.side, comment.line)
      const bucket = map.get(key)
      if (bucket) bucket.push(comment)
      else map.set(key, [comment])
    }
    return map
  }, [commentsQuery.data, path])

  const addComment = useMutation({
    mutationFn: (input: Anchor & { body: string }) =>
      api<ReviewComment>(`/api/v1/sessions/${sessionId}/comments`, {
        method: 'POST',
        json: { path, line: input.line, side: input.side, body: input.body },
      }),
    onSuccess: () => {
      setDraft(null)
      void queryClient.invalidateQueries({ queryKey: ['session-comments', sessionId] })
    },
  })

  const deleteComment = useMutation({
    mutationFn: (id: string) => api<void>(`/api/v1/sessions/${sessionId}/comments/${id}`, { method: 'DELETE' }),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ['session-comments', sessionId] }),
  })

  const virtualize = rows.length > VIRTUALIZE_ABOVE
  const virtualizer = useVirtualizer({
    count: virtualize ? rows.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 20,
    overscan: 24,
  })

  function commentsFor(anchors: Anchor[]): ReviewComment[] {
    return anchors.flatMap((a) => byAnchor.get(anchorKey(a.side, a.line)) ?? [])
  }

  function attachments(anchors: Anchor[]) {
    const comments = commentsFor(anchors)
    const open = draft && anchors.some((a) => a.side === draft.side && a.line === draft.line) ? draft : null
    if (comments.length === 0 && !open) return null
    return (
      <div className="border-y border-hairline">
        <CommentList comments={comments} meId={me?.id} onDelete={(id) => deleteComment.mutate(id)} deleting={deleteComment.isPending ? (deleteComment.variables ?? null) : null} />
        {open && (
          <DraftBox
            anchor={open}
            pending={addComment.isPending}
            onCancel={() => setDraft(null)}
            onSubmit={(body) => addComment.mutate({ ...open, body })}
          />
        )}
      </div>
    )
  }

  function anchorsForLine(line: DiffLine): Anchor[] {
    const out: Anchor[] = []
    if (line.old_no !== null) out.push({ side: 'old', line: line.old_no })
    if (line.new_no !== null) out.push({ side: 'new', line: line.new_no })
    return out
  }

  function renderRow(row: UnifiedRow | SplitRow) {
    if (row.kind === 'hunk') {
      return (
        <div
          data-testid="diff-hunk"
          className="bg-[var(--diff-hunk-bg)] px-3 py-1 text-[11px] text-fg-secondary"
        >
          {row.label}
        </div>
      )
    }
    if (row.kind === 'line') {
      const { line } = row
      return (
        <div>
          <div className={clsx('flex items-stretch', ROW_BG[line.type])}>
            <LineNo no={line.old_no} side="old" type={line.type} onComment={setDraft} />
            <LineNo no={line.new_no} side="new" type={line.type} onComment={setDraft} />
            <span aria-hidden className={clsx('w-4 shrink-0 select-none text-center', GUTTER_BG[line.type])}>
              {SIGN[line.type]}
            </span>
            <Code text={line.text} lang={lang} />
          </div>
          {attachments(anchorsForLine(line))}
        </div>
      )
    }
    const pair = row as PairRow
    const anchors: Anchor[] = []
    if (pair.left?.old_no != null) anchors.push({ side: 'old', line: pair.left.old_no })
    if (pair.right?.new_no != null) anchors.push({ side: 'new', line: pair.right.new_no })
    return (
      <div>
        <div className="grid grid-cols-2 items-stretch">
          <Half line={pair.left} side="old" lang={lang} onComment={setDraft} className="border-r border-hairline" />
          <Half line={pair.right} side="new" lang={lang} onComment={setDraft} />
        </div>
        {attachments(anchors)}
      </div>
    )
  }

  if (fileQuery.isLoading) {
    return <div className="p-4 text-[13px] text-fg-secondary">Loading the diff…</div>
  }
  if (!file) {
    return <div className="p-4 text-[13px] text-fg-secondary">That file is not part of this diff.</div>
  }

  return (
    <div
      data-testid="diff-view"
      data-mode={split ? 'split' : 'unified'}
      className="flex min-h-0 flex-1 flex-col bg-canvas"
      aria-label={`Diff for ${path}`}
    >
      <div className="flex items-center gap-2 border-b border-hairline bg-surface-1 px-3 py-2">
        <StatusChip status={file.status} />
        <span className="min-w-0 flex-1 truncate font-mono text-[12px] text-fg-primary">
          {file.old_path && file.old_path !== file.path ? `${file.old_path} → ${file.path}` : file.path}
        </span>
        <span className="shrink-0 font-mono text-[11px] tabular-nums text-fg-secondary">
          {totalLines(file.hunks)} lines
        </span>
      </div>

      {file.binary ? (
        <Notice icon>Binary file — nothing to show. Download the patch to inspect it.</Notice>
      ) : (
        <>
          {file.truncated && <Notice icon>This diff was cut off at 2 MB. Download the patch for the rest.</Notice>}
          <div
            ref={scrollRef}
            className="min-h-0 flex-1 overflow-y-auto font-mono text-[12px] leading-5"
            style={{ tabSize: 2 }}
          >
            {virtualize ? (
              <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
                {virtualizer.getVirtualItems().map((item) => (
                  <div
                    key={rows[item.index].key}
                    ref={virtualizer.measureElement}
                    data-index={item.index}
                    className="absolute left-0 top-0 w-full"
                    style={{ transform: `translateY(${item.start}px)` }}
                  >
                    {renderRow(rows[item.index])}
                  </div>
                ))}
              </div>
            ) : (
              rows.map((row) => <div key={row.key}>{renderRow(row)}</div>)
            )}
          </div>
        </>
      )}
    </div>
  )
}

function Half({
  line,
  side,
  lang,
  onComment,
  className,
}: {
  line: DiffLine | null
  side: Side
  lang: ReturnType<typeof langFor>
  onComment: (anchor: Anchor) => void
  className?: string
}) {
  if (!line) {
    return <div aria-hidden className={clsx('min-w-0 bg-surface-1/40', className)} />
  }
  const no = side === 'old' ? line.old_no : line.new_no
  return (
    <div className={clsx('flex min-w-0 items-stretch', ROW_BG[line.type], className)}>
      <LineNo no={no} side={side} type={line.type} onComment={onComment} />
      <span aria-hidden className={clsx('w-4 shrink-0 select-none text-center', GUTTER_BG[line.type])}>
        {SIGN[line.type]}
      </span>
      <Code text={line.text} lang={lang} />
    </div>
  )
}

function Notice({ children, icon }: { children: ReactNode; icon?: boolean }) {
  return (
    <div className="flex items-center gap-2 border-b border-hairline bg-surface-2 px-3 py-2 text-[12px] text-fg-secondary">
      {icon && <FileWarning size={13} aria-hidden className="shrink-0 text-state-attention" />}
      {children}
    </div>
  )
}
