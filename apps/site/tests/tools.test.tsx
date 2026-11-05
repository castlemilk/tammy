import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import ToolsPage, { metadata as toolsMetadata } from "../app/tools/page";
import GstCalculatorPage, { metadata as gstMetadata } from "../app/tools/gst-calculator/page";
import InvoiceGeneratorPage, { metadata as invoiceMetadata } from "../app/tools/invoice-generator/page";
import DueDateCalculatorPage, { metadata as dueMetadata } from "../app/tools/invoice-due-date-calculator/page";
import { addCalendarDays, aud, gstBreakdown, parseAudCents, parseQuantityMilli } from "../lib/tools/calculations";
import { calculateInvoice, validateInvoiceDraft } from "@tammy/invoices/model";
import { readFileSync } from "node:fs";

afterEach(cleanup);

describe("Tammy business tools", () => {
  it("parses AUD and quantities into exact integer minor units", () => {
    expect(parseAudCents("1,234.50")).toBe(123450);
    expect(parseAudCents("0.05")).toBe(5);
    expect(parseAudCents("1.235")).toBeNull();
    expect(parseAudCents("-1")).toBeNull();
    expect(parseAudCents("1e4")).toBeNull();
    expect(parseAudCents("")).toBeNull();
    expect(parseQuantityMilli("1.125")).toBe(1125);
    expect(parseQuantityMilli("0")).toBeNull();
    expect(parseQuantityMilli("0.001")).toBe(1);
    expect(parseQuantityMilli("1000.001")).toBeNull();
    expect(aud(123450)).toBe("$1,234.50");
  });

  it("adds and extracts standard GST with cent rounding", () => {
    expect(gstBreakdown(10000, "add")).toEqual({
      exclusiveCents: 10000, gstCents: 1000, inclusiveCents: 11000,
    });
    expect(gstBreakdown(11000, "remove")).toEqual({
      exclusiveCents: 10000, gstCents: 1000, inclusiveCents: 11000,
    });
    expect(gstBreakdown(5, "add").gstCents).toBe(1);
    expect(gstBreakdown(4, "add").gstCents).toBe(0);
    expect(gstBreakdown(11, "remove").gstCents).toBe(1);
    expect(() => gstBreakdown(-1, "add")).toThrow();
  });

  it("handles calendar end dates and leap days independent of DST", () => {
    expect(addCalendarDays("2028-02-28", 1)).toBe("2028-02-29");
    expect(addCalendarDays("2028-01-31", 30)).toBe("2028-03-01");
    expect(addCalendarDays("2026-11-01", 14)).toBe("2026-11-15");
    expect(addCalendarDays("2026-02-29", 14)).toBeNull();
    expect(addCalendarDays("not-a-date", 14)).toBeNull();
  });

  it("shares line-level invoice tax arithmetic with Tammy desktop", () => {
    const draft = validateInvoiceDraft({
      number: "INV-001", issueDate: "2026-11-04", dueDate: "2026-11-18",
      customerName: "Test customer", customerAddress: "Melbourne VIC",
      sellerAddress: "Melbourne VIC", gstRegistered: true,
      notes: "", paymentInstructions: "",
      lines: [
        { description: "Services", quantityMilli: 1500, unitPriceMinor: 10000, taxTreatment: "taxable" },
        { description: "GST-free item", quantityMilli: 1000, unitPriceMinor: 5000, taxTreatment: "gst-free" },
      ],
    });
    expect(calculateInvoice(draft)).toMatchObject({
      subtotalMinor: 20000, gstMinor: 1500, totalMinor: 21500,
    });
  });

  it("publishes distinct index and tool page routes", () => {
    render(<ToolsPage />);
    expect(screen.getByRole("heading", { name: /Good tools/ })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Invoice generator/i })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Quote generator/i })).toBeTruthy();
    expect(screen.getByRole("link", { name: /GST calculator/i })).toBeTruthy();
    expect(screen.getByRole("link", { name: /Invoice due-date calculator/i })).toBeTruthy();
    expect(toolsMetadata.alternates).toEqual({ canonical: "/tools" });
  });

  it("renders a functional GST calculator", () => {
    render(<GstCalculatorPage />);
    expect(screen.getByText("$1,100.00")).toBeTruthy();
    fireEvent.click(screen.getByLabelText("Extract GST"));
    const amount = screen.getByLabelText("Amount including GST (AUD)");
    fireEvent.change(amount, { target: { value: "110.00" } });
    expect(screen.getByText("$10.00")).toBeTruthy();
    expect(gstMetadata.alternates).toEqual({ canonical: "/tools/gst-calculator" });
  });

  it("renders a working date calculator", () => {
    render(<DueDateCalculatorPage />);
    fireEvent.change(screen.getByLabelText("Invoice issue date"), { target: { value: "2028-02-28" } });
    fireEvent.change(screen.getByLabelText("Payment terms (calendar days)"), { target: { value: "30" } });
    expect(screen.getByText("2028-03-29")).toBeTruthy();
    expect(dueMetadata.alternates).toEqual({ canonical: "/tools/invoice-due-date-calculator" });
  });

  it("keeps invoice generation explicit and browser-only", () => {
    render(<InvoiceGeneratorPage />);
    expect(screen.getByRole("button", { name: "Download invoice PDF" })).toBeTruthy();
    expect(screen.getByText(/No login, no upload, no saved data/)).toBeTruthy();
    expect(screen.getByText(/Preview only. A PDF is generated after validation/)).toBeTruthy();
    expect(invoiceMetadata.alternates).toEqual({ canonical: "/tools/invoice-generator" });
    expect(screen.getByLabelText("My business is registered for GST")).toBeTruthy();
    expect(screen.getByRole("button", { name: /Add line item/ })).toBeTruthy();
  });

  it("indexes every public tool and references the canonical host", () => {
    const sitemap = readFileSync("public/sitemap.xml", "utf8");
    for (const route of [
      "/tools", "/tools/invoice-generator", "/tools/gst-calculator",
      "/tools/invoice-due-date-calculator",
    ]) expect(sitemap).toContain("https://tammy-accounting.castlemilk.chatgpt.site" + route + "</loc>");
    expect(readFileSync("public/robots.txt", "utf8")).toContain("/sitemap.xml");
    expect(readFileSync("public/llms.txt", "utf8")).toContain("## Free tools");
  });
});
