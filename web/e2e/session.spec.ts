import { test, expect } from '@playwright/test'
import { isReal, seedToolSession } from './helpers/seed'

// Runs against both backends. Mock: msw handlers seed a fixed session
// (TOOL_FIXTURE_SESSION_ID there) whose transcript is fixture 02
// (internal/harness/claude/testdata/02_tool_read.jsonl, decoded into
// harness.Event JSON by web/src/mocks/decodeFixture.ts): a Read tool call
// on note.txt, followed by the text reply "hello". Real: seedToolSession
// (e2e/helpers/seed.ts) creates that same session against the real backend
// with a "[fixture:02]" marker fake-claude.sh understands, so the
// transcript content is identical in both modes. A user's own turn (the
// initial prompt, and any later message) is now persisted as a "user"
// transcript event by internal/sessions/service.go's recordUserTurn and
// pushed over the live event stream, so the "shows it as a user block"
// test below needs no mock-only path: the same getByText assertion covers
// both backends.

test.describe.configure({ mode: 'serial' })

test('opening the session shows the tool block and the text reply', async ({ page }, testInfo) => {
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  await expect(page.getByTestId('session-view')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Read note.txt' })).toBeVisible()

  const toolHeader = page.getByRole('button', { expanded: false }).filter({ hasText: 'Read' })
  await expect(toolHeader).toBeVisible()

  await expect(page.getByText('hello', { exact: true })).toBeVisible()
})

test('clicking the tool block expands the result panel', async ({ page }, testInfo) => {
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  const toolHeader = page.getByRole('button', { expanded: false }).filter({ hasText: 'Read' })
  await toolHeader.click()

  await expect(page.getByText('Result')).toBeVisible()
  await expect(page.getByTestId('tool-result')).toContainText('hello')
})

test('sending a message shows it as a user block', async ({ page }, testInfo) => {
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  const input = page.getByTestId('composer-input')
  await input.fill('again')
  await page.getByTestId('composer-send').click()

  await expect(page.getByText('again', { exact: true })).toBeVisible()
  await expect(input).toHaveValue('')

  if (isReal(testInfo)) {
    // "again" carries no [fixture:NN] marker, so the shell fake replays its
    // default fixture (01_simple_text.jsonl) for this turn on the process
    // seedToolSession already started - a short text reply, no tools. The
    // session was already `open` (post fixture-02 result); confirm it comes
    // back to `open` again once this second turn's own result lands.
    await expect
      .poll(
        async () => {
          const res = await page.request.get(`/api/v1/sessions/${sessionId}`, {
            headers: { 'X-Requested-With': 'styr' },
          })
          const body = (await res.json()) as { state: string }
          return body.state
        },
        { timeout: 20_000 },
      )
      .toBe('open')
  }
})

// Task 21: the side panel's Activity tab shows the fixture's Read tool call
// as a span, and the Changes tab lists the file it touched (note.txt).
test('the Activity tab shows a span for the fixture session', async ({ page }, testInfo) => {
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  await page.getByRole('tab', { name: 'Activity' }).click()
  await expect(page.getByTestId('span').first()).toBeVisible()
})

test('the Changes tab lists note.txt', async ({ page }, testInfo) => {
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  await page.getByRole('tab', { name: 'Changes' }).click()
  await expect(page.getByTestId('changes-list')).toContainText('note.txt')
})

test('clicking a file in Changes filters the transcript to blocks touching it', async ({ page }, testInfo) => {
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  await page.getByRole('tab', { name: 'Changes' }).click()
  await page.getByTestId('changes-list').getByRole('button', { name: /note\.txt/ }).click()

  await expect(page).toHaveURL(/[?&]file=/)
  await expect(page.getByRole('button', { expanded: false }).filter({ hasText: 'Read' })).toBeVisible()
  await expect(page.getByText('hello', { exact: true })).not.toBeVisible()
})

test('phone shows a tabs strip above the composer instead of a fixed side panel', async ({ page }, testInfo) => {
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  const panel = page.getByTestId('side-panel')
  await expect(panel).toBeVisible()
  const box = await panel.boundingBox()
  expect(box).not.toBeNull()

  if (testInfo.project.name.endsWith('-phone')) {
    expect(box!.width).toBeGreaterThan(350)
  } else {
    expect(box!.width).toBeLessThanOrEqual(340)
    expect(box!.width).toBeGreaterThanOrEqual(300)
  }
})
