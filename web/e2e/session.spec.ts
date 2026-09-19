import { test, expect, type Page } from '@playwright/test'
import {
  closeRealSession,
  codexPrompt,
  ensureRealCodexKey,
  ensureRealToken,
  ensureRealWorkspace,
  isReal,
  readCodexArgvLines,
  REAL_WORKSPACE_NAME,
  seedCodexSession,
  seedToolSession,
  sendRealMessage,
} from './helpers/seed'

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
// as a span, and the file it touched (note.txt) is listed under "Touched by
// tools" in the Review tab — v0.3 renamed that tab and gave it the worktree
// diff (e2e/review.spec.ts), keeping the transcript-derived list underneath.
test('the Activity tab shows a span for the fixture session', async ({ page }, testInfo) => {
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  await page.getByRole('tab', { name: 'Activity' }).click()
  await expect(page.getByTestId('span').first()).toBeVisible()
})

test('the Review tab lists note.txt among the touched files', async ({ page }, testInfo) => {
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  await page.getByRole('tab', { name: 'Review' }).click()
  await expect(page.getByTestId('changes-list')).toContainText('note.txt')
})

test('clicking a touched file filters the transcript to blocks touching it', async ({ page }, testInfo) => {
  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)

  await page.getByRole('tab', { name: 'Review' }).click()
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

// --- v1.0: the Codex harness ----------------------------------------------
// Two paths, because the two backends can only be reached two different ways:
// the mock has no server-side session store (msw only answers fetches the
// page's own JS makes), so a mock Codex session is created through the
// new-session dialog; the real backend gets one seeded over its API, running
// the Codex shell fake that playwright.config.ts points STYR_CODEX_BIN at.

test('creating a Codex session shows the harness chip and no approval affordances', async ({ page }, testInfo) => {
  test.skip(isReal(testInfo), 'real mode seeds its Codex session over the API, in the test below')

  await page.goto('/sessions?new=1')
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()

  // The note only appears once Codex is chosen: it is the one behavioural
  // difference someone picking a harness has to know about.
  await expect(dialog.getByTestId('codex-harness-note')).toHaveCount(0)
  await dialog.getByRole('combobox', { name: 'Harness' }).click()
  await page.getByRole('option', { name: 'Codex' }).click()
  await expect(dialog.getByTestId('codex-harness-note')).toContainText('sandbox policy')

  await dialog.getByLabel('Title').fill('Codex session')
  await dialog.getByLabel('Prompt').fill('hi')
  await dialog.getByRole('button', { name: 'Start session' }).click()

  await expect(page).toHaveURL(/\/sessions\/[0-9a-f-]{36}$/)
  const id = page.url().split('/').pop()!
  await expect(page.getByTestId('harness-chip')).toHaveText('Codex')

  // Even with a pending approval against it, a Codex session shows no
  // approval affordances: its permission model is the sandbox policy, so the
  // prompt would be an affordance nothing can ever answer.
  expect(await seedMockApproval(page, id)).toBe(true)
  await expect(page.getByTestId('permission-card')).toHaveCount(0)
  await expect(page.getByTestId('plan-card')).toHaveCount(0)
  await expect(page.getByTestId('harness-chip')).toHaveText('Codex')
})

/** Seeds a pending Bash approval against a mock session and makes the already
 * loaded page observe it. Both halves have to happen inside the page: msw's
 * service worker only intercepts fetches the page's own JS makes, and a
 * reload would re-evaluate the mock module and throw away the very session
 * this just seeded against (see e2e/helpers/seed.ts's note on the same
 * constraint). */
async function seedMockApproval(page: Page, sessionId: string): Promise<boolean> {
  return page.evaluate(async (id) => {
    const res = await fetch('/__mock/pending-approval', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ session_id: id }),
    })
    const client = (window as unknown as { __queryClient?: { refetchQueries: (f: { queryKey: unknown[] }) => Promise<unknown> } })
      .__queryClient
    await client?.refetchQueries({ queryKey: ['approvals'] })
    await client?.refetchQueries({ queryKey: ['session', id] })
    return res.ok
  }, sessionId)
}

// The control for the test above: the same seeded approval on a Claude
// session does show the permission card, so its absence on a Codex session is
// the harness rule and not a broken fixture.
test('a Claude session with the same pending approval does show the permission card', async ({ page }, testInfo) => {
  test.skip(isReal(testInfo), 'mock-only: uses the mock control route to seed an approval')

  const { sessionId } = await seedToolSession(page, testInfo)
  await page.goto(`/sessions/${sessionId}`)
  await expect(page.getByTestId('session-view')).toBeVisible()

  expect(await seedMockApproval(page, sessionId)).toBe(true)
  await expect(page.getByTestId('permission-card')).toBeVisible()
  await expect(page.getByTestId('harness-chip')).toHaveText('Claude Code')
})

