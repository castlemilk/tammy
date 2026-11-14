/*
 * The bench: three small working instruments inside the Free tools band. Each
 * teaches one sum that catches people out, and each answer comes from the
 * same pure function as the full tool in lib/tools. Every instrument is
 * server-rendered in its default, answered state; the islands only take over
 * when a visitor changes a number.
 *
 * No links to /tools/ or /blog/ here: the tray below and the Journal band own
 * those, and the test suite counts them.
 */
import { BenchAbn } from "./bench-abn";
import { BenchBreakEven } from "./bench-break-even";
import { BenchMargin } from "./bench-margin";

export function Bench() {
  return (
    <div className="lp-bench">
      <div className="lp-bench-head">
        <h3 className="lp-h3">Three sums that catch people out</h3>
        <p className="lp-body">Each one is worked out in this tab by the same code as the full tool.</p>
      </div>
      <fieldset className="lp-bench-pick">
        <legend className="lp-visually-hidden">Choose a sum</legend>
        <label><input type="radio" name="lp-bench-pick" value="margin" defaultChecked />Margin</label>
        <label><input type="radio" name="lp-bench-pick" value="abn" />ABN check</label>
        <label><input type="radio" name="lp-bench-pick" value="even" />Break-even</label>
      </fieldset>
      <div className="lp-bench-grid">
        <BenchMargin />
        <BenchAbn />
        <BenchBreakEven />
      </div>
    </div>
  );
}
