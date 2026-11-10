/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
/* oxlint-disable next/no-img-element -- Local SVGs and responsive artwork use native images; Vinext does not require Next image optimization. */
import type { Metadata } from "next";
import { publicContent } from "../content/public-content.generated";
import "./landing.css";
import { SITE_ORIGIN } from "../lib/site-config";
import { SiteBrand, TammyMark } from "../components/site-brand";
import { AccountingStatus } from "../components/accounting-status";
import { LinkArrow } from "../components/link-arrow";

const { identity, marketingVersion } = publicContent;
const minimumMacOSLabel = identity.minimumMacOSVersion.replace(/\.0$/, "");
const platformLabel = "macOS " + minimumMacOSLabel + " or later, Apple silicon";

export const metadata: Metadata = {
  title: "Local-first accounting for Australian businesses",
  description:
    "Meet Tammy, a private macOS accounting workspace for Australian businesses. Review source documents, reconcile imported bank statements, post journals, issue invoices and prepare local GST/BAS drafts.",
  alternates: { canonical: "/" },
  keywords: [
    "local-first accounting Australia",
    "Mac accounting software",
    "Australian small business bookkeeping",
    "private accounting for macOS",
    "GST workpaper software",
    "BAS draft preparation",
  ],
};

const steps = [
  {
    title: "Bring in the source",
    body: "Import documents and bank-statement rows, or enter transactions yourself.",
  },
  {
    title: "Check the detail",
    body: "Review retained evidence and reconcile imported bank transactions.",
  },
  {
    title: "Keep the ledger",
    body: "Post balanced manual journals and inspect a trial balance.",
  },
  {
    title: "Prepare the draft",
    body: "Build a local GST/BAS workpaper for review, without submitting it.",
  },
] as const;

const capabilities = [
  {
    example: "journals",
    title: "Journals and trial balance",
    body: "Record balanced entries and inspect the ledger and trial balance.",
    detail: "Manual posting with visible accounting records",
  },
  {
    example: "document",
    title: "Source-document review",
    body: "Keep invoices, receipts and other imported evidence close to the decisions they inform.",
    detail: "Encrypted local evidence and review states",
  },
  {
    example: "reconciliation",
    title: "Bank-statement reconciliation",
    body: "Import statement rows and review matches against the records you keep.",
    detail: "File import, not automatic bank feeds",
  },
  {
    example: "invoice",
    title: "Customer invoicing",
    body: "Prepare, review and issue invoices, with customer PDFs you can export.",
    detail: "Issue to the local ledger; export a customer PDF",
  },
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
      "Tammy currently imports bank-statement rows from files for manual matching and reconciliation. Automatic bank feeds and bank-account syncing are not part of this release.",
  },
  {
    question: "Can Tammy lodge my BAS with the ATO?",
    answer:
      "No. Tammy can prepare local GST/BAS workpaper drafts. Its SBR readiness flow is a synthetic, network-disabled simulator. A prepared or frozen draft is not a lodged BAS.",
  },
  {
    question: "Can Tammy create and send invoices?",
    answer:
      "Tammy supports encrypted drafts and customer PDF exports. The explicit issue action posts receivables, income and GST according to the workspace's accounting basis. It does not email the invoice or collect payment. Receipts are recorded separately.",
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
      "A session-scoped local API and MCP adapter can be enabled from the native Integrations menu. Connected local clients can read permitted data and propose an editable invoice for review; they cannot save, issue or post transactions through that connection.",
  },
] as const;

const structuredData = {
  "@context": "https://schema.org",
  "@type": "SoftwareApplication",
  name: identity.appStoreName,
  applicationCategory: "FinanceApplication",
  operatingSystem: "macOS " + minimumMacOSLabel + "+ (Apple silicon)",
  softwareVersion: marketingVersion,
  description:
    "Local-first Australian accounting software for journals, source-document review, imported bank-statement reconciliation, invoicing and GST/BAS workpaper drafts.",
  url: SITE_ORIGIN + "/",
  publisher: { "@type": "Organization", name: identity.publisher },
};

function Mark() {
  return <TammyMark className="landing-mark" />;
}

function Wordmark() {
  return <SiteBrand className="landing-wordmark" />;
}

function Glyph({ name }: { readonly name: string }) {
  return <img className="landing-glyph" src={"/assets/" + name + ".svg"} alt="" aria-hidden="true" />;
}

