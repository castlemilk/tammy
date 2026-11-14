/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
/*
 * Free business tools: the band shell and the tray of twelve.
 *
 * Every worked answer in the tray is computed here, at render, by the tool's
 * own pure function, from fixed example inputs. Nothing is typed in as a
 * result, so an answer can never disagree with the tool it links to. This is
 * a server component: none of the calculation code reaches the browser.
 */
import type { CSSProperties, ReactNode } from "react";
import { calculateInvoice, formatQuantity } from "@tammy/invoices/model";
import { publicTools, type PublicTool } from "../../lib/tool-library";
import { checkAbn, formattedAbn } from "../../lib/tools/abn";
import { calculateBreakEven } from "../../lib/tools/break-even";
import { addCalendarDays, aud, australianDate, gstBreakdown } from "../../lib/tools/calculations";
import { computeCashForecast, exampleForecast } from "../../lib/tools/cash-forecast";
import { estimateCashRunway, formatRunway } from "../../lib/tools/cash-runway";
import { calculateMargin, formatPercent } from "../../lib/tools/margins";
import { compileQuote } from "../../lib/tools/quote";
import { prepareSimpleBas } from "../../lib/tools/simpler-bas";
import { Bench } from "./bench";
import { Note } from "./kit";

/* Fixed example inputs. Dates are fixed so server and client agree. */
const ISSUE_DATE = { iso: "2026-11-03", label: "3 November 2026" } as const;
const TERMS_DAYS = 14;
const FORECAST_START = "2026-11-12";
const FORECAST_OPENING_CENTS = 1_200_000;
const EXAMPLE_ABN = "51 824 753 556";

type Cell = {
  href: string;
  /** Column spans in the 12, 6 and 2 column trays. */
  spans: readonly [number, number, number];
  summary: string;
  answer: ReactNode;
  /** A small drawing made from the same result. Decorative: the answer says it. */
  figure?: ReactNode;
  /** What the tool produces: a file or a print to keep, or an answer in the tab. */
  tag: string;
  keeps?: boolean;
  /** The tool's stated limit, in a few words. */
  limit: string;
  featured?: boolean;
};

/** A pause for screen readers between the parts of a cell; the layout does the same for the eye. */
function Stop({ mark = "." }: { mark?: string }) {
  return <span className="lp-visually-hidden">{mark} </span>;
}

function Fig({ children }: { children: ReactNode }) {
  return <span className="lp-tools-fig">{children}</span>;
}

/** A label, its figure and any trailing separator, kept on one line. */
function Pair({ children }: { children: ReactNode }) {
  return <span className="lp-tools-pair">{children}</span>;
}

const wholeDollars = (dollars: number) => "$" + dollars.toLocaleString("en-AU");

