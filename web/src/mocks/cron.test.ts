import { describe, expect, it } from 'vitest'
import { parseCron, previewCron } from './cron'

describe('parseCron', () => {
  it('rejects anything that is not five fields', () => {
    expect(parseCron('* * * *')).toBeNull()
    expect(parseCron('* * * * * *')).toBeNull()
    expect(parseCron('')).toBeNull()
  })

  it('rejects out-of-range and malformed fields', () => {
    expect(parseCron('60 * * * *')).toBeNull()
    expect(parseCron('* 24 * * *')).toBeNull()
    expect(parseCron('*/0 * * * *')).toBeNull()
    expect(parseCron('5-1 * * * *')).toBeNull()
  })

  it('expands steps, ranges and lists', () => {
    const cron = parseCron('0,30 9-11 * * *')!
    expect([...cron.minute]).toEqual([0, 30])
    expect([...cron.hour]).toEqual([9, 10, 11])

    const every15 = parseCron('*/15 * * * *')!
    expect([...every15.minute]).toEqual([0, 15, 30, 45])
  })

  it('understands the @daily family', () => {
    const cron = parseCron('@daily')!
    expect([...cron.minute]).toEqual([0])
    expect([...cron.hour]).toEqual([0])
  })
})

describe('previewCron', () => {
  const from = new Date(2026, 8, 19, 12, 0, 0) // 2026-09-19 12:00 local

  it('describes a daily time the way the dialog shows it', () => {
    expect(previewCron('30 7 * * *', from).description).toBe('At 07:30, every day')
  })

  it('describes a weekday, a step and an hourly cron', () => {
    expect(previewCron('0 9 * * 1', from).description).toBe('At 09:00, every Monday')
    expect(previewCron('*/5 * * * *', from).description).toBe('Every 5 minutes')
    expect(previewCron('0 * * * *', from).description).toBe('On the hour')
  })

  it('lists the next five firings in order', () => {
    const preview = previewCron('30 7 * * *', from)
    expect(preview.next).toHaveLength(5)
    expect(preview.error).toBeUndefined()
    const times = preview.next.map((iso) => new Date(iso))
    expect(times[0]!.getHours()).toBe(7)
    expect(times[0]!.getMinutes()).toBe(30)
    // 12:00 on the 19th is past 07:30, so the first firing is the 20th.
    expect(times[0]!.getDate()).toBe(20)
    for (let i = 1; i < times.length; i += 1) {
      expect(times[i]!.getTime()).toBeGreaterThan(times[i - 1]!.getTime())
    }
  })

  it('reports an error instead of guessing at a broken expression', () => {
    const preview = previewCron('not a cron', from)
    expect(preview.next).toEqual([])
    expect(preview.error).toMatch(/5-field cron/)
  })
})
