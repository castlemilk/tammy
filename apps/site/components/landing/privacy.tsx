/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */

/*
 * Privacy: the one full-bleed dark chapter. Three things that do not exist,
 * each beside the mechanism that makes it true; the line (the edge of this Mac
 * and its three gates); and the three steps of setting up a workspace, in the
 * app's own words. The only motion is the line being drawn, once.
 */
import { Icon, Note, Tick } from "./kit";
import { PrivacyLine } from "./privacy-line";
import { business } from "./scenario";

/* Sixteen dots: a masked credential. It is not a length the app requires. */
const masked = "•".repeat(16);

function Masked() {
  return (
    <>
      <span aria-hidden="true">{masked}</span>
      <span className="lp-visually-hidden">Hidden as you type</span>
    </>
  );
}

export function Privacy() {
  return (
    <section className="lp-section lp-privacy" id="privacy-first" data-tone="deep" data-room="mac" aria-labelledby="privacy-heading">
      <div className="lp-frame">
        <header className="lp-section-head lp-privacy-head">
          <h2 className="lp-h2" id="privacy-heading">
            Encrypted on disk. <br />
            Readable once you unlock it.
          </h2>
          <p className="lp-deck">
            The workspace is an encrypted file on your Mac. Tammy does not send your records anywhere unless you export them or
            enable the local API yourself.
          </p>
        </header>

        <div className="lp-privacy-body">
          <ul className="lp-privacy-nos">
            <li className="lp-privacy-no">
              <h3>No online account.</h3>
              <p className="lp-body">
                The workspace is a file on this Mac, opened with <strong>a passphrase you choose</strong> and an administrator
                sign-in. There is nothing to sign up for.
              </p>
            </li>
            <li className="lp-privacy-no">
              <h3>No sync service.</h3>
              <p className="lp-body">
                Tammy does not copy the workspace to another device or to a server. Records are kept in a{" "}
                <strong>SQLCipher-encrypted database</strong> on this Mac; some secrets and credentials are kept in the macOS
                Keychain.
              </p>
            </li>
            <li className="lp-privacy-no">
              <h3>Nothing sent automatically.</h3>
              <p className="lp-body">
                Tammy reads a file <strong>only when you choose it</strong> and writes an export only where you say. The app has
                no Gamma-owned analytics, advertising or tracking.
              </p>
            </li>
          </ul>

          <PrivacyLine />
        </div>

        <div className="lp-privacy-setup">
          <div className="lp-privacy-setup-head">
            <h3 className="lp-h3">Setting up a workspace</h3>
            <p className="lp-micro">Three steps, in the app&apos;s own words. Illustrative, with a placeholder code.</p>
          </div>

          <ol className="lp-privacy-sheets">
            <li className="lp-sheet lp-privacy-sheet">
              <h4 className="lp-privacy-step">
                <span className="lp-num">01</span> Business details
              </h4>
              <dl className="lp-privacy-fields">
                <div>
                  <dt>Business display name</dt>
                  <dd>{business.name}</dd>
                </div>
                <div>
                  <dt>Workspace passphrase</dt>
                  <dd>
                    <Masked />
                  </dd>
                </div>
                <div>
                  <dt>Administrator password</dt>
                  <dd>
                    <Masked />
                  </dd>
                </div>
              </dl>
              <span className="lp-privacy-chip" aria-hidden="true">
                Create local workspace
              </span>
            </li>

            <li className="lp-sheet lp-privacy-sheet">
              <h4 className="lp-privacy-step">
                <span className="lp-num">02</span> Save recovery code
              </h4>
              <p className="lp-privacy-app-line">Keep this one-time code somewhere safe before continuing.</p>
              <div className="lp-privacy-code">
                <p className="lp-privacy-code-label">
                  <Tick /> One-time recovery code
                </p>
                <p className="lp-privacy-code-value">EXAMPLE-0000-0000-0000</p>
              </div>
              <span className="lp-privacy-chip" aria-hidden="true">
                I saved my recovery code
              </span>
            </li>

            <li className="lp-sheet lp-privacy-sheet">
              <h4 className="lp-privacy-step">
                <span className="lp-num">03</span> Open your workspace
              </h4>
              <p className="lp-privacy-bar">
                <span className="lp-privacy-bar-crumb">
                  {business.name} <span aria-hidden="true">/</span> <strong>Overview</strong>
                </span>
                <span className="lp-privacy-bar-local">
                  <Icon name="lock" />
                  Local data
                </span>
              </p>
              <p className="lp-privacy-app-line">Your records stay on this device.</p>
            </li>
          </ol>

          <Note className="lp-privacy-limits">
            The recovery code is shown once, during setup. The publisher cannot recover a lost workspace, and encryption cannot
            protect records from someone using your unlocked Mac.
          </Note>

          <a className="lp-link lp-privacy-policy" href="/privacy">
            Read our privacy policy
          </a>
        </div>
      </div>
    </section>
  );
}
