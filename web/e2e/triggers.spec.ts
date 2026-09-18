// Triggers, Templates and Notifications (T33): create a trigger and see its
// secret once, toggle it, replay a delivery, edit a template and watch its
// preview update, and add + test a notification channel - all against the
// mock backend, whose seeded triggers, deliveries and runs cover states the
// real pipeline cannot be pushed into quickly (storm, replay chains). The
// "Triggers (real backend)" block at the bottom drives the same contract
// against the running server (T37); see runs.spec.ts for the Runs page.
import { test, expect } from '@playwright/test'
import {
  ensureRealGrafanaTemplate,
  ensureRealServiceToken,
  grafanaSamplePayload,
  isReal,
  waitForRunOutcome,
} from './helpers/seed'

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

// --- real backend ----------------------------------------------------------
// The v0.2 pipeline end to end against the running server: a trigger created
// through the UI, a Grafana payload delivered to /hooks/{slug} with the
// secret the "Webhook ready" panel showed once, and the unattended run that
// comes out of it. Serial, and each test builds on the trigger the first one
// created - the real projects share one backend and one database for the
// whole run (workers: 1, see playwright.config.ts).
test.describe('Triggers (real backend)', () => {
  test.describe.configure({ mode: 'serial' })

  // Captured by the first test from the create dialog, used by the rest.
  let created: { id: string; name: string; slug: string; secret: string } | null = null

  test('create a grafana trigger and capture its one-time secret', async ({ page }, testInfo) => {
    test.skip(!isReal(testInfo), 'real-backend only: drives the actual trigger pipeline')
    await ensureRealServiceToken(page)
    const template = await ensureRealGrafanaTemplate(page)

    const name = `Prod Grafana alerts ${Date.now()}`
    await page.goto('/triggers')
    await expect(page.getByRole('heading', { name: 'Triggers' })).toBeVisible()

    await page.getByRole('button', { name: 'New trigger' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('Name').fill(name)
    // Kind defaults to Grafana.
    await dialog.getByRole('combobox', { name: 'Template' }).click()
    await page.getByRole('option', { name: template.name }).click()
    await dialog.getByRole('button', { name: 'Create trigger' }).click()

    await expect(dialog.getByRole('heading', { name: 'Webhook ready' })).toBeVisible()
    const panel = page.getByTestId('webhook-ready')
    const url = (await panel.locator('code').first().textContent())?.trim() ?? ''
    expect(url).toContain('/hooks/')
    const secret = (await panel.locator('code', { hasText: 'styr_whs_' }).textContent())?.trim() ?? ''
    expect(secret).toMatch(/^styr_whs_/)

    await dialog.getByRole('button', { name: 'Done' }).click()
    const row = page.getByTestId(/^trigger-row-/).filter({ hasText: name })
    await expect(row).toBeVisible()
    const id = (await row.getAttribute('data-testid'))?.replace('trigger-row-', '') ?? ''
    expect(id).not.toBe('')

    created = { id, name, slug: url.slice(url.lastIndexOf('/hooks/') + '/hooks/'.length), secret }
  })

  test('delivering a Grafana payload starts a run that reaches success with a report', async ({ page }, testInfo) => {
    test.skip(!isReal(testInfo), 'real-backend only: drives the actual trigger pipeline')
    expect(created).not.toBeNull()
    const trigger = created!

    // The inbound hook is outside /api/v1: no cookie and no CSRF header, the
    // trigger's own secret is the whole credential.
    const res = await page.request.post(`/hooks/${trigger.slug}`, {
      headers: { Authorization: `Bearer ${trigger.secret}` },
      data: await grafanaSamplePayload(page),
    })
    expect(res.status()).toBe(202)
    const delivery = (await res.json()) as { delivery_id: string; status: string; run_id?: string }
    expect(delivery.status).toBe('accepted')
    expect(delivery.run_id).toBeTruthy()

    // The fake CLI replays fixture 06, whose result carries the structured
    // output the run engine stores as the report.
    const view = await waitForRunOutcome(page, delivery.run_id!)
    expect(view.run.outcome).toBe('success')

    await page.goto(`/runs/${delivery.run_id!}`)
    const report = page.getByTestId('run-report')
    await expect(report.getByText('warning', { exact: true })).toBeVisible()
    await expect(report.getByText(/Disk on host x at 91%/)).toBeVisible()
    await expect(page.getByText('Delivery payload')).toBeVisible()

    await page.getByRole('link', { name: 'Open session' }).click()
    await expect(page).toHaveURL(/\/sessions\//)
    await expect(page.getByText('Unattended run')).toBeVisible()
  })

  test('a second identical delivery is held back and shows as cooldown', async ({ page }, testInfo) => {
    test.skip(!isReal(testInfo), 'real-backend only: drives the actual trigger pipeline')
    expect(created).not.toBeNull()
    const trigger = created!

    const res = await page.request.post(`/hooks/${trigger.slug}`, {
      headers: { Authorization: `Bearer ${trigger.secret}` },
      data: await grafanaSamplePayload(page),
    })
    expect(res.status()).toBe(202)
    const delivery = (await res.json()) as { status: string; run_id?: string }
    expect(delivery.status).toBe('cooldown')
    expect(delivery.run_id).toBeUndefined()

    await page.goto('/triggers')
    const row = page.getByTestId(`trigger-row-${trigger.id}`)
    await row.getByRole('button', { name: 'Deliveries' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: `Deliveries — ${trigger.name}` })).toBeVisible()
    await expect(page.getByTestId('delivery-row').filter({ hasText: 'Cooldown' }).first()).toBeVisible()
    await expect(page.getByTestId('delivery-row').filter({ hasText: 'Accepted' }).first()).toBeVisible()
  })

  test('a wrong secret is rejected and a disabled trigger is invisible', async ({ page }, testInfo) => {
    test.skip(!isReal(testInfo), 'real-backend only: drives the actual trigger pipeline')
    expect(created).not.toBeNull()
    const trigger = created!
    const payload = await grafanaSamplePayload(page)

    const wrong = await page.request.post(`/hooks/${trigger.slug}`, {
      headers: { Authorization: 'Bearer styr_whs_not-the-secret' },
      data: payload,
    })
    expect(wrong.status()).toBe(401)

    // Disabling it through the UI makes the slug answer 404: a sender learns
    // nothing about which slugs exist (internal/triggers/deliver.go).
    await page.goto('/triggers')
    const toggle = page.getByRole('switch', { name: `Enabled for ${trigger.name}` })
    await expect(toggle).toHaveAttribute('aria-checked', 'true')
    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-checked', 'false')

    const disabled = await page.request.post(`/hooks/${trigger.slug}`, {
      headers: { Authorization: `Bearer ${trigger.secret}` },
      data: payload,
    })
    expect(disabled.status()).toBe(404)

    await toggle.click()
    await expect(toggle).toHaveAttribute('aria-checked', 'true')
  })

  test('testing an unreachable webhook channel fails without leaking its token', async ({ page }, testInfo) => {
    test.skip(!isReal(testInfo), 'real-backend only: sends through the real notifier')

    // Playwright cannot host a receiver the server could reach, so the channel
    // points at a port nothing listens on and the assertion is on the
    // documented failure contract instead (internal/api/notifications_handlers.go:
    // 502 notify_failed, and never the token in any response).
    const name = `Escalation webhook ${Date.now()}`
    await page.goto('/settings')
    await page.getByRole('button', { name: 'Add channel' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.getByLabel('Name').fill(name)
    await dialog.getByLabel('URL').fill('http://127.0.0.1:1')
    await dialog.getByRole('combobox', { name: 'Kind' }).click()
    await page.getByRole('option', { name: 'Generic webhook' }).click()
    await dialog.getByRole('button', { name: 'Add channel' }).click()
    await expect(page.getByRole('dialog')).toBeHidden()

    const row = page.getByTestId(/^notification-row-/).filter({ hasText: name })
    await expect(row).toBeVisible()
    const id = (await row.getAttribute('data-testid'))?.replace('notification-row-', '') ?? ''

    // A token the channel would send along, so "no token in the response" is
    // an assertion with something to catch.
    const token = 'styr-e2e-notify-token-9f2a71'
    const patch = await page.request.patch(`/api/v1/notifications/${id}`, {
      headers: { 'X-Requested-With': 'styr' },
      data: { token },
    })
    expect(patch.status()).toBe(200)
    expect(await patch.text()).not.toContain(token)

    const tested = await page.request.post(`/api/v1/notifications/${id}/test`, { headers: { 'X-Requested-With': 'styr' } })
    expect(tested.status()).toBe(502)
    const body = await tested.text()
    expect(body).toContain('notify_failed')
    expect(body).not.toContain(token)

    // The UI reports the same failure rather than a silent success.
    await row.getByRole('button', { name: 'Test' }).click()
    await expect(page.getByTestId('toast').filter({ hasText: 'Test notification failed' })).toBeVisible()
  })
})
