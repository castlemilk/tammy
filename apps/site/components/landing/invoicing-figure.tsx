"use client";

/*
 * Customer invoicing: an invoice sheet beside the ledger's entry lines.
 *
 * The visitor presses the app's own buttons. One press issues the invoice and
 * three ledger lines arrive together; one more records a receipt.
 *
 * Contract:
 *   - Server HTML, no script, jsdom and reduced motion get the finished books
 *     (issued, part paid). Nothing here is hidden in the markup.
 *   - Script holds one step and renders that step's words, pill and figures.
 *     Every movement is CSS in invoicing.css, keyed off data-state and
 *     data-via on the figure, so a step can never be left half-played.
 *   - Amounts are replaced whole. Nothing counts up.
 */
import { type CSSProperties, type ReactNode, useEffect, useRef, useState } from "react";
import { aud } from "../../lib/tools/calculations";
import { AppWindow, Icon, Pill, Tick } from "./kit";
import { accounts, business, invoice, overview } from "./scenario";

type Step = "draft" | "confirming" | "issued" | "paid";
/** "press" plays the choreography; "rest" is a plain state with no movement. */
type Via = "rest" | "press";
type TraceKey = "total" | "subtotal" | "gst";
type View = { step: Step; via: Via };

const FINISHED: View = { step: "paid", via: "rest" };

/** The desktop app's own confirmation, word for word. */
const CONFIRM_COPY =
  "Issuing locks this invoice, removes the draft watermark and records sales, GST and the amount owing in the business ledger. Record actual receipts separately. Your customer does not receive it automatically.";

/** Which invoice figure each ledger line came from. */
const traceByAccount: Record<string, TraceKey> = {
  [accounts.receivable.code]: "total",
  [accounts.sales.code]: "subtotal",
  [accounts.gstPayable.code]: "gst",
};

const debitCents = invoice.issueLines.reduce((sum, line) => sum + (line.side === "debit" ? line.cents : 0), 0);
const creditCents = invoice.issueLines.reduce((sum, line) => sum + (line.side === "credit" ? line.cents : 0), 0);

const postedTo = {
  total: `${accounts.receivable.code} ${accounts.receivable.name}`,
  subtotal: `${accounts.sales.code} ${accounts.sales.name}`,
  gst: `${accounts.gstPayable.code} ${accounts.gstPayable.name}`,
} as const;

