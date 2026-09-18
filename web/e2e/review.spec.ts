import { test, expect, type Page } from '@playwright/test'
import { isReal } from './helpers/seed'

// T43: the review surface (diff, inline comments, commit, PR, checkpoints,
// plan approval) against the msw mock. The backend for all of it arrives in
// T42, so every test here skips on the real projects.
//
// The mock's own state lives in the page (msw's service worker forwards each
// request back to the page runtime), so the control routes below have to be
// called with page.evaluate — a Playwright-side page.request would bypass the
// worker entirely — and a reload would re-seed the module and undo them. Same
// constraint e2e/sessions.spec.ts documents for /__mock/clear-workspaces.
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

test.beforeEach(async ({}, testInfo) => {
  test.skip(isReal(testInfo), 'backend arrives in T42')
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
