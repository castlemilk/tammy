"use client";

/*
 * Bench instrument 2: every ABN carries its own check.
 *
 * The modulus-89 check worked long-hand: take 1 from the first digit, multiply
 * each digit by its weight, add the products, divide by 89. The steps are
 * recomputed here for display only; the verdict is checkAbn's, the code behind
 * the full checker, so the working and the verdict cannot disagree.
 *
 * The working plays once when the instrument enters view (CSS choreography
 * keyed off data-play) and offers Replay. Once the visitor edits the number
 * the figures are simply replaced.
 */
import { type CSSProperties, useState } from "react";
import { checkAbn, formattedAbn } from "../../lib/tools/abn";
import { useBrowserReady } from "../use-browser-ready";
import { BenchTouched, Flash, IN_VIEW } from "./bench-parts";
import { Icon, Note, Pill } from "./kit";
import { useScenePhase, useSettledText } from "./motion";

const WEIGHTS = [10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19] as const;
const EXAMPLE_ABN = "51 824 753 556";
const RESULT_ID = "bench-abn-result";

/** A whole number with a true minus sign. */
function signed(value: number): string {
  return value < 0 ? `−${Math.abs(value)}` : String(value);
}

type Working = {
  /** The digits typed so far, at most eleven. */
  digits: string[];
  /** Eleven digits and nothing else. */
  complete: boolean;
  /** Something other than digits and spaces. */
  malformed: boolean;
  /** How many digits were typed, before trimming to eleven. */
  count: number;
  adjusted: (number | null)[];
  products: (number | null)[];
  sum: number;
};

function work(value: string): Working {
  const stripped = value.replace(/\s/g, "");
  const complete = /^\d{11}$/.test(stripped);
  const onlyDigits = /^\d*$/.test(stripped);
  const allDigits = stripped.replace(/\D/g, "");
  const digits = allDigits.slice(0, 11).split("");
  const adjusted = WEIGHTS.map((_, index) => {
    const digit = digits[index];
    return digit === undefined ? null : Number(digit) - (index === 0 ? 1 : 0);
  });
  const products = adjusted.map((digit, index) => (digit === null ? null : digit * (WEIGHTS[index] ?? 0)));
  const sum = products.reduce<number>((total, product) => total + (product ?? 0), 0);
  return { digits, complete, malformed: !onlyDigits, count: allDigits.length, adjusted, products, sum };
}

type Mode = "auto" | "in" | "static";

