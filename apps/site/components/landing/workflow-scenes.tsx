/*
 * The five illustrative screens of "How it works". Each is drawn in its
 * finished state. Parts that exist only before the step is taken carry
 * data-was, parts that arrive carry data-now, passing states carry data-mid,
 * and the app button the step presses carries data-press; workflow.css plays
 * them in order when workflow-figure.tsx sets data-play. Times are
 * milliseconds from the start of the demonstration. Amounts always arrive
 * whole. data-trace pairs a value with the record it came from, for the
 * butter trace.
 *
 * Labels, column heads, button names and notices are the desktop app's own.
 * Every figure comes from scenario.ts.
 */
import type { CSSProperties, ReactNode } from "react";
import { aud } from "../../lib/tools/calculations";
import { AppWindow, cx, Icon, type IconName, Money, Pill, Tick } from "./kit";
import { business, manualJournal, pendingDocument, purchase, statement, trialBalance } from "./scenario";

/* ---------- Choreography helpers ---------- */

const ms = (value: number) => `${value}ms`;

const timing = (at?: number, until?: number) =>
  ({
    ...(at === undefined ? null : { "--at": ms(at) }),
    ...(until === undefined ? null : { "--until": ms(until) }),
  }) as CSSProperties;

type Arrival = "fade" | "land" | "rise" | "type";

/** Shown only before the step is taken; it leaves at `until`. */
function Was({ until, className, children }: { until: number; className?: string; children: ReactNode }) {
  return (
    <span className={className} data-was="" aria-hidden="true" style={timing(undefined, until)}>
      {children}
    </span>
  );
}

/** Part of the finished state; during the demonstration it arrives at `at`. */
function Now({
  at,
  kind = "fade",
  className,
  children,
}: {
  at: number;
  kind?: Arrival;
  className?: string;
  children: ReactNode;
}) {
  return (
    <span className={className} data-now={kind} style={timing(at)}>
      {children}
    </span>
  );
}

/** A passing state: arrives at `at`, leaves at `until`, absent when finished. */
function Mid({
  at,
  until,
  kind = "fade",
  className,
  children,
}: {
  at: number;
  until: number;
  kind?: Arrival;
  className?: string;
  children: ReactNode;
}) {
  return (
    <span className={className} data-mid={kind} aria-hidden="true" style={timing(at, until)}>
      {children}
    </span>
  );
}

/** Stacks the states of one thing in one cell, so swapping never shifts layout. */
function Swap({ className, children }: { className?: string; children: ReactNode }) {
  return <span className={cx("lp-workflow-swap", className)}>{children}</span>;
}

/**
 * An app button drawn as chrome. The demonstration presses it at `at`; a tap
 * replays the step (workflow-figure.tsx listens for data-press). The Replay
 * button under the figure is the keyboard and screen-reader route.
 */
function AppButton({
  at,
  until,
  tone = "solid",
  spent = false,
  open,
  className,
  children,
}: {
  at: number;
  /** When this button gives way to its result, if it does. */
  until?: number;
  tone?: "solid" | "quiet";
  /** Used up once pressed: it rests in the app's disabled state. */
  spent?: boolean;
  /** Unavailable until this time (and spent once pressed). */
  open?: number;
  className?: string;
  children: ReactNode;
}) {
  return (
    <span
      className={cx("lp-workflow-btn", className)}
      data-tone={tone}
      data-state={open === undefined ? (spent ? "spent" : undefined) : "gated"}
      data-press=""
      data-was={until === undefined ? undefined : ""}
      aria-hidden="true"
      style={open === undefined ? timing(at, until) : ({ ...timing(at, until), "--open": ms(open) } as CSSProperties)}
    >
      {children}
    </span>
  );
}

/** A control the demonstration never touches: inert, and not a button. */
function Chrome({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <span className={cx("lp-workflow-chrome", className)} aria-hidden="true">
      {children}
    </span>
  );
}

const plain = (cents: number) => aud(cents).replace("$", "");

/** Keeps a reference such as EX-2041 on one line when its cell wraps. */
const unbroken = (text: string) => text.replaceAll("-", "\u2011");

/* ---------- The window: bar, rail, main ---------- */

type Screen = "Documents" | "Banking" | "Journals" | "Trial balance";

