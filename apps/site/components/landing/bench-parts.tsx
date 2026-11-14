"use client";

/*
 * Pieces shared by the three bench instruments: a money field, the butter
 * flash on a figure that has just changed, and the amount state that keeps a
 * text input and a slider or stepper in step.
 */
import { createContext, type ReactNode, useContext, useState } from "react";
import { aud, parseAudCents } from "../../lib/tools/calculations";
import { cx } from "./kit";
import { useBrowserReady } from "../use-browser-ready";

/**
 * Observer margin for the instruments' play-once entrances. Each island
 * watches the last line of its figure, so a figure plays when all of it is on
 * screen; the small inset keeps it clear of the very bottom edge.
 */
export const IN_VIEW = "0px 0px -8% 0px";

/** The largest amount parseAudCents accepts: nine whole digits and 99 cents. */
export const MAX_CENTS = 99_999_999_999;

/** A true minus sign for negative figures on display. */
export function minus(text: string): string {
  return text.replace("-", "−");
}

/** Cents as bare figures for a working line: "3,000.00", "−40.00". */
export function plain(cents: number): string {
  return minus(aud(cents).replace("$", ""));
}

/** Cents as input text, ungrouped so it parses straight back: "3000.00". */
export function inputText(cents: number): string {
  return `${Math.floor(cents / 100)}.${String(cents % 100).padStart(2, "0")}`;
}

/** A fraction of the figure's width as a CSS percentage. */
export function pct(fraction: number): string {
  return `${Math.round(Math.min(1, Math.max(0, fraction)) * 100_000) / 1000}%`;
}

export type Amount = {
  /** What is in the text box, valid or not. */
  text: string;
  /** The last amount that parsed, in whole cents. */
  cents: number;
  valid: boolean;
  type: (text: string) => void;
  set: (cents: number) => void;
};

/**
 * One amount with two ways in. Typing keeps the text exactly as typed and only
 * moves the figure when it parses; a slider or stepper sets whole cents and
 * rewrites the text.
 */
export function useAmount(initialCents: number): Amount {
  const [state, setState] = useState({ text: inputText(initialCents), cents: initialCents });
  return {
    text: state.text,
    cents: state.cents,
    valid: parseAudCents(state.text) !== null,
    type: (text) => {
      const parsed = parseAudCents(text);
      setState((previous) => ({ text, cents: parsed ?? previous.cents }));
    },
    set: (cents) => {
      const clamped = Math.min(MAX_CENTS, Math.max(0, Math.round(cents)));
      setState({ text: inputText(clamped), cents: clamped });
    },
  };
}

/** A labelled dollar amount, typed exactly. */
export function AmountField({
  id,
  label,
  amount,
  onType,
  errorId,
  className,
}: {
  id: string;
  label: ReactNode;
  amount: Amount;
  onType: (text: string) => void;
  errorId: string;
  className?: string;
}) {
  // Read-only in the server HTML, so without script the worked example cannot
  // be edited into a figure nothing recalculates.
  const ready = useBrowserReady();
  return (
    <div className={cx("lp-bench-field", className)}>
      <label className="lp-bench-label" htmlFor={id}>
        {label}
      </label>
      <span className="lp-bench-input" data-state={amount.valid ? undefined : "bad"}>
        <span className="lp-bench-input-prefix" aria-hidden="true">
          $
        </span>
        <input
          id={id}
          type="text"
          inputMode="decimal"
          autoComplete="off"
          enterKeyHint="done"
          maxLength={16}
          value={amount.text}
          readOnly={!ready}
          aria-invalid={!amount.valid}
          aria-describedby={amount.valid ? undefined : errorId}
          onChange={(event) => onType(event.target.value)}
        />
      </span>
    </div>
  );
}

/** True once the visitor has changed something in the instrument. */
export const BenchTouched = createContext(false);

/**
 * A figure that is replaced whole. Keyed by its value, so a new value mounts a
 * new element and the butter flash plays once on exactly what changed.
 */
export function Flash({ value, className, children }: { value: string; className?: string; children?: ReactNode }) {
  return (
    <FlashOnce key={value} className={className}>
      {children ?? value}
    </FlashOnce>
  );
}

function FlashOnce({ className, children }: { className?: string; children: ReactNode }) {
  const touched = useContext(BenchTouched);
  // Decided when this value first appears: figures that were already on the
  // page when the visitor made a first change stay still.
  const [fresh] = useState(touched);
  return (
    <span className={cx("lp-bench-flash", className)} data-fresh={fresh ? "" : undefined}>
      {children}
    </span>
  );
}
