import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { checkAbn, formattedAbn } from "../lib/tools/abn";
import { calculateMargin, formatPercent } from "../lib/tools/margins";
import AbnCheckerPage, { metadata as abnMetadata } from "../app/tools/abn-checker/page";
import ProfitMarginPage, { metadata as marginMetadata } from "../app/tools/profit-margin-calculator/page";
import GuidesPage, { metadata as guidesMetadata } from "../app/guides/page";
import InvoiceGuide, { metadata as invoiceGuideMetadata } from "../app/guides/australian-tax-invoice-checklist/page";
import GstGuide, { metadata as gstGuideMetadata } from "../app/guides/gst-inclusive-vs-exclusive/page";

afterEach(cleanup);

describe("ABN checksum and business margin calculations", () => {
  it("validates ABN checksum without implying registration", () => {
    expect(checkAbn("")).toMatchObject({ status: "empty" });
    expect(checkAbn("51 824 753 556")).toMatchObject({ status: "valid", digits: "51824753556" });
    expect(checkAbn("51824753557").status).toBe("invalid");
    expect(checkAbn("ABN 51824753556").status).toBe("invalid");
    expect(checkAbn("123").status).toBe("invalid");
    expect(formattedAbn("51824753556")).toBe("51 824 753 556");
    expect(checkAbn("51824753556").explanation).toContain("does not establish registration");
  });

  it("distinguishes margin, markup, profit and zero-denominator cases", () => {
    expect(calculateMargin(8000, 12000)).toEqual({
      profitCents: 4000, marginPercent: 33.33, markupPercent: 50,
    });
    expect(calculateMargin(12000, 8000)).toEqual({
      profitCents: -4000, marginPercent: -50, markupPercent: -33.33,
    });
    expect(calculateMargin(0, 10000)).toMatchObject({
      profitCents: 10000, marginPercent: 100, markupPercent: null,
    });
    expect(calculateMargin(10000, 0)).toMatchObject({
      profitCents: -10000, marginPercent: null, markupPercent: -100,
    });
    expect(formatPercent(null)).toBe("Not defined");
    expect(formatPercent(33.33)).toBe("33.33%");
    expect(() => calculateMargin(-1, 100)).toThrow();
  });
});

describe("search entry routes for Tammy", () => {
  it("renders an interactive ABN checker with a truthful boundary", () => {
    render(<AbnCheckerPage />);
    const field = screen.getByLabelText("Australian business number (ABN)");
    fireEvent.change(field, { target: { value: "51 824 753 556" } });
    expect(screen.getByText("Valid format")).toBeTruthy();
    expect(screen.getByText("51 824 753 556")).toBeTruthy();
    expect(screen.getByRole("link", { name: /ABN Lookup/i })).toBeTruthy();
    expect(abnMetadata.alternates).toEqual({ canonical: "/tools/abn-checker" });
  });

  it("renders a functional margin/markup comparison", () => {
    render(<ProfitMarginPage />);
    expect(screen.getByText("$40.00")).toBeTruthy();
    expect(screen.getAllByText("33.33%").length).toBeGreaterThan(0);
    expect(screen.getAllByText("50.00%").length).toBeGreaterThan(0);
    const input = screen.getByLabelText("Selling price, excluding GST (AUD)");
    fireEvent.change(input, { target: { value: "160.00" } });
    expect(screen.getByText("$80.00")).toBeTruthy();
    expect(marginMetadata.alternates).toEqual({ canonical: "/tools/profit-margin-calculator" });
  });

  it("renders guide hub and source-linked articles", () => {
    render(<GuidesPage />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("Useful notes");
    expect(screen.getByRole("link", { name: /Australian tax invoice checklist/i })).toBeTruthy();
    expect(guidesMetadata.alternates).toEqual({ canonical: "/guides" });
    cleanup();

    render(<InvoiceGuide />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("tax invoice");
    expect(screen.getByText("$1,000, including GST")).toBeTruthy();
    expect(screen.getByRole("link", { name: /ATO GSTR 2013\/1/i })).toBeTruthy();
    expect(invoiceGuideMetadata.alternates).toEqual({ canonical: "/guides/australian-tax-invoice-checklist" });
    cleanup();

    render(<GstGuide />);
    expect(screen.getByRole("heading", { level: 1 }).textContent).toContain("Inclusive, exclusive");
    expect(screen.getByText(/one-eleventh/)).toBeTruthy();
    expect(gstGuideMetadata.alternates).toEqual({ canonical: "/guides/gst-inclusive-vs-exclusive" });
  });
});
