import { vi } from "vitest";
import { afterEach, describe, expect, it } from "vitest";
import { render as renderReact, screen as screenReact, fireEvent as fireReact, cleanup as cleanupReact } from "@testing-library/react";
import { compileQuote, type QuoteInput } from "../lib/tools/quote";
import QuotePage, { metadata } from "../app/tools/quote-generator/page";

const fixture: QuoteInput = {
  sellerName: "Sample Business", sellerAbn: "51 824 753 556",
  sellerAddress: "Melbourne VIC", customerName: "Example Customer",
  customerAddress: "Victoria", reference: "QUO-001",
  quoteDate: "2026-11-05", validUntil: "2026-12-05",
  gstRegistered: true, notes: "Illustrative prices",
  lines: [
    { description: "Consulting", quantity: "1.5", unitPrice: "120.00", treatment: "taxable" },
    { description: "GST-free item", quantity: "1", unitPrice: "50.00", treatment: "gst-free" },
  ],
};
afterEach(() => cleanupReact());

describe("Tammy anonymous quote generator", () => {
  it("reuses validated exact-cent invoice totals without issuing", () => {
    const result = compileQuote(fixture);
    expect(result.totals).toMatchObject({ subtotalMinor: 23000, gstMinor: 1800, totalMinor: 24800 });
    expect(result.reference).toBe("QUO-001");
    expect(result.draft.number).toBe("QUO-001");
    expect(result.sellerAbn).toBe("51824753556");
  });

  it("never charges GST when the seller is not registered", () => {
    const result = compileQuote({ ...fixture, gstRegistered: false });
    expect(result.totals).toMatchObject({ subtotalMinor: 23000, gstMinor: 0, totalMinor: 23000 });
    expect(result.draft.lines.every((line) => line.taxTreatment === "out-of-scope")).toBe(true);
  });

  it("rejects bad dates, ABNs, malformed prices and missing line descriptions", () => {
    expect(() => compileQuote({ ...fixture, validUntil: "2026-10-01" })).toThrow();
    expect(() => compileQuote({ ...fixture, sellerAbn: "12345678901" })).toThrow();
    expect(() => compileQuote({ ...fixture, quoteDate: "" })).toThrow();
    expect(() => compileQuote({ ...fixture, lines: [{ ...fixture.lines[0]!, unitPrice: "1e4" }] })).toThrow();
    expect(() => compileQuote({ ...fixture, lines: [{ ...fixture.lines[0]!, description: "" }] })).toThrow();
  });

  it("shows quote identity and works with browser print only after input validation", () => {
    const prior = window.print;
    const spy = vi.fn();
    window.print = spy;
    try {
      renderReact(<QuotePage />);
      expect(screenReact.getByRole("heading", { level: 1 }).textContent).toContain("quote");
      const btn = screenReact.getByRole("button", { name: "Print or save quote as PDF" });
      fireReact.click(btn);
      expect(spy).not.toHaveBeenCalled();
      expect(screenReact.getAllByText(/Business name/).length).toBeGreaterThan(0);

      fireReact.change(screenReact.getByLabelText("Business name *"), { target: { value: "Sample Business" } });
      fireReact.change(screenReact.getByLabelText("Customer name *"), { target: { value: "Example Customer" } });
      fireReact.change(screenReact.getByLabelText("Quote date *"), { target: { value: "2026-11-05" } });
      fireReact.change(screenReact.getByLabelText("Valid until *"), { target: { value: "2026-12-05" } });
      fireReact.change(screenReact.getByLabelText("Line 1 description *"), { target: { value: "Service" } });
      fireReact.change(screenReact.getByLabelText("Unit price (ex GST, AUD)"), { target: { value: "100.00" } });
      fireReact.click(btn);
      expect(spy).toHaveBeenCalledOnce();
      expect(screenReact.getByText(/Not a tax invoice or payment request/)).toBeTruthy();
      expect(metadata.alternates).toEqual({ canonical: "/tools/quote-generator" });
    } finally {
      window.print = prior;
    }
  });
});
