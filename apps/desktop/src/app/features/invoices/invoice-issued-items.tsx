import { formatAud, formatQuantity, type InvoiceTotals } from "@tammy/invoices/model";
import { type ReactNode, useMemo } from "react";
import type { InvoiceAccountingRecord } from "../../../shared/invoice-accounting";
import { HorizontalScrollArea } from "../../components/horizontal-scroll-area";
import type { InvoiceRecord } from "./invoices-screen";

export function InvoiceIssuedItems({
  record,
  totals,
  accounting,
  children,
}: {
  readonly record: InvoiceRecord;
  readonly totals: InvoiceTotals;
  readonly accounting?: InvoiceAccountingRecord | undefined;
  readonly children?: ReactNode;
}) {
  const { draft } = record;
  const rows = useMemo(
    () => draft.lines.map((line) => ({ line, key: crypto.randomUUID() })),
    [draft.lines],
  );
  const fullyCosted = draft.lines.every((line) => line.unitCostMinor !== undefined);
  return (
    <section className="grid min-w-0 gap-6" aria-label="Issued invoice items">
      <HorizontalScrollArea label="Invoice line items table">
        <table
          className="w-full min-w-[680px] border-collapse text-left text-[14px]"
          aria-label="Invoice line items"
        >
          <thead className="border-b border-border text-[12px] text-muted-foreground">
            <tr>
              <th className="py-3 pr-6 font-medium" scope="col">
                Description
              </th>
              <th className="px-3 py-3 text-right font-medium" scope="col">
                Quantity
              </th>
              <th className="px-3 py-3 text-right font-medium" scope="col">
                Unit price (AUD)<span className="block font-normal">Ex GST</span>
              </th>
              <th className="px-3 py-3 text-right font-medium" scope="col">
                GST (AUD)
              </th>
              <th className="py-3 pl-3 text-right font-medium" scope="col">
                Total (AUD)
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map(({ line, key }, index) => (
              // Issued lines are immutable and retain their persisted order.
              <tr className="border-b border-border" key={key}>
                <td className="max-w-[480px] break-words py-5 pr-6 font-medium">
                  {line.description}
                </td>
                <td className="px-3 py-5 text-right tabular-nums">
                  {formatQuantity(line.quantityMilli)}
                </td>
                <td className="whitespace-nowrap px-3 py-5 text-right tabular-nums">
                  {formatAud(line.unitPriceMinor)}
                </td>
                <td className="px-3 py-5 text-right tabular-nums">
                  {formatAud(totals.lines[index]?.gstMinor ?? 0)}
                  <span className="block whitespace-nowrap text-[12px] text-muted-foreground">
                    {taxLabel[line.taxTreatment]}
                  </span>
                </td>
                <td className="whitespace-nowrap py-5 pl-3 text-right font-medium tabular-nums">
                  {formatAud(totals.lines[index]?.totalMinor ?? 0)}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </HorizontalScrollArea>
      <div className="grid grid-cols-2 items-start gap-8 justify-self-end w-full max-w-[640px] max-[700px]:grid-cols-1 max-[700px]:gap-4">
        <dl className="grid gap-3" aria-label="Invoice totals">
          <Amount label="Subtotal" minor={totals.subtotalMinor} testId="invoice-subtotal" />
          <Amount label="GST" minor={totals.gstMinor} testId="invoice-gst" />
          <Amount label="Invoice total" minor={totals.totalMinor} testId="invoice-total" strong />
        </dl>
        {accounting?.posted ? (
          <dl className="grid gap-3" aria-label="Confirmed invoice receipts">
            <Amount label="Received" minor={accounting.receivedMinor} testId="invoice-received" />
            <Amount
              label="Outstanding"
              minor={accounting.outstandingMinor}
              testId="invoice-outstanding"
              highlight
            />
          </dl>
        ) : null}
      </div>
      <details className="group min-w-0 border-t border-border pt-3 text-[13px]">
        <summary className="focus-ring w-fit max-w-full cursor-pointer rounded-lg py-3 font-medium">
          Invoice information &amp; private estimates
        </summary>
        <div className="grid min-w-0 gap-6 pt-3">
          <dl className="grid grid-cols-2 gap-5 max-[700px]:grid-cols-1">
            <Information label="Seller">
              {record.sellerName} · ABN {record.sellerAbn}
              <br />
              {draft.sellerAddress}
            </Information>
            <Information label="Customer">
              {draft.customerName}
              {draft.customerAbn ? ` · ABN ${draft.customerAbn}` : ""}
              <br />
              {draft.customerAddress}
            </Information>
            <Information label="Notes">{draft.notes || "—"}</Information>
            <Information label="Payment instructions">
              {draft.paymentInstructions || "—"}
            </Information>
            <Information label="GST registration">
              {draft.gstRegistered ? "Registered for GST" : "Not registered for GST"}
            </Information>
          </dl>
          <div className="grid gap-3 rounded-xl bg-background p-4">
            <h2 className="text-[14px] font-semibold">Internal · never on the PDF</h2>
            <dl className="grid max-w-[400px] gap-3">
              <Amount
                label={fullyCosted ? "Private cost" : "Known private cost"}
                minor={totals.costMinor}
                testId="invoice-cost"
              />
              <Amount
                label="Profit (ex GST)"
                minor={fullyCosted ? totals.marginMinor : undefined}
                testId="invoice-margin"
              />
            </dl>
            {!fullyCosted ? (
              <p className="text-muted-foreground">
                A cost is missing from at least one line; profit is unavailable.
              </p>
            ) : null}
            <p className="text-[12px] leading-5 text-muted-foreground">
              Unit costs are private estimates. Linked reviewed documents and the ledger are
              unchanged.
            </p>
            <HorizontalScrollArea label="Scrollable private invoice estimates">
              <table
                className="w-full min-w-[520px] border-collapse text-left text-[13px]"
                aria-label="Private invoice estimates"
              >
                <thead>
                  <tr className="border-b border-border">
                    <th className="py-2 pr-4 font-medium">Description</th>
                    <th className="px-3 py-2 text-right font-medium">Unit cost</th>
                    <th className="py-2 pl-3 font-medium">Source document</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map(({ line, key }) => (
                    <tr className="border-b border-border last:border-0" key={key}>
                      <td className="py-3 pr-4">{line.description}</td>
                      <td className="whitespace-nowrap px-3 py-3 text-right tabular-nums">
                        {line.unitCostMinor === undefined
                          ? "Unknown"
                          : formatAud(line.unitCostMinor)}
                      </td>
                      <td className="py-3 pl-3 break-all">
                        {line.sourceDocumentId || "Manual line"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </HorizontalScrollArea>
          </div>
          {children}
        </div>
      </details>
    </section>
  );
}

function Information({
  label,
  children,
}: {
  readonly label: string;
  readonly children: ReactNode;
}) {
  return (
    <div className="min-w-0">
      <dt className="mb-1 text-[12px] text-muted-foreground">{label}</dt>
      <dd className="whitespace-pre-wrap break-words leading-6">{children}</dd>
    </div>
  );
}
function Amount({
  label,
  minor,
  testId,
  strong,
  highlight,
}: {
  readonly label: string;
  readonly minor: number | undefined;
  readonly testId: string;
  readonly strong?: boolean;
  readonly highlight?: boolean;
}) {
  return (
    <div
      data-testid={testId}
      className={`flex flex-wrap items-baseline justify-between gap-3 text-[14px] ${strong ? "border-t border-border pt-3 font-semibold" : ""} ${highlight ? "rounded-xl bg-warning-soft px-3 py-3 font-semibold" : ""}`}
    >
      <dt>{label}</dt>
      <dd className="whitespace-nowrap tabular-nums">
        {minor === undefined ? "—" : formatAud(minor)}
      </dd>
    </div>
  );
}
const taxLabel = {
  taxable: "Taxable · 10%",
  "gst-free": "GST-free",
  "input-taxed": "Input-taxed",
  "out-of-scope": "Out of scope",
};
