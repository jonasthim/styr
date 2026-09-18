import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './app'
import './styles/tokens.css'
import './styles/base.css'

// Dynamic import: under a normal build (VITE_MOCK unset) this branch is
// never taken, so msw and its handlers/fixtures are not in the production
// bundle at all.
async function enableMocking(): Promise<void> {
  if (import.meta.env.VITE_MOCK !== '1') return
  const { worker } = await import('./mocks/browser')
  await worker.start({ onUnhandledRequest: 'bypass' })
}

const root = document.getElementById('root')
if (!root) {
  throw new Error('root element not found')
}

void enableMocking().then(() => {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
})
