/* oxlint-disable next/no-html-link-for-pages -- Shared same-origin home link. */

export function TammyMark({ className }: { className?: string }) {
  return (
    <span className={['tammy-brand-mark', className].filter(Boolean).join(' ')} aria-hidden="true">
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
      className={['tammy-brand', className].filter(Boolean).join(' ')}
      aria-label="Tammy home"
    >
      <TammyMark />
      <span>Tammy</span>
    </a>
  );
}
