// "Run a template | Run a pipeline". A trigger and a schedule each start
// exactly one of the two, so this is a choice between two states rather than
// a list to pick from - two pressed-state buttons, the same segmented shape
// the runs list uses for its outcome filter.
import clsx from 'clsx'
import { Button } from '../ui'

export type RunTarget = 'template' | 'pipeline'

export function TargetSelector({
  value,
  onChange,
  className,
}: {
  value: RunTarget
  onChange: (value: RunTarget) => void
  className?: string
}) {
  return (
    <div role="group" aria-label="What this starts" className={clsx('flex gap-1.5', className)}>
      <Button
        type="button"
        size="sm"
        variant={value === 'template' ? 'primary' : 'secondary'}
        aria-pressed={value === 'template'}
        onClick={() => onChange('template')}
      >
        Run a template
      </Button>
      <Button
        type="button"
        size="sm"
        variant={value === 'pipeline' ? 'primary' : 'secondary'}
        aria-pressed={value === 'pipeline'}
        onClick={() => onChange('pipeline')}
      >
        Run a pipeline
      </Button>
    </div>
  )
}
