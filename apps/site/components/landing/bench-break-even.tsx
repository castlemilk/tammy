"use client";

/*
 * Bench instrument 3: a $10 rise in cost can move break-even by a third.
 *
 * Sales and total cost drawn against units sold. Where they cross is
 * break-even. Raise the variable cost and the cost line tilts up, so the
 * crossing slides along the sales line. While only the variable cost has
 * moved, the starting line and its crossing stay on the chart for comparison.
 * The answers come from calculateBreakEven, the code behind the full
 * calculator; units and sales are bigint and are never run through aud().
 */
import { type CSSProperties, useState } from "react";
import { aud } from "../../lib/tools/calculations";
import { type BreakEvenResult, calculateBreakEven, formatAudBigInt } from "../../lib/tools/break-even";
import { formatPercent } from "../../lib/tools/margins";
import { AmountField, BenchTouched, Flash, IN_VIEW, MAX_CENTS, minus, pct, plain, useAmount } from "./bench-parts";
import { Note } from "./kit";
import { useScenePhase, useSettledText } from "./motion";

const START = { fixedCents: 300_000, priceCents: 12_000, variableCents: 8_000 } as const;
const STEP_CENTS = 1_000;
const ERROR_ID = "bench-even-error";

function breakEven(fixedCents: number, priceCents: number, variableCents: number): BreakEvenResult | null {
  try {
    return calculateBreakEven(fixedCents, priceCents, variableCents);
  } catch {
    return null;
  }
}

const startResult = breakEven(START.fixedCents, START.priceCents, START.variableCents);
/** Break-even for the starting example: 75 units. */
const START_UNITS = startResult?.status === "possible" ? startResult.units : null;

/** The next 1, 2 or 5 times a power of ten, so the axis ends on a round figure. */
function roundUp(value: number): number {
  if (!(value > 0)) return 200;
  const power = 10 ** Math.floor(Math.log10(value));
  const lead = value / power;
  const step = lead <= 1 ? 1 : lead <= 2 ? 2 : lead <= 5 ? 5 : 10;
  return Math.max(10, step * power);
}

type Point = { x: number; y: number };

type Chart = {
  axisEnd: number;
  sales: [Point, Point];
  cost: [Point, Point];
  crossing: Point | null;
  ghost: { cost: [Point, Point]; crossing: Point } | null;
};

/** Everything in percentages of the plot box, y measured from the top. */
function chart(fixedCents: number, priceCents: number, variableCents: number, result: BreakEvenResult): Chart {
  const contribution = result.contributionCents;
  const crossUnits = result.status === "possible" && contribution > 0 ? fixedCents / contribution : null;
  const comparing =
    START_UNITS !== null &&
    fixedCents === START.fixedCents &&
    priceCents === START.priceCents &&
    variableCents !== START.variableCents;
  const ghostUnits = comparing ? Number(START_UNITS) : null;

  const reach = Math.max(crossUnits ?? 0, ghostUnits ?? 0);
  const axisEnd = roundUp(reach * 1.5);
  const top = Math.max(priceCents * axisEnd, fixedCents + variableCents * axisEnd, 1) * 1.18;
  const at = (units: number, cents: number): Point => ({ x: (units / axisEnd) * 100, y: 100 - (cents / top) * 100 });

  return {
    axisEnd,
    sales: [at(0, 0), at(axisEnd, priceCents * axisEnd)],
    cost: [at(0, fixedCents), at(axisEnd, fixedCents + variableCents * axisEnd)],
    crossing: crossUnits === null ? null : at(crossUnits, crossUnits * priceCents),
    ghost:
      ghostUnits === null
        ? null
        : {
            cost: [at(0, START.fixedCents), at(axisEnd, START.fixedCents + START.variableCents * axisEnd)],
            crossing: at(ghostUnits, ghostUnits * priceCents),
          },
  };
}

/** The division written out, with the round-up said in words when it happens. */
function workingLine(fixedCents: number, priceCents: number, variableCents: number, result: BreakEvenResult): string {
  if (result.status === "unreachable" || result.contributionCents <= 0) {
    return `${plain(priceCents)} − ${plain(variableCents)} = ${plain(result.contributionCents)} a unit`;
  }
  const units = unitsText(result.units);
  const fixed = BigInt(fixedCents);
  const contribution = BigInt(result.contributionCents);
  if (fixed % contribution === 0n) {
    return `${plain(fixedCents)} ÷ ${plain(result.contributionCents)} = ${units}`;
  }
  const hundredths = (fixed * 100n) / contribution;
  const quotient = `${(hundredths / 100n).toLocaleString("en-AU")}.${(hundredths % 100n).toString().padStart(2, "0")}`;
  return `${plain(fixedCents)} ÷ ${plain(result.contributionCents)} = ${quotient}…, rounded up to ${units}`;
}