function motionAllowed(): boolean {
  return typeof window.matchMedia === "function" && !window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/** One-column layout below 860px, where the ledger sits under the buttons. */
function oneColumnLayout(): boolean {
  return typeof window.matchMedia === "function" && window.matchMedia("(max-width: 860px)").matches;
}

export function InvoicingFigure({ children }: { children: ReactNode }) {
  const rootRef = useRef<HTMLDivElement>(null);
  const focusAfter = useRef(false);
  const touched = useRef(false);
  const [view, setView] = useState<View>(FINISHED);
  const [lit, setLit] = useState<TraceKey | null>(null);
  const [pressing, setPressing] = useState(false);
  const [shown, setShown] = useState(false);
  const replayRef = useRef<() => void>(() => {});
  const refocusReplay = useRef(false);
  const { step, via } = view;
  const posted = step === "issued" || step === "paid";

  // Below the fold at load: go back to the draft and wait for the press. If the
  // visitor scrolls past without pressing, settle to "issued" once it is out of
  // sight. That is a state change only; nothing plays.
  useEffect(() => {
    const node = rootRef.current;
    if (!node || typeof IntersectionObserver === "undefined" || !motionAllowed()) return;

    // Play once when the figure is seen whole: a visible press, then the confirmation, then the issue.
    // It rests at Issued with Record payment left for the visitor. Any press cancels what is left.
    const timers: number[] = [];
    const later = (ms: number, run: () => void) => {
      timers.push(
        window.setTimeout(() => {
          if (!touched.current) run();
        }, ms),
      );
    };
    const play = () => {
      later(500, () => setPressing(true));
      later(840, () => {
        setPressing(false);
        setView({ step: "confirming", via: "press" });
      });
      later(2140, () => setPressing(true));
      later(2480, () => {
        setPressing(false);
        setShown(true);
        setView({ step: "issued", via: "press" });
      });
    };
    replayRef.current = () => {
      for (const timer of timers) window.clearTimeout(timer);
      timers.length = 0;
      touched.current = false;
      setPressing(false);
      setShown(false);
      setLit(null);
      setView({ step: "draft", via: "rest" });
      play();
    };
    const parts = [node.querySelector(".lp-invoicing-lines tbody"), node.querySelector(".lp-invoicing-actions")].filter(
      (part): part is Element => part !== null,
    );
    const whole = new Set<Element>();
    const watch = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.intersectionRatio >= 0.98) whole.add(entry.target);
          else whole.delete(entry.target);
        }
        if (whole.size === parts.length && !touched.current) {
          watch.disconnect();
          // Only a draft is issued. If the figure settled to Issued while out of sight, leave it as it is.
          if (node.dataset.state === "draft") play();
        }
      },
      { rootMargin: "-64px 0px 0px 0px", threshold: [0, 0.98] },
    );

    let first = true;
    const io = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          const above = entry.boundingClientRect.top < 0;
          if (first) {
            first = false;
            if (entry.isIntersecting || above) {
              io.disconnect();
              return;
            }
            setView({ step: "draft", via: "rest" });
            for (const part of parts) watch.observe(part);
          } else if (!entry.isIntersecting && above) {
            // Settle only if the demonstration never started; once it has, its own timers finish it.
            if (!touched.current && !oneColumnLayout() && timers.length === 0) setView({ step: "issued", via: "rest" });
            io.disconnect();
            return;
          }
        }
      },
      { threshold: 0 },
    );
    // A keyboard visitor who reaches the figure's controls presses for themselves:
    // nothing plays under their focus, and a demonstration already running stops where it is.
    const onFocusIn = () => {
      touched.current = true;
      setPressing(false);
      watch.disconnect();
    };
    node.addEventListener("focusin", onFocusIn);
    io.observe(node);
    return () => {
      node.removeEventListener("focusin", onFocusIn);
      io.disconnect();
      watch.disconnect();
      for (const timer of timers) window.clearTimeout(timer);
    };
  }, []);

  // After a press, focus lands on the control that replaced the pressed one.
  useEffect(() => {
    if (!focusAfter.current) return;
    focusAfter.current = false;
    rootRef.current?.querySelector<HTMLElement>("[data-primary]")?.focus({ preventScroll: true });
    // On a phone the ledger sits under the buttons: if what this press wrote is below the fold, bring it up.
    const wrote = step === "issued" ? ".lp-invoicing-lines" : step === "paid" ? ".lp-invoicing-figures" : null;
    const target = wrote ? rootRef.current?.querySelector<HTMLElement>(wrote) : null;
    if (target && typeof target.scrollIntoView === "function" && target.getBoundingClientRect().bottom > window.innerHeight) {
      target.scrollIntoView({ block: "nearest", behavior: motionAllowed() ? "smooth" : "auto" });
    }
  }, [step]);

  // Replay removes its own button while the demonstration runs: hand focus back when it returns.
  useEffect(() => {
    if (!shown || !refocusReplay.current) return;
    refocusReplay.current = false;
    if (document.activeElement === document.body) {
      rootRef.current?.querySelector<HTMLElement>(".lp-replay")?.focus({ preventScroll: true });
    }
  }, [shown]);

  const go =(next: Step, nextVia: Via = "press") => {
    touched.current = true;
    refocusReplay.current = false;
    setPressing(false);
    if (next === "draft") setShown(false);
    focusAfter.current = true;
    if (next === "draft") setLit(null);
    setView({ step: next, via: nextVia });
  };
  const toggleTrace = (key: TraceKey) => {
    const turningOn = lit !== key;
    setLit(turningOn ? key : null);
    if (turningOn && oneColumnLayout()) {
      const cell = rootRef.current?.querySelector<HTMLElement>(`.lp-invoicing-lines td .lp-trace-target[data-trace="${key}"]`);
      if (cell && typeof cell.scrollIntoView === "function") cell.scrollIntoView({ block: "nearest" });
    }
  };

  const announcement =
    via !== "press" || (step === "issued" && shown)
      ? ""
      : step === "issued"
        ? `Invoice issued. Three lines posted together: ${postedTo.total}, ${postedTo.subtotal} and ${postedTo.gst}.`
        : step === "paid"
          ? `Payment recorded. Received ${aud(invoice.receivedCents)}, outstanding ${aud(invoice.outstandingCents)}. Income is unchanged.`
          : step === "draft"
            ? "Back to the saved draft. Nothing is posted."
            : "";

  /** An invoice figure: plain while it is a draft, a trace once it has posted. */
  const figure = (key: TraceKey, cents: number) =>
    posted ? (
      <button
        type="button"
        className="lp-trace lp-invoicing-trace"
        data-trace={key}
        aria-pressed={lit === key}
        onClick={() => toggleTrace(key)}
      >
        {aud(cents)}
        <span className="lp-visually-hidden"> posted to {postedTo[key]}</span>
      </button>
    ) : (
      <span className="lp-num">{aud(cents)}</span>
    );

  const nil = (
    <>
      <span aria-hidden="true">—</span>
      <span className="lp-visually-hidden">Nothing yet</span>
    </>
  );

  return (
    <div
      className="lp-invoicing-fig"
      ref={rootRef}
      data-state={step}
      data-via={via}
      data-lit={lit ?? undefined}
      data-pressing={pressing ? "" : undefined}
    >
      <div className="lp-invoicing-doc">
        <figure className="lp-sheet lp-invoicing-sheet" aria-label={`Invoice ${invoice.number}`}>
          <div className="lp-invoicing-band">
            <div>
              <p className="lp-invoicing-kind">Tax invoice</p>
              <p className="lp-invoicing-seller">{business.name}</p>
            </div>
            <p className="lp-invoicing-status">
              <span className="lp-visually-hidden">Status: </span>
              {posted ? <Pill tone="ok">Issued</Pill> : <Pill tone="warn">Draft</Pill>}
            </p>
          </div>

          {children}

          <dl className="lp-invoicing-totals">
            <div>
              <dt>Subtotal</dt>
              <dd>{figure("subtotal", invoice.subtotalCents)}</dd>
            </div>
            <div>
              <dt>GST</dt>
              <dd>{figure("gst", invoice.gstCents)}</dd>
            </div>
            <div data-total="">
              <dt>Total</dt>
              <dd>{figure("total", invoice.totalCents)}</dd>
            </div>
          </dl>

          <p className="lp-invoicing-sheet-foot">
            <span>
              {invoice.number} · AUD{posted ? "" : " · DRAFT — not issued"}
            </span>
            <span>Page 1 of 1</span>
          </p>
        </figure>
      </div>

      <div className="lp-invoicing-dock" data-sizer={CONFIRM_COPY}>
        <div className="lp-invoicing-dock-copy">
          {step === "draft" ? (
            <>
              <p className="lp-invoicing-notice" data-tone="plain">
                Saved draft
              </p>
              <p>Nothing is in the ledger yet. Press Issue invoice and three lines post together.</p>
            </>
          ) : null}
          {step === "confirming" ? (
            <p className="lp-invoicing-confirm" id="invoicing-confirm-copy">
              {CONFIRM_COPY}
            </p>
          ) : null}
          {step === "issued" ? (
            <>
              <p className="lp-invoicing-notice">
                <Icon name="check" />
                Invoice issued.
              </p>
              <p>
                {shown
                  ? "You press Issue invoice, then Confirm issue. Three lines post together; nothing is sent automatically."
                  : "Issued invoice · read only. Record actual receipts separately; nothing is sent automatically."}
              </p>
            </>
          ) : null}
          {step === "paid" ? (
            <>
              <p className="lp-invoicing-notice">
                <Icon name="check" />
                Payment recorded.
              </p>
              <p>
                {aud(invoice.payment.cents)} received on {invoice.payment.date}, reference {invoice.payment.reference}.
              </p>
            </>
          ) : null}
        </div>

        <div className="lp-invoicing-actions">
          {step === "draft" ? (
            <button key="issue" type="button" className="lp-btn lp-btn--solid" data-primary="" onClick={() => go("confirming")}>
              Issue invoice
            </button>
          ) : null}
          {step === "confirming" ? (
            <>
              <button
                key="confirm"
                type="button"
                className="lp-btn lp-btn--solid"
                data-primary=""
                aria-describedby="invoicing-confirm-copy"
                onClick={() => go("issued")}
              >
                Confirm issue
              </button>
              <button key="keep" type="button" className="lp-btn lp-btn--outline" onClick={() => go("draft", "rest")}>
                Keep as draft
              </button>
            </>
          ) : null}
          {step === "issued" ? (
            <>
              <button key="pay" type="button" className="lp-btn lp-btn--solid" data-primary="" onClick={() => go("paid")}>
                Record payment
              </button>
              {shown ? (
                <button
                  key="replay"
                  type="button"
                  className="lp-replay"
                  onClick={() => {
                    refocusReplay.current = true;
                    replayRef.current();
                  }}
                >
                  <Icon name="replay" />
                  Replay
                </button>
              ) : null}
            </>
          ) : null}
          {step === "paid" ? (
            <button key="again" type="button" className="lp-btn lp-btn--outline" data-primary="" onClick={() => go("draft")}>
              <Icon name="replay" />
              Start again
            </button>
          ) : null}
        </div>
      </div>

      <figure className="lp-invoicing-books" aria-label="Business ledger">
        <AppWindow
          title={
            <>
              <span className="lp-invoicing-crumb">{business.name} / </span>
              <b>Journals</b>
            </>
          }
        >
          <div className="lp-invoicing-ledger">
            <div className="lp-invoicing-journal">
              <div>
                <p className="lp-app-eyebrow">Accounting · Journal detail</p>
                <p className="lp-app-title lp-invoicing-journal-title" data-empty={posted ? undefined : ""}>
                  {posted ? `Invoice ${invoice.number}` : "Nothing posted yet."}
                </p>
              </div>
              <p className="lp-invoicing-journal-meta">
                {invoice.issueShort}
                <Pill tone="ok">Posted</Pill>
              </p>
            </div>

            <div className="lp-invoicing-entry">
              <table className="lp-table lp-invoicing-lines">
                <caption>
                  <b>Entry lines</b> Read-only · amounts in AUD
                </caption>
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
                <tbody aria-hidden={posted ? undefined : true}>
                  {invoice.issueLines.map((line, index) => {
                    const key = traceByAccount[line.account.code];
                    const amount = (
                      <span className="lp-invoicing-ink lp-trace-target" data-trace={key}>
                        {aud(line.cents)}
                      </span>
                    );
                    const empty = <span className="lp-invoicing-ink lp-invoicing-nil">—</span>;
                    return (
                      <tr key={line.account.code} data-row={key} style={{ "--i": index } as CSSProperties}>
                        <th scope="row">
                          <span className="lp-invoicing-ink lp-trace-target" data-trace={key}>
                            <span className="lp-code">{line.account.code}</span>
                            {line.account.name}
                          </span>
                        </th>
                        <td data-num="">{line.side === "debit" ? amount : empty}</td>
                        <td data-num="">{line.side === "credit" ? amount : empty}</td>
                      </tr>
                    );
                  })}
                </tbody>
                <tfoot aria-hidden={posted ? undefined : true}>
                  <tr>
                    <th scope="row">
                      <span className="lp-invoicing-ink">
                        <Tick />
                        Debits equal credits
                      </span>
                    </th>
                    <td data-num="">
                      <span className="lp-invoicing-ink">{aud(debitCents)}</span>
                    </td>
                    <td data-num="">
                      <span className="lp-invoicing-ink">{aud(creditCents)}</span>
                    </td>
                  </tr>
                </tfoot>
              </table>
            </div>

            <dl className="lp-invoicing-figures">
              <div>
                <dt>
                  Invoice total <span>(AUD)</span>
                </dt>
                <dd>{posted ? aud(invoice.totalCents) : nil}</dd>
              </div>
              <div data-moves="">
                <dt>Received</dt>
                <dd>{posted ? aud(step === "paid" ? invoice.receivedCents : 0) : nil}</dd>
              </div>
              <div data-moves="" data-owing="">
                <dt>Outstanding</dt>
                <dd>{posted ? aud(step === "paid" ? invoice.outstandingCents : invoice.totalCents) : nil}</dd>
              </div>
            </dl>

            <div className="lp-invoicing-foot">
              <p className="lp-invoicing-basis">{business.basis} · GST recorded on issue</p>
              <p className="lp-invoicing-income">
                Business income (ex GST) is unchanged at{" "}
                <button
                  type="button"
                  className="lp-trace lp-invoicing-trace"
                  data-trace="subtotal"
                  aria-pressed={lit === "subtotal"}
                  onClick={() => toggleTrace("subtotal")}
                >
                  {aud(overview.businessIncomeCents)}
                  <span className="lp-visually-hidden"> posted to {postedTo.subtotal}</span>
                </button>
                . A receipt updates the bank and the balance owing; it does not add income again.
              </p>
            </div>
          </div>
        </AppWindow>
      </figure>

      <output className="lp-visually-hidden" aria-live="polite">
        {announcement}
      </output>
    </div>
  );
}
