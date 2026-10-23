import { ScreenHeading, SurfaceHeading } from "../../components/workspace-primitives";

interface EmptyLedgerScreenProps {
  readonly description: string;
  readonly emptyLabel: string;
  readonly title: string;
}

export function EmptyLedgerScreen({ description, emptyLabel, title }: EmptyLedgerScreenProps) {
  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-6">
      <ScreenHeading area="Accounting · Ledger" title={title} description={description} />
      <section className="work-surface overflow-hidden">
        <SurfaceHeading title={emptyLabel} />
        <div className="grid min-h-64 place-items-center px-5 py-10 text-center">
          <div>
            <p className="max-w-sm text-[14px] leading-6 text-muted-foreground">
              Connect a production workspace to view retained accounting records.
            </p>
          </div>
        </div>
      </section>
    </div>
  );
}
