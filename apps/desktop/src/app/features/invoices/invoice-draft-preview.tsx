import {
  formatAud,
  formatQuantity,
  type InvoiceDraftData,
  type InvoiceTotals,
} from "@tammy/invoices/model";
import type { ReactNode, Ref } from "react";
import { HorizontalScrollArea } from "../../components/horizontal-scroll-area";
import { Badge } from "../../components/ui/badge";

/** Deliberately public fields only: no private cost, margin, source ID or embedded draft JSON. */
export function invoicePreviewData(
  draft: InvoiceDraftData,
  totals: InvoiceTotals,
  lineKeys: readonly string[],
) {
  return {
    number: draft.number,
    issueDate: draft.issueDate,
    dueDate: draft.dueDate,
    customerName: draft.customerName,
    customerAddress: draft.customerAddress,
    customerAbn: draft.customerAbn,
    sellerAddress: draft.sellerAddress,
    gstRegistered: draft.gstRegistered,
    notes: draft.notes,
    paymentInstructions: draft.paymentInstructions,
    lines: draft.lines.map((line, index) => ({
      key: lineKeys[index],
      description: line.description,
      quantityMilli: line.quantityMilli,
      unitPriceMinor: line.unitPriceMinor,
      taxTreatment: line.taxTreatment,
      gstMinor: totals.lines[index]?.gstMinor ?? 0,
      totalMinor: totals.lines[index]?.totalMinor ?? 0,
    })),
    subtotalMinor: totals.subtotalMinor,
    gstMinor: totals.gstMinor,
    totalMinor: totals.totalMinor,
  };
}

