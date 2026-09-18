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

// T38: the `/` menu, the model and effort selects, and the resuming state.
// These are mock-only because they assert the mock's own seeded command list
// and its scripted resume; the real-backend block at the bottom covers the
// same two controls against the server (T37).

test('typing / lists a custom command and hides a hidden built-in', async ({ page }, testInfo) => {
  test.skip(isReal(testInfo), 'mock-only: asserts the mock-seeded command list')
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  const input = page.getByTestId('composer-input')
  await input.fill('/')

  const menu = page.getByTestId('slash-menu')
  await expect(menu).toBeVisible()
  // Seeded by the mock's MOCK_SLASH_COMMANDS.
  await expect(menu.getByRole('option', { name: /commit-commands:commit/ })).toBeVisible()
  // Styr's own commands are always offered.
  await expect(menu.getByRole('option', { name: /\/help/ })).toBeVisible()
  // …and the hidden built-ins never are: /clear resets the CLI's conversation
  // under a new session id (internal/harness/claude/testdata/PROTOCOL.md).
  await expect(menu.getByRole('option', { name: /\/clear/ })).toHaveCount(0)
  await expect(menu.getByRole('option', { name: /\/doctor/ })).toHaveCount(0)
})

test('selecting a CLI command inserts it into the composer', async ({ page }, testInfo) => {
  test.skip(isReal(testInfo), 'mock-only: asserts the mock-seeded command list')
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  const input = page.getByTestId('composer-input')
  await input.fill('/compact')
  await expect(page.getByTestId('slash-menu')).toBeVisible()
  await input.press('Enter')

  await expect(input).toHaveValue('/compact ')
  await expect(page.getByTestId('slash-menu')).toHaveCount(0)
})

test('selecting /model focuses the header model select', async ({ page }, testInfo) => {
  test.skip(isReal(testInfo), 'mock-only: asserts the mock-seeded command list')
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  const input = page.getByTestId('composer-input')
  await input.fill('/model')
  await expect(page.getByTestId('slash-menu')).toBeVisible()
  await input.press('Enter')

  await expect(input).toHaveValue('')
  await expect(page.locator('#session-model-select')).toBeFocused()
})

test('/help lists the Styr and session commands', async ({ page }, testInfo) => {
  test.skip(isReal(testInfo), 'mock-only: asserts the mock-seeded command list')
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  const input = page.getByTestId('composer-input')
  await input.fill('/help')
  await input.press('Enter')

  const help = page.getByTestId('slash-help')
  await expect(help).toBeVisible()
  await expect(help).toContainText('/interrupt')
  await expect(help).toContainText('/commit-commands:commit')
})

test('switching the model shows the resuming state', async ({ page }, testInfo) => {
  test.skip(isReal(testInfo), 'mock-only: the mock scripts the resumed init on a timer')
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  await page.locator('#session-model-select').click()
  await page.getByRole('option', { name: 'Haiku 4.5' }).click()

  await expect(page.getByTestId('model-resuming')).toContainText('Resuming with Haiku 4.5')
  // The mock reports the resumed process's init after a delay, which is what
  // clears the banner and shows the new model.
  await expect(page.getByTestId('model-resuming')).toHaveCount(0, { timeout: 15_000 })
  await expect(page.locator('#session-model-select')).toContainText('Haiku 4.5')
})

// --- real backend ----------------------------------------------------------
// The same two controls against the running server. The shell fake replays a
// recorded transcript, so the session's slash_commands are the ones the real
// CLI reported on that recording's init message (fixture 02's, which the
// seeded session replays), and a resume comes back with that same init - it
// just ignores the --model flag it was restarted with.

test('the / menu lists a command the real CLI reported and hides clear', async ({ page }, testInfo) => {
  test.skip(!isReal(testInfo), 'real-backend only: the mock seeds its own command list')
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  const input = page.getByTestId('composer-input')
  await input.fill('/')

  const menu = page.getByTestId('slash-menu')
  await expect(menu).toBeVisible()
  // Reported by the recorded init message, and a built-in that survives
  // headless mode (internal/harness/claude/builtins.go).
  await expect(menu.getByRole('option', { name: /\/compact/ })).toBeVisible()
  // …while /clear is reported too and must never be offered: the CLI resets
  // the conversation under a new session id.
  await expect(menu.getByRole('option', { name: /\/clear/ })).toHaveCount(0)
  // Styr's own commands are always there.
  await expect(menu.getByRole('option', { name: /\/help/ })).toBeVisible()
})

test('switching the model resumes the session and it settles again', async ({ page }, testInfo) => {
  test.skip(!isReal(testInfo), 'real-backend only: the mock fakes the resumed init')
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  async function sessionField<T>(field: string): Promise<T> {
    const res = await page.request.get(`/api/v1/sessions/${sessionId}`, { headers: { 'X-Requested-With': 'styr' } })
    return ((await res.json()) as Record<string, T>)[field]
  }

  await page.locator('#session-model-select').click()
  await page.getByRole('option', { name: 'Haiku 4.5' }).click()

  // The switch closes the process and starts a resumed one, which the header
  // says out loud until the CLI reports back.
  await expect(page.getByTestId('model-resuming')).toContainText('Resuming with Haiku 4.5')
  await expect.poll(() => sessionField<string>('state'), { timeout: 20_000 }).toBe('running')

  // The resumed fake only speaks when spoken to, so the next turn is what
  // makes it report its init and finish - and that is what clears the banner.
  await page.getByTestId('composer-input').fill('again')
  await page.getByTestId('composer-send').click()

  await expect(page.getByTestId('model-resuming')).toHaveCount(0, { timeout: 30_000 })
  await expect.poll(() => sessionField<string>('state'), { timeout: 30_000 }).toBe('open')
  // The fake ignores --model and reports the model its recording ran on, so
  // the assertion is that the session ends up with a model at all, not which.
  expect(await sessionField<string>('model')).not.toBe('')
})
