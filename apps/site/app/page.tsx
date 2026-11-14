import type { Metadata } from "next";
import { publicContent } from "../content/public-content.generated";
import { SITE_ORIGIN } from "../lib/site-config";
import "./landing.css";
import "../components/landing/hero.css";
import "../components/landing/gst-strip.css";
import "../components/landing/workflow.css";
import "../components/landing/invoicing.css";
import "../components/landing/features.css";
import "../components/landing/bas.css";
import "../components/landing/privacy.css";
import "../components/landing/integrations.css";
import "../components/landing/bench.css";
import "../components/landing/tools.css";
import "../components/landing/journal.css";
import "../components/landing/answers.css";
import "../components/landing/closing.css";
import { LandingFooter } from "../components/landing/footer";
import { LandingHeader } from "../components/landing/header";
import { LandingMotion } from "../components/landing/motion";
import { Hero } from "../components/landing/hero";
import { GstStrip } from "../components/landing/gst-strip";
import { Workflow } from "../components/landing/workflow";
import { Features } from "../components/landing/features";
import { Bas } from "../components/landing/bas";
import { Privacy } from "../components/landing/privacy";
import { Integrations } from "../components/landing/integrations";
import { Tools } from "../components/landing/tools";
import { Journal } from "../components/landing/journal";
import { Answers } from "../components/landing/answers";
import { Closing } from "../components/landing/closing";

const { identity, marketingVersion } = publicContent;
const minimumMacOSLabel = identity.minimumMacOSVersion.replace(/\.0$/, "");

export const metadata: Metadata = {
  title: "Local-first accounting for Australian businesses",
  description:
    "Meet Tammy, a private macOS accounting workspace for Australian businesses. Review source documents, reconcile imported bank statements, post journals, issue invoices and prepare local GST workpapers, with twelve free business tools that run in your browser.",
  alternates: { canonical: "/" },
  keywords: [
    "local-first accounting Australia",
    "Mac accounting software",
    "Australian small business bookkeeping",
    "private accounting for macOS",
    "GST workpaper software",
    "BAS draft preparation",
    "free GST calculator",
  ],
};

const structuredData = {
  "@context": "https://schema.org",
  "@type": "SoftwareApplication",
  name: identity.appStoreName,
  applicationCategory: "FinanceApplication",
  operatingSystem: "macOS " + minimumMacOSLabel + "+ (Apple silicon)",
  softwareVersion: marketingVersion,
  description:
    "Local-first Australian accounting software for journals, source-document review, purchases, imported bank-statement reconciliation, invoicing and GST workpaper drafts.",
  url: SITE_ORIGIN + "/",
  publisher: { "@type": "Organization", name: identity.publisher },
};

/*
 * The landing page. Each band is its own component under components/landing,
 * with its stylesheet imported here (route files own CSS imports on this site).
 * Order: the workspace, a tool that works today, how the books come together,
 * what else the workspace does, GST, privacy, the local API, the free tools,
 * the Journal, plain answers, availability.
 */
export default function Home() {
  return (
    <div className="landing">
      <a className="skip-link" href="#main-content">Skip to content</a>
      {/* First in the document so it stays the first JSON-LD block on the page. */}
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(structuredData) }} />
      <LandingHeader />
      <main id="main-content">
        <Hero />
        <GstStrip />
        <Workflow />
        <Features />
        <Bas />
        <Privacy />
        <Integrations />
        <Tools />
        <Journal />
        <Answers />
        <Closing />
      </main>
      <LandingFooter />
      <LandingMotion />
    </div>
  );
}
