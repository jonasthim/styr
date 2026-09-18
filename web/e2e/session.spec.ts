import { test, expect } from '@playwright/test'

// Runs against the mock backend (msw handlers; see web/src/mocks/handlers.ts).
// The mock seeds a fixed session (TOOL_FIXTURE_SESSION_ID there) whose
// transcript is fixture 02 (internal/harness/claude/testdata/02_tool_read.jsonl,
// decoded into harness.Event JSON by web/src/mocks/decodeFixture.ts): a Read
// tool call on note.txt, followed by the text reply "hello".
const SESSION_ID = '00000000-0000-4000-8000-000000000005'

test('opening the session shows the tool block and the text reply', async ({ page }) => {
  await page.goto(`/sessions/${SESSION_ID}`)

  await expect(page.getByTestId('session-view')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Read note.txt' })).toBeVisible()

  const toolHeader = page.getByRole('button', { expanded: false }).filter({ hasText: 'Read' })
  await expect(toolHeader).toBeVisible()

  await expect(page.getByText('hello', { exact: true })).toBeVisible()
})

test('clicking the tool block expands the result panel', async ({ page }) => {
  await page.goto(`/sessions/${SESSION_ID}`)

  const toolHeader = page.getByRole('button', { expanded: false }).filter({ hasText: 'Read' })
  await toolHeader.click()

  await expect(page.getByText('Result')).toBeVisible()
  await expect(page.getByTestId('tool-result')).toContainText('hello')
})

test('sending a message shows it as a user block', async ({ page }) => {
  await page.goto(`/sessions/${SESSION_ID}`)

  const input = page.getByTestId('composer-input')
  await input.fill('again')
  await page.getByTestId('composer-send').click()

  await expect(page.getByText('again', { exact: true })).toBeVisible()
  await expect(input).toHaveValue('')
})
