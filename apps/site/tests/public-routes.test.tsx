import { cleanup, isInaccessible, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { metadata } from "../app/layout";
import Home from "../app/page";
import PrivacyPage from "../app/privacy/page";
import SupportPage from "../app/support/page";

afterEach(cleanup);

describe("Tammy public routes", () => {
  it("shows local-first workflows and honest release boundaries", () => {
    const { container } = render(<Home />);

    expect(screen.getByRole("heading", { level: 1 }).textContent).toMatch(
      /Your books\.\s*On your Mac\./,
    );
    expect(screen.getAllByText(/macOS 14 or later, Apple silicon/i).length).toBeGreaterThan(0);
    expect(screen.getByText("BAS drafts, not lodgement")).toBeTruthy();
    expect(screen.getAllByText(/Gamma Systems Pty Ltd/).length).toBeGreaterThan(0);
    expect(screen.getByRole("heading", { name: "From source records to a balanced ledger." }))
      .toBeTruthy();
    expect(screen.getByRole("heading", { name: "Prepared is not lodged." })).toBeTruthy();
    expect(screen.getByRole("link", { name: "See how it works" }).getAttribute("href")).toBe(
      "#workflow",
    );
    expect(screen.getByRole("link", { name: "Enquire about Tammy" }).getAttribute("href"))
      .toMatch(/^mailto:/);
    expect(screen.queryByText(/company (?:EOFY|tax return)/i)).toBeNull();
    expect(screen.queryByRole("link", { name: /download for mac|get started for free/i }))
      .toBeNull();
    expect(container.querySelector('script[type="application/ld+json"]')?.textContent)
      .toContain('"@type":"SoftwareApplication"');
    expect(String(metadata.description)).not.toMatch(/tax return preparation/i);
  });

  it("provides accurate answers without suggesting unsupported integrations", () => {
    render(<Home />);
    expect(screen.getByText("Can Tammy lodge my BAS with the ATO?")).toBeTruthy();
    expect(screen.getByText(/Automatic bank feeds and bank-account syncing are not part/i))
      .toBeTruthy();
    expect(screen.getByText(/explicit issue action posts receivables, income and GST/i)).toBeTruthy();
    expect(screen.getByText(/does not email the invoice or collect payment/i)).toBeTruthy();
    expect(screen.queryByText(/does not.*post a receivable/i)).toBeNull();
    expect(screen.getByText(/There is no Tammy cloud workspace/i)).toBeTruthy();
    expect(screen.getByRole("navigation", { name: "Primary navigation" })).toBeTruthy();
    expect(screen.getByRole("navigation", { name: "Footer navigation" })).toBeTruthy();
  });

  it("retains all twelve tool destinations and four Journal stories on the homepage", () => {
    render(<Home />);
    const tools = within(screen.getByRole("region", { name: "Free business tools" }));
    const toolDestinations = tools.getAllByRole("link")
      .map((link) => link.getAttribute("href"))
      .filter((href) => href?.startsWith("/tools/"));
    expect(toolDestinations.sort((left, right) => (left ?? "").localeCompare(right ?? ""))).toEqual([
      "/tools/invoice-generator",
      "/tools/quote-generator",
      "/tools/expense-organiser",
      "/tools/invoice-due-date-calculator",
      "/tools/bas-workpaper",
      "/tools/sole-trader-tax-checklist",
      "/tools/gst-calculator",
      "/tools/abn-checker",
      "/tools/profit-margin-calculator",
      "/tools/break-even-calculator",
      "/tools/cash-runway-calculator",
      "/tools/cash-flow-forecast",
    ].sort((left, right) => left.localeCompare(right)));

    const journal = within(screen.getByRole("region", { name: "The Tammy Journal" }));
    const storyDestinations = journal.getAllByRole("link")
      .map((link) => link.getAttribute("href"))
      .filter((href) => href?.startsWith("/blog/"));
    expect(storyDestinations.sort((left, right) => (left ?? "").localeCompare(right ?? ""))).toEqual([
      "/blog/which-quarter-gets-the-gst",
      "/blog/not-every-bank-deposit-is-sales",
      "/blog/the-month-end-review-that-found-220",
      "/blog/the-price-rise-that-didnt-fix-margin",
    ].sort((left, right) => left.localeCompare(right)));
  });

  it("resolves every homepage fragment link to one existing target", () => {
    const { container } = render(<Home />);
    const fragmentLinks = Array.from(container.querySelectorAll('a[href^="#"]'));
    const targets = Array.from(container.querySelectorAll("[id]"));
    expect(fragmentLinks.length).toBeGreaterThan(0);
    for (const link of fragmentLinks) {
      const fragment = link.getAttribute("href") as string;
      const targetId = decodeURIComponent(fragment.slice(1));
      expect(targetId, `Empty target for ${link.textContent}`).not.toBe("");
      expect(targets.filter((target) => target.id === targetId), fragment).toHaveLength(1);
    }
  });

  it("states availability in the hero and separates browser PDFs from native accounting", () => {
    render(<Home />);
    const hero = within(screen.getByRole("region", { name: /Your books\.\s*On your Mac\./ }));
    const availability = hero.getByText("In development. Public Mac download not yet available.");
    expect(isInaccessible(availability)).toBe(false);
    expect(availability.closest("details:not([open])")).toBeNull();
    expect(hero.getByRole("link", { name: "Release status" }).getAttribute("href"))
      .toBe("#availability");
    expect(hero.getByRole("link", { name: "Try free tools" }).getAttribute("href"))
      .toBe("#free-tools");

    const invoiceFeature = screen.getByRole("heading", { name: "Customer invoicing" })
      .closest("article");
    expect(invoiceFeature).not.toBeNull();
    const invoice = within(invoiceFeature as HTMLElement);
    expect(invoice.getByRole("link", { name: "Browser invoice generator" }).getAttribute("href"))
      .toBe("/tools/invoice-generator");
    const browserBoundary = invoice.getByText("Creates a PDF independently of your Mac workspace.");
    expect(isInaccessible(browserBoundary)).toBe(false);
    expect(browserBoundary.closest("details:not([open])")).toBeNull();
    expect(invoice.getByText("Issue to the local ledger; export a customer PDF")).toBeTruthy();
  });

  it("renders the canonical app/site/support privacy boundaries", () => {
    render(<PrivacyPage />);

    expect(screen.getByRole("heading", { name: "Privacy policy" })).toBeTruthy();
    expect(screen.getByText(/does not automatically send your workspace records/i)).toBeTruthy();
    expect(screen.getByRole("navigation", { name: "Privacy policy contents" })).toBeTruthy();
    expect(screen.getByText(/Relationship Authorisation Manager, or RAM/)).toBeTruthy();
    expect(screen.getByText(/synthetic, network-disabled simulator/)).toBeTruthy();
    expect(
      screen.getByText(/hosting provider may process technical request information/i),
    ).toBeTruthy();
    expect(screen.getByText(/Google processes it as the email service provider/i)).toBeTruthy();
    expect(screen.getByRole("link", { name: "ABN Lookup page" }).getAttribute("href")).toBe(
      "https://abr.business.gov.au/",
    );
    expect(
      screen.getByRole("link", { name: "Office of the Australian Information Commissioner" }),
    ).toBeTruthy();
  });

  it("renders only the canonical support and deletion inventory", () => {
    render(<SupportPage />);

    expect(
      screen.getByRole("link", { name: /ben\.ebsworth@gmail\.com/i }).getAttribute("href"),
    ).toBe("mailto:ben.ebsworth@gmail.com");
    expect(screen.getByText(/com\.tammy\.workspace/)).toBeTruthy();
    expect(screen.queryByText(/simulator-v2/)).toBeNull();
    expect(screen.getByText(/version 0\.1\.0/i)).toBeTruthy();
    expect(screen.getByText(/app deletion alone does not remove/i)).toBeTruthy();
    expect(screen.getByText(/privacy, access, or correction request/i)).toBeTruthy();
    expect(screen.getByText(/synthetic, network-disabled simulator/)).toBeTruthy();
  });
});
