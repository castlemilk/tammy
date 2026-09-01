/* oxlint-disable next/no-html-link-for-pages -- Vinext serves these static same-origin routes without a Next runtime. */
import type { Metadata } from "next";
import { PolicyDocument } from "../../components/policy-document";
import { publicContent } from "../../content/public-content.generated";

const { identity, policy } = publicContent;

export const metadata: Metadata = {
  title: "Privacy policy",
  description: `${identity.appStoreName}'s privacy policy for the macOS app and public website.`,
  alternates: { canonical: "/privacy" },
};

export default function PrivacyPage() {
  return (
    <div className="site-shell">
      <a className="skip-link" href="#main-content">
        Skip to content
      </a>
      <header className="site-header">
        <a className="wordmark" href="/" aria-label={`${identity.installedName} home`}>
          {identity.installedName}
        </a>
        <nav aria-label="Primary navigation">
          <a aria-current="page" href="/privacy">
            Privacy
          </a>
          <a href="/support">Support</a>
        </nav>
      </header>

      <main className="document-page" id="main-content">
        <header className="document-heading">
          <p className="eyebrow">Your data, clearly explained</p>
          <h1>Privacy policy</h1>
          <p>
            Effective {policy.effectiveDate}. Last updated {policy.effectiveDate}.
          </p>
        </header>
        <PolicyDocument sections={policy.sections} />
        <p className="document-contact">
          Questions about this policy? Visit <a href="/support">Support</a>.
        </p>
      </main>

      <footer className="site-footer">
        <span>{identity.publisher}</span>
        <span>{identity.copyright}</span>
      </footer>
    </div>
  );
}
