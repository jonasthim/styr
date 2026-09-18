import { test, expect, type Page } from '@playwright/test'
import { isReal, seedPlanSession, seedWorktreeSession, WORKTREE_FILE } from './helpers/seed'

// The review surface (diff, inline comments, commit, PR, checkpoints, plan
// approval), in two lanes.
//
// Mock: the msw fixture diff — three files in the shapes the renderer has to
// get right (modified, added, deleted) — exercises the layout and the
// dialogs. The mock's own state lives in the page (msw's service worker
// forwards each request back to the page runtime), so the control routes
// below have to be called with page.evaluate — a Playwright-side
// page.request would bypass the worker entirely — and a reload would re-seed
// the module and undo them. Same constraint e2e/sessions.spec.ts documents
// for /__mock/clear-workspaces.
//
// Real: a throwaway git repo registered as a worktree-enabled workspace
// (e2e/helpers/seed.ts), a session whose prompt makes the shell fake write a
// real file into its own worktree, and then the whole review round trip
// against the actual endpoints — diff, comment, send review, checkpoints,
// rewind, commit, pull request and discard — plus a plan-mode session whose
// ExitPlanMode request is approved from the session view.
const MOCK_TOOL_SESSION_ID = '00000000-0000-4000-8000-000000000005'
const MODIFIED_FILE = 'internal/sessions/service.go'

interface MockWindow {
  __queryClient: { refetchQueries: (f: { queryKey: string[] }) => Promise<unknown> }
}

async function mockPost(page: Page, path: string): Promise<void> {
  const ok = await page.evaluate(async (p) => (await fetch(p, { method: 'POST' })).ok, path)
  if (!ok) throw new Error(`${path} failed`)
}

async function refetch(page: Page, key: string): Promise<void> {
  await page.evaluate(async (k) => {
    await (window as unknown as MockWindow).__queryClient.refetchQueries({ queryKey: [k] })
  }, key)
}

async function openReview(page: Page): Promise<void> {
  await page.goto(`/sessions/${MOCK_TOOL_SESSION_ID}`)
  await expect(page.getByTestId('session-view')).toBeVisible()
  await page.getByRole('tab', { name: 'Review' }).click()
  await expect(page.getByTestId('review-files')).toBeVisible()
}

async function openModifiedFile(page: Page): Promise<void> {
  await openReview(page)
  await page.getByTestId('review-files').getByRole('button', { name: new RegExp(MODIFIED_FILE) }).click()
  await expect(page.getByTestId('diff-view')).toBeVisible()
}

