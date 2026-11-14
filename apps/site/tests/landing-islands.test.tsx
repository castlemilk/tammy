import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import Home from "../app/page";
import { Bas } from "../components/landing/bas";
import { BenchAbn } from "../components/landing/bench-abn";
import { BenchBreakEven } from "../components/landing/bench-break-even";
import { BenchMargin } from "../components/landing/bench-margin";
import { GstStripInstrument } from "../components/landing/gst-strip-instrument";
import { Integrations } from "../components/landing/integrations";
import { Invoicing } from "../components/landing/invoicing";
import { gstWorkpaper, invoice } from "../components/landing/scenario";
import { aud, australianDate } from "../lib/tools/calculations";
import { publicTools } from "../lib/tool-library";

afterEach(cleanup);

const figures = (container: HTMLElement) => [...container.querySelectorAll(".lp-gst-fig")].map((node) => node.textContent);

describe("GST strip instrument", () => {
  it("starts on the worked example and answers typed amounts in both modes", () => {
    const { container } = render(<GstStripInstrument />);
    const amount = screen.getByLabelText("Price including GST") as HTMLInputElement;
    expect(amount.value).toBe("110.00");
    expect(amount.readOnly).toBe(false);
    expect(figures(container)).toEqual(["$100.00", "$10.00", "$110.00"]);

    // Unedited: switching mode swaps in the other side of the same sale.
    fireEvent.click(screen.getByRole("radio", { name: "Add GST" }));
    expect((screen.getByLabelText("Price excluding GST") as HTMLInputElement).value).toBe("100.00");
    expect(figures(container)).toEqual(["$100.00", "$10.00", "$110.00"]);
    fireEvent.click(screen.getByRole("radio", { name: "Extract GST" }));

    fireEvent.change(screen.getByLabelText("Price including GST"), { target: { value: "1234.56" } });
    expect(figures(container)).toEqual(["$1,122.33", "$112.23", "$1,234.56"]);
    expect(container.querySelector(".lp-gst-sum")?.textContent).toBe("1,234.56 ÷ 11 = 112.23, rounded to the cent");
    expect(container.querySelector(".lp-gst-aside")?.textContent).toContain("10% of $1,234.56 is $123.46");

    // Edited: switching mode keeps what the visitor typed.
    fireEvent.click(screen.getByRole("radio", { name: "Add GST" }));
    expect((screen.getByLabelText("Price excluding GST") as HTMLInputElement).value).toBe("1234.56");
    expect(figures(container)).toEqual(["$1,234.56", "$123.46", "$1,358.02"]);
  });

  it("refuses an amount it cannot parse without inventing figures", () => {
    const { container } = render(<GstStripInstrument />);
    const amount = screen.getByLabelText("Price including GST");
    fireEvent.change(amount, { target: { value: "abc" } });
    expect(figures(container)).toEqual(["–", "–", "–"]);
    expect(amount.getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByText("Enter an amount like 110.00")).toBeTruthy();
    for (const ok of ["$110", "110.", "1,100.00"]) {
      fireEvent.change(amount, { target: { value: ok } });
      expect(amount.getAttribute("aria-invalid")).toBe("false");
    }
    fireEvent.change(amount, { target: { value: "99999999999" } });
    expect(amount.getAttribute("aria-invalid")).toBe("true");
  });
});

describe("bench instruments", () => {
  it("margin: answers, and refuses a bad amount", () => {
    const { container } = render(<BenchMargin />);
    const answers = () => [...container.querySelectorAll(".lp-bench-answers dd")].map((node) => node.textContent);
    expect(answers()).toEqual(["$40.00", "33.33%", "50.00%"]);
    fireEvent.change(screen.getByLabelText("Cost, ex GST"), { target: { value: "150" } });
    // Negative figures are set with a true minus sign.
    expect(answers()).toEqual(["\u2212$30.00", "\u221225.00%", "\u221220.00%"]);
    fireEvent.change(screen.getByLabelText("Cost, ex GST"), { target: { value: "x" } });
    expect(answers()).toEqual(["–", "–", "–"]);
    expect(screen.getByText("Enter dollars and cents, like 80.00.")).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Cost, ex GST, slider"), { target: { value: "6000" } });
    expect((screen.getByLabelText("Cost, ex GST") as HTMLInputElement).value).toBe("60.00");
    expect(answers()).toEqual(["$60.00", "50.00%", "100.00%"]);
  });

  it("ABN: passes, fails, counts digits and rejects letters", () => {
    render(<BenchAbn />);
    const input = screen.getByLabelText(/^ABN/);
    expect(screen.getByText("Checksum passes")).toBeTruthy();
    expect(screen.getByText("51 824 753 556")).toBeTruthy();
    fireEvent.change(input, { target: { value: "51 824 753 557" } });
    expect(screen.getByText("Checksum fails")).toBeTruthy();
    fireEvent.change(input, { target: { value: "51 8" } });
    expect(screen.getByText("3 of 11 digits")).toBeTruthy();
    fireEvent.change(input, { target: { value: "abc" } });
    expect(screen.getByText("Not checked")).toBeTruthy();
    expect(screen.getByText("An ABN has 11 digits. Use digits and spaces only.")).toBeTruthy();
    fireEvent.change(input, { target: { value: "518247535561" } });
    expect(screen.getByText("An ABN has 11 digits. This has 12.")).toBeTruthy();
  });

  it("break-even: 75 units, a $10 rise makes it 100, and no break-even is said in words", () => {
    const { container } = render(<BenchBreakEven />);
    const big = () => container.querySelector(".lp-bench-big")?.textContent;
    expect(big()).toBe("75 units");
    fireEvent.click(screen.getByRole("button", { name: "Raise the variable cost by $10" }));
    expect(big()).toBe("100 units");
    expect(container.querySelector(".lp-bench-even-delta")?.textContent).toBe("25 more than at $80.00, up 33.33%");
    expect(container.querySelector(".lp-bench-working")?.textContent).toBe("3,000.00 ÷ 30.00 = 100 units");
    fireEvent.change(screen.getByLabelText("Price per unit"), { target: { value: "80" } });
    expect(big()).toBe("No break-even");
    fireEvent.change(screen.getByLabelText("Fixed costs"), { target: { value: "" } });
    expect(big()).toBe("–");
    expect(screen.getByText("Enter dollars and cents, like 80.00.")).toBeTruthy();
  });
});

