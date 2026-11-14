/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */

/*
 * The GST workpaper. A local draft whose every figure can be traced to the
 * record behind it, and an export path that runs to the edge of the Mac
 * workspace and stops there.
 *
 * Everything is server-rendered in its finished state. The basis switch is a
 * pair of native radios and the stale-source demonstration is a checkbox with a
 * form reset, so both work before hydration and without script; bas.css reads
 * them with :has(). The one island (bas-live) announces each change to
 * assistive technology, returns focus after a regeneration, and tells the
 * stylesheet when figures have just been replaced.
 */
import type { CSSProperties } from "react";
import { aud } from "../../lib/tools/calculations";
import { BasLive } from "./bas-live";
import { Icon, Note } from "./kit";
import { business, gstWorkpaper, invoice, purchase } from "./scenario";

/*
 * The same quarter on each GST basis. On the cash basis the core attributes
 * sales to each recorded receipt (services/core/internal/reporting/bas_sources.go):
 * the tax date is the payment date, G1 is the amount received and 1A is the GST
 * allocated to it. The purchase was paid in full inside the quarter, so its
 * credit is the same on both.
 */
const views = [
  {
    basis: "noncash",
    line: "Non-cash basis · sales follow invoice issue dates",
    taxDate: invoice.issueShort,
    source: "Issued invoice",
    figures: gstWorkpaper.nonCash,
  },
  {
    basis: "cash",
    line: "Cash basis · sales follow recorded payments",
    taxDate: invoice.payment.dateShort,
    source: "Recorded receipt",
    figures: gstWorkpaper.cash,
  },
] as const;

type View = (typeof views)[number];

function summary(view: View): string {
  const { g1Cents, a1Cents, b1Cents, netCents } = view.figures;
  return `${view.line}. Sales (G1) ${aud(g1Cents)}, GST on sales (1A) ${aud(a1Cents)}, GST credits (1B) ${aud(b1Cents)}, Net GST payable ${aud(netCents)}.`;
}

const staleWarning =
  "Sources changed since this workpaper was saved. Regenerate the local draft to include current income and GST. The figures below are the saved snapshot.";

function Figures({ view }: { view: View }) {
  const { g1Cents, a1Cents, b1Cents, netCents } = view.figures;
  const sales = `bas-src-sales-${view.basis}`;
  const working = `bas-working-${view.basis}`;
  return (
    <dl className="lp-bas-figures" data-basis={view.basis}>
      <div className="lp-bas-fig">
        <dt>Sales (G1)</dt>
        <dd>
          <button type="button" className="lp-trace lp-amount" data-key="g1" data-trace="g1" aria-label={`Sales (G1) ${aud(g1Cents)}`} aria-describedby={sales}>
            {aud(g1Cents)}
          </button>
        </dd>
      </div>
      <div className="lp-bas-fig">
        <dt>GST on sales (1A)</dt>
        <dd>
          <button type="button" className="lp-trace lp-amount" data-key="a1" data-trace="a1 net" aria-label={`GST on sales (1A) ${aud(a1Cents)}`} aria-describedby={sales}>
            {aud(a1Cents)}
          </button>
        </dd>
      </div>
      <div className="lp-bas-fig">
        <dt>GST credits (1B)</dt>
        <dd>
          <button type="button" className="lp-trace lp-amount" data-key="b1" data-trace="b1 net" aria-label={`GST credits (1B) ${aud(b1Cents)}`} aria-describedby="bas-src-purchase">
            {aud(b1Cents)}
          </button>
        </dd>
      </div>
      <div className="lp-bas-fig lp-bas-fig--net">
        <dt>Net GST payable</dt>
        <dd>
          <button type="button" className="lp-trace lp-amount" data-key="net" data-trace="net" aria-label={`Net GST payable ${aud(netCents)}`} aria-describedby={working}>
            {aud(netCents)}
          </button>
          <span className="lp-visually-hidden" id={working}>
            GST on sales (1A) {aud(a1Cents)} less GST credits (1B) {aud(b1Cents)}
          </span>
        </dd>
      </div>
    </dl>
  );
}