test.describe('review (mock backend)', () => {
  test.beforeEach(async ({}, testInfo) => {
    test.skip(isReal(testInfo), 'the mock fixture diff; the real lane is at the bottom of this file')
  })

  test('the Review tab lists the three changed files with their status chips', async ({ page }) => {
    await openReview(page)

    const files = page.getByTestId('review-file')
    await expect(files).toHaveCount(3)
    await expect(files.filter({ hasText: MODIFIED_FILE })).toContainText('M')
    await expect(files.filter({ hasText: 'docs/REVIEW.md' })).toContainText('A')
    await expect(files.filter({ hasText: 'internal/api/legacy_diff.go' })).toContainText('D')
  })

  test('opening the modified file shows its two hunks with their line numbers', async ({ page }) => {
    await openModifiedFile(page)

    const hunks = page.getByTestId('diff-hunk')
    await expect(hunks).toHaveCount(2)
    await expect(hunks.first()).toContainText('@@ -41,7 +41,9 @@')
    await expect(hunks.nth(1)).toContainText('@@ -118,6 +120,10 @@')
    await expect(page.getByTestId('diff-view')).toContainText('worktree')
  })

  test('an inline comment becomes the next prompt when the review is sent', async ({ page }) => {
    await openModifiedFile(page)

    await page.getByRole('button', { name: 'Comment on new line 46' }).click()
    const draft = page.getByTestId('comment-draft')
    await expect(draft).toBeVisible()
    await draft.fill('Guard this against an empty base ref.')
    await page.getByRole('button', { name: 'Add comment' }).click()

    await expect(page.getByTestId('review-comment').first()).toContainText('Guard this against an empty base ref.')
    await expect(page.getByTestId('review-rail')).toContainText('1 comment')

    await page.getByRole('button', { name: 'Send review' }).click()
    await page.getByRole('button', { name: 'Back to transcript' }).click()

    const transcript = page.getByTestId('transcript')
    await expect(transcript).toContainText(`${MODIFIED_FILE}:46`)
    await expect(transcript).toContainText('Guard this against an empty base ref.')
  })

  test('the Commit dialog posts the message and reports the new sha', async ({ page }) => {
    await openReview(page)

    await page.getByRole('button', { name: 'Commit' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByLabel('Message')).toHaveValue('Read note.txt')
    await expect(dialog).toContainText('+42')
    await dialog.getByRole('button', { name: 'Commit changes' }).click()

    await expect(page.getByTestId('toast')).toContainText('9f2c1ab')
  })

  test('Open PR explains that gh is unavailable when the server says so', async ({ page }) => {
    await openReview(page)
    await mockPost(page, '/__mock/gh-unavailable')

    await page.getByRole('button', { name: 'Open PR' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByLabel('Base branch')).toHaveValue('main')
    await dialog.getByRole('button', { name: 'Create pull request' }).click()

    await expect(dialog.getByTestId('pr-error')).toContainText('gh')
    await expect(dialog.getByRole('link', { name: /REVIEW\.md/ })).toBeVisible()
  })

  test('the checkpoints list offers a rewind and asks before taking it', async ({ page }) => {
    await openReview(page)

    await page.getByRole('button', { name: 'Checkpoints' }).click()
    const menu = page.getByTestId('checkpoints-menu')
    await expect(menu).toBeVisible()
    await expect(menu.getByTestId('checkpoint-row')).toHaveCount(3)

    await menu.getByTestId('checkpoint-row').first().click()
    const confirm = page.getByRole('dialog')
    await expect(confirm).toContainText('Rewind')
    await expect(confirm.getByRole('button', { name: 'Rewind to here' })).toBeVisible()
  })

  test('a plan awaiting approval is approved from the session view', async ({ page }) => {
    await page.goto(`/sessions/${MOCK_TOOL_SESSION_ID}`)
    await expect(page.getByTestId('session-view')).toBeVisible()
    await mockPost(page, '/__mock/plan-approval')
    await refetch(page, 'approvals')

    const card = page.getByTestId('plan-card')
    await expect(card).toBeVisible()
    await expect(card).toContainText('Add the worktree diff endpoints')
    await expect(card.getByTestId('plan-step').first()).toBeVisible()

    // The pointer starts at 0,0, which hovers the rail open over the left edge
    // of the page (components/shell/Rail.tsx); move it off before clicking a
    // control that sits under it.
    await page.mouse.move(700, 400)
    await card.getByRole('button', { name: 'Approve plan' }).click()
    await expect(card).toHaveCount(0)
  })

  test('the inbox renders a pending plan as a checklist', async ({ page }) => {
    await page.goto('/inbox')
    await expect(page.getByRole('heading', { name: 'Inbox' })).toBeVisible()
    await mockPost(page, '/__mock/plan-approval')
    await refetch(page, 'approvals')

    const plan = page.getByTestId('plan-summary')
    await expect(plan).toBeVisible()
    await expect(plan.getByTestId('plan-step').first()).toBeVisible()
  })

  test('the sessions list shows a diff badge for a session with changes', async ({ page }) => {
    await page.goto('/sessions')
    await expect(page.getByRole('heading', { name: 'Sessions' })).toBeVisible()

    const row = page.locator(`a[data-testid="session-row"][href$="${MOCK_TOOL_SESSION_ID}"]`)
    const badge = row.getByTestId('diff-badge')
    await expect(badge).toContainText('+42')
    await expect(badge).toContainText('18')
  })

  test('the phone layout renders the diff unified', async ({ page }, testInfo) => {
    test.skip(!testInfo.project.name.endsWith('-phone'), 'checks the phone viewport specifically')
    await openModifiedFile(page)

    await expect(page.getByTestId('diff-view')).toHaveAttribute('data-mode', 'unified')
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
    expect(overflow).toBeLessThanOrEqual(1)
  })

  test('the desktop layout renders the diff side by side', async ({ page }, testInfo) => {
    test.skip(!testInfo.project.name.endsWith('-desktop'), 'checks the desktop viewport specifically')
    await openModifiedFile(page)

    await expect(page.getByTestId('diff-view')).toHaveAttribute('data-mode', 'split')
  })
})

// --- real backend ----------------------------------------------------------

const HEADERS = { 'X-Requested-With': 'styr' }
const COMMENT = 'Name this better.'
// U+2212, the minus sign the diff counts are set in (components/review/fileMeta.tsx
// and components/sessions/SessionRow.tsx), not a hyphen.
const MINUS = '−'

/** Polls the session row until it reaches one of `states`. The review actions
 * are refused while a session is running or waiting, so every step that
 * follows a turn waits here first rather than racing the turn. */
async function waitForState(page: Page, sessionId: string, states: string[]): Promise<void> {
  await expect
    .poll(
      async () => {
        const res = await page.request.get(`/api/v1/sessions/${sessionId}`, { headers: HEADERS })
        const body = (await res.json()) as { state: string }
        return body.state
      },
      { timeout: 30_000 },
    )
    .toMatch(new RegExp(`^(${states.join('|')})$`))
}

/** Leaves the session with nothing pending: denies every approval it has (one
 * at a time - the shell fake only replays its next permission request once the
 * previous one is answered), then closes it so no further request can arrive,
 * then denies anything that slipped in between. Never allows: an allowed
 * request would run whatever the fixture recorded.
 *
 * The real projects share one backend and database for the whole run, so an
 * approval left pending would count toward every later test's inbox
 * (e2e/inbox.spec.ts asserts exact counts). */
async function drainApprovals(page: Page, sessionId: string): Promise<void> {
  async function pendingFor(): Promise<string[]> {
    const res = await page.request.get('/api/v1/approvals', { headers: HEADERS })
    const all = (await res.json()) as Array<{ id: string; session_id: string }>
    return all.filter((a) => a.session_id === sessionId).map((a) => a.id)
  }
  async function denyAll(ids: string[]): Promise<void> {
    for (const id of ids) {
      const res = await page.request.post(`/api/v1/approvals/${id}`, { headers: HEADERS, data: { decision: 'deny' } })
      expect(res.ok()).toBeTruthy()
    }
  }

  const deadline = Date.now() + 30_000
  for (;;) {
    const ids = await pendingFor()
    if (ids.length === 0) break
    await denyAll(ids)
    if (Date.now() > deadline) throw new Error(`review: session ${sessionId} kept asking for approvals`)
    await page.waitForTimeout(250)
  }

  const close = await page.request.post(`/api/v1/sessions/${sessionId}/close`, { headers: HEADERS })
  expect(close.ok()).toBeTruthy()
  await denyAll(await pendingFor())
  expect(await pendingFor()).toEqual([])
}

test.describe('review (real backend)', () => {
  test.describe.configure({ mode: 'serial' })

  test.beforeEach(async ({}, testInfo) => {
    test.skip(!isReal(testInfo), 'drives the real backend; the mock lane is above')
  })

  test('a worktree session is reviewed, committed and discarded end to end', async ({ page }) => {
    const { sessionId } = await seedWorktreeSession(page, 'Write hello')

    // The sessions list's badge comes from sessions.diff_add/diff_del, which
    // the worktree bookkeeping wrote after the turn: three added lines, none
    // removed.
    await page.goto('/sessions')
    const row = page.locator(`a[data-testid="session-row"][href$="${sessionId}"]`)
    const badge = row.getByTestId('diff-badge')
    await expect(badge).toContainText('+3')
    await expect(badge).toContainText(`${MINUS}0`)

    // The Review tab lists the one file the fake wrote, as an addition.
    await page.goto(`/sessions/${sessionId}`)
    await expect(page.getByTestId('session-view')).toBeVisible()
    await page.getByRole('tab', { name: 'Review' }).click()
    const files = page.getByTestId('review-file')
    await expect(files).toHaveCount(1)
    await expect(files.first()).toContainText(WORKTREE_FILE)
    await expect(files.first()).toContainText('A')
    await expect(files.first()).toContainText('+3')

    // GET patch is a plain download, not JSON: assert the content type the
    // "Download patch" link relies on.
    const patch = await page.request.get(`/api/v1/sessions/${sessionId}/patch`, { headers: HEADERS })
    expect(patch.status()).toBe(200)
    expect(patch.headers()['content-type']).toContain('text/x-diff')
    expect(await patch.text()).toContain(WORKTREE_FILE)

    // Opening it shows one all-add hunk: a new file has no old side, so no
    // line number on the old side is commentable.
    await files.first().click()
    await expect(page.getByTestId('diff-view')).toBeVisible()
    const hunks = page.getByTestId('diff-hunk')
    await expect(hunks).toHaveCount(1)
    await expect(hunks.first()).toContainText('@@ -0,0 +1,3 @@')
    await expect(page.getByTestId('diff-view')).toContainText('func main() {}')
    await expect(page.getByRole('button', { name: /Comment on old line/ })).toHaveCount(0)

    // An inline comment on line 1 becomes the session's next prompt.
    await page.getByRole('button', { name: 'Comment on new line 1' }).click()
    const draft = page.getByTestId('comment-draft')
    await expect(draft).toBeVisible()
    await draft.fill(COMMENT)
    await page.getByRole('button', { name: 'Add comment' }).click()
    await expect(page.getByTestId('review-comment').first()).toContainText(COMMENT)

    await page.getByRole('button', { name: 'Send review' }).click()
    await page.getByRole('button', { name: 'Back to transcript' }).click()
    await expect(page.getByTestId('transcript')).toContainText(`${WORKTREE_FILE}:1 (new): ${COMMENT}`)

    // That review was a turn of its own; the actions below are refused while
    // it runs.
    await waitForState(page, sessionId, ['open'])

    // One checkpoint: the turn that wrote the file. The review turn changed
    // nothing on disk, so it left none.
    await page.getByRole('button', { name: 'Checkpoints' }).click()
    const menu = page.getByTestId('checkpoints-menu')
    await expect(menu).toBeVisible()
    await expect(menu.getByTestId('checkpoint-row')).toHaveCount(1)

    // Rewinding asks first, then resets the worktree to that checkpoint - the
    // state it is already in, so the file survives for the commit below.
    await menu.getByTestId('checkpoint-row').first().click()
    const rewind = page.getByRole('dialog')
    await expect(rewind).toContainText('Rewind')
    await rewind.getByRole('button', { name: 'Rewind to here' }).click()
    await expect(page.getByTestId('toast')).toContainText('Rewound to turn')
    await waitForState(page, sessionId, ['open'])

    // Commit folds the checkpoint and the working tree into one commit.
    await page.getByRole('button', { name: 'Commit' }).click()
    const commit = page.getByRole('dialog')
    await expect(commit.getByLabel('Message')).toHaveValue('Write hello')
    await commit.getByLabel('Message').fill('Add a hello program')
    await commit.getByRole('button', { name: 'Commit changes' }).click()
    await expect(page.getByTestId('toast').first()).toContainText(/Committed [0-9a-f]{7}/)

    // The temp repo has no remote, so pushing the branch is refused with 409
    // no_remote - explained in the dialog rather than thrown away in a toast.
    await page.getByRole('button', { name: 'Open PR' }).click()
    const pr = page.getByRole('dialog')
    await pr.getByRole('button', { name: 'Create pull request' }).click()
    await expect(pr.getByTestId('pr-error')).toContainText('remote')
    await expect(pr.getByRole('link', { name: /REVIEW\.md/ })).toBeVisible()
    await pr.getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByRole('dialog')).toBeHidden()

    // Discard takes the worktree, the branch and the session with it.
    await page.getByRole('button', { name: 'Discard changes' }).click()
    const discard = page.getByRole('dialog')
    await expect(discard).toContainText('Discard every change?')
    await discard.getByRole('button', { name: 'Discard changes' }).click()
    await expect(page.getByRole('dialog')).toBeHidden()

    await waitForState(page, sessionId, ['closed'])
    await expect(page.getByTestId('review-files')).toHaveCount(0)
    await expect(page.getByText('This session does not run in a worktree.')).toBeVisible()
  })

  test('a plan-mode session shows its plan and continues once it is approved', async ({ page }) => {
    const sessionId = await seedPlanSession(page, 'Plan a version flag')

    await page.goto(`/sessions/${sessionId}`)
    await expect(page.getByTestId('session-view')).toBeVisible()

    const card = page.getByTestId('plan-card')
    await expect(card).toBeVisible()
    await expect(card).toContainText('--version')
    // Fixture 08's plan is prose with numbered steps, not a "- [ ]"
    // checklist, so it renders as markdown rather than as plan-step items -
    // the checklist rendering is what the mock lane above covers.
    await expect(card).toContainText('Approach')

    // The pointer starts at 0,0, which hovers the rail open over the left edge
    // of the page (components/shell/Rail.tsx); move it off before clicking a
    // control that sits under it.
    await page.mouse.move(700, 400)
    await card.getByRole('button', { name: 'Approve plan' }).click()
    await expect(card).toHaveCount(0)

    // Fixture 08 carries on with ordinary Write permission requests, so the
    // session either asks again or settles - both prove the plan itself is
    // no longer what it is blocked on.
    await waitForState(page, sessionId, ['waiting', 'open'])
    await expect(page.getByTestId('permission-card').or(page.getByTestId('composer-input')).first()).toBeVisible()

    // Fixture 08 asks twice more after the plan; nothing may be left pending.
    await drainApprovals(page, sessionId)
  })
})
