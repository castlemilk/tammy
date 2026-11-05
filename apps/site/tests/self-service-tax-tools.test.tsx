import { vi } from "vitest";
import { afterEach, describe, expect, it } from "vitest";
import { cleanup as cleanupUi, render as renderUi, screen as screenUi, fireEvent as fireUi } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { prepareSimpleBas, quarterlyBasReference } from "../lib/tools/simpler-bas";
import { blogPosts } from "../lib/blog/posts";
import BasPage, { metadata as basMetadata } from "../app/tools/bas-workpaper/page";
import TaxChecklistPage, { metadata as taxMetadata } from "../app/tools/sole-trader-tax-checklist/page";
import SelfBASArticle from "../app/blog/how-to-lodge-bas-yourself-australia/page";
import MyTaxArticle from "../app/blog/lodge-sole-trader-tax-return-mytax/page";
import DatesArticle from "../app/blog/quarterly-bas-due-dates-australia/page";

afterEach(cleanupUi);

describe("Narrow self-service BAS arithmetic", () => {
  it("derives standard labels and simple GST amount in exact AUD cents", () => {
    expect(prepareSimpleBas({
      taxableSalesInclusiveCents: 2200000, otherReportableSalesCents: 0,
      creditablePurchasesInclusiveCents: 715000,
    })).toEqual({
      totalSalesCents: 2200000,
      gstOnSalesCents: 200000, eligibleGstCreditsCents: 65000,
      netGstCents: 135000,
      entryG1Whole: 22000, entry1AWhole: 2000,
      entry1BWhole: 650, entryNetWhole: 1350,
    });
  });
  it("handles other G1 sales and whole-dollar truncation separately", () => {
    const result = prepareSimpleBas({
      taxableSalesInclusiveCents: 1101, otherReportableSalesCents: 10099,
      creditablePurchasesInclusiveCents: 0,
    });
    expect(result.totalSalesCents).toBe(11200);
    expect(result.entryG1Whole).toBe(112);
    expect(result.gstOnSalesCents).toBe(100);
    expect(result.entry1AWhole).toBe(1);
    expect(result.entry1BWhole).toBe(0);
    expect(() => prepareSimpleBas({
      taxableSalesInclusiveCents: -1, otherReportableSalesCents: 0,
      creditablePurchasesInclusiveCents: 0,
    })).toThrow();
  });
  it("lists standard BAS dates without claiming to apply extensions", () => {
    const expected = ["2026-10-28", "2027-02-28", "2027-04-28", "2027-07-28"];
    for (let i=1;i<=4;i++) {
      const date=quarterlyBasReference(2026,i as 1|2|3|4);
      expect(date.standardDueDate).toBe(expected[i-1]);
      expect(date.note).toContain("ATO statement");
    }
    expect(() => quarterlyBasReference(2026, 0 as 1)).toThrow();
  });
});

describe("Tools and article routes", () => {
  it("shows G1, 1A, 1B and live updates with explicit limitation", () => {
    const { container } = renderUi(<BasPage />);
    expect(screenUi.getByRole("heading",{level:1}).textContent).toContain("Prepare the figures");
    expect(container.querySelector(".tools-calculator-result")?.textContent).toContain("$22,000");
    expect(container.querySelector(".tools-calculator-result")?.textContent).toContain("$1,350");
    fireUi.change(screenUi.getByLabelText("Taxable sales, including 10% GST (AUD)"),{target:{value:"11000.00"}});
    expect(container.querySelector(".tools-calculator-result")?.textContent).toContain("$1,000");
    expect(basMetadata.alternates).toEqual({canonical:"/tools/bas-workpaper"});
  });
  it("prints and resets a local-only self-lodgement checklist", () => {
    const old=window.print,print=vi.fn();
    window.print=print;
    try {
      renderUi(<TaxChecklistPage />);
      expect(screenUi.getByText(/0 of 8 checks reviewed/)).toBeTruthy();
      fireUi.click(screenUi.getByRole("checkbox",{name:/Confirm the income year/i}));
      expect(screenUi.getByText(/1 of 8 checks reviewed/)).toBeTruthy();
      fireUi.click(screenUi.getByRole("button",{name:"Reset checks"}));
      expect(screenUi.getByText(/0 of 8 checks reviewed/)).toBeTruthy();
      fireUi.click(screenUi.getByRole("button",{name:"Print checklist"}));
      expect(print).toHaveBeenCalledOnce();
      expect(taxMetadata.alternates).toEqual({canonical:"/tools/sole-trader-tax-checklist"});
    } finally {window.print=old;}
  });
  it("compiles three MDX self-lodgement articles with primary sources", () => {
    for (const Page of [SelfBASArticle,MyTaxArticle,DatesArticle]) {
      const {container}=renderUi(<Page />);
      expect(container.querySelectorAll(".blog-story-copy h2[id]").length).toBe(4);
      expect(container.querySelector(".tammy-mdx-tool-link")).toBeTruthy();
      expect(container.querySelector(".tammy-mdx-source a[href^='https://']")).toBeTruthy();
      cleanupUi();
    }
    expect(blogPosts.slice(0,3).map(x=>x.slug)).toEqual([
      "how-to-lodge-bas-yourself-australia",
      "lodge-sole-trader-tax-return-mytax",
      "quarterly-bas-due-dates-australia",
    ]);
  });
  it("includes indexable source and lightweight original vectors", () => {
    const rss=readFileSync("public/rss.xml","utf8");
    const sitemap=readFileSync("public/sitemap.xml","utf8");
    for(const post of blogPosts.slice(0,3)) {
      const root="https://tammy-accounting.castlemilk.chatgpt.site/blog/"+post.slug;
      expect(rss).toContain("<link>"+root+"</link>");
      expect(sitemap).toContain("<loc>"+root+"</loc>");
      const svg=readFileSync("public"+post.cover,"utf8");
      expect(svg).toContain("<svg");
      expect(svg).not.toMatch(/<script|<text\b|<foreignObject/i);
    }
  });
});
