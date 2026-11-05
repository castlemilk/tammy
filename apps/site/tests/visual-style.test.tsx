import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { publicTools } from "../lib/tool-library";

describe("Tammy visual library review guards", () => {
  it("keeps all tool routes as actual relative links and one featured entry", () => {
    expect(publicTools.every((tool) => tool.href.startsWith("/tools/"))).toBe(true);
    expect(publicTools.filter((tool) => tool.featured).map((tool) => tool.name)).toEqual(["Invoice generator"]);
  });

  it("ships responsive focus and reduced-motion rules", () => {
    const css = readFileSync("app/tools/library.css", "utf8");
    expect(css).toContain('aria-pressed="true"');
    expect(css).toContain(":focus-visible");
    expect(css).toContain("prefers-reduced-motion: reduce");
    expect(css).toContain("max-width: 760px");
    expect(css).not.toContain("transition: all");
  });

  it("uses distinct, self-contained vector art with no unreviewed text claims", () => {
    for (const name of ["tammy-ledger-helix.svg", "tammy-invoice-fold.svg", "tammy-margin-orbit.svg"]) {
      const source = readFileSync("public/assets/" + name, "utf8");
      expect(source).toContain("<svg");
      expect(source).toContain("</svg>");
      expect(source).not.toContain("<script");
      expect(source).not.toContain("<foreignObject");
      expect(source).not.toContain("http://example.com");
      expect(source).not.toMatch(/get started|tax lodgement|bank feed|automatic matching/i);
    }
  });
});