const rail: readonly { group: string; items: readonly { name: string; icon: IconName }[] }[] = [
  {
    group: "Workspace",
    items: [
      { name: "Overview", icon: "overview" },
      { name: "Documents", icon: "document" },
      { name: "Invoices", icon: "invoice" },
    ],
  },
  {
    group: "Accounting",
    items: [
      { name: "Banking", icon: "bank" },
      { name: "Chart of accounts", icon: "chart" },
      { name: "Journals", icon: "journal" },
      { name: "Trial balance", icon: "balance" },
    ],
  },
  {
    group: "Activity",
    items: [
      { name: "GST & BAS", icon: "gst" },
      { name: "Audit trail", icon: "audit" },
    ],
  },
];

/** The app's nine destinations. Inert: it shows the breadth of the app. */
function Rail({ current }: { current: Screen }) {
  return (
    <div className="lp-workflow-rail" aria-hidden="true">
      <span className="lp-workflow-wordmark">tammy</span>
      {rail.map(({ group, items }) => (
        <div className="lp-workflow-rail-group" key={group}>
          <span className="lp-app-eyebrow">{group}</span>
          {items.map(({ name, icon }) => (
            <span className="lp-workflow-rail-item" data-current={name === current ? "" : undefined} key={name}>
              <Icon name={icon} />
              {name}
            </span>
          ))}
        </div>
      ))}
    </div>
  );
}

function Stage({ screen, children }: { screen: Screen; children: ReactNode }) {
  return (
    <AppWindow
      className="lp-workflow-window"
      title={
        <span className="lp-workflow-crumb">
          <span className="lp-workflow-org">{business.name} / </span>
          <b>{screen}</b>
        </span>
      }
      label={null}
      local={null}
      bar={
        <>
          <span className="lp-window-label">
            Illustrative<span className="lp-workflow-roomy"> workspace</span>
            <span className="lp-workflow-snug"> · synthetic</span>
            <span className="lp-workflow-roomy"> figures</span>
          </span>
          <span className="lp-window-local lp-workflow-roomy">
            <Icon name="lock" />
            Local data
          </span>
        </>
      }
    >
      <div className="lp-workflow-app">
        <Rail current={screen} />
        <div className="lp-workflow-main">{children}</div>
      </div>
    </AppWindow>
  );
}

/** A retained file as the Documents list shows it: type, name, then supplier and reference. */
function FileName({ kind, file, supplier, reference }: { kind: string; file: string; supplier: string; reference: string }) {
  return (
    <span className="lp-workflow-file">
      <span className="lp-workflow-filekind">{kind}</span>
      <span className="lp-workflow-filename">
        <b>{file}</b>
        <span>
          {supplier}
          <span className="lp-workflow-roomy"> · {reference}</span>
        </span>
      </span>
    </span>
  );
}

function AccountName({ account }: { account: { code: string; name: string } }) {
  return (
    <span className="lp-workflow-acct">
      <span className="lp-code">{account.code}</span>
      <span>{account.name}</span>
    </span>
  );
}

function Field({
  label,
  span = 2,
  trace,
  className,
  children,
}: {
  label: string;
  /** Columns of the six-column field grid. */
  span?: 2 | 3 | 6;
  trace?: string;
  className?: string;
  children: ReactNode;
}) {
  return (
    <div className={cx("lp-workflow-field", className)} data-span={span} data-trace={trace}>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </div>
  );
}

/* ---------- 1. Documents: a person reviews the source ---------- */

const review = { checks: 520, gap: 170, save: 1900, saved: 2220 } as const;

/** The six details a person checks, in the order the review panel lists them. */
const details = [
  { key: "supplier", label: "Supplier", value: purchase.supplier, source: null, span: 6, core: true },
  { key: "reference", label: "Invoice number", value: purchase.reference, source: "Tax invoice", span: 3, core: false },
  { key: "date", label: "Date", value: purchase.dateShort, source: null, span: 3, core: false },
  { key: "subtotal", label: "Subtotal", value: plain(purchase.subtotalCents), source: "Office supplies", span: 2, core: true },
  { key: "gst", label: "GST", value: plain(purchase.gstCents), source: "GST", span: 2, core: true },
  { key: "total", label: "Total", value: plain(purchase.totalCents), source: "Total", span: 2, core: true },
] as const;

