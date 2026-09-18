// Label + control + hint/error, with the ids wired up. Children is a render
// function so the control gets the generated id and aria-describedby rather
// than the component guessing at them by cloning.
import { useId, type ReactNode } from 'react'
import clsx from 'clsx'

export interface FieldRenderArgs {
  id: string
  'aria-describedby': string | undefined
  'aria-invalid': boolean | undefined
}

export interface FieldProps {
  label: ReactNode
  hint?: ReactNode
  error?: ReactNode
  /** Right-aligned note on the label line, e.g. "optional". */
  labelAside?: ReactNode
  className?: string
  children: (args: FieldRenderArgs) => ReactNode
}

export function Field({ label, hint, error, labelAside, className, children }: FieldProps) {
  const id = useId()
  const messageId = `${id}-message`
  const message = error ?? hint

  return (
    <div className={clsx('flex flex-col gap-1.5', className)}>
      <div className="flex items-baseline justify-between gap-2">
        <label htmlFor={id} className="text-[12px] font-medium text-fg-secondary">
          {label}
        </label>
        {labelAside && <span className="text-[11px] text-fg-muted">{labelAside}</span>}
      </div>
      {children({
        id,
        'aria-describedby': message ? messageId : undefined,
        'aria-invalid': error ? true : undefined,
      })}
      {message && (
        <p
          id={messageId}
          role={error ? 'alert' : undefined}
          className={clsx('text-[12px]', error ? 'text-fg-danger' : 'text-fg-muted')}
        >
          {message}
        </p>
      )}
    </div>
  )
}