function trayCells(): Cell[] {
  /* Invoice generator: one taxable line through the shared invoice model. */
  const line = { quantityMilli: 1_500, unitPriceMinor: 12_000 };
  const invoice = calculateInvoice({
    number: "INV-001",
    issueDate: ISSUE_DATE.iso,
    dueDate: "",
    customerName: "",
    customerAddress: "",
    sellerAddress: "",
    gstRegistered: true,
    notes: "",
    paymentInstructions: "",
    lines: [{ description: "Example line", ...line, taxTreatment: "taxable" }],
  });

  /* Quote generator: the quote tool's own compile step, which reuses that model. */
  const quoteLine = { quantity: "3", unitPrice: "85.00" };
  const quote = compileQuote({
    sellerName: "Example Trading Co",
    sellerAbn: "",
    sellerAddress: "",
    customerName: "Example Studio",
    customerAddress: "",
    reference: "QUO-001",
    quoteDate: ISSUE_DATE.iso,
    validUntil: "2026-12-03",
    gstRegistered: true,
    notes: "",
    lines: [{ description: "Example line", ...quoteLine, treatment: "taxable" }],
  });
  const quoteUnitCents = quote.draft.lines[0]?.unitPriceMinor ?? 0;
  const quoteQuantity = quote.draft.lines[0]?.quantityMilli ?? 1_000;

  const gst = gstBreakdown(11_000, "remove");

  /* The workpaper's own defaults: $22,000 taxable sales, $7,150 purchases. */
  const bas = prepareSimpleBas({
    taxableSalesInclusiveCents: 2_200_000,
    otherReportableSalesCents: 0,
    creditablePurchasesInclusiveCents: 715_000,
  });

  const abn = checkAbn(EXAMPLE_ABN);

  const dueIso = addCalendarDays(ISSUE_DATE.iso, TERMS_DAYS);

  const margin = calculateMargin(8_000, 12_000);

  const fixedCostsCents = 300_000;
  const breakEven = calculateBreakEven(fixedCostsCents, 12_000, 8_000);

  const runway = estimateCashRunway(2_400_000, 600_000, 1_000_000);

  const forecast = computeCashForecast(FORECAST_START, FORECAST_OPENING_CENTS, exampleForecast(FORECAST_START));
  const lowestWeek = forecast.weeks.find((week) => week.closingCents === forecast.lowestClosingCents);
  const tallestCents = Math.max(1, ...forecast.weeks.map((week) => week.closingCents));

  return [
    {
      href: "/tools/invoice-generator",
      spans: [5, 3, 2],
      featured: true,
      summary: "Build an Australian invoice with itemised GST and export a PDF to keep.",
      answer: (
        <span className="lp-tools-sum">
          <span className="lp-tools-sum-row">
            <span>
              {formatQuantity(line.quantityMilli)} × {aud(line.unitPriceMinor)}
            </span>{" "}
            <span className="lp-num">{aud(invoice.subtotalMinor)}</span>
          </span>
          <Stop mark="," />
          <span className="lp-tools-sum-row">
            <span>GST at 10%</span> <span className="lp-num">{aud(invoice.gstMinor)}</span>
          </span>
          <Stop mark="," />
          <span className="lp-tools-sum-row lp-tools-sum-total">
            <span>Total</span>{" "}
            <span className="lp-num">
              <span className="lp-trace-target">{aud(invoice.totalMinor)}</span>
            </span>
          </span>
        </span>
      ),
      tag: "A4 PDF",
      keeps: true,
      limit: "Taxable and GST-free lines only",
    },
    {
      href: "/tools/quote-generator",
      spans: [4, 3, 1],
      summary: "An itemised estimate with GST, valid until a date you set.",
      answer: (
        <>
          <Fig>{formatQuantity(quoteQuantity)} × {aud(quoteUnitCents)}</Fig>{" "}
          plus GST is <Fig>{aud(quote.totals.totalMinor)}</Fig>
        </>
      ),
      tag: "PDF, print, draft file",
      keeps: true,
      limit: "Not a tax invoice",
    },
    {
      href: "/tools/invoice-due-date-calculator",
      spans: [3, 3, 1],
      summary: "Turn agreed payment terms into an exact due date.",
      answer: dueIso ? (
        <>
          <Fig>{TERMS_DAYS} days</Fig> from {ISSUE_DATE.label} is <Fig>{australianDate(dueIso)}</Fig>
        </>
      ) : (
        "An exact date from the invoice date and the terms"
      ),
      tag: "Worked in the tab",
      limit: "Calendar days",
    },
    {
      href: "/tools/gst-calculator",
      spans: [3, 2, 1],
      summary: "Add GST to a price, or find the GST inside a total.",
      answer: (
        <>
          <Fig>{aud(gst.inclusiveCents)}</Fig> includes <Fig>{aud(gst.gstCents)}</Fig> GST
        </>
      ),
      tag: "Worked in the tab",
      limit: "10% rate only",
    },
    {
      href: "/tools/abn-checker",
      spans: [4, 4, 1],
      summary: "Check an ABN's eleven digits against its checksum.",
      answer:
        abn.status === "valid" ? (
          <>
            <Fig>{formattedAbn(abn.digits)}</Fig> passes the checksum
          </>
        ) : (
          "Pass or fail for any eleven digits"
        ),
      tag: "Worked in the tab",
      limit: "Format only",
    },
    {
      href: "/tools/bas-workpaper",
      spans: [4, 3, 1],
      summary: "See how G1, 1A and 1B come out of three totals.",
      answer: (
        <>
          <Pair>
            G1 <Fig>{wholeDollars(bas.entryG1Whole)}</Fig>,
          </Pair>{" "}
          <Pair>
            1A <Fig>{wholeDollars(bas.entry1AWhole)}</Fig>,
          </Pair>{" "}
          <Pair>
            1B <Fig>{wholeDollars(bas.entry1BWhole)}</Fig>
          </Pair>
        </>
      ),
      tag: "Worked in the tab",
      limit: "Preparation only",
    },
    {
      href: "/tools/profit-margin-calculator",
      spans: [5, 3, 1],
      summary: "Profit, margin and markup from one cost and one price.",
      answer: (
        <>
          <Pair>
            <Fig>{aud(margin.profitCents)}</Fig> profit,
          </Pair>{" "}
          <Pair>
            <Fig>{formatPercent(margin.marginPercent)}</Fig> margin,
          </Pair>{" "}
          <Pair>
            <Fig>{formatPercent(margin.markupPercent)}</Fig> markup
          </Pair>
        </>
      ),
      tag: "Worked in the tab",
      limit: "Per unit, before GST",
    },
    {
      href: "/tools/break-even-calculator",
      spans: [3, 2, 1],
      summary: "The units to sell before fixed costs are covered.",
      answer:
        breakEven.status === "possible" ? (
          <>
            <Fig>{breakEven.units.toLocaleString("en-AU")} units</Fig> cover <Fig>{aud(fixedCostsCents)}</Fig>
          </>
        ) : (
          "The units and sales that cover fixed costs"
        ),
      tag: "Worked in the tab",
      limit: "One product",
    },
    {
      href: "/tools/cash-runway-calculator",
      spans: [3, 2, 1],
      summary: "How many months the cash lasts at the current burn.",
      answer:
        runway.status === "decreasing" ? (
          <>
            <Fig>{formatRunway(runway.runwayTenths)}</Fig> at <Fig>{aud(-runway.monthlyNetCents)}</Fig> a month
          </>
        ) : (
          "Months of cover at a constant monthly rate"
        ),
      tag: "Worked in the tab",
      limit: "Constant rate",
    },
    {
      href: "/tools/cash-flow-forecast",
      spans: [3, 2, 2],
      summary: "Put dated money in and out on a calendar and see the lowest week.",
      answer: lowestWeek ? (
        <>
          Week {lowestWeek.week} closes lowest: <Fig>{aud(lowestWeek.closingCents)}</Fig>
        </>
      ) : (
        <>
          Closing cash never falls below <Fig>{aud(forecast.lowestClosingCents)}</Fig>
        </>
      ),
      figure: (
        <svg className="lp-tools-bars" viewBox="0 0 142 36" aria-hidden="true" focusable="false">
          {forecast.weeks.map((week, index) => {
            const height = Math.max(2, Math.round((Math.max(0, week.closingCents) / tallestCents) * 34));
            return (
              <rect
                key={week.week}
                className={week === lowestWeek ? "lp-tools-bar lp-tools-bar-low" : "lp-tools-bar"}
                x={index * 12}
                y={36 - height}
                width="10"
                height={height}
                rx="1.5"
              />
            );
          })}
        </svg>
      ),
      tag: "CSV",
      keeps: true,
      limit: "Figures you enter",
    },
    {
      href: "/tools/expense-organiser",
      spans: [3, 3, 1],
      summary: "Record receipts, note the GST evidence and export a worksheet.",
      answer: (
        <>
          Up to <Fig>100 rows</Fig>, exported as CSV or JSON
        </>
      ),
      tag: "CSV and JSON",
      keeps: true,
      limit: "Clears on reload",
    },
    {
      href: "/tools/sole-trader-tax-checklist",
      spans: [3, 3, 1],
      summary: "The business records to review before your individual return.",
      answer: (
        <>
          <Fig>Eight checks</Fig> before you lodge in myTax
        </>
      ),
      tag: "Printable",
      keeps: true,
      limit: "Calculates no tax",
    },
  ];
}