function unitsText(units: bigint): string {
  return `${units.toLocaleString("en-AU")} ${units === 1n ? "unit" : "units"}`;
}

const xy = (point: Point): CSSProperties => ({ left: pct(point.x / 100), top: pct(point.y / 100) });

export function BenchBreakEven() {
  const fixed = useAmount(START.fixedCents);
  const price = useAmount(START.priceCents);
  const variable = useAmount(START.variableCents);
  const [touched, setTouched] = useState(false);
  // Watches the axis under the plot, so the lines are drawn once the whole
  // chart is on screen.
  const [ref, phase] = useScenePhase<HTMLDivElement>(IN_VIEW);

  const change = (apply: () => void) => {
    apply();
    setTouched(true);
  };

  const result = breakEven(fixed.cents, price.cents, variable.cents);
  const valid = fixed.valid && price.valid && variable.valid && result !== null;
  const model = result === null ? null : chart(fixed.cents, price.cents, variable.cents, result);
  const possible = result !== null && result.status === "possible";
  const units = possible ? result.units : null;

  // The comparison with where the example started, while only the cost moved.
  const delta = model?.ghost && units !== null && START_UNITS !== null ? units - START_UNITS : null;
  const deltaPercent =
    delta !== null && START_UNITS !== null && START_UNITS > 0n
      ? formatPercent(Math.round((Math.abs(Number(delta)) / Number(START_UNITS)) * 10_000) / 100)
      : null;

  const unitsFigure = !valid ? "–" : units === null ? "No break-even" : units.toLocaleString("en-AU");
  const contributionText = valid ? minus(aud(result.contributionCents)) : "–";
  const salesText = valid && possible ? formatAudBigInt(result.revenueCents) : "–";

  const spoken = useSettledText(
    !valid ? "" : units === null ? "No number of sales covers the fixed costs at these prices." : `Break-even ${unitsText(units)}. Contribution per unit ${contributionText}.`,
    touched,
  );

  const chartLabel = !valid
    ? "The chart is waiting for three valid amounts."
    : units === null
      ? "Sales and total cost against units sold. Total cost stays above sales, so the lines never cross."
      : `Sales and total cost against units sold. The lines cross at ${unitsText(units)}.`;

  // The steppers stay focusable at their limits: aria-disabled, not disabled,
  // so a keyboard user does not lose focus when the limit is reached.
  const canLower = variable.cents >= STEP_CENTS;
  const canRaise = variable.cents + STEP_CENTS <= MAX_CENTS;

  return (
    <article
      className="lp-sheet lp-bench-card lp-bench-even"
      aria-labelledby="bench-even-heading"
      data-play={touched ? "static" : phase}
      data-touched={touched ? "" : undefined}
      data-invalid={valid ? undefined : ""}
    >
      <BenchTouched value={touched}>
        <div className="lp-bench-body">
          <div className="lp-bench-intro">
            <h4 className="lp-bench-title" id="bench-even-heading">
              A $10 rise in cost can move break-even by a third.
            </h4>
            <p className="lp-small">
              Each sale keeps only the gap between its price and its cost. Narrow the gap and the target climbs faster than
              the cost did.
            </p>
          </div>

          <div className="lp-bench-controls lp-bench-even-controls">
            <AmountField
              id="bench-even-fixed"
              label="Fixed costs"
              amount={fixed}
              errorId={ERROR_ID}
              onType={(text) => change(() => fixed.type(text))}
            />
            <AmountField
              id="bench-even-price"
              label="Price per unit"
              amount={price}
              errorId={ERROR_ID}
              onType={(text) => change(() => price.type(text))}
            />
            <div className="lp-bench-even-stepper">
              <AmountField
                id="bench-even-variable"
                label="Variable cost per unit"
                amount={variable}
                errorId={ERROR_ID}
                onType={(text) => change(() => variable.type(text))}
              />
              <button
                type="button"
                className="lp-btn lp-btn--outline lp-bench-step"
                data-step="down"
                aria-label="Lower the variable cost by $10"
                aria-disabled={!canLower}
                onClick={() => {
                  if (canLower) change(() => variable.set(variable.cents - STEP_CENTS));
                }}
              >
                <span aria-hidden="true">−</span> $10
              </button>
              <button
                type="button"
                className="lp-btn lp-btn--solid lp-bench-step"
                data-step="up"
                aria-label="Raise the variable cost by $10"
                aria-disabled={!canRaise}
                onClick={() => {
                  if (canRaise) change(() => variable.set(variable.cents + STEP_CENTS));
                }}
              >
                <span aria-hidden="true">+</span> $10
              </button>
            </div>
            {valid ? (
              <p className="lp-micro lp-bench-even-hint">{units === null ? "At these figures the lines never cross." : "Press + $10 and watch where the lines cross."}</p>
            ) : (
              <p className="lp-bench-error" id={ERROR_ID}>
                Enter dollars and cents, like 80.00.
              </p>
            )}
          </div>

          <figure className="lp-bench-even-figure">
            <figcaption className="lp-visually-hidden">{chartLabel}</figcaption>
            <div className="lp-bench-even-chart" aria-hidden="true">
              <p className="lp-bench-even-legend">
                <span data-line="sales">Sales</span>
                <span data-line="cost">Total cost</span>
                {model?.ghost ? <span data-line="ghost">Total cost at {aud(START.variableCents)}</span> : null}
              </p>
              <div className="lp-bench-even-plot">
                {model ? (
                  <>
                    <svg viewBox="0 0 100 100" preserveAspectRatio="none" focusable="false">
                      {model.ghost ? (
                        <line data-line="ghost" x1={model.ghost.cost[0].x} y1={model.ghost.cost[0].y} x2={model.ghost.cost[1].x} y2={model.ghost.cost[1].y} />
                      ) : null}
                      <line data-line="cost" x1={model.cost[0].x} y1={model.cost[0].y} x2={model.cost[1].x} y2={model.cost[1].y} />
                      <line data-line="sales" x1={model.sales[0].x} y1={model.sales[0].y} x2={model.sales[1].x} y2={model.sales[1].y} />
                    </svg>
                    {model.ghost && START_UNITS !== null ? (
                      <>
                        <i className="lp-bench-even-drop" data-ghost="" style={xy(model.ghost.crossing)} />
                        <i className="lp-bench-even-dot" data-ghost="" style={xy(model.ghost.crossing)} />
                        <span className="lp-bench-even-was" style={{ left: pct(model.ghost.crossing.x / 100) }}>
                          {START_UNITS.toLocaleString("en-AU")}
                        </span>
                      </>
                    ) : null}
                    {model.crossing ? (
                      <>
                        <i className="lp-bench-even-drop" style={xy(model.crossing)} />
                        <i className="lp-bench-even-dot" key={unitsFigure} style={xy(model.crossing)} />
                      </>
                    ) : null}
                  </>
                ) : null}
              </div>
              <div ref={ref} className="lp-bench-even-axis" data-stack={units !== null && units >= 500n ? "" : undefined}>
                {model ? (
                  <>
                    {model.crossing && model.crossing.x < 8 ? null : <span data-at="start">0</span>}
                    {model.crossing && units !== null ? (
                      <span data-at="crossing" style={{ left: pct(model.crossing.x / 100) }}>
                        {units.toLocaleString("en-AU")}
                      </span>
                    ) : null}
                    <span data-at="end">{model.axisEnd.toLocaleString("en-AU")} units</span>
                  </>
                ) : null}
              </div>
            </div>
          </figure>

          <div className="lp-bench-even-result">
            <p className="lp-bench-even-units">
              <span className="lp-bench-label">Break-even</span>
              <span className="lp-bench-big" data-words={valid && units === null ? "" : undefined} data-long={units !== null && unitsFigure.length > 9 ? "" : undefined}>
                <Flash value={unitsFigure} />
                {valid && units !== null ? <span className="lp-bench-even-unit">{units === 1n ? " unit" : " units"}</span> : null}
              </span>
              <span className="lp-bench-even-delta">
                {!valid ? (
                  "Three amounts are needed."
                ) : units === null ? (
                  "No number of sales covers the fixed costs at these prices."
                ) : delta !== null && delta !== 0n && deltaPercent !== null ? (
                  <Flash value={`${delta}`}>
                    <strong>
                      {(delta < 0n ? -delta : delta).toLocaleString("en-AU")} {delta > 0n ? "more" : "fewer"}
                    </strong>{" "}
                    than at {aud(START.variableCents)}, {delta > 0n ? "up" : "down"} {deltaPercent}
                  </Flash>
                ) : (
                  "sales to cover the fixed costs"
                )}
              </span>
            </p>
            <dl className="lp-bench-even-facts">
              <div>
                <dt>Contribution per unit</dt>
                <dd>
                  <Flash value={contributionText} />
                </dd>
              </div>
              <div>
                <dt>Sales at break-even</dt>
                <dd>
                  <Flash value={salesText} />
                </dd>
              </div>
            </dl>
            <p className="lp-bench-working">{valid ? workingLine(fixed.cents, price.cents, variable.cents, result) : "–"}</p>
          </div>
          <p className="lp-visually-hidden" aria-live="polite">{spoken}</p>

          <Note className="lp-bench-note">
            One product at one price. No tax, finance or demand limits.{" "}
            <span className="lp-bench-even-story">
              The Journal tells this story in <cite>The price rise that didn&apos;t fix the margin</cite>.
            </span>
          </Note>
        </div>
      </BenchTouched>
    </article>
  );
}
