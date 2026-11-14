/*
 * Band 4: what the workspace does beyond the five steps. The invoicing article
 * is the large thing, the Overview scene is medium, and the closing list is
 * quiet: it names what has no scene of its own, each with its limit folded in.
 */
import { FeaturesOverview } from "./features-overview";
import { Invoicing } from "./invoicing";

const also: readonly { name: string; text: string }[] = [
  {
    name: "Chart of accounts",
    text: "An Australian small-business chart is installed at setup. You add ordinary accounts; protected control and system accounts cannot be posted to by hand.",
  },
  {
    name: "GST basis",
    text: "Cash or non-cash, chosen at setup. GST on sales and purchases follows it.",
  },
  {
    name: "Audit trail",
    text: "One dated list of journals, bank lines, documents and the current GST draft. A convenience view of loaded records, not a verified audit log.",
  },
  {
    name: "Exact money",
    text: "Amounts are held as whole cents, so GST and totals are worked out exactly rather than on rounded decimals.",
  },
];

export function Features() {
  return (
    <section
      className="lp-section lp-features"
      id="features"
      data-tone="paper"
      data-room="mac"
      aria-labelledby="features-heading"
    >
      <div className="lp-frame">
        <header className="lp-section-head">
          <h2 className="lp-h2" id="features-heading">
            What else the workspace does.
          </h2>
          <p className="lp-deck">
            Beyond the five steps: invoices that post when you issue them, income read from the same entries, and the
            chart of accounts and GST basis underneath.
          </p>
        </header>

        <Invoicing />

        <FeaturesOverview />

        <div className="lp-features-also">
          <h3 className="lp-h3">Also in the workspace</h3>
          <dl className="lp-features-also-list">
            {also.map((item) => (
              <div key={item.name} className="lp-features-also-item">
                <dt>{item.name}</dt>
                <dd>{item.text}</dd>
              </div>
            ))}
          </dl>
        </div>
      </div>
    </section>
  );
}
