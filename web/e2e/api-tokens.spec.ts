// Personal API tokens (T36): create, see the raw secret exactly once, list
// shows only its prefix, then revoke. Runs against both backends - the
// real backend's handlers (internal/api/api_tokens_handlers.go) are part of
// this same card, unlike most other UI cards whose real-mode e2e waits for
// a later integration wave.
import { test, expect } from '@playwright/test'

test.describe('Profile - API tokens', () => {
  test('create a token, see it once, list shows only its prefix, then revoke', async ({ page }, testInfo) => {
    const name = `e2e-token-${testInfo.project.name}-${Date.now()}`

    await page.goto('/profile')
    await expect(page.getByRole('heading', { name: 'Profile' })).toBeVisible()

    const card = page.getByTestId('api-tokens-card')
    await expect(card).toBeVisible()
    await card.getByRole('button', { name: 'Create token' }).click()

    const dialog = page.getByRole('dialog')
    await expect(dialog.getByRole('heading', { name: 'Create token' })).toBeVisible()
    await dialog.getByLabel('Name').fill(name)
    await dialog.getByRole('button', { name: 'Create token' }).click()

    // The raw token is shown exactly once, right here.
    await expect(dialog.getByRole('heading', { name: 'Token created' })).toBeVisible()
    await expect(dialog.getByTestId('api-token-warning')).toBeVisible()
    const tokenValue = (await dialog.getByTestId('api-token-value').innerText()).trim()
    expect(tokenValue.startsWith('styr_pat_')).toBe(true)
    expect(tokenValue.length).toBeGreaterThan('styr_pat_'.length)

    await dialog.getByRole('button', { name: 'Done' }).click()
    await expect(dialog).toBeHidden()

    // The list shows the new row with only its prefix - never the full
    // secret shown in the dialog above.
    const row = page.locator('[data-testid^="api-token-row-"]').filter({ hasText: name })
    await expect(row).toBeVisible()
    const rowText = await row.innerText()
    expect(rowText).not.toContain(tokenValue)
    // The prefix cell (rendered as "<12-char prefix>…") is a real prefix of
    // the token shown above, not some unrelated placeholder.
    const prefixCellText = (await row.locator('td').nth(1).innerText()).replace('…', '').trim()
    expect(prefixCellText.length).toBeGreaterThan(0)
    expect(prefixCellText.length).toBeLessThan(tokenValue.length)
    expect(tokenValue.startsWith(prefixCellText)).toBe(true)

    // Revoke, with confirmation.
    await row.getByRole('button', { name: `Revoke ${name}` }).click()
    const confirmDialog = page.getByRole('dialog')
    await expect(confirmDialog.getByRole('heading', { name: `Revoke ${name}?` })).toBeVisible()
    await confirmDialog.getByRole('button', { name: 'Revoke', exact: true }).click()
    await expect(confirmDialog).toBeHidden()
    await expect(row).toHaveCount(0)
  })

  test('creating a token without a name is blocked client-side', async ({ page }) => {
    await page.goto('/profile')
    await expect(page.getByRole('heading', { name: 'Profile' })).toBeVisible()

    const card = page.getByTestId('api-tokens-card')
    await card.getByRole('button', { name: 'Create token' }).click()

    const dialog = page.getByRole('dialog')
    const submit = dialog.getByRole('button', { name: 'Create token' })
    await expect(submit).toBeDisabled()
  })
})
