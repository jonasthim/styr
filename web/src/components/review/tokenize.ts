// A deliberately small syntax tokenizer for diff lines. Four roles — keyword,
// string, number, comment — because the job is to find a string literal or a
// commented-out line at speed while reading a diff, not to repaint the file.
// A real highlighter (Shiki, Prism) would be an order of magnitude more code
// and bytes than the whole review surface, and the design system's own rule is
// "syntax colouring via a light tokenizer, no heavy highlighter"
// (docs/superpowers/plans/2026-09-18-styr-v0.3-review.md, "Frontend").
//
// It is line-local by design: a diff shows fragments of a file, so there is no
// reliable multi-line state to carry. An unterminated string or a block
// comment that spans a hunk boundary simply stops colouring at the end of the
// line, which is the failure mode a reader can live with.

export type Lang = 'go' | 'ts' | 'js' | 'py' | 'yaml' | 'md' | 'sh' | 'text'

export type TokenKind = 'plain' | 'keyword' | 'string' | 'number' | 'comment'

export interface Token {
  text: string
  kind: TokenKind
}

const BY_EXTENSION: Record<string, Lang> = {
  go: 'go',
  ts: 'ts',
  tsx: 'ts',
  mts: 'ts',
  cts: 'ts',
  js: 'js',
  jsx: 'js',
  mjs: 'js',
  cjs: 'js',
  py: 'py',
  pyi: 'py',
  yaml: 'yaml',
  yml: 'yaml',
  md: 'md',
  markdown: 'md',
  sh: 'sh',
  bash: 'sh',
  zsh: 'sh',
  fish: 'sh',
}

/** The language for a path, by extension, then by a couple of well-known
 * extensionless names. Unknown files render uncoloured rather than guessed. */
export function langFor(path: string): Lang {
  const name = path.split('/').pop() ?? path
  if (name === 'Dockerfile' || name === 'Makefile') return 'text'
  if (name.startsWith('.') && !name.slice(1).includes('.')) return 'text'
  const ext = name.includes('.') ? name.split('.').pop()!.toLowerCase() : ''
  return BY_EXTENSION[ext] ?? 'text'
}

const KEYWORDS: Record<Lang, string[]> = {
  go: ['break','case','chan','const','continue','default','defer','else','fallthrough','for','func','go','goto','if','import','interface','map','package','range','return','select','struct','switch','type','var','nil','true','false','error','string','int','int64','bool','byte','rune'],
  ts: ['as','async','await','break','case','catch','class','const','continue','default','delete','do','else','enum','export','extends','finally','for','from','function','if','implements','import','in','instanceof','interface','let','new','of','return','satisfies','static','switch','this','throw','try','type','typeof','var','void','while','yield','null','undefined','true','false'],
  js: ['async','await','break','case','catch','class','const','continue','default','delete','do','else','export','extends','finally','for','from','function','if','import','in','instanceof','let','new','of','return','static','switch','this','throw','try','typeof','var','void','while','yield','null','undefined','true','false'],
  py: ['and','as','assert','async','await','break','class','continue','def','del','elif','else','except','finally','for','from','global','if','import','in','is','lambda','none','nonlocal','not','or','pass','raise','return','try','while','with','yield','True','False','None','self'],
  yaml: [],
  md: [],
  sh: ['case','do','done','elif','else','esac','export','fi','for','function','if','in','local','readonly','return','then','until','while','set','source'],
  text: [],
}

const LINE_COMMENT: Record<Lang, string | null> = {
  go: '//',
  ts: '//',
  js: '//',
  py: '#',
  yaml: '#',
  md: null,
  sh: '#',
  text: null,
}

function isWordChar(ch: string): boolean {
  return /[A-Za-z0-9_$]/.test(ch)
}

function push(out: Token[], text: string, kind: TokenKind): void {
  if (text === '') return
  const last = out[out.length - 1]
  if (last && last.kind === kind) last.text += text
  else out.push({ text, kind })
}

/** Markdown gets structure, not syntax: a heading, a list marker, a fence and
 * inline code are what a reader scans for in a prose diff. */
function tokenizeMarkdown(text: string): Token[] {
  const trimmed = text.trimStart()
  if (trimmed.startsWith('#') || trimmed.startsWith('```')) return [{ text, kind: 'keyword' }]
  if (trimmed.startsWith('>')) return [{ text, kind: 'comment' }]
  const marker = /^(\s*(?:[-*+]|\d+\.)\s)/.exec(text)
  if (marker) {
    return [
      { text: marker[1], kind: 'keyword' },
      { text: text.slice(marker[1].length), kind: 'plain' },
    ]
  }
  return [{ text, kind: 'plain' }]
}

/** YAML gets its keys, its comments and its quoted scalars. */
function tokenizeYaml(text: string): Token[] {
  const out: Token[] = []
  const key = /^(\s*-?\s*)([A-Za-z0-9_.-]+)(:)/.exec(text)
  let i = 0
  if (key) {
    push(out, key[1], 'plain')
    push(out, key[2], 'keyword')
    push(out, key[3], 'plain')
    i = key[0].length
  }
  for (; i < text.length; i += 1) {
    const ch = text[i]
    if (ch === '#') {
      push(out, text.slice(i), 'comment')
      return out
    }
    if (ch === '"' || ch === "'") {
      const end = closingQuote(text, i, ch, false)
      push(out, text.slice(i, end), 'string')
      i = end - 1
      continue
    }
    push(out, ch, 'plain')
  }
  return out
}

/** Index just past the closing quote (or the end of the line). */
function closingQuote(text: string, start: number, quote: string, escapes: boolean): number {
  for (let i = start + 1; i < text.length; i += 1) {
    if (escapes && text[i] === '\\') {
      i += 1
      continue
    }
    if (text[i] === quote) return i + 1
  }
  return text.length
}

export function tokenize(text: string, lang: Lang): Token[] {
  if (lang === 'text' || text === '') return [{ text, kind: 'plain' }]
  if (lang === 'md') return tokenizeMarkdown(text)
  if (lang === 'yaml') return tokenizeYaml(text)

  const keywords = new Set(KEYWORDS[lang])
  const comment = LINE_COMMENT[lang]
  const out: Token[] = []

  for (let i = 0; i < text.length; i += 1) {
    const rest = text.slice(i)
    if (comment && rest.startsWith(comment)) {
      push(out, rest, 'comment')
      break
    }
    if (rest.startsWith('/*')) {
      const end = rest.indexOf('*/')
      const chunk = end === -1 ? rest : rest.slice(0, end + 2)
      push(out, chunk, 'comment')
      i += chunk.length - 1
      continue
    }
    const ch = text[i]
    if (ch === '"' || ch === "'" || ch === '`') {
      const end = closingQuote(text, i, ch, true)
      push(out, text.slice(i, end), 'string')
      i = end - 1
      continue
    }
    if (/[0-9]/.test(ch) && (i === 0 || !isWordChar(text[i - 1]))) {
      const match = /^[0-9][0-9_a-fA-FxXoObB.]*/.exec(rest)!
      push(out, match[0], 'number')
      i += match[0].length - 1
      continue
    }
    if (isWordChar(ch)) {
      const match = /^[A-Za-z0-9_$]+/.exec(rest)!
      push(out, match[0], keywords.has(match[0]) ? 'keyword' : 'plain')
      i += match[0].length - 1
      continue
    }
    push(out, ch, 'plain')
  }

  return out
}