function TrayCell({ tool, cell, index }: { tool: PublicTool; cell: Cell | undefined; index: number }) {
  const [wide, medium, narrow] = cell?.spans ?? [3, 2, 1];
  const style = { "--c12": wide, "--c6": medium, "--c2": narrow, "--i": index } as CSSProperties;
  return (
    <li className="lp-tools-slot" data-featured={cell?.featured ? "" : undefined} style={style}>
      <a className="lp-tools-cell" href={tool.href}>
        <span className="lp-tools-name">{tool.name}</span>
        <Stop />
        <span className="lp-tools-summary">{cell?.summary ?? tool.summary}</span>{" "}
        {cell ? (
          <span className="lp-tools-answer">
            {cell.featured ? (
              cell.answer
            ) : (
              <span className="lp-tools-answer-text">
                <span className="lp-trace-target">{cell.answer}</span>
              </span>
            )}
            {cell.figure ? <span className="lp-tools-figure">{cell.figure}</span> : null}
            <Stop />
          </span>
        ) : null}
        <span className="lp-tools-meta">
          <span className="lp-tools-tag" data-keeps={cell?.keeps ? "" : undefined}>
            {cell?.tag ?? tool.note}
          </span>
          <Stop />
          {cell ? <span className="lp-tools-limit">{cell.limit}</span> : null}
        </span>
      </a>
    </li>
  );
}

