/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import type { CSSProperties } from "react";
import { GstStripInstrument } from "./gst-strip-instrument";
import { Arrow, Note } from "./kit";

/* The tear between the Mac workspace above and a browser tool here. */
const tornFromCanvas = { "--l-perf": "var(--l-canvas)" } as CSSProperties;

/*
 * The GST instrument: one slim band under the hero where a visitor types a
 * price and sees how much of it is GST, with the working shown. Reading order
 * is question, switch, amount, measure, answers, working, the line, the link.
 */
export function GstStrip() {
  return (
    <section
      className="lp-section lp-gst lp-perf"
      id="try-gst"
      data-tone="oat"
      data-room="browser"
      aria-labelledby="gst-strip-heading"
      style={tornFromCanvas}
    >
      <div className="lp-frame lp-gst-grid">
        <div className="lp-gst-ask">
          <p className="lp-gst-lead">
            A free tool in your browser. <strong>This works now.</strong>
          </p>
          <h2 className="lp-h3" id="gst-strip-heading">How much of the price is GST?</h2>
        </div>

        <GstStripInstrument />

        <p className="lp-gst-line"><span className="lp-gst-line-extract">One-eleventh, not ten per cent.</span><span className="lp-gst-line-add">Ten per cent on top is <span className="lp-gst-line-keep">one-eleventh</span> of the total.</span></p>

        <a className="lp-link lp-gst-more" href="/tools/gst-calculator">
          Open the full GST calculator
          <Arrow />
        </a>

        <Note className="lp-gst-note">
          Standard 10% rate on a fully taxable price. It does not decide whether GST applies to a sale.
        </Note>
      </div>
    </section>
  );
}
