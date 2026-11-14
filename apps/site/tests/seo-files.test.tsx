import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { metadata as homeMetadata } from "../app/page";
import { metadata as rootMetadata } from "../app/layout";

const readPublic = (name: string) =>
  readFileSync(new URL("../public/" + name, import.meta.url), "utf8");

describe("Tammy public SEO and assets", () => {
  it("keeps the canonical origin consistent across the site map and robots", () => {
    const origin = "https://tammy.tax";
    const robots = readPublic("robots.txt");
    const sitemap = readPublic("sitemap.xml");
    expect(String(rootMetadata.metadataBase)).toBe(origin + "/");
    expect(robots).toContain(origin + "/sitemap.xml");
    expect(sitemap).toContain(origin + "/privacy");
    expect(sitemap).toContain(origin + "/support");
    expect(sitemap).not.toContain("/login");
  });

  it("describes real offline accounting capabilities without fictional marketing claims", () => {
    expect(homeMetadata.alternates?.canonical).toBe("/");
    expect(String(homeMetadata.description)).toMatch(/reconcile imported bank statements/i);
    const llms = readPublic("llms.txt");
    expect(llms).toMatch(/not automatic bank feeds/i);
    expect(llms).toMatch(/not ATO lodgement/i);
    expect(llms).not.toMatch(/\$\d+\/month/);
  });

  it("ships accessible, local vector artwork", () => {
    for (const name of ["contour-lines", "engraved-mountain", "desktop", "journals",
      "privacy", "source-document", "review", "reconciliation", "invoices", "gst-bas"]) {
      const contents = readPublic("assets/" + name + ".svg");
      expect(contents).toMatch(/<svg[\s>]/);
      expect(contents).toContain("viewBox=");
    }
  });
});
