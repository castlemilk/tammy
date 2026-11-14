"use client";

/*
 * Bench instrument 1: margin is not markup.
 *
 * One bar the length of the selling price. The profit is the apricot part. A
 * ruler above it is marked off in tenths of the price, a ruler below it in
 * tenths of the cost, so the same dollars read as two different percentages.
 * The answers come from calculateMargin, the code behind the full calculator.
 */
import { type CSSProperties, useState } from "react";
import { aud } from "../../lib/tools/calculations";
import { calculateMargin, formatPercent, type MarginResult } from "../../lib/tools/margins";
import { AmountField, type Amount, BenchTouched, Flash, IN_VIEW, minus, pct, plain, useAmount } from "./bench-parts";
import { Note } from "./kit";
import { useScenePhase, useSettledText } from "./motion";

const SLIDER_MAX_CENTS = 24_000;
const SLIDER_STEP_CENTS = 100;
const ERROR_ID = "bench-margin-error";

function margins(costCents: number, saleCents: number): MarginResult | null {
  try {
    return calculateMargin(costCents, saleCents);
  } catch {
    return null;
  }
}

type Tick = { x: number; rank: 0 | 1 | 2 };

/**
 * Tick marks for a ruler whose 100% falls at `unit` (0 to 1) along the figure.
 * Tenths while they stay readable, then halves, then wholes, then none.
 */
function rulerTicks(unit: number): Tick[] {
  const perWhole = unit >= 1 / 6 ? 10 : unit >= 1 / 30 ? 2 : unit >= 1 / 60 ? 1 : 0;
  if (perWhole === 0) return [];
  const step = unit / perWhole;
  const ticks: Tick[] = [];
  for (let k = 0; k * step <= 1 + 1e-9; k += 1) {
    ticks.push({ x: Math.min(1, k * step), rank: k % perWhole === 0 ? 2 : perWhole === 10 && k % 5 === 0 ? 1 : 0 });
  }
  return ticks;
}

const TICK_LENGTH = [6, 10, 16] as const;

function Ruler({ side, baseCents, fullCents, spanFrom }: { side: "price" | "cost"; baseCents: number; fullCents: number; spanFrom: number }) {
  const unit = baseCents > 0 && fullCents > 0 ? baseCents / fullCents : 0;
  const ticks = unit > 0 ? rulerTicks(unit) : [];
  const endPercent = unit > 0 && 1 - unit > 0.16 ? Math.round(100 / unit) : null;
  const hundredAtEnd = unit > 0.93;

  return (
    <div className="lp-bench-ruler" data-side={side}>
      <div className="lp-bench-ruler-scale">
        {unit > 0.08 ? <span data-at="start">0</span> : null}
        {unit > 0 ? (
          <span data-at={hundredAtEnd ? "end" : "mid"} data-whole="" style={hundredAtEnd ? undefined : { left: pct(unit) }}>
            100%
          </span>
        ) : (
          <span data-at="start">{side === "price" ? "No price to measure against" : "No cost to measure against"}</span>
        )}
        {endPercent !== null ? <span data-at="end">{endPercent.toLocaleString("en-AU")}%</span> : null}
      </div>
      <div className="lp-bench-ruler-band">
        {spanFrom < 1 && fullCents > 0 ? <span className="lp-bench-ruler-span" style={{ left: pct(spanFrom) }} /> : null}
        <svg viewBox="0 0 1000 16" preserveAspectRatio="none" focusable="false">
          {ticks.map((tick, index) => {
            const length = TICK_LENGTH[tick.rank];
            const x = Math.round(tick.x * 10_000) / 10;
            return (
              <line
                // The ticks are a fixed sequence for a given ratio.
                key={index}
                data-rank={tick.rank}
                x1={x}
                x2={x}
                y1={side === "price" ? 16 - length : 0}
                y2={side === "price" ? 16 : length}
              />
            );
          })}
        </svg>
      </div>
    </div>
  );
}

function Slide({
  id,
  label,
  sliderLabel,
  amount,
  onChange,
}: {
  id: string;
  label: string;
  sliderLabel: string;
  amount: Amount;
  onChange: (apply: () => void) => void;
}) {
  const sliderCents = Math.min(amount.cents, SLIDER_MAX_CENTS);
  return (
    <div className="lp-bench-slide">
      <AmountField id={id} label={label} amount={amount} errorId={ERROR_ID} onType={(text) => onChange(() => amount.type(text))} />
      <input
        className="lp-bench-range"
        type="range"
        min={0}
        max={SLIDER_MAX_CENTS}
        step={SLIDER_STEP_CENTS}
        value={sliderCents}
        aria-label={sliderLabel}
        aria-valuetext={aud(amount.cents)}
        style={{ "--p": pct(sliderCents / SLIDER_MAX_CENTS) } as CSSProperties}
        onChange={(event) => onChange(() => amount.set(Number(event.target.value)))}
      />
    </div>
  );
}

