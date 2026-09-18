// Triggers, Templates and Notifications (T33): create a trigger and see its
// secret once, toggle it, replay a delivery, edit a template and watch its
// preview update, and add + test a notification channel. Mock only for now
// (real-mode backend for this contract arrives in T34/T35); see runs.spec.ts
// for the Runs page and the phone-layout assertion.
import { test, expect } from '@playwright/test'
import { isReal } from './helpers/seed'

// Fixed mock ids (web/src/mocks/triggersHandlers.ts) - duplicated as
// literals rather than imported: e2e specs run outside Vite, so they can't
// pull in a module that imports other mock modules (see handlers.ts's own
// comment on TOOL_FIXTURE_SESSION_ID for the same reason).
const TEMPLATE_GRAFANA_ID = 'tmpl-grafana'
const TRIGGER_GRAFANA_NAME = 'Prod Grafana alerts'

test.describe('Triggers', () => {
  test('create a trigger and see the secret once', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T35')
    await page.goto('/triggers')
    await expect(page.getByRole('heading', { name: 'Triggers' })).toBeVisible()

    await page.getByRole('button', { name: 'New trigger' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: 'New trigger' })).toBeVisible()

    await dialog.getByLabel('Name').fill('Nightly backup check')
    // Kind defaults to Grafana, which is what we want (exercises the
    // contact-point snippet below).
    await dialog.getByRole('combobox', { name: 'Template' }).click();
    await page.getByRole('option', { name: 'Grafana alert investigation' }).click()

    await dialog.getByRole('button', { name: 'Create trigger' }).click()

    await expect(dialog.getByRole('heading', { name: 'Webhook ready' })).toBeVisible()
    const panel = page.getByTestId('webhook-ready')
    await expect(panel.locator('code').first()).toContainText('/hooks/nightly-backup-check')
    // The secret is only ever shown here - it starts with the plan's fixed
    // webhook secret prefix.
    await expect(panel.locator('code', { hasText: 'styr_whs_' })).toBeVisible()
    await expect(panel.getByText('Grafana contact point')).toBeVisible()
    await expect(panel.getByText('This is the only time the secret is shown')).toBeVisible()

    await dialog.getByRole('button', { name: 'Done' }).click()
    await expect(page.getByRole('dialog')).toBeHidden()

    await expect(page.getByTestId(/^trigger-row-/).filter({ hasText: 'Nightly backup check' })).toBeVisible()
  })

  test('toggle a trigger enabled', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T35')
    await page.goto('/triggers')
    const toggle = page.getByRole('switch', { name: `Enabled for ${TRIGGER_GRAFANA_NAME}` })
    await expect(toggle).toHaveAttribute('aria-checked', 'true')

    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-checked', 'false')

    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-checked', 'true')
  })

  test('open deliveries and replay one', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T35')
    await page.goto('/triggers')

    const row = page.getByTestId(/^trigger-row-/).filter({ hasText: TRIGGER_GRAFANA_NAME })
    await row.getByRole('button', { name: 'Deliveries' }).click()

    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: `Deliveries — ${TRIGGER_GRAFANA_NAME}` })).toBeVisible()

    // Every status the plan's delivery pipeline can produce is represented.
    for (const status of ['Accepted', 'Deduped', 'Cooldown', 'Storm']) {
      await expect(dialog.getByText(status, { exact: true }).first()).toBeVisible()
    }

    const acceptedRow = page.getByTestId('delivery-row').filter({ hasText: 'Accepted' }).first()
    const deliveriesBefore = await page.getByTestId('delivery-row').count()
    await acceptedRow.getByRole('button', { name: 'Replay' }).click()

    await expect(page.getByTestId('toast').filter({ hasText: 'Replay started' })).toBeVisible()
    await expect(page.getByTestId('delivery-row')).toHaveCount(deliveriesBefore + 1)
  })

  test('send a test payload and follow it to the run it started', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T35')
    await page.goto('/triggers')

    // The generic trigger has no dedupe key template, so its test payload is
    // always accepted - a clean, uncomplicated path to a run link.
    const row = page.getByTestId(/^trigger-row-/).filter({ hasText: 'Ad hoc webhook' })
    await row.getByRole('button', { name: 'Send test' }).click()

    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: /Send test payload/ })).toBeVisible()
    await expect(dialog.getByLabel('Payload')).not.toHaveValue('')

    await dialog.getByRole('button', { name: 'Send' }).click()
    const result = page.getByTestId('test-payload-result')
    await expect(result).toBeVisible()
    await expect(result.getByText('Accepted')).toBeVisible()

    await result.getByRole('link', { name: 'View run' }).click()
    await expect(page).toHaveURL(/\/runs\/.+/)
    await expect(page.getByTestId('run-report')).toBeVisible()
  })

  test('editing a template updates its preview', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T35')
    await page.goto(`/templates/${TEMPLATE_GRAFANA_ID}`)
    await expect(page.getByRole('heading', { name: 'Grafana alert investigation' })).toBeVisible()

    const previewPrompt = page.getByTestId('template-preview-prompt')
    await expect(previewPrompt).toContainText('HighMemoryUsage')

    await page.getByLabel('Prompt template').fill('A distinctly different prompt for {{ .status }}.')
    await expect(previewPrompt).toHaveText('A distinctly different prompt for firing.', { timeout: 3000 })
  })

  test('add a notification channel and send a test', async ({ page }, testInfo) => {
    test.skip(isReal(testInfo), 'backend arrives in T35')
    await page.goto('/settings')
    await expect(page.getByRole('heading', { name: 'Settings' })).toBeVisible()

    await page.getByRole('button', { name: 'Add channel' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('Name').fill('Escalation webhook')
    await dialog.getByLabel('URL').fill('https://example.com/hook')
    await dialog.getByRole('combobox', { name: 'Kind' }).click()
    await page.getByRole('option', { name: 'Generic webhook' }).click()
    await dialog.getByRole('button', { name: 'Add channel' }).click()
    await expect(page.getByRole('dialog')).toBeHidden()

    const row = page.getByTestId(/^notification-row-/).filter({ hasText: 'Escalation webhook' })
    await expect(row).toBeVisible()
    await row.getByRole('button', { name: 'Test' }).click()
    await expect(page.getByTestId('toast').filter({ hasText: 'Test notification sent' })).toBeVisible()
  })
})
