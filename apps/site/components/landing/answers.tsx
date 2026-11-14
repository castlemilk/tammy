/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
/*
 * Band 10: plain answers. The page's limits are printed once, here, so the
 * bands above can lead with what the product does.
 *
 *   1. One T-account (the only one on the page): what is in the books today on
 *      the left, what is not on the right. Rows face each other where a limit
 *      belongs to a capability (imported rows opposite bank feeds, the GST
 *      workpaper opposite lodgement). Type and two rules; nothing moves.
 *   2. Six questions as native <details>. They open without script; the eased
 *      height and fade are a CSS upgrade (see answers.css).
 *
 * Server component: no client code in this band.
 */
import type { CSSProperties } from "react";
import { publicContent } from "../../content/public-content.generated";

const { identity, marketingVersion } = publicContent;
const minimumMacOSLabel = identity.minimumMacOSVersion.replace(/\.0$/, "");

/*
 * The account. The FAQ below owns several exact boundary sentences, so these
 * entries are phrased differently on purpose. Order matters: entry n on the
 * left faces entry n on the right.
 */
const inTheBooks = [
  "Balanced journals and a trial balance as at any date",
  "Source documents in the encrypted workspace, reviewed by you",
  "Purchases posted from reviewed documents, with dated payments",
  "Invoices posted when issued, payments recorded, a customer PDF",
  "Imported bank-statement rows, matched by hand and reconciled",
  "An income overview with PDF and CSV export",
  "A local GST workpaper with the record behind each figure",
  "An opt-in local API and MCP connection to read and propose",
] as const;

const notInTheBooks = [
  "Profit and loss or balance sheet reports",
  "Reading receipts in the cloud",
  "Payroll, inventory or more than one currency",
  "Emailing invoices, payment collection or reminders",
  "Bank feeds or automatic matching",
  "A cloud workspace or sync between devices",
  "Lodging a BAS or any return with the ATO",
  "A public Mac download, yet",
] as const;

const questions = [
  {
    question: "Does Tammy work without a cloud account?",
    answer:
      "Yes. The current macOS app keeps its accounting workspace in a local encrypted database. There is no Tammy cloud workspace or cross-device sync in this release. The public website itself is hosted online.",
  },
  {
    question: "Can Tammy connect directly to my bank?",
    answer:
      "No. Tammy currently imports bank-statement rows for manual matching and reconciliation. Automatic bank feeds and bank-account syncing are not part of this release.",
  },
  {
    question: "Can Tammy lodge my BAS with the ATO?",
    answer:
      "No. Tammy can prepare local GST/BAS workpaper drafts. Its SBR readiness flow is a synthetic, network-disabled simulator. A prepared or frozen draft is not a lodged BAS.",
  },
  {
    question: "Can Tammy create and send invoices?",
    answer:
      "It creates them; it does not send them. Tammy supports encrypted drafts and customer PDF exports. The explicit issue action posts receivables, income and GST according to the workspace's accounting basis. It does not email the invoice or collect payment. Receipts are recorded separately.",
  },
  {
    question: "What Mac do I need?",
    answer:
      "The current supported target is macOS " +
      minimumMacOSLabel +
      " or later on Apple silicon (arm64). Tammy is development software, not a publicly qualified App Store release.",
  },
  {
    question: "Can local tools integrate with Tammy?",
    answer:
      "Yes, once you enable it. A session-scoped local API and MCP adapter can be switched on from the native Integrations menu. Connected local clients can read permitted data and propose an editable invoice for review; they cannot save, issue or post transactions through that connection.",
  },
] as const;

/* The two sides share one set of ledger rows at desktop widths. */
const accountRows = { "--lp-answers-rows": Math.max(inTheBooks.length, notInTheBooks.length) } as CSSProperties;

/* Two columns of three, so opening an answer moves only its own column. */
const questionColumns = [questions.slice(0, 3), questions.slice(3)] as const;

const faqStructuredData = {
  "@context": "https://schema.org",
  "@type": "FAQPage",
  mainEntity: questions.map(({ question, answer }) => ({
    "@type": "Question",
    name: question,
    acceptedAnswer: { "@type": "Answer", text: answer },
  })),
};

export function Answers() {
  return (
    <section className="lp-section lp-answers" id="questions" data-tone="oat" data-room="mac" aria-labelledby="questions-heading">
      <div className="lp-frame">
        <header className="lp-section-head lp-answers-head">
          <h2 className="lp-h2" id="questions-heading">
            What Tammy does today, and what it does not.
          </h2>
          <p className="lp-deck">Set out the way an accountant would: one account, two sides.</p>
        </header>

        <div className="lp-answers-account">
          <p className="lp-answers-account-name">{"The Mac workspace, version " + marketingVersion}</p>
          <div className="lp-answers-t" style={accountRows}>
            <div className="lp-answers-side">
              <h3 className="lp-h3 lp-answers-side-head">In the books today</h3>
              <ul className="lp-answers-entries">
                {inTheBooks.map((entry) => (
                  <li key={entry}>{entry}</li>
                ))}
              </ul>
            </div>
            <div className="lp-answers-side">
              <h3 className="lp-h3 lp-answers-side-head">Not in the books</h3>
              <ul className="lp-answers-entries">
                {notInTheBooks.map((entry) => (
                  <li key={entry}>{entry}</li>
                ))}
              </ul>
            </div>
          </div>
        </div>

        <div className="lp-answers-faq">
          <div className="lp-answers-faq-head">
            <h3 className="lp-h3">Six questions, answered plainly</h3>
            <a className="lp-link" href="/support">
              See support and technical details
            </a>
          </div>
          <div className="lp-answers-faq-cols">
            {questionColumns.map((column) => (
              <div className="lp-answers-faq-col" key={column[0]?.question}>
                {column.map(({ question, answer }) => (
                  <details className="lp-answers-item" name="lp-answers-faq" key={question}>
                    <summary className="lp-answers-q">
                      {question}
                      <span className="lp-answers-plus" aria-hidden="true" />
                    </summary>
                    <div className="lp-answers-a">
                      <p>{answer}</p>
                    </div>
                  </details>
                ))}
              </div>
            ))}
          </div>
        </div>
      </div>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(faqStructuredData).replace(/</g, "\\u003c") }}
      />
    </section>
  );
}
