/* oxlint-disable next/no-html-link-for-pages -- Vinext serves these static same-origin routes without a Next runtime. */
import { publicContent } from '../content/public-content.generated';

const { identity } = publicContent;
const architectureLabel = identity.architectures.includes('arm64')
  ? 'Apple silicon'
  : identity.architectures.join(', ');
const lodgementBoundary = identity.capabilityBoundary.atoLodgement.replaceAll(
  '-',
  ' ',
);
const minimumMacOSLabel = identity.minimumMacOSVersion.replace(/\.0$/, '');

export default function Home() {
  return (
    <div className="site-shell">
      <a className="skip-link" href="#main-content">
        Skip to content
      </a>
      <header className="site-header">
        <a
          className="wordmark"
          href="/"
          aria-label={`${identity.installedName} home`}
        >
          {identity.installedName}
        </a>
        <nav aria-label="Primary navigation">
          <a href="/privacy">Privacy</a>
          <a href="/support">Support</a>
        </nav>
      </header>

      <main id="main-content">
        <section className="hero" aria-labelledby="hero-heading">
          <p className="eyebrow">Private desktop accounting</p>
          <h1 id="hero-heading">Local accounting for Australia</h1>
          <p className="hero-copy">
            Create an encrypted workspace, post and inspect journals, review
            source documents, reconcile bank transactions, and build a local GST
            workpaper draft on your Mac.
          </p>
          <p className="boundary-note">
            Reporting is {identity.capabilityBoundary.reporting}; ATO and SBR
            submissions are {lodgementBoundary} in this release.
          </p>
          <div className="hero-actions" aria-label="Learn more">
            <a className="primary-link" href="/privacy">
              Read our privacy policy
            </a>
            <a className="secondary-link" href="/support">
              Get support
            </a>
          </div>
          <p className="platform-note">
            {identity.installedName} requires macOS {minimumMacOSLabel} or later
            on {architectureLabel} (arm64).
          </p>
        </section>

        <aside className="trust-strip" aria-label="Local data promise">
          <span className="trust-mark" aria-hidden="true" />
          <div>
            <p>Your records stay on this Mac</p>
            <span>
              Accounting records remain in your local encrypted workspace for
              this release.
            </span>
          </div>
        </aside>
      </main>

      <footer className="site-footer">
        <span>{identity.publisher}</span>
        <span>{identity.copyright}</span>
      </footer>
    </div>
  );
}