/** A value under the highlighter: the field and its line in the source light together. */
function Traced({ index, children }: { index: number; children: ReactNode }) {
  return (
    <span className="lp-trace-target" style={timing(review.checks + index * review.gap)}>
      {children}
    </span>
  );
}

export function DocumentsScene() {
  return (
    <Stage screen="Documents">
      <div className="lp-workflow-pagehead lp-workflow-tall lp-workflow-roomy">
        <p className="lp-app-title">Documents</p>
        <Chrome>
          <Icon name="upload" />
          Upload document
        </Chrome>
      </div>

      <div className="lp-workflow-card">
        <table className="lp-table lp-workflow-table lp-workflow-docs">
          <thead>
            <tr>
              <th scope="col">
                Document<span className="lp-workflow-roomy"> &amp; supplier</span>
              </th>
              <th scope="col" data-num="">
                Total · AUD
              </th>
              <th scope="col">Status</th>
            </tr>
          </thead>
          <tbody>
            <tr data-selected="">
              <th scope="row">
                <FileName
                  kind={purchase.fileKind}
                  file={purchase.file}
                  supplier={purchase.supplier}
                  reference={purchase.reference}
                />
              </th>
              <td data-num="">
                <Money cents={purchase.totalCents} />
              </td>
              <td aria-label="Reviewed">
                <Swap>
                  <Was until={review.saved}>
                    <Pill tone="warn" dot>
                      Needs review
                    </Pill>
                  </Was>
                  <Now at={review.saved} kind="land">
                    <Pill tone="ok" tick>
                      Reviewed
                    </Pill>
                  </Now>
                </Swap>
              </td>
            </tr>
            <tr className="lp-workflow-spare lp-workflow-tall">
              <th scope="row">
                <FileName
                  kind={pendingDocument.fileKind}
                  file={pendingDocument.file}
                  supplier={pendingDocument.supplier}
                  reference={pendingDocument.reference}
                />
              </th>
              <td data-num="">
                <Money cents={pendingDocument.totalCents} />
              </td>
              <td>
                <Pill tone="warn" dot>
                  Needs review
                </Pill>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <div
        className="lp-workflow-card lp-workflow-review lp-workflow-traced"
        style={{ "--clear": ms(review.saved) } as CSSProperties}
      >
        <div className="lp-workflow-source">
          <p className="lp-workflow-panelhead">
            <b>Extracted source text</b>
            <span>Native PDF text</span>
          </p>
          <div className="lp-workflow-sourcetext">
            <p data-trace="supplier">
              <Traced index={0}>{purchase.supplier}</Traced>
            </p>
            {details.slice(1).map((detail, index) => (
              <p data-trace={detail.key} key={detail.key}>
                {detail.source ? <span>{detail.source}</span> : null}
                <Traced index={index + 1}>{detail.key === "date" ? purchase.date : detail.value}</Traced>
              </p>
            ))}
          </div>
        </div>

        <div className="lp-workflow-check">
          <p className="lp-workflow-panelhead">
            <b>Check the details</b>
            <Swap className="lp-workflow-roomy">
              <Was until={review.saved + 120}>
                <Pill tone="warn" dot>
                  Needs review
                </Pill>
              </Was>
              <Now at={review.saved + 120} kind="land">
                <Pill tone="ok" tick>
                  Reviewed
                </Pill>
              </Now>
            </Swap>
          </p>
          <dl className="lp-workflow-fields">
            {details.map((detail, index) => (
              <Field
                label={detail.label}
                span={detail.span}
                trace={detail.key}
                className={detail.core ? undefined : "lp-workflow-roomy"}
                key={detail.key}
              >
                <Traced index={index}>{detail.value}</Traced>
              </Field>
            ))}
          </dl>
          <div className="lp-workflow-actions">
            <AppButton at={review.save} spent>
              <Swap>
                <Was until={review.saved}>Save review</Was>
                <Now at={review.saved}>Review saved</Now>
              </Swap>
            </AppButton>
            <Chrome className="lp-workflow-roomy">Cancel changes</Chrome>
          </div>
        </div>
      </div>
    </Stage>
  );
}

/* ---------- 2. Purchases: the reviewed document posts once ---------- */

const posting = { add: 600, confirmBox: 900, confirm: 1900, posted: 2250, gap: 140 } as const;

