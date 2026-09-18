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

  // The phrasing is internal/schedules/cron.go's describeCron, word for
  // word: the mock has to say what the scheduler would say.
  it('describes a daily time the way the scheduler does', () => {
    expect(previewCron('30 7 * * *', from)?.description).toBe('every day at 07:30')
  })

  it('describes a weekday, a step, an hourly cron and a descriptor', () => {
    expect(previewCron('0 9 * * 1', from)?.description).toBe('every Monday at 09:00')
    expect(previewCron('*/5 * * * *', from)?.description).toBe('every 5 minutes')
    expect(previewCron('* * * * *', from)?.description).toBe('every minute')
    expect(previewCron('0 * * * *', from)?.description).toBe('every hour at :00')
    expect(previewCron('@daily', from)?.description).toBe('every day at 00:00')
  })

  it('lists the next five firings in order', () => {
    const preview = previewCron('30 7 * * *', from)!
    expect(preview.next).toHaveLength(5)
    const times = preview.next.map((iso) => new Date(iso))
    expect(times[0]!.getHours()).toBe(7)
    expect(times[0]!.getMinutes()).toBe(30)
    // 12:00 on the 19th is past 07:30, so the first firing is the 20th.
    expect(times[0]!.getDate()).toBe(20)
    for (let i = 1; i < times.length; i += 1) {
      expect(times[i]!.getTime()).toBeGreaterThan(times[i - 1]!.getTime())
    }
  })

  it('answers nothing at all for a broken expression, so the handler 422s', () => {
    expect(previewCron('not a cron', from)).toBeNull()
    expect(previewCron('', from)).toBeNull()
  })
})