export function BenchMargin() {
  const cost = useAmount(8_000);
  const sale = useAmount(12_000);
  const [touched, setTouched] = useState(false);
  // Watches the lower reading, so the rulers are laid down once the whole
  // figure is on screen and not when the top of a tall card first shows.
  const [ref, phase] = useScenePhase<HTMLParagraphElement>(IN_VIEW);

  const change = (apply: () => void) => {
    apply();
    setTouched(true);
  };

  const result = margins(cost.cents, sale.cents);
  const valid = cost.valid && sale.valid && result !== null;
  const profitCents = result?.profitCents ?? 0;
  const loss = profitCents < 0;

  // The figure is as wide as the larger of the two amounts.
  const fullCents = Math.max(cost.cents, sale.cents);
  const keep = fullCents > 0 ? Math.min(cost.cents, sale.cents) / fullCents : 0;
  const profitWord = loss ? "loss" : "profit";

  const marginText = valid ? minus(formatPercent(result.marginPercent)) : "–";
  const markupText = valid ? minus(formatPercent(result.markupPercent)) : "–";
  const profitText = valid ? minus(aud(profitCents)) : "–";
  const spoken = useSettledText(valid ? `Profit ${profitText}, margin ${marginText}, markup ${markupText}.` : "", touched);

  const figureLabel = valid
    ? `A bar for the ${aud(sale.cents)} selling price and the ${aud(cost.cents)} cost, with a ${profitWord} of ${aud(Math.abs(profitCents))}. ` +
      `Measured against the price that is ${marginText}. Measured against the cost it is ${markupText}.`
    : "The bar is waiting for two valid amounts.";

  return (
    <article
      className="lp-sheet lp-bench-card lp-bench-margin"
      aria-labelledby="bench-margin-heading"
      data-play={touched ? "static" : phase}
      data-touched={touched ? "" : undefined}
      data-invalid={valid ? undefined : ""}
    >
      <BenchTouched value={touched}>
        <div className="lp-bench-body">
          <div className="lp-bench-intro">
            <h4 className="lp-bench-title" id="bench-margin-heading">
              Margin is not markup.
            </h4>
            <p className="lp-small">One profit, read against two rulers. The price gives margin; the cost gives markup.</p>
          </div>

          <div className="lp-bench-controls">
            <Slide id="bench-margin-cost" label="Cost, ex GST" sliderLabel="Cost, ex GST, slider" amount={cost} onChange={change} />
            <Slide id="bench-margin-sale" label="Selling price, ex GST" sliderLabel="Selling price, ex GST, slider" amount={sale} onChange={change} />
            {valid ? null : (
              <p className="lp-bench-error" id={ERROR_ID}>
                Enter dollars and cents, like 80.00.
              </p>
            )}
          </div>

          <figure className="lp-bench-margin-figure">
            <figcaption className="lp-visually-hidden">{figureLabel}</figcaption>
            <p className="lp-bench-margin-reading" data-side="price">
              <span className="lp-bench-label">Against the price</span>
              <span className="lp-bench-working">
                {valid ? (
                  <>
                    <span className="lp-bench-mark">{plain(profitCents)}</span> ÷ {plain(sale.cents)}{" "}
                    {result.marginPercent === null ? "is not defined" : <>= {marginText} margin</>}
                  </>
                ) : (
                  "–"
                )}
              </span>
            </p>

            <div className="lp-bench-margin-measure" aria-hidden="true">
              <Ruler side="price" baseCents={sale.cents} fullCents={fullCents} spanFrom={keep} />
              <div className="lp-bench-bar" data-empty={fullCents === 0 ? "" : undefined}>
                {keep > 0 ? (
                  <span className="lp-bench-bar-seg" data-seg="base" style={{ width: pct(keep) }}>
                    <span className="lp-bench-bar-name">{loss ? "Price" : "Cost"}</span>
                    <span className="lp-bench-bar-amount">{aud(loss ? sale.cents : cost.cents)}</span>
                  </span>
                ) : null}
                {profitCents !== 0 ? (
                  <span className="lp-bench-bar-seg" data-seg={profitWord} style={{ width: pct(1 - keep) }}>
                    <span className="lp-bench-bar-name">{loss ? "Loss" : "Profit"}</span>
                    <span className="lp-bench-bar-amount">{aud(Math.abs(profitCents))}</span>
                  </span>
                ) : null}
              </div>
              <Ruler side="cost" baseCents={cost.cents} fullCents={fullCents} spanFrom={keep} />
            </div>

            <p ref={ref} className="lp-bench-margin-reading" data-side="cost">
              <span className="lp-bench-label">Against the cost</span>
              <span className="lp-bench-working">
                {valid ? (
                  <>
                    <span className="lp-bench-mark">{plain(profitCents)}</span> ÷ {plain(cost.cents)}{" "}
                    {result.markupPercent === null ? "is not defined" : <>= {markupText} markup</>}
                  </>
                ) : (
                  "–"
                )}
              </span>
            </p>
          </figure>

          <dl className="lp-bench-answers">
            <div>
              <dt>Profit</dt>
              <dd data-long={profitText.length > 9 ? "" : undefined}>
                <Flash value={profitText} />
              </dd>
            </div>
            <div>
              <dt>Margin</dt>
              <dd data-long={marginText.length > 9 ? "" : undefined}>
                <Flash value={marginText} />
              </dd>
            </div>
            <div>
              <dt>Markup</dt>
              <dd data-long={markupText.length > 9 ? "" : undefined}>
                <Flash value={markupText} />
              </dd>
            </div>
          </dl>
          <p className="lp-visually-hidden" aria-live="polite">{spoken}</p>

          <Note className="lp-bench-note">Per unit, before GST. Overheads and fees are not included.</Note>
        </div>
      </BenchTouched>
    </article>
  );
}
