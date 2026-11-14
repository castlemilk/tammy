/*
 * Local API and MCP. The visitor works the gate: enables access, watches a
 * client on the same Mac read the books and stage a draft invoice, then
 * presses the one button the client does not have.
 *
 * Everything static (the transcript, the Invoices list, the editor body) is
 * rendered here on the server and handed to the small client island in
 * integrations-gate.tsx, which only decides which of it is showing.
 */
import type { CSSProperties, ReactNode } from "react";
import { aud } from "../../lib/tools/calculations";
import { IntegrationsGate } from "./integrations-gate";
import { Icon, Money, Note, Pill } from "./kit";
import { business, invoice, proposedInvoice, purchase } from "./scenario";

/** The fourteen read tools, in the order the local API lists them. */
const READ_TOOLS = [
  "tammy.accounting.get_trial_balance",
  "tammy.accounting.list_accounts",
  "tammy.accounting.list_journals",
  "tammy.banking.get_summary",
  "tammy.documents.list_metadata",
  "tammy.identity.get_current_user",
  "tammy.invoices.list_cost_sources",
  "tammy.invoices.list_invoices",
  "tammy.organisation.get_current",
  "tammy.reporting.get_attention_summary",
  "tammy.reporting.get_current_bas_draft",
  "tammy.reporting.get_capability",
  "tammy.sbr.get_readiness",
  "tammy.system.get_diagnostics",
] as const;

const RESOURCE_COUNT = 10;
const PROPOSAL_TOOL_COUNT = 1;

/*
 * The proposal is dated the scenario's "today" (the trial balance is as at
 * 12 November 2026) and falls due on the default fourteen-day terms.
 */
const PROPOSAL_ISSUE = "12 Nov 2026";
const PROPOSAL_DUE = "26 Nov 2026";

/** When a transcript line or a gate crossing arrives, after reading starts. */
const at = (ms: number) => ({ "--at": `${ms}ms` }) as CSSProperties;
/** Order in which the editor's parts arrive once the draft is opened. */
const part = (index: number) => ({ "--p": index }) as CSSProperties;

function Calls() {
  return (
    <>
      <span aria-hidden="true"> →</span>
      <span className="lp-visually-hidden"> calls</span>
    </>
  );
}

function Returns() {
  return (
    <>
      <span aria-hidden="true"> ←</span>
      <span className="lp-visually-hidden"> returns</span>
    </>
  );
}

/** A figure in the transcript that can be traced to where it lands in the draft. */
function Trace({ id, children }: { id: "cost" | "price" | "total"; children: string }) {
  return (
    <button type="button" className="lp-trace" data-trace={id} aria-label={`${children}: trace this figure into the draft`}>
      {children}
    </button>
  );
}

const request = (
  <li data-who="you">
    <span className="lp-integrations-who">You</span>
    <span className="lp-integrations-said">
      Invoice {proposedInvoice.customer} for the office supplies we bought, plus {proposedInvoice.markupPercent}%.
    </span>
  </li>
);

const exchange = (
  <>
    <li data-who="client" data-arrives="" style={at(80)}>
      <span className="lp-integrations-who">
        Client
        <Calls />
      </span>
      <span className="lp-integrations-said">
        <b>tammy.invoices.list_cost_sources</b>
      </span>
    </li>
    <li data-who="tammy" data-arrives="" style={at(780)}>
      <span className="lp-integrations-who">
        Tammy
        <Returns />
      </span>
      <span className="lp-integrations-said">
        1 reviewed expense: {purchase.supplier}, <span className="lp-num">{purchase.reference}</span>,{" "}
        <Trace id="cost">{aud(purchase.subtotalCents)}</Trace> + {aud(purchase.gstCents)} GST
      </span>
    </li>
    <li data-who="client" data-arrives="" style={at(1320)}>
      <span className="lp-integrations-who">
        Client
        <Calls />
      </span>
      <span className="lp-integrations-said">
        <b>tammy.invoices.propose_draft</b>{" "}
        <span className="lp-integrations-args">
          (<span className="lp-num">{proposedInvoice.number}</span>, {proposedInvoice.customer}, &quot;{proposedInvoice.description}&quot;,{" "}
          <Trace id="price">{aud(proposedInvoice.subtotalCents)}</Trace>, Taxable)
        </span>
      </span>
    </li>
    <li data-who="tammy" data-arrives="" style={at(2020)}>
      <span className="lp-integrations-who">
        Tammy
        <Returns />
      </span>
      <span className="lp-integrations-said">
        staged: true, reviewRequired: true, total <Trace id="total">{aud(proposedInvoice.totalCents)}</Trace> incl.{" "}
        {aud(proposedInvoice.gstCents)} GST
      </span>
    </li>
    <li data-who="client" data-arrives="" style={at(2500)}>
      <span className="lp-integrations-who">Client</span>
      <span className="lp-integrations-said">
        I&apos;ve staged a draft for {aud(proposedInvoice.totalCents)}. Nothing is saved. Open Invoices in Tammy and choose &quot;Use MCP
        draft&quot;.
      </span>
    </li>
  </>
);