function WorkspacePreview() {
  return (
    <figure className="landing-preview" aria-label="Illustrative Tammy workspace">
      <div className="landing-preview-workspace">
        <div className="landing-preview-rail">
          <div className="landing-preview-identity">
            <Mark />
            Tammy
          </div>
          <span className="landing-preview-nav-item">Overview</span>
          <span className="landing-preview-nav-item landing-preview-nav-active">Journals</span>
          <span className="landing-preview-nav-item">Documents</span>
          <span className="landing-preview-nav-item">Banking</span>
          <span className="landing-preview-nav-item">GST &amp; BAS</span>
        </div>
        <div className="landing-preview-main">
          <div className="landing-preview-topline">
            <span>Local accounting workspace</span>
            <span className="landing-preview-local">On this Mac</span>
          </div>
          <h3>Journals</h3>
          <p>Review the records behind every balance.</p>
          <div className="landing-preview-columns" aria-hidden="true">
            <span>Record</span>
            <span>State</span>
          </div>
          <div className="landing-preview-row">
            <span>Office equipment</span>
            <AccountingStatus status="posted" />
          </div>
          <div className="landing-preview-row landing-preview-row-selected">
            <span>Monthly insurance</span>
            <AccountingStatus status="reviewed" />
          </div>
          <div className="landing-preview-row">
            <span>Manual adjustment</span>
            <AccountingStatus status="draft" />
          </div>
          <div className="landing-preview-summary">
            <span className="landing-preview-summary-icon"><Glyph name="source-document" /></span>
            <span><strong>Source evidence</strong><small>Retained in the encrypted workspace</small></span>
            <span className="landing-preview-check" aria-hidden="true">✓</span>
          </div>
        </div>
      </div>
      <figcaption>Illustrative interface. Synthetic examples, not a live account or actual application capture.</figcaption>
    </figure>
  );
}

function FeatureExample({ kind }: { readonly kind: (typeof capabilities)[number]["example"] }) {
  if (kind === "journals") {
    return (
      <figure className="landing-feature-example">
        <div className="landing-feature-example-head"><span>Office supplies</span><AccountingStatus status="draft" /></div>
        <table className="landing-feature-table">
          <caption>Journal EXAMPLE-J001 · AUD</caption>
          <thead><tr><th scope="col">Account</th><th scope="col">Dr</th><th scope="col">Cr</th></tr></thead>
          <tbody>
            <tr><th scope="row">Stationery expense</th><td>100.00</td><td>—</td></tr>
            <tr><th scope="row">GST paid</th><td>10.00</td><td>—</td></tr>
            <tr><th scope="row">Bank</th><td>—</td><td>110.00</td></tr>
          </tbody>
          <tfoot><tr><th scope="row">Total</th><td>110.00</td><td>110.00</td></tr></tfoot>
        </table>
        <p className="landing-feature-note">Debits and credits balance. Posting remains a separate action.</p>
        <figcaption>Synthetic journal example, not an app screenshot.</figcaption>
      </figure>
    );
  }

  if (kind === "document") {
    return (
      <figure className="landing-feature-example">
        <div className="landing-feature-example-head"><span>Retained invoice</span><AccountingStatus status="reviewed" /></div>
        <dl className="landing-feature-record">
          <div><dt>Reference</dt><dd>EXAMPLE-018</dd></div>
          <div><dt>Supplier</dt><dd>Example Supplies</dd></div>
          <div><dt>Total</dt><dd>AUD 110.00</dd></div>
          <div><dt>Ledger status</dt><dd>Not posted</dd></div>
        </dl>
        <p className="landing-feature-note">Reviewed means the document details were checked. It does not mean a journal was posted.</p>
        <figcaption>Synthetic source-review example, not an app screenshot.</figcaption>
      </figure>
    );
  }

  if (kind === "reconciliation") {
    return (
      <figure className="landing-feature-example">
        <div className="landing-feature-example-head"><span>Statement comparison</span><AccountingStatus status="unmatched" /></div>
        <table className="landing-feature-table">
          <caption>File import · 7 November 2026 · AUD</caption>
          <thead><tr><th scope="col">Record</th><th scope="col">Money out</th></tr></thead>
          <tbody>
            <tr><th scope="row">Imported statement row<small>Example Supplies</small></th><td>110.00</td></tr>
            <tr><th scope="row">Ledger payment<small>Example Supplies</small></th><td>110.00</td></tr>
          </tbody>
        </table>
        <p className="landing-feature-note">Compare the imported row with your records and choose a match manually. Equal amounts alone do not establish a match.</p>
        <figcaption>Synthetic reconciliation example, not an app screenshot.</figcaption>
      </figure>
    );
  }

  return (
    <figure className="landing-feature-example">
      <div className="landing-feature-example-head"><span>Invoice EXAMPLE-004</span><AccountingStatus status="issued" /></div>
      <div className="landing-feature-invoice">
        <p><strong>Example Customer</strong><span>Design services · 2 × AUD 100.00</span></p>
        <dl className="landing-feature-record">
          <div><dt>Subtotal</dt><dd>AUD 200.00</dd></div>
          <div><dt>GST</dt><dd>AUD 20.00</dd></div>
          <div className="landing-feature-invoice-total"><dt>Total</dt><dd>AUD 220.00</dd></div>
        </dl>
      </div>
      <p className="landing-feature-note">An issued invoice can be exported as a customer PDF. Issuing does not email it or record a payment.</p>
      <figcaption>Synthetic PDF specimen, not an app screenshot or a real invoice.</figcaption>
    </figure>
  );
}

