import { origin } from "./tools-shell";

export function GuideStructuredData({
  name, description, path
}: { readonly name: string; readonly description: string; readonly path: string }) {
  const data = {
    "@context": "https://schema.org",
    "@type": "Article",
    headline: name,
    description,
    mainEntityOfPage: origin + path,
    inLanguage: "en-AU",
    author: { "@type": "Organization", name: "Gamma Systems Pty Ltd" },
    publisher: { "@type": "Organization", name: "Gamma Systems Pty Ltd" }
  };
  return <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(data) }} />;
}
