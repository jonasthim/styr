import { describe, expect, it } from 'vitest'
import { hunkLabel, splitRows, totalLines, unifiedRows, type PairRow } from './diffRows'
import { langFor, tokenize } from './tokenize'
import type { DiffHunk } from '../../api/types'

const hunk: DiffHunk = {
  old_start: 41,
  old_lines: 4,
  new_start: 41,
  new_lines: 5,
  lines: [
    { type: 'ctx', old_no: 41, new_no: 41, text: 'func New() *Service {' },
    { type: 'del', old_no: 42, new_no: 0, text: '\treturn &Service{}' },
    { type: 'add', old_no: 0, new_no: 42, text: '\treturn &Service{' },
    { type: 'add', old_no: 0, new_no: 43, text: '\t\tworktree: "",' },
    { type: 'ctx', old_no: 43, new_no: 44, text: '}' },
  ],
}

describe('hunk rows', () => {
  it('labels a hunk the way git does', () => {
    expect(hunkLabel(hunk)).toBe('@@ -41,4 +41,5 @@')
  })

  it('counts every line across hunks', () => {
    expect(totalLines([hunk, hunk])).toBe(10)
  })

  it('emits one unified row per line, under a header row', () => {
    const rows = unifiedRows([hunk])
    expect(rows).toHaveLength(6)
    expect(rows[0].kind).toBe('hunk')
    expect(rows.filter((r) => r.kind === 'line')).toHaveLength(5)
  })
})

describe('splitRows', () => {
  it('pairs a deletion with the addition that replaced it', () => {
    const pairs = splitRows([hunk]).filter((r): r is PairRow => r.kind === 'pair')
    expect(pairs).toHaveLength(4)
    expect(pairs[1].left?.old_no).toBe(42)
    expect(pairs[1].right?.new_no).toBe(42)
  })

  it('leaves the other half blank when one side runs out', () => {
    const pairs = splitRows([hunk]).filter((r): r is PairRow => r.kind === 'pair')
    expect(pairs[2].left).toBeNull()
    expect(pairs[2].right?.new_no).toBe(43)
  })

  it('repeats a context line on both sides', () => {
    const pairs = splitRows([hunk]).filter((r): r is PairRow => r.kind === 'pair')
    expect(pairs[0].left).toBe(pairs[0].right)
  })
})

describe('tokenize', () => {
  it('picks a language from the path', () => {
    expect(langFor('internal/sessions/service.go')).toBe('go')
    expect(langFor('docs/REVIEW.md')).toBe('md')
    expect(langFor('.gitignore')).toBe('text')
  })

  it('colours a Go keyword, string and trailing comment', () => {
    const tokens = tokenize('func run() { s := "hi" } // done', 'go')
    expect(tokens.find((t) => t.kind === 'keyword')?.text).toBe('func')
    expect(tokens.find((t) => t.kind === 'string')?.text).toBe('"hi"')
    expect(tokens.find((t) => t.kind === 'comment')?.text).toBe('// done')
  })

  it('does not treat a word ending in a keyword as one', () => {
    const tokens = tokenize('myfunc()', 'go')
    expect(tokens.every((t) => t.kind !== 'keyword')).toBe(true)
  })

  it('reads a yaml key and its comment', () => {
    const tokens = tokenize('  name: "styr" # the service', 'yaml')
    expect(tokens.find((t) => t.kind === 'keyword')?.text).toBe('name')
    expect(tokens.find((t) => t.kind === 'comment')?.text).toBe('# the service')
  })
})
