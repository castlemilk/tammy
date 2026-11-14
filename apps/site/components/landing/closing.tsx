/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
/*
 * Band 11: availability. The page ends by saying plainly where things stand:
 * the Mac app is development software with no public download, and the
 * browser tools work today. One enquiry action, one way to the tools.
 *
 * Composition: the statement and the brand mark share the top row as two
 * masses of the same height; the copy and the actions share the row beneath;
 * the version line closes the band. The only motion is the mark opening once
 * (closing-mark.tsx).
 */
import { publicContent } from "../../content/public-content.generated";
import { ClosingMark } from "./closing-mark";
import { Arrow } from "./kit";

const { identity, marketingVersion } = publicContent;
const minimumMacOSLabel = identity.minimumMacOSVersion.replace(/\.0$/, "");
const platformLabel = "macOS " + minimumMacOSLabel + " or later, Apple silicon";
// The App Store public-site check (scripts/check-public-site.mjs) requires the home page
// to state the reporting boundary, so it comes from the same identity data.
const boundaryLabel =
  "Reports are " +
  identity.capabilityBoundary.reporting +
  (identity.capabilityBoundary.atoLodgement === "not-lodged" ? " and not lodged" : "");
const enquiryHref = "mailto:" + identity.supportEmail + "?subject=Tammy%20development%20access";

export function Closing() {
  return (
    <section className="lp-section lp-closing" id="availability" data-tone="forest" data-room="mac" aria-labelledby="closing-heading">
      <div className="lp-frame">
        <div className="lp-closing-top">
          <div className="lp-closing-lead">
            <h2 className="lp-h2 lp-closing-title" id="closing-heading">
              <span>The Mac app is in development.</span> <span>The tools are ready now.</span>
            </h2>
          </div>
          <ClosingMark />
        </div>

        <div className="lp-closing-bottom">
          <div className="lp-closing-copy">
            <p>Write to the publisher to ask about development access.</p>
            <p>A public Mac download is not available from this site yet.</p>
          </div>
          <div className="lp-closing-actions">
            <a className="lp-btn lp-btn--light" href={enquiryHref}>
              Enquire about Tammy
              <Arrow />
            </a>
            <a className="lp-btn lp-btn--ghost" href="/tools">
              Browse the free tools
            </a>
            <a className="lp-link lp-closing-support" href="/support">
              Get support
            </a>
          </div>
        </div>

        <p className="lp-closing-version">
          <span>{"Version " + marketingVersion}</span>
          <span>Development software</span>
          <span>{platformLabel}</span>
          <span>{boundaryLabel}</span>
        </p>
      </div>
    </section>
  );
}