/** The Invoices list as the app sets it. The buttons are inert: the scene presses "Use MCP draft" itself. */
function InvoiceList({ kept = false }: { kept?: boolean }) {
  return (
    <>
      <div className="lp-integrations-viewhead">
        <div>
          <p className="lp-app-title">Invoices</p>
          <p className="lp-integrations-sub">Create, view and manage your invoices.</p>
        </div>
        <div className="lp-integrations-chips" aria-hidden="true">
          <span className="lp-integrations-chip">New invoice</span>
          <span className="lp-integrations-chip" data-press="">
            Use MCP draft
          </span>
        </div>
      </div>
      <div className="lp-integrations-card">
        <table className="lp-table">
          <thead>
            <tr>
              <th scope="col">Number</th>
              <th scope="col" data-mid="">
                Customer
              </th>
              <th scope="col" data-wide="">
                Due
              </th>
              <th scope="col" data-num="">
                Invoice total&nbsp;· AUD
              </th>
              <th scope="col">Status</th>
            </tr>
          </thead>
          <tbody>
            {kept ? (
              <tr>
                <th scope="row">{proposedInvoice.number}</th>
                <td data-mid="">{proposedInvoice.customer}</td>
                <td data-wide="">{PROPOSAL_DUE}</td>
                <td data-num="">
                  <Money cents={proposedInvoice.totalCents} className="lp-amount" />
                </td>
                <td>
                  <Pill tone="warn">Draft</Pill>
                </td>
              </tr>
            ) : null}
            <tr>
              <th scope="row">{invoice.number}</th>
              <td data-mid="">{invoice.customer}</td>
              <td data-wide="">{invoice.dueShort}</td>
              <td data-num="">
                <Money cents={invoice.totalCents} className="lp-amount" />
              </td>
              <td>
                <Pill tone="ok">Issued</Pill>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

/** The invoice editor filled from the staged proposal: fields, the line, the totals. */
const editor = (
  <>
    <dl className="lp-integrations-fields" data-part="" style={part(2)}>
      <Field label="Invoice number">{proposedInvoice.number}</Field>
      <Field label="Customer name">{proposedInvoice.customer}</Field>
      <Field label="Issue date">{PROPOSAL_ISSUE}</Field>
      <Field label="Due date">{PROPOSAL_DUE}</Field>
    </dl>
    <div className="lp-integrations-card" data-part="" style={part(3)}>
      <table className="lp-table lp-integrations-lines">
        <thead>
          <tr>
            <th scope="col">Description</th>
            <th scope="col" data-num="">
              Quantity
            </th>
            <th scope="col" data-num="">
              Unit cost <small>Private</small>
            </th>
            <th scope="col" data-num="">
              Selling price <small>Ex GST</small>
            </th>
            <th scope="col">GST treatment</th>
            <th scope="col" data-num="">
              Line total <small>Incl GST</small>
            </th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <th scope="row" data-label="Description">
              {proposedInvoice.description}
            </th>
            <td data-num="" data-label="Quantity">
              1
            </td>
            <td data-num="" data-label="Unit cost · Private">
              <span className="lp-trace-target" data-trace="cost">
                <Money cents={proposedInvoice.costCents} />
              </span>
            </td>
            <td data-num="" data-label="Selling price · Ex GST">
              <span className="lp-trace-target" data-trace="price">
                <Money cents={proposedInvoice.subtotalCents} />
              </span>
            </td>
            <td data-label="GST treatment">Taxable · 10%</td>
            <td data-num="" data-label="Line total · Incl GST">
              <Money cents={proposedInvoice.totalCents} className="lp-amount" />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div className="lp-integrations-sums" data-part="" style={part(4)}>
      <div className="lp-integrations-private">
        <p>
          <Icon name="eye" />
          Private estimates · never on the PDF
        </p>
        <dl>
          <div>
            <dt>Private cost</dt>
            <dd>
              <Money cents={proposedInvoice.costCents} />
            </dd>
          </div>
          <div>
            <dt>Profit (ex GST)</dt>
            <dd>
              <Money cents={proposedInvoice.marginCents} />
            </dd>
          </div>
        </dl>
      </div>
      <dl className="lp-integrations-totals">
        <div>
          <dt>Subtotal</dt>
          <dd>
            <Money cents={proposedInvoice.subtotalCents} />
          </dd>
        </div>
        <div>
          <dt>GST</dt>
          <dd>
            <Money cents={proposedInvoice.gstCents} />
          </dd>
        </div>
        <div className="lp-total-band">
          <dt>Invoice total</dt>
          <dd>
            <span className="lp-trace-target" data-trace="total">
              <Money cents={proposedInvoice.totalCents} />
            </span>
          </dd>
        </div>
      </dl>
    </div>
  </>
);

export function Integrations() {
  return (
    <section className="lp-section lp-integrations" id="integrations" data-tone="ink" data-room="mac" aria-labelledby="integrations-heading">
      <div className="lp-frame">
        <div className="lp-integrations-head">
          <div className="lp-integrations-headline">
            <h2 className="lp-h2" id="integrations-heading">
              <span>An assistant can read and propose.</span> <span>Only you can save.</span>
            </h2>
            <p className="lp-deck">
              The assistant is not part of Tammy. It is your own MCP client, an app on the same Mac that speaks the Model Context Protocol, and
              Tammy accepts no connection from anywhere else. Access is off until you enable it from Tammy&apos;s Integrations menu, and it ends
              when you revoke it, sign in again or quit.
            </p>
          </div>
          <ul className="lp-integrations-tally" aria-label="What the local API exposes">
            <li>
              <span className="lp-integrations-count">{READ_TOOLS.length}</span>
              <span className="lp-integrations-unit">read tools</span>
            </li>
            <li>
              <span className="lp-integrations-count">{RESOURCE_COUNT}</span>
              <span className="lp-integrations-unit">resources</span>
            </li>
            <li>
              <span className="lp-integrations-count">{PROPOSAL_TOOL_COUNT}</span>
              <span className="lp-integrations-unit">proposal tool</span>
            </li>
            <li data-none="">
              <span className="lp-integrations-count">0</span>
              <span className="lp-integrations-unit">that save, issue, post or send</span>
            </li>
          </ul>
        </div>

        <IntegrationsGate
          windowTitle={`${business.name} / Invoices`}
          invoiceNumber={proposedInvoice.number}
          request={request}
          exchange={exchange}
          list={<InvoiceList />}
          listKept={<InvoiceList kept />}
          editor={editor}
        />

        <div className="lp-integrations-facts">
          <div className="lp-integrations-statement">
            <p className="lp-body">
              Reads cover the signed-in organisation: accounts, journals, the trial balance, a banking summary, document metadata, invoices,
              the current GST draft and more.
            </p>
            <p className="lp-integrations-cannot">
              Connected clients cannot save, issue, post or send. No remote access, and the private connection file stops working when access
              ends.
            </p>
          </div>
          <Note className="lp-integrations-limit">
            <strong>Lists return the first 200 entries</strong>, 20 for invoices. It is inspection, not a full export. What your client does with
            what it reads, including sending it to an online service, is up to that client.
          </Note>
          <details className="lp-integrations-tools">
            <summary>
              <Icon name="plus" />
              The fourteen read tools
            </summary>
            <ul>
              {READ_TOOLS.map((tool) => (
                <li key={tool}>{tool}</li>
              ))}
            </ul>
            <p className="lp-micro">Each takes no arguments and is marked read-only.</p>
          </details>
        </div>
      </div>
    </section>
  );
}