export function InvoiceDraftPreview({
  data,
  seller,
  saved,
  logo,
  headingRef,
  action,
  children,
}: {
  readonly data: ReturnType<typeof invoicePreviewData> | undefined;
  readonly seller: { readonly name: string; readonly abn: string } | undefined;
  readonly saved: boolean;
  readonly logo?: ReactNode;
  readonly headingRef?: Ref<HTMLHeadingElement>;
  readonly action?: ReactNode;
  readonly children?: ReactNode;
}) {
  return (
    <aside className="grid min-w-0 content-start gap-3" aria-label="Draft preview workspace">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2
          className="focus-ring w-fit rounded-lg text-[16px] font-semibold"
          ref={headingRef}
          tabIndex={-1}
        >
          Draft preview
        </h2>
        <Badge variant="outline">{saved ? "Saved draft" : "Not saved"}</Badge>
      </div>
      <p className="text-[12px] leading-5 text-muted-foreground">
        On-screen preview. PDF export uses the saved draft.
      </p>
      <section
        className="grid min-w-0 gap-3 rounded-xl border border-border bg-surface p-5 max-[500px]:p-4"
        aria-label="Draft invoice preview"
      >
        <div className="grid min-w-0 gap-2 border-b border-border pb-3">
          {logo ? <div className="flex min-h-14 items-center">{logo}</div> : null}
          <div className="flex min-w-0 flex-wrap items-start justify-between gap-2">
            <p className="min-w-0 break-words font-serif text-[22px] leading-7 text-forest">
              {seller?.name ?? "Your organisation"}
            </p>
            <span className="text-[12px] font-semibold uppercase tracking-wide">
              Draft · not issued
            </span>
          </div>
          <p className="text-[12px] text-muted-foreground">
            {data?.gstRegistered ? "Tax invoice" : "Invoice"} · AUD
          </p>
          {seller ? (
            <p className="text-[12px] text-muted-foreground">ABN {seller.abn}</p>
          ) : (
            <p className="text-[12px] leading-5 text-muted-foreground">
              Seller name and ABN appear after saving.
            </p>
          )}
        </div>
        {data ? (
          <>
            <dl className="grid gap-2 text-[12px]">
              <Meta label="Invoice number">{data.number || "Unnumbered draft"}</Meta>
              <Meta label="Issue date">{displayDate(data.issueDate)}</Meta>
              <Meta label="Due date">{displayDate(data.dueDate)}</Meta>
            </dl>
            <div className="grid grid-cols-2 gap-3 text-[14px] max-[500px]:grid-cols-1">
              <div>
                <p className="mb-1 text-[12px] text-muted-foreground">Bill to</p>
                <p className="break-words font-medium">
                  {data.customerName || "Customer to be completed"}
                </p>
                <p className="whitespace-pre-wrap break-words leading-5">
                  {data.customerAddress || "Address to be completed"}
                </p>
                {data.customerAbn ? (
                  <p className="mt-1 text-[12px]">ABN {data.customerAbn}</p>
                ) : null}
              </div>
              {data.sellerAddress ? (
                <div>
                  <p className="mb-1 text-[12px] text-muted-foreground">From</p>
                  <p className="whitespace-pre-wrap break-words leading-5">{data.sellerAddress}</p>
                </div>
              ) : null}
            </div>
            <HorizontalScrollArea label="Draft preview line items">
              <table
                className="w-full min-w-[280px] border-collapse text-left text-[14px]"
                aria-label="Draft preview items"
              >
                <thead>
                  <tr className="border-y border-border text-[12px] text-muted-foreground">
                    <th className="py-2 pr-3 font-medium" scope="col">
                      Description
                    </th>
                    <th className="py-2 text-right font-medium" scope="col">
                      Amount (AUD)
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {data.lines.map((line) => (
                    <tr className="border-b border-border" key={line.key}>
                      <td className="max-w-[180px] break-words py-3 pr-3 align-top">
                        <p className="font-medium">{line.description || "Item description"}</p>
                        <p className="mt-1 text-[12px] text-muted-foreground">
                          {formatQuantity(line.quantityMilli)} × {formatAud(line.unitPriceMinor)} ex
                          GST
                        </p>
                        <p className="mt-1 text-[12px] text-muted-foreground">
                          {treatmentLabel[line.taxTreatment]} · GST {formatAud(line.gstMinor)}
                        </p>
                      </td>
                      <td className="whitespace-nowrap py-3 text-right align-top tabular-nums">
                        {formatAud(line.totalMinor)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </HorizontalScrollArea>
          </>
        ) : (
          <p className="rounded-xl bg-background p-3 text-[13px] leading-6 text-muted-foreground">
            Check the invoice details to update this preview. Your entries are unchanged.
          </p>
        )}
        <dl className="grid gap-3 text-[14px]" aria-label="Invoice totals">
          <Total label="Subtotal" minor={data?.subtotalMinor} testId="invoice-subtotal" />
          <Total label="GST" minor={data?.gstMinor} testId="invoice-gst" />
          <Total label="Invoice total" minor={data?.totalMinor} testId="invoice-total" prominent />
        </dl>
        {data?.paymentInstructions ? (
          <div className="text-[12px] leading-5">
            <p className="mb-1 font-semibold">Payment instructions</p>
            <p className="whitespace-pre-wrap break-words">{data.paymentInstructions}</p>
          </div>
        ) : null}
        {data?.notes ? (
          <p className="whitespace-pre-wrap break-words text-[12px] leading-5 text-muted-foreground">
            {data.notes}
          </p>
        ) : null}
      </section>
      {children}
      {action}
    </aside>
  );
}

function Meta({ label, children }: { readonly label: string; readonly children: ReactNode }) {
  return (
    <div className="flex flex-wrap justify-between gap-2">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="break-words text-right">{children}</dd>
    </div>
  );
}
function Total({
  label,
  minor,
  testId,
  prominent,
}: {
  readonly label: string;
  readonly minor: number | undefined;
  readonly testId: string;
  readonly prominent?: boolean;
}) {
  return (
    <div
      className={`flex flex-wrap items-baseline justify-between gap-3 ${prominent ? "rounded-xl bg-warning-soft p-3 font-semibold" : ""}`}
      data-testid={testId}
    >
      <dt>{label}</dt>
      <dd className="whitespace-nowrap tabular-nums">
        {minor === undefined ? "—" : formatAud(minor)}
      </dd>
    </div>
  );
}
function displayDate(value: string): string {
  return value
    ? new Intl.DateTimeFormat("en-AU", {
        day: "numeric",
        month: "short",
        year: "numeric",
        timeZone: "UTC",
      }).format(new Date(`${value}T00:00:00Z`))
    : "To be completed";
}
const treatmentLabel = {
  taxable: "Taxable · 10%",
  "gst-free": "GST-free",
  "input-taxed": "Input-taxed",
  "out-of-scope": "Out of scope",
};
