import { render, screen, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { BasScreen } from "./bas";
import type { BasWorkpaper } from "./types";

const base: BasWorkpaper = {
  status: "draft_not_lodged",
  periodStart: "2024-04-01",
  periodEnd: "2024-06-30",
  salesG1Minor: null,
  gstOnSales1aMinor: null,
  gstCredits1bMinor: 0,
  netGstPayableMinor: null,
  candidateGstMinor: null,
  sourcesChanged: false,
  reviewWarnings: [],
  limitations: ["Sales (G1), GST on sales (1A) and net GST are not shown."],
  sources: [],
  candidates: [],
};

function serve(workpaper: BasWorkpaper | null) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({ workpaper }, { headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

it("shows reviewed documents of any date as provisional candidates outside GST credits (1B)", async () => {
  serve({
    ...base,
    candidateGstMinor: "9007199254742343",
    candidates: [
      {
        id: "document-1",
        date: "2024-05-12",
        supplierName: "Officeworks",
        invoiceNumber: "INV-1",
        candidateGstMinor: "9007199254740993",
      },
      {
        id: "document-2",
        date: "2025-09-01",
        supplierName: "Next year",
        invoiceNumber: "NY-1",
        candidateGstMinor: 1300,
      },
      {
        id: "document-3",
        date: null,
        supplierName: "Undated",
        invoiceNumber: "UD-1",
        candidateGstMinor: 50,
      },
    ],
  });
  render(<BasScreen />);

  await screen.findByRole("heading", { name: "Provisional GST candidates" });
  expect(screen.queryByRole("heading", { name: "Reviewed source documents" })).toBeNull();
  expect(
    screen.getByText(
      "Reviewed documents not yet posted as purchases, of any date, kept out of GST credits (1B)",
    ),
  ).toBeTruthy();
  const credits = screen.getByText("GST credits (1B)").parentElement;
  expect(credits?.textContent).toContain("$0.00");
  const row = screen.getByRole("row", { name: /Officeworks/ });
  expect(within(row).getByText("$90,071,992,547,409.93")).toBeTruthy();
  expect(screen.getByRole("row", { name: /Next year/ }).textContent).toContain("01/09/2025");
  expect(
    within(screen.getByRole("row", { name: /Undated/ })).getAllByRole("cell")[0]?.textContent,
  ).toBe("—");
  // The total is the core's figure for every candidate, not a period subtotal.
  expect(
    screen.getByRole("row", { name: /Total provisional candidates, all dates/ }).textContent,
  ).toContain("$90,071,992,547,423.43");
});

it("keeps legacy reviewed-document credit sources in their own table", async () => {
  serve({
    ...base,
    gstCredits1bMinor: 2900,
    sources: [
      {
        id: "document-1",
        date: "2024-05-12",
        supplierName: "Officeworks",
        invoiceNumber: "INV-1",
        grossMinor: 31900,
        gstCreditMinor: 2900,
      },
    ],
  });
  render(<BasScreen />);

  await screen.findByRole("heading", { name: "Reviewed source documents" });
  expect(screen.queryByRole("heading", { name: "Provisional GST candidates" })).toBeNull();
  expect(screen.getByRole("row", { name: /Officeworks/ }).textContent).toContain("$29.00");
});

it("says when records changed after the draft and lists the core's review warnings", async () => {
  serve({
    ...base,
    sourcesChanged: true,
    reviewWarnings: ["3 retained purchase review rows include legacy candidates."],
  });
  render(<BasScreen />);

  expect(
    await screen.findByText(
      "Records changed after this draft was created. Create another draft to include the changes.",
    ),
  ).toBeTruthy();
  expect(screen.getByText("Review before relying on this draft")).toBeTruthy();
  expect(
    screen.getByText("3 retained purchase review rows include legacy candidates."),
  ).toBeTruthy();
});

it("shows no staleness notice for a current draft", async () => {
  serve(base);
  render(<BasScreen />);

  await screen.findByRole("heading", { name: "GST workpaper" });
  expect(screen.queryByText(/Records changed after this draft was created/)).toBeNull();
  expect(screen.queryByText("Review before relying on this draft")).toBeNull();
});

it("explains that candidates are not limited to the draft period", async () => {
  serve(null);
  render(<BasScreen />);

  await screen.findByRole("heading", { name: "Create a local draft" });
  expect(
    screen.getByText(
      "Reviewed documents not yet posted as purchases are listed as provisional GST candidates, whatever their date",
    ),
  ).toBeTruthy();
  expect(screen.queryByText(/in the period/)).toBeNull();
});
