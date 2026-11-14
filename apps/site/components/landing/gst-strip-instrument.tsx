"use client";

/*
 * The working parts of the GST instrument: the two-way switch, the amount, the
 * eleven-block measure, the three answers and the working line.
 *
 * The server renders the answered state for $110.00, so the strip is complete
 * before hydration and without script. Figures are replaced whole on every
 * keystroke and never tweened; whatever just changed is remounted (keyed on a
 * revision counter) so its butter highlight restarts.
 */
import { type CSSProperties, useReducer, useSyncExternalStore } from "react";
import { aud, gstBreakdown, parseAudCents, type GstMode } from "../../lib/tools/calculations";
import { useSettledText } from "./motion";

/** The same $100.00 + $10.00 sale, seen from either side. */
const START: Record<GstMode, string> = { remove: "110.00", add: "100.00" };
const DASH = "–";
const TEN = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10] as const;

type Reading = {
  valid: boolean;
  ex: string;
  gst: string;
  inc: string;
  /** The arithmetic as typed on a calculator, split at the equals sign. */
  sumLeft: string;
  sumRight: string;
  rounded: boolean;
  /**
   * The second line of working, about the inclusive price `asideOf`.
   * "slip": ten per cent of it, the wrong answer people reach for.
   * "check": one-eleventh of it, which is the GST that was just added.
   */
  aside: "slip" | "check" | null;
  asideOf: string;
  asideIs: string;
  asideRounded: boolean;
  /** How long the longest answer is; the three share one type size. */
  size: "xl" | "l" | "m" | "s" | "xs";
};

const plain = (cents: number) => aud(cents).slice(1);

/**
 * What the visitor means while still typing: "$110" and "110." are 110.
 * Everything else is left for parseAudCents to accept or refuse.
 */
const typed = (amount: string) => amount.trim().replace(/^\$\s*/, "").replace(/\.$/, "");

function read(amount: string, mode: GstMode): Reading {
  const cents = parseAudCents(typed(amount));
  if (cents === null) {
    return { valid: false, ex: DASH, gst: DASH, inc: DASH, sumLeft: DASH, sumRight: "", rounded: false, aside: null, asideOf: "", asideIs: "", asideRounded: false, size: "xl" };
  }
  const { exclusiveCents, gstCents, inclusiveCents } = gstBreakdown(cents, mode);
  const ex = aud(exclusiveCents);
  const gst = aud(gstCents);
  const inc = aud(inclusiveCents);
  const extracting = mode === "remove";
  const tenPerCentCents = gstBreakdown(inclusiveCents, "add").gstCents;
  const oneEleventhCents = gstBreakdown(inclusiveCents, "remove").gstCents;
  const aside = extracting
    ? tenPerCentCents !== gstCents ? "slip" : null
    : oneEleventhCents === gstCents && gstCents > 0 ? "check" : null;
  const longest = Math.max(ex.length, gst.length, inc.length);
  return {
    valid: true,
    ex,
    gst,
    inc,
    sumLeft: extracting ? plain(inclusiveCents) + " ÷ 11" : plain(exclusiveCents) + " × 10%",
    sumRight: "= " + plain(gstCents),
    rounded: extracting ? inclusiveCents % 11 !== 0 : exclusiveCents % 10 !== 0,
    aside,
    asideOf: aside ? inc : "",
    asideIs: aside === "slip" ? aud(tenPerCentCents) : aside === "check" ? gst : "",
    asideRounded: aside === "slip" ? inclusiveCents % 10 !== 0 : aside === "check" ? inclusiveCents % 11 !== 0 : false,
    size: longest <= 7 ? "xl" : longest <= 9 ? "l" : longest <= 11 ? "m" : longest <= 14 ? "s" : "xs",
  };
}

/** One counter per figure; it moves only when that figure's text changes. */
type Revisions = { ex: number; gst: number; inc: number; sum: number; aside: number; field: number };

type State = {
  amount: string;
  mode: GstMode;
  /** The visitor has typed, so a mode change must leave their amount alone. */
  edited: boolean;
  /** The switch has been used: from here on the tear replays on every change. */
  moved: boolean;
  rev: Revisions;
};

