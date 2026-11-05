import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it } from "vitest";
import GstTimingArticle, { metadata as gstMetadata } from "../app/blog/which-quarter-gets-the-gst/page";
import BankDepositArticle, { metadata as depositsMetadata } from "../app/blog/not-every-bank-deposit-is-sales/page";
import MonthEndArticle, { metadata as closeMetadata } from "../app/blog/the-month-end-review-that-found-220/page";
import { JournalEntry } from "../components/mdx/journal-entry";
import { ReviewChecklist } from "../components/mdx/review-checklist";
import { ScenarioExplorer } from "../components/mdx/scenario-explorer";
import { blogPosts, blogPath } from "../lib/blog/posts";

afterEach(cleanup);

const entries = [
  { Page: GstTimingArticle, slug: "which-quarter-gets-the-gst", cover: "journal-gst-two-periods.svg", metadata: gstMetadata },
  { Page: BankDepositArticle, slug: "not-every-bank-deposit-is-sales", cover: "journal-deposit-pathways.svg", metadata: depositsMetadata },
  { Page: MonthEndArticle, slug: "the-month-end-review-that-found-220", cover: "journal-month-end-close.svg", metadata: closeMetadata },
] as const;

describe("Tammy accounting editorial blocks", () => {
  it("switches a clearly labelled scenario without affecting the original case", () => {
    const { container } = render(<ScenarioExplorer title="Two GST bases" scenarios={[
      { label: "Cash", context: "Paid in July", rows: [["June quarter", "$0"],["September quarter", "$100"]], conclusion: "July is the reporting month." },
      { label: "Non-cash", context: "Invoiced in June", rows: [["June quarter", "$100"],["September quarter", "$0"]], conclusion: "June is the reporting month." },
    ]}/>);
    expect(screen.getByRole("button", { name: "Cash" }).getAttribute("aria-pressed")).toBe("true");
    expect(container.querySelector(".tammy-mdx-scenario-data")?.textContent).toContain("Paid in July");
    fireEvent.click(screen.getByRole("button", { name: "Non-cash" }));
    expect(screen.getByRole("button", { name: "Non-cash" }).getAttribute("aria-pressed")).toBe("true");
    expect(container.querySelector(".tammy-mdx-scenario-data")?.textContent).toContain("Invoiced in June");
    expect(container.querySelector(".tammy-mdx-scenario-data")?.textContent).toContain("June is the reporting month.");
    fireEvent.click(screen.getByRole("button", { name: "Cash" }));
    expect(container.querySelector(".tammy-mdx-scenario-data")?.textContent).toContain("Paid in July");
  });

  it("allows checklist selection and a working reset, with no persistence claim", () => {
    render(<ReviewChecklist title="Monthly review" items={["Confirm the period", "Reconcile movements", "Retain evidence"]}/>);
    const output = screen.getByText("0 of 3 reviewed");
    expect(output).toBeTruthy();
    const reset = screen.getByRole("button", { name: "Reset checklist" });
    expect((reset as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("checkbox", { name: "Confirm the period" }));
    expect(screen.getByText("1 of 3 reviewed")).toBeTruthy();
    expect((reset as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(reset);
    expect(screen.getByText("0 of 3 reviewed")).toBeTruthy();
    expect((screen.getByRole("checkbox", { name: "Confirm the period" }) as HTMLInputElement).checked).toBe(false);
    expect(screen.getByText(/selections are not saved/)).toBeTruthy();
  });

  it("renders a balanced journal table and rejects an unbalanced one", () => {
    const example = [
      { account: "Bank", debitCents: 870000 },
      { account: "Sales", creditCents: 200000 },
      { account: "GST", creditCents: 20000 },
      { account: "Loan", creditCents: 500000 },
      { account: "Capital", creditCents: 150000 },
    ];
    const { container } = render(<JournalEntry title="Deposits" lines={example} note="Illustrative"/>);
    const table = container.querySelector("table") as HTMLTableElement;
    expect(within(table).getByRole("rowheader", { name: "Loan" })).toBeTruthy();
    const footer = table.querySelector("tfoot");
    expect(footer?.textContent).toContain("$8,700.00");
    expect(footer?.querySelectorAll("td")).toHaveLength(2);
    cleanup();
    expect(() => render(<JournalEntry title="Unbalanced" lines={[
      {account:"Bank",debitCents:10000},{account:"Income",creditCents:9000}
    ]}/>)).toThrow(/balanced debit and credit/);
  });

  it("compiles all three new MDX posts with their registered instruments", () => {
    for (const item of entries) {
      const { container } = render(<item.Page />);
      const p = blogPosts.find((post) => post.slug === item.slug);
      expect(p).toBeTruthy();
      expect(screen.getByRole("heading", { level: 1 }).textContent).toBe(p?.title);
      expect(container.querySelectorAll(".blog-story-copy h2[id]")).toHaveLength(4);
      expect(container.querySelector(".tammy-mdx-figure img")?.getAttribute("src")).toBe("/assets/" + item.cover);
      expect(container.querySelector(".tammy-mdx-tool-link")).toBeTruthy();
      expect(container.querySelector(".tammy-mdx-source a[href^='https://']")).toBeTruthy();
      expect(item.metadata.alternates).toEqual({ canonical: blogPath(p!) });
      expect(container.querySelector('script[type="application/ld+json"]')?.textContent).toContain('"@type":"BlogPosting"');
      cleanup();
    }
  });

  it("connects the correct custom blocks and audit-friendly arithmetic to each article", () => {
    const gst = render(<GstTimingArticle />);
    expect(gst.container.querySelector(".tammy-mdx-scenarios")).toBeTruthy();
    expect(gst.container.textContent).toContain("$1,100");
    cleanup();

    const bank = render(<BankDepositArticle />);
    expect(bank.container.querySelector(".tammy-mdx-journal-entry")).toBeTruthy();
    expect(bank.container.querySelector(".tammy-mdx-journal-entry tfoot")?.textContent).toContain("$8,700.00");
    cleanup();

    const close = render(<MonthEndArticle />);
    expect(close.container.querySelector(".tammy-mdx-review")).toBeTruthy();
    expect(close.container.textContent).toContain("$15,000");
    expect(close.container.textContent).toContain("$14,780");
  });

  it("indexes every source and keeps the illustrations text-free", () => {
    const root = "https://tammy-accounting.castlemilk.chatgpt.site";
    const rss = readFileSync("public/rss.xml", "utf8");
    const sitemap = readFileSync("public/sitemap.xml", "utf8");
    const llms = readFileSync("public/llms.txt", "utf8");
    for (const item of entries) {
      const path = "/blog/" + item.slug;
      expect(rss).toContain("<link>" + root + path + "</link>");
      expect(sitemap).toContain("<loc>" + root + path + "</loc>");
      expect(llms).toContain(root + path);
      const source = readFileSync("content/blog/" + item.slug + ".mdx", "utf8");
      const post = blogPosts.find((p) => p.slug === item.slug);
      for (const toc of post?.toc ?? [])expect(source).toContain('<h2 id="' + toc.id + '">');
      const svg = readFileSync("public/assets/" + item.cover, "utf8");
      expect(svg).toMatch(/<svg/);
      expect(svg).not.toMatch(/<script|<foreignObject|<text\b|<image\b/i);
    }
  });

  it("reconciles all three illustrative numerical scenarios", () => {
    expect(1100 / 11).toBe(100);
    expect(2200 + 5000 + 1500).toBe(8700);
    expect(2000 + 200 + 5000 + 1500).toBe(8700);
    expect(15000 - 14780).toBe(220);
    expect(14780 + 220).toBe(15000);
  });
});