export function Bas() {
  const [nonCash, cash] = views;

  return (
    <section className="lp-section lp-bas" id="bas" data-tone="draft" data-room="mac" aria-labelledby="bas-heading">
      <div className="lp-frame lp-bas-frame">
        <div className="lp-bas-head">
          <h2 className="lp-h2" id="bas-heading">Prepared is not lodged.</h2>
          <p className="lp-deck">
            Tammy builds a local GST workpaper from the invoices you issued and the purchases you posted, and shows the
            record behind every figure.
          </p>
          <p className="lp-bas-boundary">Tammy never submits BAS information to the ATO.</p>
          <a className="lp-link" href="/privacy">
            Understand the reporting boundary
          </a>
        </div>

        <div className="lp-bas-handles">
          <fieldset className="lp-bas-handle">
            <legend className="lp-label">See the same quarter on the</legend>
            <div className="lp-bas-seg">
              <input
                className="lp-visually-hidden"
                type="radio"
                name="bas-basis"
                id="bas-basis-noncash"
                value="noncash"
                defaultChecked
              />
              <label htmlFor="bas-basis-noncash">Non-cash basis</label>
              <input className="lp-visually-hidden" type="radio" name="bas-basis" id="bas-basis-cash" value="cash" />
              <label htmlFor="bas-basis-cash">Cash basis</label>
            </div>
            {/* Both captions keep their place, so the sheet below never jumps. */}
            <div className="lp-bas-captions">
              <p className="lp-micro" data-basis-note="noncash">
                Sales follow the invoice: {invoice.number} counts in full from its issue date, {invoice.issueDate}. The{" "}
                {aud(nonCash.figures.b1Cents)} purchase credit follows the supplier&apos;s invoice.
              </p>
              <p className="lp-micro" data-basis-note="cash">
                Sales follow recorded payments: the {aud(invoice.payment.cents)} received on {invoice.payment.date}{" "}
                carries {aud(cash.figures.a1Cents)} of GST. The purchase was paid in full, so its{" "}
                {aud(cash.figures.b1Cents)} credit stands.
              </p>
            </div>
          </fieldset>

          <form className="lp-bas-handle" id="bas-stale-form">
            <p className="lp-label">After the workpaper is saved</p>
            <input className="lp-visually-hidden" type="checkbox" id="bas-stale" />
            <label className="lp-btn lp-btn--outline lp-btn--sm lp-bas-change" htmlFor="bas-stale">
              <span data-when="fresh">Change a source</span>
              <span data-when="stale">Source changed</span>
            </label>
            <div className="lp-bas-captions">
              <p className="lp-micro" data-when-note="fresh">
                Stands in for any change to the records behind these figures.
              </p>
              <p className="lp-micro" data-when-note="stale">
                The workpaper says so, and keeps its saved figures until you press Regenerate local draft.
              </p>
            </div>
          </form>

          <Note className="lp-bas-note">
            Workpapers cover periods ending 1 July 2023 to 30 June 2027.
          </Note>
        </div>

        <div className="lp-bas-paper">
          <div className="lp-sheet lp-bas-sheet">
            <div className="lp-bas-sheet-head">
              <div className="lp-bas-sheet-title">
                <p className="lp-bas-flag">
                  <span>Illustrative workpaper · synthetic figures</span>
                </p>
                <h3 className="lp-app-title">GST &amp; BAS</h3>
                <p className="lp-bas-sub">A local workpaper from invoice sales and reviewed expenses.</p>
              </div>
              <div className="lp-bas-status">
                <p className="lp-bas-status-title">{gstWorkpaper.status}</p>
                <p className="lp-bas-status-sub">Review locally when ready.</p>
              </div>
            </div>

            <div className="lp-bas-review">
              {views.map((view) => (
                <p className="lp-bas-basis-line" data-basis={view.basis} key={view.basis}>
                  {view.line}
                </p>
              ))}
              <p className="lp-bas-period">Saved reporting period: {business.quarter}</p>
              <div className="lp-bas-stale" data-when="stale">
                <p>{staleWarning}</p>
                <button type="reset" form="bas-stale-form" className="lp-btn lp-btn--solid lp-btn--sm">
                  Regenerate local draft
                </button>
              </div>
            </div>

            {views.map((view) => (
              <Figures view={view} key={view.basis} />
            ))}
            <p className="lp-bas-hint">Point at a figure, or tap it, to see its source.</p>

            <div className="lp-bas-sources lp-bas-sales" data-scene="">
              <div className="lp-bas-sources-head">
                <div>
                  <h4 id="bas-sales-title">Invoice sales sources</h4>
                  <p>GST-inclusive sales and GST attributed to this period. Out-of-scope lines are excluded.</p>
                </div>
              </div>
              <table className="lp-table lp-bas-table" aria-labelledby="bas-sales-title">
                <thead>
                  <tr>
                    <th scope="col">Tax date</th>
                    <th scope="col">Customer</th>
                    <th scope="col">Invoice</th>
                    <th scope="col">Source</th>
                    <th scope="col" data-num="">
                      Sales (G1)
                    </th>
                    <th scope="col" data-num="">
                      GST (1A)
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {views.map((view) => (
                    <tr id={`bas-src-sales-${view.basis}`} data-basis={view.basis} key={view.basis}>
                      <td data-label="Tax date">{view.taxDate}</td>
                      <td data-label="Customer">{invoice.customer}</td>
                      <td data-label="Invoice">{invoice.number}</td>
                      <td data-label="Source">{view.source}</td>
                      <td data-label="Sales (G1)" data-num="">
                        <span className="lp-trace-target" data-trace="g1">
                          {aud(view.figures.g1Cents)}
                        </span>
                      </td>
                      <td data-label="GST (1A)" data-num="">
                        <span className="lp-trace-target" data-trace="a1">
                          {aud(view.figures.a1Cents)}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            <div className="lp-bas-sources lp-bas-docs">
              <div className="lp-bas-sources-head">
                <div>
                  <h4 id="bas-docs-title">Reconciled purchase sources</h4>
                  <p>Saved source allocations and claimed GST for this workpaper.</p>
                </div>
              </div>
              <table className="lp-table lp-bas-table" aria-labelledby="bas-docs-title">
                <thead>
                  <tr>
                    <th scope="col">Invoice date</th>
                    <th scope="col">Supplier</th>
                    <th scope="col">Invoice</th>
                    <th scope="col" data-num="">
                      Gross
                    </th>
                    <th scope="col" data-num="">
                      Claimed GST
                    </th>
                  </tr>
                </thead>
                <tbody>
                  <tr id="bas-src-purchase">
                    <td data-label="Invoice date">{purchase.dateShort}</td>
                    <td data-label="Supplier">{purchase.supplier}</td>
                    <td data-label="Invoice">{purchase.reference}</td>
                    <td data-label="Gross" data-num="">
                      {aud(purchase.totalCents)}
                    </td>
                    <td data-label="Claimed GST" data-num="">
                      <span className="lp-trace-target" data-trace="b1">
                        {aud(purchase.gstCents)}
                      </span>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div className="lp-bas-foot">
              <p className="lp-bas-owner">{business.name}</p>
              <p className="lp-bas-exports">
                <span className="lp-visually-hidden">Exports in the app: </span>
                <span className="lp-bas-chip">
                  <Icon name="download" />
                  Export PDF
                </span>{" "}
                <span className="lp-bas-chip">
                  <Icon name="download" />
                  Export CSV
                </span>
              </p>
            </div>
          </div>
        </div>

        {/* The edge of the Mac workspace. The export path reaches it and stops. */}
        <div className="lp-bas-edge">
          <svg className="lp-bas-rule lp-bas-rule--v" aria-hidden="true" focusable="false">
            <line className="lp-line" x1="1" y1="0" x2="1" y2="100%" />
          </svg>
          <svg className="lp-bas-rule lp-bas-rule--h" aria-hidden="true" focusable="false">
            <line className="lp-line" x1="0" y1="1" x2="100%" y2="1" />
          </svg>
          <div className="lp-bas-way" data-scene="">
            <div className="lp-bas-way-clip">
              {/* Drawn at one unit per pixel and clipped to length, so the 1.5px stroke and its dash never scale. */}
              <svg className="lp-bas-way-x" width="128" height="24" viewBox="0 0 128 24" aria-hidden="true" focusable="false">
                <path className="lp-line" data-enter="draw" pathLength={1} d="M0 12H128" />
              </svg>
              <svg className="lp-bas-way-y" width="24" height="96" viewBox="0 0 24 96" aria-hidden="true" focusable="false">
                <path className="lp-line" data-enter="draw" pathLength={1} d="M12 0V96" />
              </svg>
            </div>
            <i className="lp-bas-stop" aria-hidden="true" data-enter="pop" style={{ "--d": "620ms" } as CSSProperties} />
          </div>
          <p className="lp-bas-edge-label">BAS drafts, not lodgement</p>
          <p className="lp-bas-ato">The ATO</p>
          <p className="lp-visually-hidden">
            The export path stops at the edge of the Mac workspace. Tammy sends nothing on to the ATO.
          </p>
        </div>

        <BasLive nonCash={summary(nonCash)} cash={summary(cash)} stale={staleWarning} fresh="Local draft regenerated." />
      </div>
    </section>
  );
}
