/* oxlint-disable next/no-html-link-for-pages -- Vinext serves these static same-origin routes without a Next runtime. */
import type { Metadata } from 'next';
import { publicContent } from '../../content/public-content.generated';

const { deletionGuidance, identity, marketingVersion } = publicContent;
const minimumMacOSLabel = identity.minimumMacOSVersion.replace(/\.0$/, '');

export const metadata: Metadata = {
  title: 'Support',
  description: `Support, diagnostics, and safe local-data removal guidance for ${identity.appStoreName}.`,
};

export default function SupportPage() {
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
          <a aria-current="page" href="/support">
            Support
          </a>
        </nav>
      </header>

      <main className="document-page" id="main-content">
        <header className="document-heading">
          <p className="eyebrow">Help with Tammy</p>
          <h1>Support</h1>
          <p>
            Email{' '}
            <a href={`mailto:${identity.supportEmail}`}>
              {identity.supportEmail}
            </a>
            .
          </p>
        </header>

        <section className="policy-section">
          <h2>What to include</h2>
          <p>
            Tell us you use version {marketingVersion}, macOS{' '}
            {minimumMacOSLabel} or later, and{' '}
            {identity.architectures.join(', ')}. Include the exact error wording
            you observed and the steps needed to reproduce it.
          </p>
          <p className="warning">
            Do not send accounting data, source documents, passwords, recovery
            codes, machine credentials, or cryptographic keys.
          </p>
        </section>

        <section className="policy-section">
          <h2>Remove Tammy data from this Mac</h2>
          <p>
            This is irreversible. App deletion alone does not remove Tammy
            records or Keychain entries. Back up anything you must retain before
            continuing.
          </p>
          <ol>
            <li>Close Tammy.</li>
            <li>
              In Finder, choose Go to Folder and remove the
              workspace/application data at{' '}
              <code>{deletionGuidance.containerDisplayPath}</code>. If present,
              remove only the group container ending in{' '}
              <code>{deletionGuidance.groupContainerSuffix}</code>.
            </li>
            <li>
              Open Keychain Access, search for each Tammy-owned service below,
              and delete only entries whose service name matches exactly.
              <ul>
                {deletionGuidance.keychainServices.map((service) => (
                  <li key={service}>
                    <code>{service}</code>
                  </li>
                ))}
              </ul>
            </li>
          </ol>
        </section>

        <p className="document-contact">
          Read the <a href="/privacy">privacy policy</a> for Tammy’s full
          data-handling boundary.
        </p>
      </main>

      <footer className="site-footer">
        <span>{identity.publisher}</span>
        <span>{identity.copyright}</span>
      </footer>
    </div>
  );
}