export default function Home() {
  return (
    <div className="landing">
      <a className="skip-link" href="#main-content">Skip to content</a>
      <div className="landing-frame">
        <header className="landing-header">
          <Wordmark />
          <nav className="landing-header-nav" aria-label="Primary navigation">
            <a href="#features">Features</a>
            <a href="/tools">Free tools</a>
            <a href="/blog">Journal</a>
            <a href="#questions">Questions</a>
          </nav>
          <a className="landing-header-action" href="#availability">Development access <LinkArrow /></a>
        </header>

        <main id="main-content" tabIndex={-1}>
          <section className="landing-hero" aria-labelledby="landing-heading">
            <div className="landing-hero-copy">
              <h1 id="landing-heading">Your books.{" "}<br /><span>On your Mac.</span></h1>
              <p className="landing-hero-description">
                Accounting for Australian businesses, kept local. Review documents,
                reconcile statements, post journals and prepare GST/BAS drafts in
                one encrypted macOS workspace.
              </p>
              <div className="landing-hero-actions">
                <a className="landing-button landing-button-primary" href="#workflow">See how it works <LinkArrow /></a>
                <a className="landing-button landing-button-outline" href="#free-tools">Try free tools</a>
              </div>
              <p className="landing-platform"><Glyph name="desktop" /> {platformLabel}</p>
              <p className="landing-availability-note">In development. Public Mac download not yet available. <a href="#availability">Release status</a></p>
            </div>
            <div className="landing-hero-art">
              <WorkspacePreview />
            </div>
          </section>

          <div className="landing-factline" aria-label="Tammy at a glance">
            <span><Glyph name="desktop" /> Works locally on your Mac</span>
            <span><Glyph name="privacy" /> Encrypted accounting records</span>
            <span><Glyph name="gst-bas" /> BAS drafts, not lodgement</span>
          </div>

          <section className="landing-section landing-workflow" id="workflow" aria-labelledby="workflow-heading">
            <div className="landing-section-heading">
              <h2 id="workflow-heading">From source records to a balanced ledger.</h2>
            </div>
            <div className="landing-workflow-content">
              <ol className="landing-step-list">
                {steps.map((step, index) => (
                  <li key={step.title}>
                    <div className="landing-step-top"><span>{String(index + 1).padStart(2, "0")}</span></div>
                    <h3>{step.title}</h3>
                    <p>{step.body}</p>
                  </li>
                ))}
              </ol>
              <p className="landing-flow-note">These are distinct, human-controlled workflows, not a promise of automatic document-to-ledger posting.</p>
            </div>
            <figure className="landing-product-visual">
              <img
                src="/assets/source-ledger-1200.webp"
                srcSet="/assets/source-ledger-640.webp 640w, /assets/source-ledger-1200.webp 1200w"
                sizes="(max-width: 600px) calc(100vw - 32px), (max-width: 840px) calc(100vw - 48px), (max-width: 1460px) 50vw, 690px"
                width={1200}
                height={800}
                loading="lazy"
                decoding="async"
                alt="Illustration of a receipt, pencil and ruled accounting ledger"
              />
              <figcaption>Original illustration. Source documents and journal entries remain separate, reviewable records.</figcaption>
            </figure>
          </section>

          <section className="landing-section landing-features" id="features" aria-labelledby="features-heading">
            <div className="landing-section-heading">
              <h2 id="features-heading">Accounting tools</h2>
            </div>
            <div className="landing-feature-grid">
              {capabilities.map((capability) => (
                <article className="landing-feature" key={capability.title}>
                  <h3>{capability.title}</h3>
                  <p>{capability.body}</p>
                  <FeatureExample kind={capability.example} />
                  <span className="landing-feature-detail">{capability.detail}</span>
                  {capability.example === "invoice" && (
                    <>
                      <a className="landing-feature-tool-link" href="/tools/invoice-generator">Browser invoice generator <LinkArrow /></a>
                      <p className="landing-browser-tool-note">Creates a PDF independently of your Mac workspace.</p>
                    </>
                  )}
                </article>
              ))}
            </div>
          </section>

          <section className="landing-section landing-bas" id="bas" aria-labelledby="bas-heading">
            <div className="landing-bas-copy">
              <h2 id="bas-heading">Australian accounting, with GST in view.</h2>
              <p>AUD amounts, GST workflows, Australian chart-of-accounts conventions and local reporting drafts are part of Tammy’s current accounting foundation.</p>
            </div>
            <div className="landing-bas-boundary">
              <h3>Prepared is not lodged.</h3>
              <p>Tammy helps you prepare local GST/BAS workpapers with reviewable calculations and retained sources. The current release does not submit tax reports to the ATO.</p>
              <a className="landing-text-link" href="/privacy">Understand the reporting boundary <LinkArrow /></a>
            </div>
          </section>

          <section className="landing-section landing-privacy" id="privacy-first" aria-labelledby="privacy-heading">
            <div className="landing-privacy-copy">
              <h2 id="privacy-heading">An encrypted workspace on your Mac.</h2>
              <p>Tammy stores accounting records in an encrypted workspace on your Mac. Some credentials live in macOS Keychain. There is no hosted Tammy business account or cloud sync in this release.</p>
              <a className="landing-text-link landing-text-link-light" href="/privacy">Read our privacy policy <LinkArrow /></a>
            </div>
            <ul className="landing-privacy-facts">
              <li>SQLCipher-encrypted local workspace</li>
              <li>Explicit local document imports and exports</li>
              <li>Optional, session-scoped local API and MCP access</li>
            </ul>
          </section>

          <section className="landing-section landing-tools-section" id="free-tools" aria-labelledby="landing-tools-heading">
            <div className="landing-tools-intro">
              <h2 id="landing-tools-heading">Free business tools</h2>
              <p>Create invoices, calculate GST, check an ABN, or understand pricing margins. Free utilities that keep your information in this browser tab.</p>
              <a className="landing-text-link" href="/tools">Explore all free tools <LinkArrow /></a>
              <a className="landing-text-link landing-guides-link" href="/guides">Read the Australian accounting guides <LinkArrow /></a>
            </div>
            <div className="landing-tools-groups">
              <section className="landing-tools-group" aria-labelledby="landing-tools-invoices-records">
                <h3 id="landing-tools-invoices-records">Invoices &amp; records</h3>
                <div className="landing-tools-list">
                  <a href="/tools/invoice-generator"><strong>Australian invoice generator</strong><span>Make and download an invoice PDF</span><LinkArrow /></a>
                  <a href="/tools/quote-generator"><strong>Quote and estimate generator</strong><span>Build and download a quote PDF</span><LinkArrow /></a>
                  <a href="/tools/expense-organiser"><strong>Receipt and expense organiser</strong><span>Keep a private source record worksheet</span><LinkArrow /></a>
                  <a href="/tools/invoice-due-date-calculator"><strong>Invoice due-date calculator</strong><span>Work out payment terms</span><LinkArrow /></a>
                </div>
              </section>
              <section className="landing-tools-group" aria-labelledby="landing-tools-tax-gst">
                <h3 id="landing-tools-tax-gst">Tax &amp; GST</h3>
                <div className="landing-tools-list">
                  <a href="/tools/bas-workpaper"><strong>Simpler BAS workpaper</strong><span>Review GST figures before lodging</span><LinkArrow /></a>
                  <a href="/tools/sole-trader-tax-checklist"><strong>Sole trader tax checklist</strong><span>Prepare your myTax records</span><LinkArrow /></a>
                  <a href="/tools/gst-calculator"><strong>GST calculator</strong><span>Add or remove 10% GST</span><LinkArrow /></a>
                  <a href="/tools/abn-checker"><strong>ABN format checker</strong><span>Check the number before the register</span><LinkArrow /></a>
                </div>
              </section>
              <section className="landing-tools-group" aria-labelledby="landing-tools-planning-pricing">
                <h3 id="landing-tools-planning-pricing">Planning &amp; pricing</h3>
                <div className="landing-tools-list">
                  <a href="/tools/profit-margin-calculator"><strong>Profit margin calculator</strong><span>Understand markup and gross margin</span><LinkArrow /></a>
                  <a href="/tools/break-even-calculator"><strong>Break-even calculator</strong><span>Know the unit sales target</span><LinkArrow /></a>
                  <a href="/tools/cash-runway-calculator"><strong>Cash runway calculator</strong><span>Estimate monthly cash coverage</span><LinkArrow /></a>
                  <a href="/tools/cash-flow-forecast"><strong>12-week cash flow forecast</strong><span>Plan dated receipts and payments</span><LinkArrow /></a>
                </div>
              </section>
            </div>
          </section>

          <section className="landing-section landing-journal" aria-labelledby="landing-journal-heading">
            <div className="landing-journal-intro">
              <h2 id="landing-journal-heading">The Tammy Journal</h2>
              <p>Carefully worked notes on GST timing, bank receipts, month-end reviews and business decisions.</p>
              <a className="landing-text-link" href="/blog">Explore the journal <LinkArrow /></a>
            </div>
            <div className="landing-journal-stories">
              <a href="/blog/which-quarter-gets-the-gst"><span>GST</span><strong>The BAS quarter has two clocks.</strong><LinkArrow /></a>
              <a href="/blog/not-every-bank-deposit-is-sales"><span>Bookkeeping</span><strong>Not every bank deposit is sales.</strong><LinkArrow /></a>
              <a href="/blog/the-month-end-review-that-found-220"><span>Bookkeeping</span><strong>The month-end close that found $220.</strong><LinkArrow /></a>
              <a href="/blog/the-price-rise-that-didnt-fix-margin"><span>Pricing</span><strong>The price rise that didn&apos;t fix the margin.</strong><LinkArrow /></a>
            </div>
          </section>

          <section className="landing-section landing-faq" id="questions" aria-labelledby="questions-heading">
            <div className="landing-faq-intro">
              <h2 id="questions-heading">Questions about Tammy</h2>
              <p>What Tammy does today, and what it does not.</p>
              <a className="landing-text-link" href="/support">See support and technical details <LinkArrow /></a>
            </div>
            <div className="landing-faq-items">
              {questions.map(({ question, answer }) => (
                <details key={question}>
                  <summary>{question}<span aria-hidden="true">+</span></summary>
                  <p>{answer}</p>
                </details>
              ))}
            </div>
          </section>

          <section className="landing-closing" id="availability" aria-labelledby="closing-heading">
            <div>
              <h2 id="closing-heading">Tammy is in development</h2>
              <p>Explore the local-first approach and contact the publisher about development access. A public Mac download is not available from this site yet.</p>
              <div className="landing-closing-actions">
                <a className="landing-button landing-button-light" href={"mailto:" + identity.supportEmail + "?subject=Tammy%20development%20access"}>Enquire about Tammy <LinkArrow /></a>
                <a className="landing-button landing-button-ghost" href="/support">Get support</a>
              </div>
              <span className="landing-closing-version">Version {marketingVersion} · Development software · {platformLabel}</span>
            </div>
          </section>
        </main>

        <footer className="landing-footer">
          <div><Wordmark /><p>Private accounting for Australian businesses.</p></div>
          <nav aria-label="Footer navigation">
            <a href="#workflow">How it works</a>
            <a href="#features">Features</a>
            <a href="/tools">Free tools</a>
            <a href="/guides">Guides</a>
            <a href="/blog">Journal</a>
            <a href="/privacy">Privacy policy</a>
            <a href="/support">Support</a>
          </nav>
          <p>{identity.publisher}{" "}<br />{identity.copyright}</p>
        </footer>
      </div>
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(structuredData) }} />
    </div>
  );
}