export function BenchAbn() {
  const [value, setValue] = useState(EXAMPLE_ABN);
  const [mode, setMode] = useState<Mode>("auto");
  const [run, setRun] = useState(0);
  const [touched, setTouched] = useState(false);
  // Watches the result row, so the working plays once the tiles above it and
  // the verdict are all on screen.
  const [ref, phase] = useScenePhase<HTMLDivElement>(IN_VIEW);
  const ready = useBrowserReady();

  const check = checkAbn(value);
  const working = work(value);
  const passes = working.complete && check.status === "valid";
  const fails = working.complete && check.status !== "valid";
  const quotient = Math.trunc(working.sum / 89);
  const remainder = working.sum % 89;
  const message = working.malformed
    ? "An ABN has 11 digits. Use digits and spaces only."
    : working.count > 11
      ? `An ABN has 11 digits. This has ${working.count}.`
      : "An ABN has 11 digits.";
  const spoken = useSettledText(
    working.complete
      ? `${passes ? "Checksum passes" : "Checksum fails"}: ${signed(working.sum)} divided by 89 leaves ${signed(remainder)}.`
      : working.malformed || working.count > 11
        ? message
        : "",
    touched,
  );

  return (
    <article
      className="lp-sheet lp-bench-card lp-bench-abn"
      aria-labelledby="bench-abn-heading"
      data-play={mode === "auto" ? phase : mode}
      data-touched={touched ? "" : undefined}
    >
      <BenchTouched value={touched}>
        <div className="lp-bench-body">
          <div className="lp-bench-intro">
            <h4 className="lp-bench-title" id="bench-abn-heading">
              Every ABN carries its own check.
            </h4>
            <p className="lp-small">
              Take 1 from the first digit, multiply each digit by its weight and add them up. A well-formed ABN divides by 89
              with nothing left over. The number below is the Australian Business Register&apos;s own worked example.
            </p>
          </div>

          <div className="lp-bench-controls lp-bench-abn-controls">
            <div className="lp-bench-field">
              <label className="lp-bench-label" htmlFor="bench-abn-input">
                ABN <span>11 digits, spaces optional</span>
              </label>
              <span className="lp-bench-input" data-state={fails || working.malformed || working.count > 11 ? "bad" : undefined}>
                <input
                  id="bench-abn-input"
                  type="text"
                  inputMode="numeric"
                  autoComplete="off"
                  enterKeyHint="done"
                  maxLength={24}
                  value={value}
                  readOnly={!ready}
                  aria-invalid={check.status === "invalid"}
                  aria-describedby={RESULT_ID}
                  onChange={(event) => {
                    setValue(event.target.value);
                    setMode("static");
                    setTouched(true);
                  }}
                />
              </span>
            </div>
            <button
              type="button"
              className="lp-replay lp-bench-replay"
              onClick={() => {
                setTouched(false);
                setMode("in");
                setRun((count) => count + 1);
              }}
            >
              <Icon name="replay" />
              Replay
            </button>
          </div>

          <div className="lp-bench-abn-figure">
            {/* The keyed parts remount on Replay, so the CSS sequence starts again. */}
            <ol className="lp-bench-abn-cols" key={run} aria-label="The check, digit by digit">
              {WEIGHTS.map((weight, index) => {
                const digit = working.digits[index];
                const adjusted = working.adjusted[index] ?? null;
                const product = working.products[index] ?? null;
                const steppedDown = index === 0 && digit !== undefined;
                return (
                  <li
                    // The eleven positions are fixed.
                    key={index}
                    className="lp-bench-abn-col"
                    data-empty={digit === undefined ? "" : undefined}
                    style={{ "--i": index } as CSSProperties}
                  >
                    <span className="lp-bench-abn-tile" data-was={steppedDown ? digit : undefined}>
                      {steppedDown ? (
                        <>
                          <s className="lp-bench-abn-was">{digit}</s>
                          <span className="lp-visually-hidden"> less 1 is </span>
                        </>
                      ) : null}
                      {adjusted === null ? (
                        <span className="lp-visually-hidden">blank</span>
                      ) : (
                        <Flash className="lp-bench-abn-digit" value={signed(adjusted)} />
                      )}
                    </span>
                    <span className="lp-bench-abn-weight">
                      <span aria-hidden="true">×</span>
                      <span className="lp-visually-hidden"> times </span>
                      {weight}
                    </span>
                    <span className="lp-bench-abn-product">
                      {product === null ? null : (
                        <>
                          <span className="lp-visually-hidden"> is </span>
                          <Flash value={signed(product)} />
                        </>
                      )}
                    </span>
                  </li>
                );
              })}
              <li className="lp-bench-abn-col lp-bench-abn-total">
                <span className="lp-bench-abn-total-label">Sum</span>
                <span className="lp-bench-abn-product">
                  <span className="lp-visually-hidden"> of the products is </span>
                  {working.complete ? <Flash value={signed(working.sum)} /> : "–"}
                </span>
              </li>
            </ol>

            <div ref={ref} className="lp-bench-abn-result" id={RESULT_ID}>
              {working.complete ? (
                <p className="lp-bench-working lp-bench-abn-division" key={run}>
                  {signed(working.sum)} ÷ 89 = {signed(quotient)}{" "}
                  <span className="lp-bench-mark">remainder {signed(remainder)}</span>
                </p>
              ) : (
                <p className="lp-bench-abn-division lp-bench-abn-message">{message}</p>
              )}
              <p className="lp-bench-abn-verdict" key={`verdict-${run}`}>
                {passes ? (
                  <Pill tone="ok" tick>
                    Checksum passes
                  </Pill>
                ) : fails ? (
                  <Pill tone="warn">Checksum fails</Pill>
                ) : (
                  <Pill>{working.malformed || working.count > 11 ? "Not checked" : `${working.count} of 11 digits`}</Pill>
                )}
                {working.complete ? <span className="lp-num lp-bench-abn-number">{formattedAbn(check.digits)}</span> : null}
              </p>
            </div>
            <p className="lp-visually-hidden" aria-live="polite">{spoken}</p>
          </div>

          <Note className="lp-bench-note">
            A format check only. It does not look up the Australian Business Register or prove a business is registered.
          </Note>
        </div>
      </BenchTouched>
    </article>
  );
}