describe("invoicing figure", () => {
  it("walks draft, confirm, issued, paid and back, replacing figures whole", () => {
    const { container } = render(<Invoicing />);
    const fig = container.querySelector(".lp-invoicing-fig") as HTMLElement;
    const figureOf = (label: string) => within(fig).getByText(label).closest("div")?.querySelector("dd")?.textContent;
    expect(fig.dataset.state).toBe("paid");
    expect(figureOf("Outstanding")).toBe(aud(invoice.outstandingCents));

    fireEvent.click(within(fig).getByRole("button", { name: "Start again" }));
    expect(fig.dataset.state).toBe("draft");
    expect(within(fig).getByText("Draft")).toBeTruthy();
    expect(within(fig).getByText("Nothing posted yet.")).toBeTruthy();
    expect(within(fig).queryAllByRole("button", { name: /posted to/ }).length).toBe(1);

    fireEvent.click(within(fig).getByRole("button", { name: "Issue invoice" }));
    expect(fig.dataset.state).toBe("confirming");
    fireEvent.click(within(fig).getByRole("button", { name: "Keep as draft" }));
    expect(fig.dataset.state).toBe("draft");
    fireEvent.click(within(fig).getByRole("button", { name: "Issue invoice" }));
    fireEvent.click(within(fig).getByRole("button", { name: "Confirm issue" }));
    expect(fig.dataset.state).toBe("issued");
    expect(within(fig).getByText("Issued")).toBeTruthy();
    expect(figureOf("Received")).toBe("$0.00");
    expect(figureOf("Outstanding")).toBe(aud(invoice.totalCents));

    fireEvent.click(within(fig).getByRole("button", { name: "Record payment" }));
    expect(fig.dataset.state).toBe("paid");
    expect(figureOf("Received")).toBe(aud(invoice.receivedCents));
    expect(figureOf("Outstanding")).toBe(aud(invoice.outstandingCents));
    expect(fig.querySelector("output")?.textContent).toContain("Income is unchanged");
  });
});

describe("local API gate", () => {
  it("is staged at rest; only a person saves; revoking keeps a saved draft and discards a staged one", () => {
    const { container } = render(<Integrations />);
    const scene = container.querySelector(".lp-integrations-scene") as HTMLElement;
    const s = within(scene);
    expect(scene.dataset.state).toBe("staged");
    expect(s.getByText("Not saved")).toBeTruthy();

    fireEvent.click(s.getByRole("button", { name: "Revoke access" }));
    expect(scene.dataset.state).toBe("off");
    expect(scene.dataset.off).toBe("discarded");
    expect(s.queryByRole("button", { name: "Save draft" })?.hasAttribute("disabled")).toBe(true);

    // jsdom has no matchMedia, so enabling goes straight to the staged state.
    fireEvent.click(s.getByRole("button", { name: "Enable access" }));
    expect(scene.dataset.state).toBe("staged");
    fireEvent.click(s.getByRole("button", { name: "Save draft" }));
    expect(scene.dataset.state).toBe("saved");
    expect(s.getByText("Saved draft")).toBeTruthy();
    // Once saved the button keeps its place, disabled, so nothing in the footer moves.
    expect((s.getByRole("button", { name: "Save draft" }) as HTMLButtonElement).disabled).toBe(true);

    fireEvent.click(s.getByRole("button", { name: "Revoke access" }));
    expect(scene.dataset.off).toBe("kept");
    expect(s.getByText("Access is off again.")).toBeTruthy();
    fireEvent.click(s.getByRole("button", { name: "Replay" }));
    expect(scene.dataset.off).toBe("fresh");
    expect(s.getByRole("button", { name: "Enable access" })).toBeTruthy();
  });
});

