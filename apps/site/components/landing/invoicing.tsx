/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */

/*
 * Customer invoicing: the signature scene of the features band. Rendered by
 * features.tsx inside .lp-frame on the paper ground.
 *
 * This file is the server part: the head, the sheet's fixed contents, the list
 * of what else invoicing does and the pinned browser-tool boundary. The press
 * interaction lives in the small island in invoicing-figure.tsx.
 */
import type { ReactNode } from "react";
import { aud } from "../../lib/tools/calculations";
import { InvoicingFigure } from "./invoicing-figure";
import { Note } from "./kit";
import { invoice } from "./scenario";

const alsoHere: { lead: string; detail: ReactNode }[] = [
  {
    lead: "A GST treatment on every line",
    detail: (
      <>
        <span>Taxable · 10%</span>, <span>GST-free</span>, <span>Input-taxed</span> or <span>Out of scope</span>.
      </>
    ),
  },
  {
    lead: "Lines from reviewed expenses",
    detail: "Add a markup. Each line stays linked to its source document.",
  },
  {
    lead: "Private cost and profit",
    detail: "Shown while you edit, never on the customer PDF.",
  },
  {
    lead: "A company template",
    detail: "Your logo, payment instructions and terms, saved once.",
  },
  {
    lead: "An A4 customer PDF",
    detail: "Made on your Mac from the saved invoice.",
  },
  {
    lead: "Issued invoices are locked",
    detail: "A draft can change. An issued invoice is read only.",
  },
];

/** The parts of the invoice sheet that never change with its state. */
function SheetBody() {
  return (
    <>
      <dl className="lp-invoicing-meta">
        <div>
          <dt>Invoice number</dt>
          <dd>{invoice.number}</dd>
        </div>
        <div>
          <dt>Issue date</dt>
          <dd>{invoice.issueShort}</dd>
        </div>
        <div>
          <dt>Due date</dt>
          <dd>{invoice.dueShort}</dd>
        </div>
        <div data-wide="">
          <dt>Bill to</dt>
          <dd>{invoice.customer}</dd>
        </div>
      </dl>

      <table className="lp-invoicing-items" aria-label="Invoice lines">
        <thead>
          <tr>
            <th scope="col">Description</th>
            <th scope="col" data-num="">
              Qty
            </th>
            <th scope="col" data-num="">
              Amount ex GST
            </th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <th scope="row">
              {invoice.line.description}
              <span>{invoice.line.treatment}</span>
            </th>
            <td data-num="">{invoice.line.quantity}</td>
            <td data-num="">{aud(invoice.line.unitCents)}</td>
          </tr>
        </tbody>
      </table>
      <p className="lp-invoicing-fineprint">
        GST is calculated at 10% on taxable line amounts and rounded per line.
      </p>
    </>
  );
}

export function Invoicing() {
  return (
    <article className="lp-invoicing" aria-labelledby="invoicing-heading">
      <header className="lp-invoicing-head">
        <div className="lp-invoicing-title">
          <h3 className="lp-h3" id="invoicing-heading">
            Customer invoicing
          </h3>
          <p className="lp-invoicing-summary">Issue to the local ledger; export a customer PDF</p>
        </div>
        <p className="lp-invoicing-statement">
          <span>Issue once.</span> <span>Three lines post together.</span>
        </p>
        <p className="lp-body lp-invoicing-lede">
          Build an invoice from lines, each with its own GST treatment; drafts are saved encrypted. Issuing locks the
          document and records the sale in the ledger. Payments received are recorded separately, and the customer PDF
          is made on your Mac.
        </p>
        <Note className="lp-invoicing-limit">
          Issuing records the sale. Nothing is sent to your customer, and no payment is taken.
        </Note>
      </header>

      <InvoicingFigure>
        <SheetBody />
      </InvoicingFigure>

      <div className="lp-invoicing-more">
        <h4 className="lp-label lp-invoicing-more-title">Invoicing in detail</h4>
        <ul className="lp-invoicing-list">
          {alsoHere.map((item) => (
            <li key={item.lead}>
              <strong>{item.lead}</strong>
              <span>{item.detail}</span>
            </li>
          ))}
        </ul>

        <p className="lp-invoicing-browser">
          <span className="lp-invoicing-browser-ask">Need an invoice today?</span>
          <span className="lp-invoicing-browser-tool">
            <a className="lp-link" href="/tools/invoice-generator">
              Browser invoice generator
            </a>
            <span>Creates a PDF independently of your Mac workspace.</span>
          </span>
        </p>
      </div>
    </article>
  );
}