function EntryLines({
  lines,
  startAt,
  trace,
}: {
  lines: readonly { side: "debit" | "credit"; account: { code: string; name: string }; cents: number }[];
  startAt: number;
  /** Lines whose amount is traced back to a field: account code to trace key, and when the stroke is drawn. */
  trace?: { keys: Readonly<Record<string, string>>; at: number };
}) {
  const total = lines.filter((line) => line.side === "debit").reduce((sum, line) => sum + line.cents, 0);
  const amount = (line: (typeof lines)[number]) =>
    trace?.keys[line.account.code] ? (
      <span className="lp-trace-target" style={timing(trace.at)}>
        <Money cents={line.cents} />
      </span>
    ) : (
      <Money cents={line.cents} />
    );
  return (
    <table className="lp-table lp-workflow-table lp-workflow-ledger" data-now="fade" style={timing(startAt - 120)}>
      <caption>Journal detail · Purchase accounting</caption>
      <thead>
        <tr>
          <th scope="col">Account</th>
          <th scope="col" data-num="">
            Debit
          </th>
          <th scope="col" data-num="">
            Credit
          </th>
        </tr>
      </thead>
      <tbody>
        {lines.map((line, index) => (
          <tr
            data-now="row"
            data-trace={trace?.keys[line.account.code]}
            style={timing(startAt + 60 + index * posting.gap)}
            key={line.account.code}
          >
            <th scope="row">
              <AccountName account={line.account} />
            </th>
            <td data-num="">{line.side === "debit" ? amount(line) : null}</td>
            <td data-num="">{line.side === "credit" ? amount(line) : null}</td>
          </tr>
        ))}
      </tbody>
      <tfoot>
        <tr data-now="row" style={timing(startAt + 120 + lines.length * posting.gap)}>
          <th scope="row">Total</th>
          <td data-num="">
            <Money cents={total} />
          </td>
          <td data-num="">
            <Money cents={total} />
          </td>
        </tr>
      </tfoot>
    </table>
  );
}

export function PurchaseScene() {
  const linesAt = posting.posted + 150;
  const landed = linesAt + purchase.lines.length * posting.gap + 260;
  // Once posted, the GST and the payable are traced back to the allocation they came from.
  const tracedAt = landed + 280;
  const [, gstLine, payableLine] = purchase.lines;
  return (
    <Stage screen="Documents">
      <div className="lp-workflow-pagehead lp-workflow-tall lp-workflow-roomy">
        <p className="lp-app-title">Documents</p>
      </div>

      <div className="lp-workflow-card lp-workflow-docrow lp-workflow-roomy">
        <FileName
          kind={purchase.fileKind}
          file={purchase.file}
          supplier={purchase.supplier}
          reference={purchase.reference}
        />
        <Pill tone="ok" tick>
          Reviewed
        </Pill>
        <Money cents={purchase.totalCents} className="lp-workflow-strong" />
      </div>

      <div
        className="lp-workflow-card lp-workflow-panel lp-workflow-traced"
        style={{ "--clear": ms(tracedAt + 1150) } as CSSProperties}
      >
        <p className="lp-workflow-panelhead">
          <b>Purchase accounting</b>
          <Swap>
            <Was until={landed}>
              <Pill tone="warn">Reviewed source · not added to accounts</Pill>
            </Was>
            <Now at={landed} kind="land">
              <Pill tone="ok">Added to accounts · {business.basis}</Pill>
            </Now>
          </Swap>
        </p>

        <dl className="lp-workflow-fields lp-workflow-alloc lp-workflow-roomy">
          <Field label="Account 1">
            {purchase.lines[0].account.code} — {purchase.lines[0].account.name}
          </Field>
          <Field label="Gross 1 (AUD)" trace="gross">
            <span className="lp-trace-target" style={timing(tracedAt)}>
              {plain(purchase.totalCents)}
            </span>
          </Field>
          <Field label="Eligible GST 1 (AUD)" trace="gst">
            <span className="lp-trace-target" style={timing(tracedAt + 140)}>
              {plain(purchase.gstCents)}
            </span>
          </Field>
        </dl>

        <div className="lp-workflow-layers">
          <Was until={posting.posted} className="lp-workflow-ask">
            <AppButton at={posting.add}>Add purchase to accounts</AppButton>
            <Now at={posting.confirmBox} kind="rise" className="lp-workflow-confirm">
              <span>Records the expense once and creates the supplier payable. No bank payment is made.</span>
              <span className="lp-workflow-actions">
                <AppButton at={posting.confirm}>Confirm purchase posting</AppButton>
                <Chrome className="lp-workflow-roomy">Keep reviewing</Chrome>
              </span>
            </Now>
          </Was>
          <EntryLines
            lines={purchase.lines}
            startAt={linesAt}
            trace={{ keys: { [payableLine.account.code]: "gross", [gstLine.account.code]: "gst" }, at: tracedAt }}
          />
        </div>
      </div>
    </Stage>
  );
}