describe("GST workpaper", () => {
  it("prints both bases from the scenario and announces a switch", () => {
    const { container } = render(<Bas />);
    for (const [basis, figuresFor] of [["noncash", gstWorkpaper.nonCash], ["cash", gstWorkpaper.cash]] as const) {
      const shown = [...container.querySelectorAll(`.lp-bas-figures[data-basis="${basis}"] .lp-trace`)].map((node) => node.textContent);
      expect(shown).toEqual([aud(figuresFor.g1Cents), aud(figuresFor.a1Cents), aud(figuresFor.b1Cents), aud(figuresFor.netCents)]);
    }
    expect((container.querySelector("#bas-basis-noncash") as HTMLInputElement).checked).toBe(true);
    fireEvent.click(container.querySelector("#bas-basis-cash") as HTMLInputElement);
    const live = container.querySelector(".lp-bas [aria-live]")?.textContent ?? "";
    expect(live).toContain("Cash basis");
    expect(live).toContain(`Net GST payable ${aud(gstWorkpaper.cash.netCents)}`);
    expect((container.querySelector(".lp-bas") as HTMLElement).dataset.touched).toBe("");
  });
});

describe("landing page structure", () => {
  it("has one rail of nine screens with one checked, nine panels and ten notes", () => {
    const { container } = render(<Home />);
    const radios = [...container.querySelectorAll<HTMLInputElement>('input[name="lp-hero-screen"]')];
    expect(radios.map((radio) => radio.value)).toEqual(["overview", "documents", "invoices", "banking", "chart", "journals", "trial-balance", "gst", "audit"]);
    expect(radios.filter((radio) => radio.checked).map((radio) => radio.value)).toEqual(["trial-balance"]);
    const panels = [...container.querySelectorAll(".lp-hero-panel")].map((panel) => panel.getAttribute("data-panel") ?? "");
    expect(new Set(panels)).toEqual(new Set(radios.map((radio) => radio.value)));
    expect(panels.length).toBe(9);
    expect(container.querySelectorAll(".lp-hero-note").length).toBe(10);
  });

  it("gives every one of the twelve tools a worked answer computed by its own function", () => {
    render(<Home />);
    const tray = within(screen.getByRole("list", { name: "All twelve, each with a worked answer" }));
    const cells = tray.getAllByRole("listitem");
    expect(cells.length).toBe(publicTools.length);
    expect(cells.length).toBe(12);
    for (const cell of cells) expect(cell.querySelector(".lp-tools-answer")?.textContent?.trim().length).toBeGreaterThan(0);
    const answerFor = (href: string) => cells.find((cell) => cell.querySelector(`a[href="${href}"]`))?.querySelector(".lp-tools-answer")?.textContent ?? "";
    expect(answerFor("/tools/gst-calculator")).toContain("$110.00 includes $10.00 GST");
    expect(answerFor("/tools/abn-checker")).toContain("51 824 753 556 passes the checksum");
    expect(answerFor("/tools/invoice-due-date-calculator")).toContain(australianDate("2026-11-17"));
    expect(answerFor("/tools/break-even-calculator")).toContain("75 units cover $3,000.00");
    expect(answerFor("/tools/profit-margin-calculator")).toContain("$40.00 profit");
    expect(answerFor("/tools/bas-workpaper")).toMatch(/G1 \$22,000.*1A \$2,000.*1B \$650/);
    expect(answerFor("/tools/invoice-generator")).toContain("$198.00");
    expect(answerFor("/tools/quote-generator")).toContain("$280.50");
  });

  it("keeps ids unique, ARIA references resolvable and nothing hidden in the server markup", () => {
    const { container } = render(<Home />);
    const ids = [...container.querySelectorAll("[id]")].map((node) => node.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const attribute of ["aria-labelledby", "aria-describedby", "aria-controls", "for", "form"]) {
      for (const node of container.querySelectorAll(`[${attribute}]`)) {
        for (const id of (node.getAttribute(attribute) ?? "").split(/\s+/)) expect(ids, `${attribute}="${id}"`).toContain(id);
      }
    }
    expect(container.querySelector("[hidden]")).toBeNull();
    expect(container.querySelector('[data-scene]:not([data-scene=""]), [data-play="armed"], [data-phase="armed"]')).toBeNull();
    const blocks = [...container.querySelectorAll('script[type="application/ld+json"]')].map((node) => JSON.parse(node.textContent ?? "{}"));
    expect(blocks.map((block) => block["@type"])).toEqual(["SoftwareApplication", "FAQPage"]);
    const visible = [...container.querySelectorAll(".lp-answers-item")].map((item) => ({
      name: item.querySelector("summary")?.textContent,
      text: item.querySelector(".lp-answers-a")?.textContent,
    }));
    expect(blocks[1].mainEntity.map((entry: { name: string; acceptedAnswer: { text: string } }) => ({ name: entry.name, text: entry.acceptedAnswer.text }))).toEqual(visible);
  });

  it("stays inside its markup budget", () => {
    const { container } = render(<Home />);
    // 3,470 elements when this was written. The hero's nine panels and the five
    // workflow windows are most of it; raise the budget deliberately, not by drift.
    expect(container.querySelectorAll("*").length).toBeLessThan(3800);
  });
});
