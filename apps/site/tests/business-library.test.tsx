import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { calculateBreakEven, formatAudBigInt } from "../lib/tools/break-even";
import { estimateCashRunway, formatRunway } from "../lib/tools/cash-runway";
import { blogPosts, blogPath } from "../lib/blog/posts";
import BlogIndex, { metadata as blogMetadata } from "../app/blog/page";
import MarginArticle, { metadata as marginBlogMetadata } from "../app/blog/margin-is-not-markup/page";
import InvoiceArticle, { metadata as invoiceBlogMetadata } from "../app/blog/an-invoice-is-not-cash/page";
import CashFlowArticle, { metadata as cashBlogMetadata } from "../app/blog/the-12-week-cash-flow-view/page";
import BreakEvenPage, { metadata as breakEvenMetadata } from "../app/tools/break-even-calculator/page";
import CashRunwayPage, { metadata as cashRunwayMetadata } from "../app/tools/cash-runway-calculator/page";

afterEach(cleanup);

describe("exact AUD business models", () => {
  it("calculates whole-unit break-even correctly", () => {
    expect(calculateBreakEven(300000, 12000, 8000)).toEqual({
      status: "possible", contributionCents: 4000, units: 75n, revenueCents: 900000n,
    });
    expect(calculateBreakEven(300001, 12000, 8000)).toMatchObject({ units: 76n });
    expect(calculateBreakEven(300000, 8000, 8000)).toMatchObject({
      status: "unreachable", units: null,
    });
    expect(calculateBreakEven(300000, 7000, 8000)).toMatchObject({
      status: "unreachable", contributionCents: -1000,
    });
    expect(calculateBreakEven(0, 8000, 8000)).toMatchObject({
      status: "possible", units: 0n,
    });
    expect(() => calculateBreakEven(-1, 12000, 8000)).toThrow();
  });

  it("formats large calculated revenues without floating-point overflow", () => {
    expect(formatAudBigInt(900000n)).toBe("$9,000.00");
    expect(formatAudBigInt(-123456n)).toBe("-$1,234.56");
    expect(formatAudBigInt(900719925474099300n)).toBe("$9,007,199,254,740,993.00");
  });

  it("distinguishes net burn, positive cash flow and no available cash", () => {
    expect(estimateCashRunway(2400000, 600000, 1000000)).toEqual({
      status: "decreasing", monthlyNetCents: -400000, runwayTenths: 60,
    });
    expect(formatRunway(60)).toBe("6.0 months");
    expect(estimateCashRunway(0, 600000, 1000000)).toMatchObject({
      status: "decreasing", runwayTenths: 0,
    });
    expect(estimateCashRunway(2000000, 1000000, 900000)).toMatchObject({
      status: "nondecreasing", runwayTenths: null,
    });
    expect(estimateCashRunway(2000000, 1000000, 1000000)).toMatchObject({
      status: "nondecreasing", monthlyNetCents: 0,
    });
    expect(() => estimateCashRunway(NaN, 100, 200)).toThrow();
    expect(() => estimateCashRunway(-1, 100, 200)).toThrow();
  });
});

describe("new calculators and articles are usable and indexable", () => {
  it("updates the break-even calculation from the form", () => {
    render(<BreakEvenPage />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("costs");
    expect(screen.getByText("75")).toBeTruthy();
    expect(screen.getByText("$9,000.00")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Selling price per unit, ex GST (AUD)"), { target: { value: "80.00" } });
    expect(screen.getAllByText("Not reachable").length).toBeGreaterThan(0);
    expect(breakEvenMetadata.alternates).toEqual({ canonical: "/tools/break-even-calculator" });
  });

  it("shows runway and the no-net-burn state", () => {
    const { container } = render(<CashRunwayPage />);
    expect(container.querySelector(".tools-result-total dd")?.textContent).toBe("6.0 months");
    fireEvent.change(screen.getByLabelText("Expected cash payments per month (AUD)"), {
      target: { value: "6000.00" },
    });
    expect(screen.getByText("No net burn")).toBeTruthy();
    expect(cashRunwayMetadata.alternates).toEqual({ canonical: "/tools/cash-runway-calculator" });
  });

  it("renders a curated journal with working article links", () => {
    render(<BlogIndex />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("Keeping the books");
    expect(screen.getByRole("link", { name: /Margin is not markup/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /An invoice is not cash/i })).toBeTruthy();
    expect(blogPosts).toHaveLength(7);
    expect(new Set(blogPosts.map(blogPath)).size).toBe(blogPosts.length);
    expect(blogMetadata.alternates).toEqual({ canonical: "/blog" });
  });

  it("publishes original worked examples and dated structured data", () => {
    const cases = [
      { Page: MarginArticle, title: "Margin is not markup.", metadata: marginBlogMetadata, route: "/blog/margin-is-not-markup" },
      { Page: InvoiceArticle, title: "An invoice is not cash.", metadata: invoiceBlogMetadata, route: "/blog/an-invoice-is-not-cash" },
      { Page: CashFlowArticle, title: "See the next twelve weeks.", metadata: cashBlogMetadata, route: "/blog/the-12-week-cash-flow-view" },
    ];
    for (const item of cases) {
      const { container } = render(<item.Page />);
      expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(item.title);
      expect(container.querySelectorAll(".blog-story-copy section[id]").length).toBeGreaterThanOrEqual(4);
      expect(container.querySelector('script[type="application/ld+json"]')?.textContent).toContain('"@type":"BlogPosting"');
      expect(container.querySelector('script[type="application/ld+json"]')?.textContent).toContain('"datePublished":"2026-11-04"');
      expect(item.metadata.alternates).toEqual({ canonical: item.route });
      cleanup();
    }
  });

  it("keeps the blog, XML feed and new utilities in search inventory", () => {
    const sitemap = readFileSync("public/sitemap.xml", "utf8");
    const feed = readFileSync("public/rss.xml", "utf8");
    const llms = readFileSync("public/llms.txt", "utf8");
    for (const path of [
      "/tools/break-even-calculator", "/tools/cash-runway-calculator",
      "/blog", ...blogPosts.map(blogPath),
    ]) {
      expect(sitemap).toContain("https://tammy-accounting.castlemilk.chatgpt.site" + path + "</loc>");
      expect(llms).toContain("https://tammy-accounting.castlemilk.chatgpt.site" + path);
    }
    for (const post of blogPosts) expect(feed).toContain("<title>" + post.title + "</title>");
  });
});
