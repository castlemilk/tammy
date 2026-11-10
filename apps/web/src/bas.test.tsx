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
  limitations: ["Sales GST and net GST payable are not calculated by this workpaper."],
  sources: [],
  candidates: [],
};

function serve(workpaper: BasWorkpaper) {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () =>
      Response.json({ workpaper }, { headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

it("shows reviewed documents as provisional candidates outside GST credits (1B)", async () => {
  serve({
    ...base,
    candidateGstMinor: "9007199254740993",
    candidates: [
      {
        id: "document-1",
        date: "2024-05-12",
        supplierName: "Officeworks",
        invoiceNumber: "INV-1",
        candidateGstMinor: "9007199254740993",
      },
    ],
  });
  render(<BasScreen />);

  await screen.findByRole("heading", { name: "Provisional GST candidates" });
  expect(screen.queryByRole("heading", { name: "Reviewed source documents" })).toBeNull();
  const credits = screen.getByText("GST credits (1B)").parentElement;
  expect(credits?.textContent).toContain("$0.00");
  const row = screen.getByRole("row", { name: /Officeworks/ });
  expect(within(row).getByText("$90,071,992,547,409.93")).toBeTruthy();
  expect(screen.getByRole("row", { name: /Total provisional candidates/ }).textContent).toContain(
    "$90,071,992,547,409.93",
  );
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