export function Tools() {
  const cells = trayCells();
  const byHref = new Map(publicTools.map((tool) => [tool.href, tool]));
  /* Tray order first; a tool added to the catalogue later still gets a cell. */
  const placed = cells.flatMap((cell) => {
    const tool = byHref.get(cell.href);
    return tool ? [{ tool, cell }] : [];
  });
  const known = new Set(cells.map((cell) => cell.href));
  const unplaced = publicTools.filter((tool) => !known.has(tool.href));

  return (
    <section
      className="lp-section lp-tools lp-perf"
      id="free-tools"
      data-room="browser"
      aria-labelledby="landing-tools-heading"
      style={{ "--l-perf": "var(--l-ink)" } as CSSProperties}
    >
      <div className="lp-frame">
        <header className="lp-tools-head">
          <span className="lp-tools-twelve" aria-hidden="true">12</span>
          <h2 className="lp-h2" id="landing-tools-heading">Free business tools</h2>
          <p className="lp-deck">They work in your browser today. Nothing to sign up for.</p>
          <Note className="lp-tools-boundary">
            Every tool does its working in your browser tab. Nothing here issues an invoice in Tammy, files a BAS or
            sends your figures to a server.
          </Note>
        </header>

        <Bench />

        <div className="lp-tools-drawer">
          <div className="lp-tools-drawer-head">
            <h3 className="lp-h3" id="tools-tray-heading">All twelve, each with a worked answer</h3>
            <p className="lp-small">Example figures in, each tool&apos;s own answer out.</p>
          </div>
          <ul className="lp-tools-tray" aria-labelledby="tools-tray-heading" data-scene="">
            {placed.map(({ tool, cell }, index) => (
              <TrayCell key={tool.href} tool={tool} cell={cell} index={index} />
            ))}
            {unplaced.map((tool, index) => (
              <TrayCell key={tool.href} tool={tool} cell={undefined} index={placed.length + index} />
            ))}
          </ul>
        </div>

        <div className="lp-tools-more">
          <p className="lp-tools-more-links">
            <a className="lp-link" href="/tools">See all tools</a>
            <a className="lp-link" href="/guides">Read the guides</a>
          </p>
          <p className="lp-small">
            The two guides cover the Australian tax invoice checklist, and GST-inclusive and exclusive pricing.
          </p>
        </div>
      </div>
    </section>
  );
}
