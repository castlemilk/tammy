import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { useState } from "react";
import { afterEach, describe, expect, it } from "vitest";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Select } from "../components/ui/select";
import { Label } from "../components/ui/label";
import { Card } from "../components/ui/card";
import { Checkbox } from "../components/ui/checkbox";
import { SegmentedControl } from "../components/gamma/segmented-control";
import BasWorkpaperPage from "../app/tools/bas-workpaper/page";
import TaxChecklistPage from "../app/tools/sole-trader-tax-checklist/page";
import QuotePage from "../app/tools/quote-generator/page";
import { GstCalculator } from "../components/gst-calculator";

afterEach(cleanup);

describe("Tammy shadcn / Gamma / Tailwind contract", () => {
  it("renders owned shadcn-style controls with native semantics", () => {
    const { container } = render(<Card>
      <Label htmlFor="example-input">Amount in AUD</Label>
      <Input id="example-input" value="120.00" readOnly />
      <Label htmlFor="example-choice">Example period</Label>
      <Select id="example-choice" defaultValue="2026">
        <option value="2026">2026</option>
        <option value="2027">2027</option>
      </Select>
      <Label htmlFor="example-check"><Checkbox id="example-check" />Reviewed</Label>
      <Button onClick={() => {}}>Continue</Button>
    </Card>);
    expect(screen.getByLabelText("Amount in AUD").getAttribute("data-slot")).toBe("input");
    expect(screen.getByLabelText("Example period").getAttribute("data-slot")).toBe("select");
    expect(screen.getByRole("checkbox", { name: "Reviewed" }).getAttribute("data-slot")).toBe("checkbox");
    expect(screen.getByRole("button", { name: "Continue" }).getAttribute("type")).toBe("button");
    expect(container.querySelector('[data-slot="card"]')).toBeTruthy();
  });

  it("retains Gamma's controlled native radio semantics", () => {
    function Example() {
      const [value, setValue] = useState("1");
      return <SegmentedControl label="Quarter" options={[
        { value: "1", label: "Q1" },
        { value: "2", label: "Q2" },
      ]} value={value} onChange={setValue} block />;
    }
    render(<Example />);
    expect(screen.getByRole("radiogroup", { name: "Quarter" })).toBeTruthy();
    const q1 = screen.getByRole("radio", { name: "Q1" }) as HTMLInputElement;
    const q2 = screen.getByRole("radio", { name: "Q2" }) as HTMLInputElement;
    expect(q1.checked).toBe(true);
    fireEvent.click(q2);
    expect(q2.checked).toBe(true);
    expect(q1.checked).toBe(false);
  });

  it("uses Gamma quarter choice and shadcn inputs without changing BAS arithmetic", () => {
    const { container } = render(<BasWorkpaperPage />);
    expect(container.querySelectorAll('[data-slot="input"]')).toHaveLength(3);
    expect(container.querySelector('[data-slot="select"]')).toBeTruthy();
    expect(container.querySelectorAll('[data-slot="card"]')).toHaveLength(2);
    fireEvent.click(screen.getByRole("radio", { name: "Q2" }));
    expect(container.querySelector(".tools-calculator-result")?.textContent).toContain("2027-02-28");
    expect(container.querySelector(".tools-calculator-result")?.textContent).toContain("$1,350");
  });

  it("uses the same controls in local tax checklist and printable quote", () => {
    const c = render(<TaxChecklistPage />);
    expect(c.container.querySelectorAll('[data-slot="checkbox"]')).toHaveLength(8);
    expect(c.container.querySelector('[data-slot="select"]')).toBeTruthy();
    fireEvent.click(screen.getByRole("checkbox", { name: /Confirm the income year/i }));
    expect(screen.getByText(/1 of 8 checks reviewed/)).toBeTruthy();
    cleanup();
    const quote = render(<QuotePage />);
    expect(quote.container.querySelectorAll('[data-slot="input"]').length).toBeGreaterThanOrEqual(5);
    expect(quote.container.querySelector('[data-slot="button"]')).toBeTruthy();
  });

  it("connects GST invalid input to its error and recovers without losing mode", () => {
    const { container } = render(<GstCalculator />);
    fireEvent.click(screen.getByRole("radio", { name: "Extract GST" }));
    const amount = screen.getByLabelText("Amount including GST (AUD)");
    expect(amount.getAttribute("data-slot")).toBe("input");
    fireEvent.change(amount, { target: { value: "110.001" } });
    expect(amount.getAttribute("aria-invalid")).toBe("true");
    expect(amount.getAttribute("aria-describedby")).toContain(screen.getByRole("alert").id);
    expect(container.querySelector(".tools-result-total dd")?.textContent).toBe("–");
    fireEvent.change(amount, { target: { value: "110.00" } });
    expect(amount.getAttribute("aria-invalid")).toBe("false");
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByText("$10.00")).toBeTruthy();
    expect((screen.getByRole("radio", { name: "Extract GST" }) as HTMLInputElement).checked).toBe(true);
  });

  it("keeps one Soft Precision token bridge and scoped, reduced-motion Gamma styling", () => {
    const globals = readFileSync("app/globals.css", "utf8");
    const bridge = readFileSync("app/design-system.css", "utf8");
    const gamma = readFileSync("components/gamma/segmented-control.css", "utf8");
    const layout = readFileSync("app/tools/layout.tsx", "utf8");
    expect(globals).toContain('@import "tailwindcss"');
    expect(globals).toContain('@import "shadcn/tailwind.css"');
    expect(globals).toContain('@import "./design-system.css"');
    expect(bridge).toContain("--gamma-accent: var(--t-forest)");
    expect(bridge).toContain("--gamma-border: var(--t-line)");
    expect(gamma).toContain(".segmented-control-input:focus-visible");
    expect(gamma).toContain("@media (prefers-reduced-motion: reduce)");
    expect(layout).toContain("gamma/segmented-control.css");
  });
});