/* ---------- 3. Journals: one amount, both sides ---------- */

const entry = { amount: 450, debit: 900, credit: 1400, strip: 1850, post: 2800, posted: 3120 } as const;

export function JournalScene() {
  const amount = <Money cents={manualJournal.cents} />;
  // The app writes an account in a select as its code, an em dash, its name.
  const debitAccount = `${manualJournal.debit.code} — ${manualJournal.debit.name}`;
  const creditAccount = `${manualJournal.credit.code} — ${manualJournal.credit.name}`;
  return (
    <Stage screen="Journals">
      <div className="lp-workflow-pagehead lp-workflow-tall lp-workflow-roomy">
        <p className="lp-app-title">Journals</p>
        <span className="lp-workflow-sub">Review both sides before posting to your books.</span>
      </div>

      <div className="lp-workflow-card lp-workflow-panel lp-workflow-entry">
        <p className="lp-workflow-panelhead">
          <b className="lp-workflow-serif">Manual entry</b>
          <Now at={entry.posted} kind="land">
            <Pill tone="ok">Posted</Pill>
          </Now>
        </p>

        <dl className="lp-workflow-fields lp-workflow-entryfields lp-workflow-roomy">
          <Field label="Date">{manualJournal.dateShort}</Field>
          <Field label="Description" span={6}>
            {manualJournal.description}
          </Field>
          <Field label="Amount (AUD)">
            <Now at={entry.amount}>{plain(manualJournal.cents)}</Now>
          </Field>
        </dl>

        <table className="lp-table lp-workflow-table lp-workflow-sides">
          <thead>
            <tr>
              <th scope="col">Ordinary account</th>
              <th scope="col" data-num="">
                Debit<span className="lp-workflow-roomy"> · AUD</span>
              </th>
              <th scope="col" data-num="">
                Credit<span className="lp-workflow-roomy"> · AUD</span>
              </th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <th scope="row" aria-label={`Debit account: ${debitAccount}`}>
                <span className="lp-workflow-select">
                  <Now at={entry.debit}>{debitAccount}</Now>
                </span>
              </th>
              <td data-num="" aria-label={aud(manualJournal.cents)}>
                <Swap>
                  <Was until={entry.debit}>–</Was>
                  <Now at={entry.debit}>{amount}</Now>
                </Swap>
              </td>
              <td data-num="">–</td>
            </tr>
            <tr>
              <th scope="row" aria-label={`Credit account: ${creditAccount}`}>
                <span className="lp-workflow-select">
                  <Now at={entry.credit}>{creditAccount}</Now>
                </span>
              </th>
              <td data-num="">–</td>
              <td data-num="" aria-label={aud(manualJournal.cents)}>
                <Swap>
                  <Was until={entry.credit}>–</Was>
                  <Now at={entry.credit}>{amount}</Now>
                </Swap>
              </td>
            </tr>
          </tbody>
        </table>

        <dl className="lp-workflow-strip" style={timing(entry.strip)}>
          <div>
            <dt>
              Total debit
              <span className="lp-workflow-roomy"> · AUD</span>
            </dt>
            <dd>
              <Now at={entry.strip + 160}>{amount}</Now>
            </dd>
          </div>
          <div>
            <dt>
              Total credit
              <span className="lp-workflow-roomy"> · AUD</span>
            </dt>
            <dd>
              <Now at={entry.strip + 280}>{amount}</Now>
            </dd>
          </div>
          <div>
            <dt>
              Difference
              <span className="lp-workflow-roomy"> · AUD</span>
            </dt>
            <dd>
              <Now at={entry.strip + 400}>
                <Money cents={0} />
              </Now>
            </dd>
          </div>
        </dl>

        <div className="lp-workflow-panelfoot">
          <span className="lp-workflow-sub lp-workflow-roomy">
            Two ordinary accounts. Posting records an equal debit and credit in your books.
          </span>
          <AppButton at={entry.post} spent>
            Post journal
          </AppButton>
        </div>
      </div>
    </Stage>
  );
}

