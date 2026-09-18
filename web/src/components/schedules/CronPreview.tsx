// The live preview under the cron input: one sentence saying what the
// expression means, then the next five times it would fire. Debounced
// against POST /schedules/preview so typing a cron doesn't hammer the
// endpoint, and deliberately a read of the server's own answer rather than a
// second implementation in the browser - the scheduler's clock is the one
// that matters.
import { useQuery } from '@tanstack/react-query'
import { api } from '../../api/client'
import type { CronPreview as CronPreviewResult } from '../../api/types'
import { useDebounced } from '../../hooks/useDebounced'
import { absoluteTime, previewTime, untilTime } from './chips'

export function CronPreview({ cron }: { cron: string }) {
  const debounced = useDebounced(cron.trim(), 350)
  const preview = useQuery({
    queryKey: ['cron-preview', debounced],
    queryFn: () => api<CronPreviewResult>('/api/v1/schedules/preview', { method: 'POST', json: { cron: debounced } }),
    enabled: debounced.length > 0,
  })

  if (!debounced) {
    return (
      <p className="text-[12px] text-fg-muted">
        Five fields: minute, hour, day of month, month, day of week. <code className="font-mono">@daily</code> works too.
      </p>
    )
  }

  if (preview.data?.error) {
    return (
      <p role="alert" className="text-[12px] text-fg-danger">
        {preview.data.error}
      </p>
    )
  }

  return (
    <div
      data-testid="cron-preview"
      className="rounded-[var(--radius-control)] border border-hairline bg-surface-1 px-3 py-2.5"
    >
      <p className="text-[12px] font-medium text-fg-primary">{preview.data?.description ?? 'Working it out…'}</p>
      <ol className="mt-2 flex flex-col gap-1">
        {(preview.data?.next ?? []).map((at) => (
          <li
            key={at}
            data-testid="cron-preview-time"
            className="flex items-baseline justify-between gap-3 font-mono text-[11px] tabular-nums text-fg-secondary"
          >
            <span title={absoluteTime(at)}>{previewTime(at)}</span>
            <span className="shrink-0 text-fg-muted">{untilTime(at)}</span>
          </li>
        ))}
      </ol>
      <p className="mt-2 text-[11px] text-fg-muted">Times are the server's.</p>
    </div>
  )
}
