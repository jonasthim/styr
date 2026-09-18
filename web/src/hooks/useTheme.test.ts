// Unit test for useTheme.ts's pure mode resolver. This file intentionally
// tests only resolveTheme, not the zustand store or its DOM/localStorage
// side effects: those run at module scope (see useTheme.ts) and need a
// browser, which this project's default (node) vitest environment does not
// provide. resolveTheme itself takes no DOM dependency, so it is safe to
// import and exercise directly.
import { describe, expect, it } from 'vitest'
import { resolveTheme } from './useTheme'

describe('resolveTheme', () => {
  it('resolves an explicit "dark" mode to dark regardless of the OS preference', () => {
    expect(resolveTheme('dark', true)).toBe('dark')
    expect(resolveTheme('dark', false)).toBe('dark')
  })

  it('resolves an explicit "light" mode to light regardless of the OS preference', () => {
    expect(resolveTheme('light', true)).toBe('light')
    expect(resolveTheme('light', false)).toBe('light')
  })

  it('resolves "system" mode to the OS preference', () => {
    expect(resolveTheme('system', true)).toBe('dark')
    expect(resolveTheme('system', false)).toBe('light')
  })
})
