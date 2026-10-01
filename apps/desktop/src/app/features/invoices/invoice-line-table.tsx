import { formatAud, type InvoiceTotals } from "@tammy/invoices/model";
import { Plus, Trash2 } from "lucide-react";
import { Button } from "../../components/ui/button";
import type { EditorLine } from "./invoice-editor";

export const invoiceFieldClass =
  "focus-ring h-8 w-full min-w-0 rounded-[4px] border border-border bg-background px-2 text-[11px] text-foreground disabled:opacity-70";

export function InvoiceLineTable({
  lines,
  totals,
  gstRegistered,
  busy,
  readOnly,
  onEdit,
  onAdd,
  onRemove,
}: {
  readonly lines: readonly EditorLine[];
  readonly totals: InvoiceTotals | undefined;
  readonly gstRegistered: boolean;
  readonly busy: boolean;
  readonly readOnly: boolean;
  readonly onEdit: (key: string, value: Partial<EditorLine>) => void;
  readonly onAdd: () => void;
  readonly onRemove: (key: string) => void;
}) {
  const disabled = busy || readOnly;
  return (
    <section className="grid min-w-0 gap-3" aria-labelledby="invoice-lines-heading">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-[12px] font-semibold" id="invoice-lines-heading">
          Invoice line items
        </h2>
        {!readOnly ? (
          <Button
            className="h-8 text-[10px]"
            disabled={busy || lines.length >= 50}
            onClick={onAdd}
            type="button"
            variant="outline"
          >
            <Plus aria-hidden="true" className="size-3" /> Add line item
          </Button>
        ) : null}
      </div>
      <div className="focus-ring min-w-0 overflow-x-auto rounded-[6px] border border-border bg-surface">
        <table
          className="w-full min-w-[790px] table-fixed border-collapse text-[11px]"
          aria-label="Invoice line items"
        >
          <colgroup>
            <col className="w-[27%]" />
            <col className="w-[9%]" />
            <col className="w-[12%]" />
            <col className="w-[12%]" />
            <col className="w-[19%]" />
            <col className="w-[14%]" />
            {!readOnly ? <col className="w-[7%]" /> : null}
          </colgroup>
          <thead className="border-b border-border bg-muted/40 text-left text-[10px] text-muted-foreground">
            <tr>
              <th className="px-3 py-3 font-medium" scope="col">
                Description
              </th>
              <th className="px-2 py-3 text-right font-medium" scope="col">
                Quantity
              </th>
              <th className="px-2 py-3 text-right font-medium" scope="col">
                Unit cost <span className="block text-[9px] font-normal">Private</span>
              </th>
              <th className="px-2 py-3 text-right font-medium" scope="col">
                Selling price <span className="block text-[9px] font-normal">Ex GST</span>
              </th>
              <th className="px-2 py-3 font-medium" scope="col">
                GST treatment
              </th>
              <th className="px-3 py-3 text-right font-medium" scope="col">
                Line total <span className="block text-[9px] font-normal">Incl GST</span>
              </th>
              {!readOnly ? (
                <th className="px-2 py-3" scope="col">
                  <span className="sr-only">Remove</span>
                </th>
              ) : null}
            </tr>
          </thead>
          <tbody>
            {lines.map((line, index) => (
              <tr className="border-b border-border last:border-0" key={line.key}>
                <td className="px-3 py-3 align-top">
                  <input
                    aria-label={`Description ${index + 1}`}
                    className={invoiceFieldClass}
                    disabled={disabled}
                    maxLength={500}
                    onChange={(event) => onEdit(line.key, { description: event.target.value })}
                    value={line.description}
                  />
                  <p className="mt-1.5 text-[9px] leading-4 text-muted-foreground">
                    {line.sourceDocumentId
                      ? "Linked reviewed expense · internal only"
                      : "Manual line"}
                  </p>
                </td>
                <td className="px-2 py-3 align-top">
                  <input
                    aria-label={`Quantity ${index + 1}`}
                    className={`${invoiceFieldClass} text-right tabular-nums`}
                    disabled={disabled}
                    inputMode="decimal"
                    onChange={(event) => onEdit(line.key, { quantity: event.target.value })}
                    value={line.quantity}
                  />
                </td>
                <td className="px-2 py-3 align-top">
                  <input
                    aria-label={`Unit cost ${index + 1}`}
                    className={`${invoiceFieldClass} text-right tabular-nums`}
                    disabled={disabled}
                    inputMode="decimal"
                    onChange={(event) => onEdit(line.key, { unitCost: event.target.value })}
                    value={line.unitCost}
                  />
                </td>
                <td className="px-2 py-3 align-top">
                  <input
                    aria-label={`Unit price ${index + 1}`}
                    className={`${invoiceFieldClass} text-right tabular-nums`}
                    disabled={disabled}
                    inputMode="decimal"
                    onChange={(event) => onEdit(line.key, { unitPrice: event.target.value })}
                    value={line.unitPrice}
                  />
                </td>
                <td className="px-2 py-3 align-top">
                  <select
                    aria-label={`GST treatment ${index + 1}`}
                    className={invoiceFieldClass}
                    disabled={disabled}
                    onChange={(event) =>
                      onEdit(line.key, {
                        taxTreatment: event.target.value as EditorLine["taxTreatment"],
                      })
                    }
                    value={line.taxTreatment}
                  >
                    <option disabled={!gstRegistered} value="taxable">
                      Taxable · 10%
                    </option>
                    <option value="gst-free">GST-free</option>
                    <option value="input-taxed">Input-taxed</option>
                    <option value="out-of-scope">Out of scope</option>
                  </select>
                </td>
                <td className="px-3 py-3 text-right align-top tabular-nums">
                  <span className="block py-2">
                    {totals ? formatAud(totals.lines[index]?.totalMinor ?? 0) : "—"}
                  </span>
                </td>
                {!readOnly ? (
                  <td className="px-2 py-3 align-top">
                    <Button
                      aria-label={`Remove line ${index + 1}`}
                      className="h-8 px-2"
                      disabled={busy || lines.length === 1}
                      onClick={() => onRemove(line.key)}
                      type="button"
                      variant="ghost"
                    >
                      <Trash2 aria-hidden="true" className="size-3.5" />
                    </Button>
                  </td>
                ) : null}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
