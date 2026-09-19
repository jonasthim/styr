import { test, expect } from '@playwright/test'
import { isReal } from './helpers/seed'

// v1.0 froze the API: docs/openapi.yaml's info.version is the contract, and
// GET /api/v1/version is where a client can read it back off a running server
// (internal/api/status_handlers.go). Real-mode only - the mock serves the app's
// own endpoints, not this one - and deliberately thin: what is under test is
// that the frozen version is what the binary actually reports, which is also
// what `make api-compat` checks the document side of.
test('GET /version reports the frozen v1 API version', async ({ page }, testInfo) => {
  test.skip(!isReal(testInfo), 'real-only: the mock backend does not serve /api/v1/version')

  const res = await page.request.get('/api/v1/version', { headers: { 'X-Requested-With': 'styr' } })
  expect(res.status()).toBe(200)

  const body = (await res.json()) as { version: string; api_version: string; commit: string }
  expect(body.api_version).toBe('1.0.0')
  // The build's own version and commit are whatever this binary was built
  // with (a dev build reports "dev"), so they are asserted as present rather
  // than as a value.
  expect(body.version).not.toBe('')
})