test('a real Codex session replays its fixture and can be interrupted', async ({ page }, testInfo) => {
  test.skip(!isReal(testInfo), 'real-only: needs the Codex shell fake behind STYR_CODEX_BIN')

  const { sessionId } = await seedCodexSession(page, `Codex pong ${Date.now()}`)
  await page.goto(`/sessions/${sessionId}`)

  await expect(page.getByTestId('session-view')).toBeVisible()
  await expect(page.getByTestId('harness-chip')).toHaveText('Codex')

  // Codex fixture 01: the agent message "pong", then a completed turn, which
  // the transcript folds into a text block and a result separator.
  await expect(page.getByText('pong', { exact: true })).toBeVisible()
  await expect(page.getByText(/Turn finished/)).toBeVisible()

  // No approvals anywhere on a Codex session, however the turn went.
  await expect(page.getByTestId('permission-card')).toHaveCount(0)

  // Interrupt reaches the harness. Driven over the API rather than through the
  // header button: a fixture replay finishes in milliseconds, so the button's
  // running-only window is not something a click can be timed against, while
  // the path under test (service -> registry -> codex process) is the same
  // either way. It answers 202 whether or not the turn is still going, and the
  // session stays settled rather than failing.
  const interrupted = await page.request.post(`/api/v1/sessions/${sessionId}/interrupt`, {
    headers: { 'X-Requested-With': 'styr' },
  })
  expect(interrupted.status()).toBe(202)

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
})

// The harness select is not only a mock fixture: against the real backend the
// two kinds really are two different CLIs (two shell fakes behind
// STYR_CLAUDE_BIN and STYR_CODEX_BIN), started back to back from the same
// dialog, and each session keeps the harness it was started on.
test('the new-session dialog starts a Claude session and a Codex session back to back', async ({ page }, testInfo) => {
  test.skip(!isReal(testInfo), 'real-only: the mock version of this is the dialog test above')

  await ensureRealToken(page)
  await ensureRealCodexKey(page)
  await ensureRealWorkspace(page)

  const claudeId = await startThroughDialog(page, 'Claude Code', `Dialog claude ${Date.now()}`)
  await expect(page.getByTestId('harness-chip')).toHaveText('Claude Code')

  const codexId = await startThroughDialog(page, 'Codex', `Dialog codex ${Date.now()}`)
  await expect(page.getByTestId('harness-chip')).toHaveText('Codex')
  await expect(page.getByTestId('permission-card')).toHaveCount(0)

  expect(codexId).not.toBe(claudeId)
  // The chip reads the session row, so the first session is still on Claude
  // after a Codex one was started next to it.
  await page.goto(`/sessions/${claudeId}`)
  await expect(page.getByTestId('harness-chip')).toHaveText('Claude Code')
})

/** Starts one session through the new-session dialog on the named harness and
 * returns its id. The workspace is chosen explicitly (the select defaults to
 * whichever workspace happens to be first, and this run has several) so both
 * sessions run in the plain repo workspace rather than a worktree one. */
async function startThroughDialog(page: Page, harnessLabel: string, title: string): Promise<string> {
  await page.goto('/sessions?new=1')
  const dialog = page.getByRole('dialog')
  await expect(dialog).toBeVisible()

  await dialog.getByRole('combobox', { name: 'Workspace' }).click()
  await page.getByRole('option', { name: new RegExp(`^${REAL_WORKSPACE_NAME} `) }).click()
  await dialog.getByRole('combobox', { name: 'Harness' }).click()
  await page.getByRole('option', { name: harnessLabel, exact: true }).click()

  await dialog.getByLabel('Title').fill(title)
  await dialog.getByLabel('Prompt').fill('[fixture:01] say pong')
  await dialog.getByRole('button', { name: 'Start session' }).click()

  await expect(page).toHaveURL(/\/sessions\/[0-9a-f-]{36}$/)
  await expect(page.getByTestId('session-view')).toBeVisible()
  return page.url().split('/').pop()!
}

// Thread continuity (T65): `codex exec` runs one process per turn, and a Styr
// resume builds a whole new process object. Before the session row carried the
// CLI's thread id, that new process started a fresh Codex thread and the
// CLI-side context was gone. This drives the real path - close the session,
// send again - and reads the Codex fake's argv log to prove the reopened
// process ran `exec resume <thread id>` rather than a bare `exec`.
//
// A close is used rather than waiting out the idle timeout, which is minutes
// long by design; both end in the same place, a session with no live process.
test('resuming a closed Codex session continues its thread', async ({ page }, testInfo) => {
  test.skip(!isReal(testInfo), "real-only: reads the Codex shell fake's argv log")

  const { sessionId } = await seedCodexSession(page, `Codex resume ${Date.now()}`)
  await closeRealSession(page, sessionId)

  // Only the lines this turn adds are inspected: the log file is at a fixed
  // path, so it may still carry an earlier run's lines.
  const before = readCodexArgvLines().length
  await sendRealMessage(page, sessionId, codexPrompt('[fixture:04] what did you say before?'))
  const added = readCodexArgvLines().slice(before)

  expect(added.length).toBeGreaterThan(0)
  // The thread id fixtures 01 and 04 were recorded under: the reopened process
  // resumed the same conversation the first one started.
  const resumed = added.filter((line) => line.includes(`resume\t${CODEX_FIXTURE_THREAD}`))
  expect(resumed.length, `no resumed invocation in:\n${added.join('\n')}`).toBeGreaterThan(0)

  await page.goto(`/sessions/${sessionId}`)
  await expect(page.getByTestId('harness-chip')).toHaveText('Codex')
})

/** The thread id internal/harness/codex/testdata/01_simple_text.jsonl and
 * 04_resume.jsonl were recorded under - what the fake reports as its
 * `thread.started` id, and therefore what Styr stores and resumes by. */
const CODEX_FIXTURE_THREAD = '01a0b707-ce58-7660-b301-4939ce14c766'
