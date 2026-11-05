/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
/* oxlint-disable next/no-img-element -- These local SVGs are vector assets, not raster photos; Vinext does not require Next image optimization. */
import type { Metadata } from "next";
import { publicContent } from "../content/public-content.generated";
import "./landing.css";
import { SITE_ORIGIN } from "../lib/site-config";

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
    icon: "source-document",
    title: "Bring in the source",
    body: "Import documents and bank-statement rows, or enter transactions yourself.",
  },
  {
    icon: "review",
    title: "Check the detail",
    body: "Review retained evidence and reconcile imported bank transactions.",
  },
  {
    icon: "journals",
    title: "Keep the ledger",
    body: "Post balanced manual journals and inspect a trial balance.",
  },
  {
    icon: "gst-bas",
    title: "Prepare the draft",
    body: "Build a local GST/BAS workpaper for review, without submitting it.",
  },
] as const;

const capabilities = [
  {
    icon: "journals",
    title: "Journals and trial balance",
    body: "Record balanced entries in a ledger you can inspect, rather than a black box.",
    detail: "Manual posting with visible accounting records",
  },
  {
    icon: "source-document",
    title: "Source-document review",
    body: "Keep invoices, receipts and other imported evidence close to the decisions they inform.",
    detail: "Encrypted local evidence and review states",
  },
  {
    icon: "reconciliation",
    title: "Bank-statement reconciliation",
    body: "Import statement rows and review matches against the records you keep.",
    detail: "File import, not automatic bank feeds",
  },
  {
    icon: "invoices",
    title: "Customer invoicing",
    body: "Prepare, review and issue invoices, with customer PDFs you can export.",
    detail: "No automatic email, payment collection or ledger posting",
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
      "Tammy supports encrypted drafts, issuing an invoice and exporting a customer PDF. Issuing does not send an email, post a receivable or track payments.",
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
  return (
    <span className="landing-mark" aria-hidden="true">
      <span />
      <span />
    </span>
  );
}

function Wordmark() {
  return (
    <a className="landing-wordmark" href="/" aria-label="Tammy home">
      <Mark />
      <span>Tammy</span>
    </a>
  );
}

function Glyph({ name }: { readonly name: string }) {
  return <img className="landing-glyph" src={"/assets/" + name + ".svg"} alt="" aria-hidden="true" />;
}

function WorkspacePreview() {
  return (
    <figure className="landing-preview" aria-label="Illustrative Tammy workspace">
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
          <span className="landing-preview-status">Posted</span>
        </div>
        <div className="landing-preview-row landing-preview-row-selected">
          <span>Monthly insurance</span>
          <span className="landing-preview-status">Reviewed</span>
        </div>
        <div className="landing-preview-row">
          <span>Manual adjustment</span>
          <span className="landing-preview-status">Draft</span>
        </div>
        <div className="landing-preview-summary">
          <span className="landing-preview-summary-icon"><Glyph name="source-document" /></span>
          <span><strong>Source evidence</strong><small>Retained in the encrypted workspace</small></span>
          <span className="landing-preview-check" aria-hidden="true">✓</span>
        </div>
      </div>
      <figcaption>Illustrative interface. Synthetic examples, not a live account or actual application capture.</figcaption>
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
            <a href="#workflow">How it works</a>
            <a href="#features">Features</a>
            <a href="/tools">Free tools</a>
            <a href="/blog">Journal</a>
            <a href="#privacy-first">Local-first</a>
            <a href="#questions">Questions</a>
          </nav>
          <a className="landing-header-action" href="#product">Explore Tammy <span aria-hidden="true">↗</span></a>
        </header>

        <main id="main-content">
          <section className="landing-hero" aria-labelledby="landing-heading">
            <div className="landing-hero-copy">
              <p className="landing-introduction">Private accounting for Australian businesses.</p>
              <h1 id="landing-heading">Your books.{" "}<br /><span>On your Mac.</span></h1>
              <p className="landing-hero-description">
                The records behind your business, kept local. Review documents,
                reconcile statements, post journals and prepare GST/BAS drafts in
                one encrypted macOS workspace.
              </p>
              <div className="landing-hero-actions">
                <a className="landing-button landing-button-primary" href="#workflow">See how it works <span aria-hidden="true">↗</span></a>
                <a className="landing-button landing-button-outline" href="#availability">Release status</a>
              </div>
              <p className="landing-platform"><Glyph name="desktop" /> {platformLabel}</p>
            </div>
            <div className="landing-hero-art">
              <img className="landing-contours landing-contours-hero" src="/assets/contour-lines.svg" alt="" aria-hidden="true" />
              <div className="landing-hero-art-slab" aria-hidden="true" />
              <WorkspacePreview />
              <div className="landing-source-slip" aria-hidden="true">
                <span>Source document</span>
                <span className="landing-slip-lines"><i /><i /><i /></span>
                <span className="landing-slip-check">Reviewed ✓</span>
              </div>
            </div>
          </section>

          <div className="landing-factline" aria-label="Tammy at a glance">
            <span><Glyph name="desktop" /> Works locally on your Mac</span>
            <span><Glyph name="privacy" /> Encrypted accounting records</span>
            <span><Glyph name="gst-bas" /> BAS drafts, not lodgement</span>
          </div>

          <section className="landing-section landing-workflow" id="workflow" aria-labelledby="workflow-heading">
            <div className="landing-section-heading landing-section-heading-split">
              <div>
                <p className="landing-kicker">A clear accounting process</p>
                <h2 id="workflow-heading">From source records{" "}<br />to a balanced ledger.</h2>
              </div>
              <p>Bring evidence into view, make deliberate accounting decisions, and keep the result inspectable.</p>
            </div>
            <ol className="landing-step-list">
              {steps.map((step, index) => (
                <li key={step.title}>
                  <div className="landing-step-top"><span>{String(index + 1).padStart(2, "0")}</span><Glyph name={step.icon} /></div>
                  <h3>{step.title}</h3>
                  <p>{step.body}</p>
                </li>
              ))}
            </ol>
            <p className="landing-flow-note">These are distinct, human-controlled workflows, not a promise of automatic document-to-ledger posting.</p>
          </section>

          <section className="landing-section landing-features" id="features" aria-labelledby="features-heading">
            <div className="landing-section-heading">
              <p className="landing-kicker">The essentials, considered</p>
              <h2 id="features-heading">Accounting without the noise.</h2>
              <p>Familiar tools, exact financial records and a place for the evidence that matters.</p>
            </div>
            <div className="landing-feature-grid">
              {capabilities.map((capability) => (
                <article className="landing-feature" key={capability.title}>
                  <span className="landing-feature-icon"><Glyph name={capability.icon} /></span>
                  <h3>{capability.title}</h3>
                  <p>{capability.body}</p>
                  <span className="landing-feature-detail">{capability.detail}</span>
                </article>
              ))}
            </div>
          </section>

          <section className="landing-section landing-product" id="product" aria-labelledby="product-heading">
            <div className="landing-product-copy">
              <p className="landing-kicker">Designed to be understood</p>
              <h2 id="product-heading">Know what changed.{" "}<br />Know why.</h2>
              <p>Keep a close eye on the books, with source evidence, explicit review states and financial records that remain available to inspect.</p>
              <a className="landing-text-link" href="#workflow">Explore the accounting process <span aria-hidden="true">↗</span></a>
            </div>
            <div className="landing-product-visual">
              <div className="landing-product-paper">
                <span>Retained document</span>
                <Glyph name="source-document" />
                <i /><i /><i />
                <strong>Reviewed</strong>
              </div>
              <div className="landing-product-book">
                <div className="landing-product-book-head"><Glyph name="ledger" /><span>Journal detail</span></div>
                <div className="landing-product-book-line"><span>Account</span><span>Debit</span><span>Credit</span></div>
                <div className="landing-product-book-line"><span>Expense account</span><span>Dr</span><span>–</span></div>
                <div className="landing-product-book-line"><span>Business account</span><span>–</span><span>Cr</span></div>
                <div className="landing-product-book-foot">Balanced entry</div>
              </div>
              <div className="landing-product-stamp">Source. Review. Record.</div>
            </div>
          </section>

          <section className="landing-section landing-bas" id="bas" aria-labelledby="bas-heading">
            <div className="landing-bas-copy">
              <p className="landing-kicker">Australian accounting, thoughtfully scoped</p>
              <h2 id="bas-heading">Prepared is not lodged.</h2>
              <p>Tammy helps you prepare local GST/BAS workpapers with reviewable calculations and retained sources. The current release does not submit tax reports to the ATO.</p>
              <a className="landing-text-link" href="/privacy">Understand the reporting boundary <span aria-hidden="true">↗</span></a>
            </div>
            <div className="landing-bas-stages" aria-label="Local BAS preparation stages">
              <div><span>01</span><strong>Prepare</strong><small>Gather local GST evidence</small></div>
              <div><span>02</span><strong>Review</strong><small>Check the draft and its sources</small></div>
              <div className="landing-bas-stage-last"><span>03</span><strong>Draft, not lodged</strong><small>No ATO submission in this release</small></div>
            </div>
          </section>

          <section className="landing-section landing-privacy" id="privacy-first" aria-labelledby="privacy-heading">
            <div className="landing-privacy-visual" aria-hidden="true">
              <img src="/assets/contour-lines.svg" alt="" />
              <div className="landing-lock-scene">
                <span className="landing-lock-shackle" />
                <span className="landing-lock-body"><Mark /></span>
              </div>
            </div>
            <div className="landing-privacy-copy">
              <p className="landing-kicker">Local by design</p>
              <h2 id="privacy-heading">Your data stays{" "}<br />in your hands.</h2>
              <p>Tammy stores accounting records in an encrypted workspace on your Mac. Some credentials live in macOS Keychain. There is no hosted Tammy business account or cloud sync in this release.</p>
              <ul>
                <li>SQLCipher-encrypted local workspace</li>
                <li>Explicit local document imports and exports</li>
                <li>Optional, session-scoped local API and MCP access</li>
              </ul>
              <a className="landing-text-link landing-text-link-light" href="/privacy">Read our privacy policy <span aria-hidden="true">↗</span></a>
            </div>
          </section>

          <section className="landing-section landing-australia" aria-labelledby="australia-heading">
            <div className="landing-australia-heading">
              <p className="landing-kicker">Made for the way you work</p>
              <h2 id="australia-heading">Australian by design.{" "}<br />Focused by choice.</h2>
            </div>
            <div className="landing-australia-content">
              <p>AUD amounts, GST workflows, Australian chart-of-accounts conventions and local reporting drafts are part of Tammy’s current accounting foundation.</p>
              <p>Built as a focused desktop workspace, not an all-purpose business platform.</p>
            </div>
          </section>

          <section className="landing-section landing-faq" id="questions" aria-labelledby="questions-heading">
            <div className="landing-faq-intro">
              <p className="landing-kicker">Useful answers</p>
              <h2 id="questions-heading">The details matter.</h2>
              <p>No feature promises hiding in the fine print. Here is what Tammy does today, and what it does not.</p>
              <a className="landing-text-link" href="/support">See support and technical details <span aria-hidden="true">↗</span></a>
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


          <section className="landing-section landing-tools-section" id="free-tools" aria-labelledby="landing-tools-heading">
            <div className="landing-tools-intro">
              <p className="landing-kicker">For the everyday numbers</p>
              <h2 id="landing-tools-heading">Little tools. <br />Big clarity.</h2>
              <p>Create invoices, calculate GST, check an ABN, or understand pricing margins. Free utilities that keep your information in this browser tab.</p>
              <a className="landing-text-link" href="/tools">Explore all free tools <span aria-hidden="true">↗</span></a>
              <a className="landing-text-link landing-guides-link" href="/guides">Read the Australian accounting guides <span aria-hidden="true">↗</span></a>
            </div>
            <div className="landing-tools-list">
              <a href="/tools/invoice-generator"><span className="landing-tools-number">01</span><strong>Australian invoice generator</strong><span>Make and download an invoice PDF</span><span aria-hidden="true">↗</span></a>
              <a href="/tools/quote-generator"><span className="landing-tools-number">02</span><strong>Quote and estimate generator</strong><span>Prepare a printable quote</span><span aria-hidden="true">↗</span></a>
              <a href="/tools/gst-calculator"><span className="landing-tools-number">05</span><strong>GST calculator</strong><span>Add or remove 10% GST</span><span aria-hidden="true">↗</span></a>
              <a href="/tools/invoice-due-date-calculator"><span className="landing-tools-number">06</span><strong>Invoice due-date calculator</strong><span>Work out payment terms</span><span aria-hidden="true">↗</span></a>
              <a href="/tools/abn-checker"><span className="landing-tools-number">07</span><strong>ABN format checker</strong><span>Check the number before the register</span><span aria-hidden="true">↗</span></a>
              <a href="/tools/profit-margin-calculator"><span className="landing-tools-number">08</span><strong>Profit margin calculator</strong><span>Understand markup and gross margin</span><span aria-hidden="true">↗</span></a>
              <a href="/tools/break-even-calculator"><span className="landing-tools-number">09</span><strong>Break-even calculator</strong><span>Know the unit sales target</span><span aria-hidden="true">↗</span></a>
              <a href="/tools/cash-runway-calculator"><span className="landing-tools-number">10</span><strong>Cash runway calculator</strong><span>Estimate monthly cash coverage</span><span aria-hidden="true">↗</span></a>
              <a href="/tools/cash-flow-forecast"><span className="landing-tools-number">11</span><strong>12-week cash flow forecast</strong><span>Plan dated receipts and payments</span><span aria-hidden="true">↗</span></a>
            </div>
          </section>

          <section className="landing-section landing-journal" aria-labelledby="landing-journal-heading">
            <div className="landing-journal-intro">
              <p className="landing-kicker">The Tammy Journal</p>
              <h2 id="landing-journal-heading">Better questions, clearer numbers.</h2>
              <p>Carefully worked notes on GST timing, bank receipts, month-end reviews and business decisions.</p>
              <a className="landing-text-link" href="/blog">Explore the journal <span aria-hidden="true">↗</span></a>
            </div>
            <div className="landing-journal-stories">
              <a href="/blog/which-quarter-gets-the-gst"><span>GST</span><strong>The BAS quarter has two clocks.</strong><span aria-hidden="true">↗</span></a>
              <a href="/blog/not-every-bank-deposit-is-sales"><span>Bookkeeping</span><strong>Not every bank deposit is sales.</strong><span aria-hidden="true">↗</span></a>
              <a href="/blog/the-month-end-review-that-found-220"><span>Bookkeeping</span><strong>The month-end close that found $220.</strong><span aria-hidden="true">↗</span></a>
              <a href="/blog/the-price-rise-that-didnt-fix-margin"><span>Pricing</span><strong>The price rise that didn&apos;t fix the margin.</strong><span aria-hidden="true">↗</span></a>
            </div>
          </section>

          <section className="landing-closing" id="availability" aria-labelledby="closing-heading">
            <img src="/assets/contour-lines.svg" className="landing-closing-contours" alt="" aria-hidden="true" />
            <div>
              <p className="landing-kicker">Tammy is in development</p>
              <h2 id="closing-heading">Your books deserve{" "}<br />a quieter home.</h2>
              <p>Explore the local-first approach and contact the publisher about development access. A public Mac download is not available from this site yet.</p>
              <div className="landing-closing-actions">
                <a className="landing-button landing-button-light" href={"mailto:" + identity.supportEmail + "?subject=Tammy%20development%20access"}>Enquire about Tammy <span aria-hidden="true">↗</span></a>
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
