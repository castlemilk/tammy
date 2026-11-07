/* oxlint-disable next/no-html-link-for-pages -- Shared same-origin home link. */
import { cn } from '../lib/utils';

export function TammyMark({ className }: { className?: string }) {
  return (
    <span className={cn('tammy-brand-mark', className)} aria-hidden="true">
      <span />
      <span />
    </span>
  );
}

/** One wordmark for marketing, tools, guides and the Journal. */
export function SiteBrand({ className }: { className?: string }) {
  return (
    <a
      href="/"
      className={cn('tammy-brand', className)}
      aria-label="Tammy home"
    >
      <TammyMark />
      <span>Tammy</span>
    </a>
  );
}