/* ---------- 4. Banking: a person makes every match ---------- */

/** Two matches, one after the other, then the reconciliation. */
const match = [
  { press: 350, sheet: 600, typed: 850, confirm: 1450, matched: 1720 },
  { press: 2050, sheet: 2250, typed: 2450, confirm: 2950, matched: 3200 },
] as const;
const reconcile = { press: 3700, done: 3980 } as const;

/** The match panel opens and closes once per match; its button is pressed twice. */
const sheetTiming = {
  "--in-1": ms(match[0].sheet),
  "--out-1": ms(match[0].matched - 40),
  "--in-2": ms(match[1].sheet),
  "--out-2": ms(match[1].matched - 40),
  "--press-1": ms(match[0].confirm),
  "--press-2": ms(match[1].confirm),
} as CSSProperties;

export function BankingScene() {
  const rows = statement.rows.map((row, index) => ({ ...row, at: match[index] ?? match[0] }));
  const allMatched = match[match.length - 1]?.matched ?? 0;
  return (
    <Stage screen="Banking">
      <div className="lp-workflow-pagehead lp-workflow-tall lp-workflow-roomy">
        <p className="lp-app-title">Banking</p>
      </div>

      <div className="lp-workflow-card lp-workflow-panel lp-workflow-bank">
        <p className="lp-workflow-panelhead">
          <b className="lp-workflow-serif lp-workflow-roomy">Statement lines</b>
          <span>Imported CSV · no live bank feed</span>
        </p>

        <table className="lp-table lp-workflow-table lp-workflow-lines">
          <thead>
            <tr>
              <th scope="col" className="lp-workflow-wide">
                Date
              </th>
              <th scope="col">
                Description<span className="lp-workflow-roomy"> &amp; reference</span>
              </th>
              <th scope="col" data-num="">
                Amount<span className="lp-workflow-roomy"> · AUD</span>
              </th>
              <th scope="col">State</th>
              <th scope="col" data-num="">
                Action
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row, index) => (
              <tr key={row.reference}>
                <td className="lp-workflow-wide lp-num">{row.date}</td>
                <th scope="row" aria-label={`${row.description}. Reviewed source reference ${row.reference}`}>
                  <span className="lp-workflow-line">
                    <b>{unbroken(row.description)}</b>
                    <Swap>
                      <Was until={row.at.matched}>No reviewed reference yet</Was>
                      <Now at={row.at.matched}>{row.reference}</Now>
                    </Swap>
                  </span>
                </th>
                <td data-num="">
                  <Money cents={row.cents} className="lp-workflow-strong" />
                </td>
                <td aria-label="Reconciled">
                  <Swap>
                    <Was until={row.at.matched}>
                      <Pill tone="warn">Unmatched</Pill>
                    </Was>
                    <Mid at={row.at.matched} until={reconcile.done + index * 110} kind="land">
                      <Pill tone="ok" tick>
                        Matched
                      </Pill>
                    </Mid>
                    <Now at={reconcile.done + index * 110} kind="land">
                      <Pill tone="ok">Reconciled</Pill>
                    </Now>
                  </Swap>
                </td>
                <td data-num="" aria-label="Complete">
                  <Swap className="lp-workflow-action">
                    <AppButton at={row.at.press} until={row.at.matched} tone="quiet">
                      Match
                    </AppButton>
                    <span style={timing(row.at.matched + 80)}>
                      <Tick data-now="draw" />
                    </span>
                  </Swap>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        <div className="lp-workflow-layers lp-workflow-recon">
          <div className="lp-workflow-summary">
            <dl className="lp-workflow-metrics lp-workflow-roomy">
              <div>
                <dt>Latest import balance · AUD</dt>
                <dd>
                  <Money cents={statement.closingCents} />
                </dd>
              </div>
              <div>
                <dt>Unmatched</dt>
                <dd>
                  <Swap>
                    <Was until={match[0].matched}>2</Was>
                    <Mid at={match[0].matched} until={allMatched}>
                      1
                    </Mid>
                    <Now at={allMatched}>0</Now>
                  </Swap>
                </dd>
              </div>
              <div>
                <dt>Unreconciled</dt>
                <dd>
                  <Swap>
                    <Was until={reconcile.done}>2</Was>
                    <Now at={reconcile.done}>0</Now>
                  </Swap>
                </dd>
              </div>
            </dl>
            <div className="lp-workflow-panelfoot">
              <Swap className="lp-workflow-sub">
                <Was until={allMatched}>Review every unmatched line before completing reconciliation.</Was>
                <Mid at={allMatched} until={reconcile.done}>
                  Every imported line is matched. You can complete reconciliation.
                </Mid>
                <Now at={reconcile.done}>
                  Reconciliation complete · {statement.rows.length} lines reconciled.{" "}
                  <b>Closing balance {aud(statement.closingCents)}.</b>
                </Now>
              </Swap>
              <AppButton at={reconcile.press} open={allMatched}>
                Complete reconciliation
              </AppButton>
            </div>
          </div>

          {/* The match panel: open only while a person is making a match. */}
          <div className="lp-workflow-sheet" aria-hidden="true" style={sheetTiming}>
            <span className="lp-workflow-sheetfield">
              <span className="lp-workflow-sheetlabel">
                <b>Review a match</b>
                Reviewed source reference
              </span>
              <span className="lp-workflow-input">
                <Swap>
                  {rows.map((row) => (
                    <Mid at={row.at.typed} until={row.at.matched + 200} kind="type" key={row.reference}>
                      {row.reference}
                    </Mid>
                  ))}
                </Swap>
              </span>
            </span>
            <span className="lp-workflow-actions">
              <span className="lp-workflow-btn lp-workflow-confirmmatch" data-tone="solid" data-press="">
                Confirm match
              </span>
              <Chrome className="lp-workflow-roomy">Cancel match</Chrome>
            </span>
          </div>
        </div>
      </div>
    </Stage>
  );
}

