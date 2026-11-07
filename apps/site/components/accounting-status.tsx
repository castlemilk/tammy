import { cn } from '../lib/utils';

const labels = {
  draft: 'Draft',
  reviewed: 'Reviewed',
  posted: 'Posted',
  unmatched: 'Not matched',
  issued: 'Issued',
} as const;

/** Static record label, not a live announcement or a change in ledger state. */
export function AccountingStatus({
  status,
  className,
}: {
  status: keyof typeof labels;
  className?: string;
}) {
  return (
    <span className={cn('accounting-status', className)} data-state={status}>
      {labels[status]}
    </span>
  );
}
