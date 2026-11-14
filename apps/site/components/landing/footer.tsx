/* oxlint-disable next/no-html-link-for-pages -- Vinext serves static same-origin routes. */
import { publicContent } from "../../content/public-content.generated";
import { SiteBrand } from "../site-brand";

const { identity } = publicContent;

const groups = [
  {
    title: "Tammy for Mac",
    links: [
      { href: "#workflow", label: "How it works" },
      { href: "#features", label: "Features" },
      { href: "#privacy-first", label: "Privacy and encryption" },
      { href: "#questions", label: "Questions" },
    ],
  },
  {
    title: "Free to use",
    links: [
      { href: "/tools", label: "All free tools" },
      { href: "/guides", label: "Guides" },
      { href: "/blog", label: "Journal" },
      { href: "/rss.xml", label: "Journal RSS" },
    ],
  },
  {
    title: "The small print",
    links: [
      { href: "/privacy", label: "Privacy policy" },
      { href: "/support", label: "Support" },
      { href: "#availability", label: "Release status" },
    ],
  },
] as const;

export function LandingFooter() {
  return (
    <footer className="lp-footer">
      <div className="lp-frame lp-footer-inner">
        <div>
          <SiteBrand />
          <p className="lp-footer-lede">Private accounting for Australian businesses, kept on your Mac.</p>
        </div>
        <nav className="lp-footer-nav" aria-label="Footer navigation">
          {groups.map((group) => (
            <div key={group.title}>
              <h2>{group.title}</h2>
              <ul>
                {group.links.map((link) => (
                  <li key={link.href}>
                    <a href={link.href}>{link.label}</a>
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </nav>
        <p className="lp-footer-legal">
          <span>{identity.copyright}</span>
          <span>Examples on this page are synthetic. Tammy is development software.</span>
        </p>
      </div>
    </footer>
  );
}
