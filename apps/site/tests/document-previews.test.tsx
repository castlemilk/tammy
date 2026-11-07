import { afterEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { renderToString } from "react-dom/server";
import { InvoiceGenerator } from "../components/invoice-generator";
import { QuoteGenerator } from "../components/quote-generator";

afterEach(cleanup);

describe("document preview and PDF column agreement", () => {
  for (const type of ["Invoice", "Quote"] as const) {
    it(`${type} prevents editing server-rendered controls before the browser is ready`, () => {
      const container = document.createElement("div");
      container.innerHTML = renderToString(type === "Invoice" ? <InvoiceGenerator /> : <QuoteGenerator />);
      const fields = container.querySelector("fieldset.tools-document-fields") as HTMLFieldSetElement;
      expect(fields.disabled).toBe(true);
      expect(fields.getAttribute("aria-busy")).toBe("true");
      expect(fields.querySelector("input")?.matches(":disabled")).toBe(true);
      expect(fields.querySelector("button")?.matches(":disabled")).toBe(true);
      expect(fields.textContent).toContain("Preparing form…");
      expect(fields.querySelector("noscript")?.textContent).toContain("Enable JavaScript");
    });
    it(`${type} shows unit price independently of quantity and keeps totals outside the scroll region`, () => {
      render(type === "Invoice" ? <InvoiceGenerator /> : <QuoteGenerator />);
      expect(screen.getByLabelText("Business name *").matches(":disabled")).toBe(false);
      const fill = (label: string | RegExp, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });
      fill("Business name *", "Synthetic business");
      expect(within(screen.getByRole("complementary", { name: `Live ${type.toLowerCase()} preview` })).getAllByText("Synthetic business").length).toBeGreaterThan(0);
      fill(type === "Invoice" ? "ABN *" : "ABN (optional)", "51824753556");
      fill(type === "Invoice" ? "Business address *" : "Business address", "1 Example Street");
      fill(type === "Invoice" ? "Customer or business name *" : "Customer name *", "Example customer");
      fill(type === "Invoice" ? "Customer address *" : "Customer address", "2 Example Street");
      fill(type === "Invoice" ? /Issue date/ : "Quote date *", "2026-11-07");
      fill(type === "Invoice" ? "Due date *" : "Valid until *", "2026-11-21");
      fill(type === "Invoice" ? "Description" : "Line 1 description *", "Synthetic service");
      fill("Quantity", "2");
      fill("Unit price (ex GST, AUD)", "125.00");
      fireEvent.click(screen.getByLabelText("My business is registered for GST"));

      const region = screen.getByRole("region", { name: `${type} line items` });
      expect(region.tabIndex).toBe(0);
      expect(within(region).getByRole("columnheader", { name: "Unit ex GST" })).toBeTruthy();
      const cells = () => within(region).getAllByRole("cell").map((cell) => cell.textContent);
      expect(cells()).toEqual(["Synthetic service", "2", "$125.00", "$25.00", "$275.00"]);
      expect(within(region).queryByText("Subtotal (ex GST)")).toBeNull();
      fill("Quantity", "1.5");
      expect(cells()).toEqual(["Synthetic service", "1.5", "$125.00", "$18.75", "$206.25"]);
    });
  }
});