type Action = { type: "amount"; value: string } | { type: "mode"; value: GstMode };

const initialState: State = {
  amount: START.remove,
  mode: "remove",
  edited: false,
  moved: false,
  rev: { ex: 0, gst: 0, inc: 0, sum: 0, aside: 0, field: 0 },
};

function reduce(state: State, action: Action): State {
  let { amount, mode, edited, moved } = state;
  if (action.type === "amount") {
    if (action.value === amount) return state;
    amount = action.value;
    edited = true;
  } else {
    if (action.value === mode) return state;
    mode = action.value;
    moved = true;
    if (!edited) amount = START[mode];
  }
  const before = read(state.amount, state.mode);
  const after = read(amount, mode);
  const bump = (count: number, changed: boolean) => (changed ? count + 1 : count);
  return {
    amount,
    mode,
    edited,
    moved,
    rev: {
      ex: bump(state.rev.ex, before.ex !== after.ex),
      gst: bump(state.rev.gst, before.gst !== after.gst),
      inc: bump(state.rev.inc, before.inc !== after.inc),
      sum: bump(state.rev.sum, before.sumLeft !== after.sumLeft || before.sumRight !== after.sumRight),
      aside: bump(state.rev.aside, before.aside !== after.aside || before.asideIs !== after.asideIs),
      // Only when the instrument, not the visitor, rewrote the amount.
      field: bump(state.rev.field, action.type === "mode" && amount !== state.amount),
    },
  };
}

const subscribeToNothing = () => () => {};
const onClient = () => true;
const onServer = () => false;

/** `data-flash` only once a figure has changed, so nothing flashes at load. */
const flash = (revision: number) => (revision > 0 ? "" : undefined);

