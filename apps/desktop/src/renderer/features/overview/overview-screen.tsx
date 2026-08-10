import { Building2, Calculator, FileText } from "lucide-react";

const cards = [
  {
    detail: "Connect Documents to begin review",
    icon: FileText,
    label: "Documents",
    value: "—",
  },
  {
    detail: "Connect Banking to reconcile",
    icon: Building2,
    label: "Banking",
    value: "—",
  },
  {
    detail: "No workpaper created",
    icon: Calculator,
    label: "GST & BAS",
    value: "Draft",
  },
] as const;

export function OverviewScreen() {
  return (
    <div className="mx-auto grid w-full max-w-[920px] gap-5">
      <div>
        <h1 className="text-[18px] font-semibold tracking-[-0.02em] text-foreground">Overview</h1>
        <p className="mt-1 text-[11px] leading-5 text-muted-foreground">
          Your local accounting workspace at a glance.
        </p>
      </div>

      <div className="grid grid-cols-3 gap-3 max-[760px]:grid-cols-1">
        {cards.map((card) => {
          const Icon = card.icon;
          return (
            <section className="rounded-[6px] border border-border bg-surface p-3" key={card.label}>
              <div className="flex items-start gap-2.5">
                <span className="grid size-9 shrink-0 place-items-center rounded-full bg-success-soft text-forest">
                  <Icon aria-hidden="true" className="size-4" strokeWidth={1.7} />
                </span>
                <div className="min-w-0">
                  <h2 className="text-[10px] font-semibold text-foreground">{card.label}</h2>
                  <p className="mt-0.5 text-[17px] font-semibold leading-5 text-foreground">{card.value}</p>
                  <p className="mt-1 text-[9px] leading-4 text-muted-foreground">{card.detail}</p>
                </div>
              </div>
            </section>
          );
        })}
      </div>

      <section className="overflow-hidden rounded-[6px] border border-border bg-surface">
        <div className="border-b border-border px-3 py-2.5">
          <h2 className="text-[11px] font-semibold text-foreground">Needs review</h2>
        </div>
        <div className="grid min-h-36 place-items-center px-5 py-8 text-center">
          <div>
            <p className="text-[11px] font-semibold text-foreground">Nothing needs review</p>
            <p className="mt-1 text-[10px] leading-4 text-muted-foreground">
              Items will appear here after a production workspace is connected.
            </p>
          </div>
        </div>
      </section>
    </div>
  );
}
