interface EmptyLedgerScreenProps {
  readonly description: string;
  readonly emptyLabel: string;
  readonly title: string;
}

export function EmptyLedgerScreen({ description, emptyLabel, title }: EmptyLedgerScreenProps) {
  return (
    <div className="mx-auto grid w-full max-w-[920px] gap-5">
      <div>
        <h1 className="text-[18px] font-semibold tracking-[-0.02em] text-foreground">{title}</h1>
        <p className="mt-1 text-[11px] leading-5 text-muted-foreground">{description}</p>
      </div>
      <section className="overflow-hidden rounded-[6px] border border-border bg-surface">
        <div className="grid min-h-64 place-items-center px-5 py-10 text-center">
          <div>
            <p className="text-[12px] font-semibold text-foreground">{emptyLabel}</p>
            <p className="mt-1 max-w-sm text-[10px] leading-4 text-muted-foreground">
              Connect a production workspace to view retained accounting records.
            </p>
          </div>
        </div>
      </section>
    </div>
  );
}