export function GstStripInstrument() {
  const [state, dispatch] = useReducer(reduce, initialState);
  // False on the server and during hydration: controls stay inert until script
  // can answer them, and stay a static worked example when script never runs.
  const ready = useSyncExternalStore(subscribeToNothing, onClient, onServer);
  const { mode, rev } = state;
  const extracting = mode === "remove";
  const reading = read(state.amount, mode);
  // The polite live region hears the answer once the visitor pauses, not per keystroke.
  const spoken = useSettledText(
    reading.valid ? `Excluding GST ${reading.ex}, GST ${reading.gst}, including GST ${reading.inc}.` : "",
    state.edited || state.moved,
  );

  return (
    <>
      <div className="lp-gst-console">
        <fieldset className="lp-gst-switch" data-mode={mode}>
          <legend className="lp-visually-hidden">GST calculation</legend>
          <label className="lp-gst-opt">
            <input
              type="radio"
              name="gst-strip-mode"
              value="remove"
              checked={extracting}
              disabled={!ready}
              onChange={() => dispatch({ type: "mode", value: "remove" })}
            />
            <span>Extract GST</span>
          </label>
          <label className="lp-gst-opt">
            <input
              type="radio"
              name="gst-strip-mode"
              value="add"
              checked={!extracting}
              disabled={!ready}
              onChange={() => dispatch({ type: "mode", value: "add" })}
            />
            <span>Add GST</span>
          </label>
        </fieldset>
        <noscript>
          <p className="lp-gst-nojs">A worked example. Turn on JavaScript to enter your own price.</p>
        </noscript>

        <div className="lp-gst-field" data-invalid={reading.valid || state.amount.trim() === "" ? undefined : ""}>
          <label className="lp-gst-field-label" htmlFor="gst-strip-amount">
            {extracting ? "Price including GST" : "Price excluding GST"}
          </label>
          <span className="lp-gst-entry">
            <span className="lp-gst-dollar" aria-hidden="true">$</span>
            <input
              className="lp-gst-input"
              id="gst-strip-amount"
              type="text"
              inputMode="decimal"
              autoComplete="off"
              autoCorrect="off"
              spellCheck={false}
              enterKeyHint="done"
              maxLength={16}
              value={state.amount}
              readOnly={!ready}
              aria-invalid={!reading.valid}
              aria-describedby={reading.valid ? undefined : "gst-strip-hint"}
              onChange={(event) => dispatch({ type: "amount", value: event.target.value })}
            />
            <i
              key={rev.field}
              className="lp-gst-entry-mark"
              data-flash={flash(rev.field)}
              style={{ "--lp-gst-chars": state.amount.length } as CSSProperties}
              aria-hidden="true"
            />
          </span>
        </div>
        <p className="lp-gst-hint" id="gst-strip-hint" aria-live="polite">
          {reading.valid ? null : "Enter an amount like 110.00"}
        </p>
      </div>

      <div className="lp-gst-measure" data-scene="" data-mode={mode} data-moved={state.moved ? "" : undefined}>
        {/* The bar is a drawing; the sentence after it says the same thing in words. */}
        <div className="lp-gst-bar" aria-hidden="true">
          {TEN.map((n) => (
            <span key={n} className="lp-gst-block" />
          ))}
          <span className="lp-gst-tear" />
          <span className="lp-gst-block lp-gst-stub" />
        </div>
        <p className="lp-visually-hidden">
          {extracting
            ? "A bar of eleven equal blocks. Ten are the price excluding GST. The eleventh, torn off along a perforation, is the GST."
            : "A bar of ten equal blocks for the price excluding GST, with an eleventh block of GST added on the end."}
        </p>
        <p className="lp-gst-caps">
          <span>Ten-elevenths</span>
          <span className="lp-gst-caps-stub">One-eleventh</span>
        </p>
      </div>

      <div className="lp-gst-readout" data-size={reading.size}>
        <dl className="lp-gst-rows">
          <div className="lp-gst-row" data-part="ex" data-echo={extracting ? undefined : ""}>
            <dt>
              <i className="lp-gst-key" aria-hidden="true" />
              Excluding GST
            </dt>
            <dd>
              <span key={rev.ex} className="lp-gst-fig" data-flash={flash(rev.ex)}>{reading.ex}</span>
            </dd>
          </div>
          <div className="lp-gst-row" data-part="gst">
            <dt>
              <i className="lp-gst-key" aria-hidden="true" />
              GST
            </dt>
            <dd>
              <span key={rev.gst} className="lp-gst-fig" data-flash={flash(rev.gst)}>{reading.gst}</span>
            </dd>
          </div>
          <div className="lp-gst-row" data-part="inc" data-echo={extracting ? "" : undefined}>
            <dt>Including GST</dt>
            <dd>
              <span key={rev.inc} className="lp-gst-fig" data-flash={flash(rev.inc)}>{reading.inc}</span>
            </dd>
          </div>
        </dl>
      </div>

      <p className="lp-visually-hidden" aria-live="polite">{spoken}</p>

      <div className="lp-gst-working">
        <p className="lp-gst-sum">
          <span key={rev.sum} className="lp-gst-sum-line" data-flash={flash(rev.sum)}>
            <span className="lp-gst-sum-part">{reading.sumLeft}</span>
            {reading.sumRight ? (
              <>
                {" "}
                <span className="lp-gst-sum-part">{reading.sumRight}</span>
              </>
            ) : null}
            {reading.rounded ? <span className="lp-gst-sum-round"><span className="lp-gst-sum-comma">, </span>rounded to the cent</span> : null}
          </span>
        </p>
        {reading.aside ? (
          <p className="lp-gst-aside" data-kind={reading.aside}>
            <span className="lp-gst-aside-name">{reading.aside === "slip" ? "Common slip:" : "Check:"}</span>{" "}
            <span className="lp-gst-aside-part">
              {reading.aside === "slip" ? "10% of " : "one-eleventh of "}
              {reading.asideOf}
            </span>{" "}
            <span className="lp-gst-aside-part">
              is{" "}
              <span key={rev.aside} className="lp-gst-aside-is" data-flash={flash(rev.aside)}>{reading.asideIs}</span>
              {reading.asideRounded ? ", to the cent" : null}
            </span>
          </p>
        ) : null}
      </div>
    </>
  );
}