/* ---------- 5. Trial balance: debits equal credits ---------- */

const balance = { refresh: 450, rows: 720, gap: 140 } as const;

export function TrialBalanceScene() {
  const totalAt = balance.rows + trialBalance.rows.length * balance.gap + 160;
  return (
    <Stage screen="Trial balance">
      <div className="lp-workflow-pagehead">
        <p className="lp-workflow-titleblock lp-workflow-roomy">
          <span className="lp-app-eyebrow">Accounting · Reports</span>
          <span className="lp-app-title">Trial balance</span>
        </p>
        <p className="lp-workflow-asat">
          <span>
            As at <b>{trialBalance.asAt}</b>
          </span>
          <AppButton at={balance.refresh} tone="quiet" className="lp-workflow-iconbtn">
            <Icon name="replay" />
          </AppButton>
        </p>
      </div>

      <div className="lp-workflow-card">
        <table className="lp-table lp-workflow-table lp-workflow-tb">
          <thead>
            <tr>
              <th scope="col">Account</th>
              <th scope="col" data-num="">
                Debit<span className="lp-workflow-roomy"> (AUD)</span>
              </th>
              <th scope="col" data-num="">
                Credit<span className="lp-workflow-roomy"> (AUD)</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {trialBalance.rows.map((row, index) => (
              <tr data-now="row" style={timing(balance.rows + index * balance.gap)} key={row.account.code}>
                <th scope="row">
                  <AccountName account={row.account} />
                </th>
                <td data-num="" data-zero={row.debitCents === 0 ? "" : undefined}>
                  <Money cents={row.debitCents} />
                </td>
                <td data-num="" data-zero={row.creditCents === 0 ? "" : undefined}>
                  <Money cents={row.creditCents} />
                </td>
              </tr>
            ))}
          </tbody>
          <tfoot>
            <tr data-now="row" style={timing(totalAt)}>
              <th scope="row">
                Total
                <span className="lp-workflow-agree" style={timing(totalAt + 260)}>
                  <Tick data-now="draw" />
                  <Now at={totalAt + 380}>Debits equal credits</Now>
                </span>
              </th>
              <td data-num="">
                <Money cents={trialBalance.totalCents} />
              </td>
              <td data-num="">
                <Money cents={trialBalance.totalCents} />
              </td>
            </tr>
          </tfoot>
        </table>
      </div>
    </Stage>
  );
}
