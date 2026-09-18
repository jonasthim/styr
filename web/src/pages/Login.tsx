import { useQuery } from '@tanstack/react-query'
import { q } from '../api/queries'

// Dark canvas, one centred column, no marketing. Layout and copy follow the
// Login entry in docs/specs/2026-09-18-styr-design.md ("Information
// architecture" #12) and the "Design system" section.
export function Login() {
  const providers = useQuery(q.providers())
  const error = new URLSearchParams(window.location.search).get('error')

  return (
    <main className="flex min-h-screen items-center justify-center bg-canvas px-4">
      <div className="w-full max-w-[360px]">
        <div className="mb-8 text-center">
          <h1 className="text-[28px] font-semibold tracking-[-0.02em] text-fg-primary">Styr</h1>
          <p className="mt-2 text-[13px] text-fg-secondary">Mission control for your coding agents</p>
        </div>

        {error && (
          <div role="alert" className="mb-4 rounded-[var(--radius-2)] border border-hairline bg-surface-1 p-3">
            <p className="text-[13px] text-fg-primary">Sign-in failed</p>
            <p className="mt-1 text-[12px] text-fg-secondary">
              Try again, or contact your administrator if this keeps happening.
            </p>
            <p className="mt-1 font-mono text-[11px] text-fg-muted">error: {error}</p>
          </div>
        )}

        <div className="flex flex-col gap-2">
          {providers.data?.map((provider) => (
            <a
              key={provider.slug}
              href={`/api/v1/auth/login/${provider.slug}`}
              className="flex h-10 items-center justify-center rounded-[var(--radius-2)] border border-hairline bg-surface-1 text-[13px] font-medium text-fg-primary transition-colors duration-150 hover:bg-surface-2"
            >
              Continue with {provider.name}
            </a>
          ))}
          {providers.isSuccess && providers.data.length === 0 && (
            <p className="text-center text-[13px] text-fg-muted">No sign-in providers are configured.</p>
          )}
        </div>
      </div>
    </main>
  )
}
