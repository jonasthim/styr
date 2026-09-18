// Shared vocabulary for a changed file: the status letter and the +/− counts.
// One place so the file list, the diff header and the commit dialog all say
// the same thing about the same file.
import clsx from 'clsx'
import type { FileStatus } from '../../api/types'

const STATUS_LABEL: Record<FileStatus, string> = {
  A: 'Added',
  M: 'Modified',
  D: 'Deleted',
  R: 'Renamed',
}

const STATUS_CLASS: Record<FileStatus, string> = {
  A: 'border-state-running/40 bg-state-running/10 text-state-running',
  M: 'border-[var(--accent-border)] bg-accent-subtle text-accent',
  D: 'border-state-failed/40 bg-state-failed/10 text-fg-danger',
  R: 'border-state-attention/40 bg-state-attention/10 text-state-attention',
}

export function StatusChip({ status, className }: { status: FileStatus; className?: string }) {
  return (
    <span
      title={STATUS_LABEL[status]}
      aria-label={STATUS_LABEL[status]}
      className={clsx(
        'inline-flex h-4 w-4 shrink-0 items-center justify-center rounded-[var(--radius-1)] border',
        'font-mono text-[10px] font-semibold leading-none',
        STATUS_CLASS[status],
        className,
      )}
    >
      {status}
    </span>
  )
}

/** `+42 −18`, in the mono tabular shape every number in Styr that moves is
 * set in. Green and red carry the sign too, so the pair reads at a glance. */
export function DiffCount({ add, del, className }: { add: number; del: number; className?: string }) {
  return (
    <span className={clsx('shrink-0 font-mono text-[11px] tabular-nums', className)}>
      <span className="text-state-running">+{add}</span>{' '}
      <span className="text-fg-danger">{'−'}{del}</span>
    </span>
  )
}
