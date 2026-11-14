/*
 * Hero. The first viewport is the product: the statement, and under it an
 * illustrative Tammy window that opens the way the workspace is stored
 * (encrypted) and unlocks into a readable trial balance, with a rail the
 * visitor can click through and one margin note that follows the rail.
 *
 * The load moment is CSS keyframes only (hero.css): nothing here needs script
 * to be complete, and the h1, lede, buttons and availability line never move.
 */
import { publicContent } from "../../content/public-content.generated";
import { HeroNotes } from "./hero-notes";
import { HeroWindow } from "./hero-window";
import { Arrow, FigNote, Icon } from "./kit";

const minimumMacOSLabel = publicContent.identity.minimumMacOSVersion.replace(/\.0$/, "");
const platformLabel = "macOS " + minimumMacOSLabel + " or later, Apple silicon";

export function Hero() {
  return (
    <section className="lp-section lp-hero" aria-labelledby="landing-heading" data-room="mac">
      <div className="lp-frame">
        <div className="lp-grid lp-hero-top">
          <div className="lp-hero-statement">
            <h1 className="lp-h1" id="landing-heading">
              Your books.{" "}
              <br />
              <span>On your Mac.</span>
            </h1>
          </div>
          <div className="lp-hero-intro">
            <p className="lp-hero-lede">
              <strong>Encrypted accounting for Australian small businesses, kept on your Mac.</strong> Invoices,
              purchases, bank reconciliation and GST workpapers in one local workspace. Twelve free tools work in
              your browser today.
            </p>
            <div className="lp-hero-actions">
              <a className="lp-btn lp-btn--solid" href="#workflow">
                See how it works
                <Arrow />
              </a>
              <a className="lp-btn lp-btn--outline" href="#free-tools">
                Try free tools
              </a>
            </div>
            <div className="lp-hero-status">
              <p className="lp-hero-platform">
                <Icon name="mac" />
                {platformLabel}
              </p>
              <p className="lp-hero-availability">
                In development. Public Mac download not yet available. <a href="#availability">Release status</a>
              </p>
            </div>
          </div>
        </div>

        <figure className="lp-grid lp-hero-stage">
          <HeroWindow />
          <HeroNotes />
          <FigNote className="lp-hero-caption">
            Locked rows stand in for encrypted data. They are not real ciphertext.
          </FigNote>
        </figure>
      </div>
    </section>
  );
}
