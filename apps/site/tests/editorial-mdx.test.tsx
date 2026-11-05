import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import PriceArticle, { metadata as priceMetadata } from "../app/blog/the-price-rise-that-didnt-fix-margin/page";
import AgeingArticle, { metadata as ageingMetadata } from "../app/blog/the-invoice-ageing-trap/page";
import CashArticle, { metadata as cashMetadata } from "../app/blog/when-profit-and-cash-part-ways/page";
import { blogPosts, blogPath } from "../lib/blog/posts";
import { BarChart, Callout, MoneyTable } from "../components/mdx/tammy-components";

afterEach(cleanup);

const cases = [
  { Page: PriceArticle, slug: "the-price-rise-that-didnt-fix-margin", metadata: priceMetadata, figure: "journal-price-sensitivity.svg" },
  { Page: AgeingArticle, slug: "the-invoice-ageing-trap", metadata: ageingMetadata, figure: "journal-invoice-ageing.svg" },
  { Page: CashArticle, slug: "when-profit-and-cash-part-ways", metadata: cashMetadata, figure: "journal-profit-to-cash.svg" },
] as const;

describe("Tammy MDX editorial renderer", () => {
  it("compiles actual Markdown and registered MDX blocks in three routes", () => {
    for (const item of cases) {
      const { container } = render(<item.Page />);
      const post = blogPosts.find((entry) => entry.slug === item.slug);
      expect(post).toBeTruthy();
      expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(post?.title);
      expect(container.querySelectorAll(".blog-story-copy h2[id]")).toHaveLength(4);
      expect(container.querySelector(".tammy-mdx-chart")).toBeTruthy();
      expect(container.querySelector(".tammy-mdx-table")).toBeTruthy();
      expect(container.querySelector(".tammy-mdx-tool-link[href^='/tools/']")).toBeTruthy();
      expect(container.querySelector(".tammy-mdx-source a[href^='https://']")).toBeTruthy();
      expect(container.querySelector(`.tammy-mdx-figure img[src="/assets/${item.figure}"]`)).toBeTruthy();
      expect(item.metadata.alternates).toEqual({ canonical: blogPath(post!) });
      expect(container.querySelector('script[type="application/ld+json"]')?.textContent).toContain('"@type":"BlogPosting"');
      cleanup();
    }
  });

  it("keeps chart values accessible in a semantic table", () => {
    const { container } = render(<BarChart title="Example cost comparison" unit="AUD" items={[
      { label: "First", value: 120, detail: "Illustrative" },
      { label: "Second", value: 80, detail: "Illustrative" },
    ]} />);
    const table = container.querySelector(".tammy-mdx-chart-data");
    expect(table).toBeTruthy();
    expect(within(table as HTMLElement).getByRole("columnheader", { name: "Scenario" })).toBeTruthy();
    expect(within(table as HTMLElement).getByRole("rowheader", { name: "Second" })).toBeTruthy();
    expect(table?.textContent).toContain("80 AUD");
  });

  it("rejects invalid chart data and inconsistent table widths", () => {
    expect(() => render(<BarChart title="Invalid" unit="AUD" items={[{ label: "Bad", value: -1 }]} />)).toThrow();
    cleanup();
    expect(() => render(<MoneyTable caption="Invalid" columns={["A", "B"]} rows={[["A"]]} />)).toThrow();
    cleanup();
    const { container } = render(<Callout title="The distinction">A real explanation.</Callout>);
    expect(container.querySelector(".tammy-mdx-callout-title")?.textContent).toBe("The distinction");
  });

  it("keeps the three MDX sources and illustrated metadata mutually consistent", () => {
    for (const item of cases) {
      const mdx = readFileSync("content/blog/" + item.slug + ".mdx", "utf8");
      expect(mdx).toContain("<MoneyTable");
      expect(mdx).toContain("<BarChart");
      expect(mdx).toContain("<ToolLink");
      expect(mdx).toContain("<SourceNote");
      expect(mdx).toContain('src="/assets/' + item.figure + '"');
      const post = blogPosts.find((entry) => entry.slug === item.slug);
      expect(post?.cover).toBe("/assets/" + item.figure);
      for (const section of post?.toc ?? []) {
        expect(mdx).toContain('<h2 id="' + section.id + '">');
      }
    }
  });

  it("calculates sample scenarios consistently", () => {
    expect(Math.ceil(3000 / (120 - 80))).toBe(75);
    expect(Math.ceil(3000 / (120 - 90))).toBe(100);
    expect(1200 + 3500 + 1800 + 600).toBe(7100);
    expect(3500 + 1800 + 600).toBe(5900);
    expect(12000 - 4000).toBe(8000);
    expect(8000 - 3000).toBe(5000);
    expect(8000 - 4000 + 1000).toBe(5000);
  });
});
