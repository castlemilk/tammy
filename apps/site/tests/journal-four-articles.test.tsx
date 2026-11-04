import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import BlogPage from "../app/blog/page";
import GstArticle, { metadata as gstMetadata } from "../app/blog/when-to-register-for-gst-australia/page";
import RecordsArticle, { metadata as recordsMetadata } from "../app/blog/business-receipts-five-year-record-keeping/page";
import BasArticle, { metadata as basMetadata } from "../app/blog/simpler-bas-g1-1a-1b-explained/page";
import ReconciliationArticle, { metadata as bankMetadata } from "../app/blog/bank-statement-reconciliation-steps/page";
import { blogPosts, blogPath } from "../lib/blog/posts";

const latestPosts = [
  { path: "/blog/when-to-register-for-gst-australia", cover: "journal-gst-threshold.svg", Page: GstArticle, metadata: gstMetadata },
  { path: "/blog/business-receipts-five-year-record-keeping", cover: "journal-records-archive.svg", Page: RecordsArticle, metadata: recordsMetadata },
  { path: "/blog/simpler-bas-g1-1a-1b-explained", cover: "journal-bas-workpaper.svg", Page: BasArticle, metadata: basMetadata },
  { path: "/blog/bank-statement-reconciliation-steps", cover: "journal-bank-reconciliation.svg", Page: ReconciliationArticle, metadata: bankMetadata },
] as const;

afterEach(cleanup);

describe("Tammy Journal source-backed article expansion", () => {
  it("publishes exactly four new complete posts, each with standalone local editorial artwork", () => {
    expect(blogPosts.length).toBe(7);
    const latest = blogPosts.filter(post => post.publishedAt === "2026-11-05");
    expect(latest).toHaveLength(4);
    expect(blogPosts.slice(0, 4)).toEqual(latest);
    expect(new Set(blogPosts.map(post => post.slug)).size).toBe(blogPosts.length);
    for (const post of latest) {
      expect(post.cover).toMatch(/^\/assets\/journal-[a-z-]+\.svg$/);
      expect(post.coverAlt?.length).toBeGreaterThan(35);
      expect(post.toc).toHaveLength(4);
      expect(new Set(post.toc.map(item => item.id)).size).toBe(4);
      const svg = readFileSync("public" + post.cover, "utf8");
      expect(svg).toContain("<svg");
      expect(svg).toContain("</svg>");
      expect(svg).not.toMatch(/<script|<foreignObject|<text\b|<image\b/i);
      expect(svg).not.toMatch(/(?:href|xlink:href)="https?:\/\//);
    }
  });

  it("highlights the newest original story and shows illustrated archive entries", () => {
    const { container } = render(<BlogPage />);
    const title = screen.getByRole("heading", { level: 2, name: /When GST registration becomes necessary/ });
    expect(title).toBeTruthy();
    expect(container.querySelector(".blog-feature-hero-art")?.getAttribute("src")).toBe("/assets/journal-gst-threshold.svg");
    expect(container.querySelectorAll(".blog-archive-art").length).toBe(3);
    expect(screen.getByRole("link", { name: /Keep the evidence behind the entry/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Make the bank and ledger agree/ })).toBeTruthy();
  });

  it("renders each editorial article with genuine headings, sources, images and JSON-LD", () => {
    for (const item of latestPosts) {
      const { container } = render(<item.Page />);
      expect(screen.getByRole("heading", { level: 1 })).toBeTruthy();
      expect(container.querySelectorAll(".blog-story-copy section[id]")).toHaveLength(4);
      const img = container.querySelector(".blog-story-illustration");
      expect(img?.getAttribute("src")).toBe("/assets/" + item.cover);
      expect(img?.getAttribute("alt")?.length).toBeGreaterThan(30);
      expect(container.querySelectorAll('.blog-story-copy a[href^="https://"]').length).toBeGreaterThan(0);
      const json = container.querySelector('script[type="application/ld+json"]')?.textContent || "";
      expect(json).toContain('"@type":"BlogPosting"');
      expect(json).toContain('"datePublished":"2026-11-05"');
      expect(item.metadata.alternates).toEqual({ canonical: item.path });
      const post = blogPosts.find(post => blogPath(post) === item.path);
      expect(post).toBeTruthy();
      for (const section of post?.toc ?? []) {
        expect(container.querySelector("#" + section.id)).toBeTruthy();
      }
      cleanup();
    }
  });

  it("keeps BAS and reconciliation illustrations numerically consistent", () => {
    const { container } = render(<BasArticle />);
    const basRows = [...container.querySelectorAll(".blog-data-table tbody tr")].map(row=>row.textContent);
    expect(basRows.some(row=>row?.includes("G1") && row?.includes("$22,000"))).toBe(true);
    expect(basRows.some(row=>row?.includes("1A") && row?.includes("$2,000"))).toBe(true);
    expect(basRows.some(row=>row?.includes("1B") && row?.includes("$650"))).toBe(true);
    expect(basRows.some(row=>row?.includes("Difference") && row?.includes("$1,350"))).toBe(true);
    cleanup();
    const bank = render(<ReconciliationArticle />);
    const lastRow = bank.container.querySelector(".blog-data-table tbody tr:last-child")?.textContent ?? "";
    expect(lastRow).toContain("$10,650.00");
    expect(lastRow).toContain("$10,690.00");
    expect(bank.container.textContent).toContain("$40");
  });

  it("has a unique sitemap and RSS entry for each new article", () => {
    const sitemap = readFileSync("public/sitemap.xml","utf8");
    const rss = readFileSync("public/rss.xml","utf8");
    const llms = readFileSync("public/llms.txt","utf8");
    expect((rss.match(/<item>/g) ?? []).length).toBe(7);
    for (const item of latestPosts) {
      const url = "https://tammy-accounting.castlemilk.chatgpt.site" + item.path;
      expect(sitemap).toContain("<loc>" + url + "</loc>");
      expect(rss).toContain("<link>" + url + "</link>");
      expect(llms).toContain(url);
    }
  });
});
