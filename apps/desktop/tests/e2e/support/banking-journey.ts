import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { create } from "@bufbuild/protobuf";
import { expect, type Page } from "@playwright/test";
import {
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  BankStatementLineStatus,
  GetBankingSummaryRequestSchema,
  GetBankingSummaryResponseSchema,
  ListBankStatementLinesRequestSchema,
  ListBankStatementLinesResponseSchema,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
import { PageRequestSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { createProtoMethodCodec } from "../../../src/shared/proto-ipc";
import { invokeConveyorFrame } from "./conveyor";
import type { AuthenticatedAccountingWorkspace } from "./current-accounting-workflow";

export const bankingCapturePhase = process.env.TAMMY_BANKING_CAPTURE;
if (
  bankingCapturePhase !== undefined &&
  bankingCapturePhase !== "before" &&
  bankingCapturePhase !== "after"
) {
  throw new Error("BANKING_CAPTURE_PHASE_INVALID");
}
const linesCodec = createProtoMethodCodec({
  input: ListBankStatementLinesRequestSchema,
  output: ListBankStatementLinesResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 262144,
});
const summaryCodec = createProtoMethodCodec({
  input: GetBankingSummaryRequestSchema,
  output: GetBankingSummaryResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 16384,
});
const journalsCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  output: ListJournalsResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 1048576,
});
const statementRows = [
  "2024-05-01,Synthetic Cedar Studio invoice SYN-2001,780.00",
  "2024-05-02,Synthetic Paper House supplies,-200.00",
  "2024-05-03,Synthetic Grove software subscription,-31.90",
  "2024-05-04,Synthetic Bower Studio invoice SYN-2002,240.00",
  "2024-05-05,Synthetic Cedar Supplies invoice SYN-2003,-110.00",
];

export async function runBankingReviewJourney(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
) {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("link", { name: "Banking", exact: true }).click();
  const importButton = page.getByRole("button", { name: "Import statement", exact: true });
  await expect(importButton).toBeEnabled();
  const original = await readBanking(page, workspace);
  const originalJournals = await journalIds(page, workspace);
  const originalIds = original.lines.map((line) => line.id);
  await importButton.focus();
  await page.keyboard.press("Space");
  const opening = page.getByLabel("Opening balance · AUD", { exact: true });
  const rows = page.getByRole("textbox", { name: "CSV rows", exact: true });
  await expect(opening).toBeFocused();
  await opening.fill("1000.00");
  await rows.fill("Synthetic malformed row with no three-column CSV structure");
  const form = page.getByRole("form", { name: "Statement import", exact: true });
  await form.getByRole("button", { name: "Import statement", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("The statement was not confirmed");
  if (bankingCapturePhase !== "before") {
    await expect(page.getByRole("region", { name: "Banking error", exact: true })).toBeFocused();
  }
  await expect(rows).toHaveValue("Synthetic malformed row with no three-column CSV structure");
  expect((await readBanking(page, workspace)).lines.map((line) => line.id)).toEqual(originalIds);
  await capture(page, "01-import-error", "import");
  await page.getByRole("button", { name: "Cancel import", exact: true }).click();
  await expect(importButton).toBeFocused();
  await page.keyboard.press("Space");
  await expect(opening).toHaveValue("");
  await expect(rows).toHaveValue("");
  await opening.fill("1000.00");
  await rows.fill(statementRows.join("\n"));
  await capture(page, "02-import-review", "import");
  await form.getByRole("button", { name: "Import statement", exact: true }).dblclick();
  await expect(page.getByRole("status").filter({ hasText: "Statement imported" })).toContainText(
    "5 lines retained",
  );
  if (bankingCapturePhase !== "before") {
    await expect(
      page.getByRole("region", { name: "Banking confirmation", exact: true }),
    ).toBeFocused();
  }
  let retained = await readBanking(page, workspace);
  expect(retained.summary.importedLineCount).toBe(original.summary.importedLineCount + 5);
  expect(retained.summary.latestClosingBalance?.minorUnits).toBe(167810n);
  expect(retained.lines).toHaveLength(original.lines.length + 5);
  await capture(page, "03-statement-list");
  const synthetic = retained.lines.filter((line) => !originalIds.includes(line.id));
  expect(synthetic).toHaveLength(5);
  const first = synthetic.find((line) => line.description.includes("Cedar Studio"));
  if (!first) throw new Error("SYNTHETIC_BANK_LINE_MISSING");
  const table = page.getByRole("table", { name: "Statement lines", exact: true });
  const firstRow = table.getByRole("row").filter({ hasText: first.description });
  const firstMatch = firstRow.getByRole("button", { name: "Match", exact: true });
  await firstMatch.focus();
  await page.keyboard.press("Space");
  const reference = page.getByLabel("Reviewed source reference", { exact: true });
  await expect(reference).toBeFocused();
  await expect(reference).toHaveCSS("outline-style", "solid");
  if (bankingCapturePhase !== "before") {
    const listBounds = await table.boundingBox();
    const reviewBounds = await page
      .getByRole("form", { name: "Match review", exact: true })
      .boundingBox();
    if (!listBounds || !reviewBounds) throw new Error("BANKING_REVIEW_LAYOUT_MISSING");
    expect(reviewBounds.x).toBeGreaterThanOrEqual(listBounds.x + listBounds.width);
    await expect(
      page.getByRole("region", { name: "Reconciliation summary", exact: true }),
    ).toHaveCSS("background-color", "rgb(231, 237, 226)");
  }
  await reference.fill("Cancelled synthetic reference");
  if (bankingCapturePhase !== "before") {
    await firstMatch.click();
    await expect(reference).toHaveValue("Cancelled synthetic reference");
  }
  await capture(page, "04-selected-match");
  await capture(page, "05-match-detail", "match");
  if (bankingCapturePhase === "before")
    await page.getByRole("button", { name: "Cancel match", exact: true }).click();
  else await page.keyboard.press("Escape");
  await expect(firstMatch).toBeFocused();
  await expect(page.getByRole("form", { name: "Match review", exact: true })).toHaveCount(0);
  expect(
    (await readBanking(page, workspace)).lines.find((line) => line.id === first.id)?.version,
  ).toBe(1n);

  const search = page.getByLabel("Search statement lines", { exact: true });
  await search.fill("Cedar Studio");
  await firstMatch.focus();
  await page.keyboard.press("Space");
  await expect(reference).toHaveValue("");
  await reference.fill("SYN-2001 · reviewed receipt reference");
  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of bankingCapturePhase === "before" ? [390, 320] : [900, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
        ),
      )
      .toBe(true);
    if (bankingCapturePhase !== "before") {
      const scroll = page.getByRole("region", { name: "Scrollable statement lines", exact: true });
      await scroll.evaluate((node) => {
        node.scrollLeft = 0;
      });
      await scroll.focus();
      await expect(scroll).toBeFocused();
      await expect(scroll).toHaveCSS("outline-style", "solid");
      const overflows = await scroll.evaluate((node) => node.scrollWidth > node.clientWidth);
      if (width <= 390) expect(overflows).toBe(true);
      await page.keyboard.press("ArrowRight");
      if (overflows)
        await expect.poll(() => scroll.evaluate((node) => node.scrollLeft)).toBeGreaterThan(0);
      else await expect.poll(() => scroll.evaluate((node) => node.scrollLeft)).toBe(0);
    }
    await capture(page, `06-statement-${width}`);
    await reference.focus();
    await expect(reference).toHaveCSS("outline-style", "solid");
    await capture(page, `07-match-focus-${width}`, "focus");
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("button", { name: "Confirm match", exact: true }).dblclick();
  await expect(page.getByRole("status").filter({ hasText: "Match saved" })).toContainText(
    "Match saved",
  );
  if (bankingCapturePhase !== "before") {
    await expect(
      page.getByRole("region", { name: "Banking confirmation", exact: true }),
    ).toBeFocused();
    const statuses = page.getByRole("group", { name: "Statement status", exact: true });
    await statuses.getByRole("button", { name: /^Unmatched/ }).click();
    await expect(page.getByText("No matching statement lines", { exact: true })).toBeVisible();
    await statuses.getByRole("button", { name: /^Matched/ }).click();
    await expect(firstRow.getByText("Matched", { exact: true })).toBeVisible();
    await statuses.getByRole("button", { name: /^All lines/ }).click();
  }
  await expect(search).toHaveValue("Cedar Studio");
  retained = await readBanking(page, workspace);
  expect(retained.lines.find((line) => line.id === first.id)).toMatchObject({
    version: 2n,
    status: BankStatementLineStatus.MATCHED,
    matchReference: "SYN-2001 · reviewed receipt reference",
  });
  await capture(page, "08-match-confirmed");
  await search.fill("No synthetic statement line matches this");
  await expect(page.getByText("No matching statement lines", { exact: true })).toBeVisible();
  await capture(page, "09-search-empty");
  await page.getByRole("button", { name: "Clear search", exact: true }).click();
  await expect(search).toBeFocused();
  for (const line of synthetic.filter((line) => line.id !== first.id)) {
    const row = table.getByRole("row").filter({ hasText: line.description });
    await row.getByRole("button", { name: "Match", exact: true }).click();
    await reference.fill(`SYNTHETIC-REVIEW ${line.description}`);
    await page.getByRole("button", { name: "Confirm match", exact: true }).click();
    await expect(row.getByText("Matched", { exact: true })).toBeVisible();
  }
  const complete = page.getByRole("button", { name: "Complete reconciliation", exact: true });
  await expect(complete).toBeEnabled();
  await capture(page, "10-ready-to-reconcile");
  await complete.dblclick();
  await expect(
    page.getByRole("status").filter({ hasText: "Reconciliation complete" }),
  ).toContainText("5 lines reconciled");
  await expect(complete).toBeDisabled();
  await capture(page, "11-reconciled");
  retained = await readBanking(page, workspace);
  expect(retained.summary.unmatchedLineCount).toBe(0);
  expect(retained.summary.unreconciledLineCount).toBe(0);
  expect(retained.summary.latestClosingBalance?.minorUnits).toBe(167810n);
  expect(
    retained.lines
      .filter((line) => !originalIds.includes(line.id))
      .map((line) => [line.version, line.status]),
  ).toEqual(Array.from({ length: 5 }, () => [3n, BankStatementLineStatus.RECONCILED]));
  expect(await journalIds(page, workspace)).toEqual(originalJournals);
  await page.getByRole("link", { name: "Overview", exact: true }).click();
  await page.goBack();
  await expect(table).toBeVisible();
  await page.goForward();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  await page.goBack();
  expect((await readBanking(page, workspace)).summary.unreconciledLineCount).toBe(0);
}

async function readBanking(page: Page, workspace: AuthenticatedAccountingWorkspace) {
  const authentication = { actorUserId: workspace.userId, sessionId: workspace.sessionId };
  const lines = linesCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "banking",
      "listBankStatementLines",
      linesCodec.encodeRequest(
        create(ListBankStatementLinesRequestSchema, {
          authentication,
          organisationId: workspace.organisationId,
          page: create(PageRequestSchema, { pageSize: 200 }),
        }),
      ),
    ),
  );
  const summary = summaryCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "banking",
      "getBankingSummary",
      summaryCodec.encodeRequest(
        create(GetBankingSummaryRequestSchema, {
          authentication,
          organisationId: workspace.organisationId,
        }),
      ),
    ),
  );
  return { lines: lines.lines, summary };
}
async function journalIds(page: Page, workspace: AuthenticatedAccountingWorkspace) {
  const response = journalsCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "accounting",
      "listJournals",
      journalsCodec.encodeRequest(
        create(ListJournalsRequestSchema, {
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
          organisationId: workspace.organisationId,
          page: create(PageRequestSchema, { pageSize: 200 }),
        }),
      ),
    ),
  );
  return response.journals.map((journal) => journal.id).sort();
}
async function capture(
  page: Page,
  name: string,
  anchor: "page" | "import" | "match" | "focus" = "page",
) {
  if (!bankingCapturePhase) return;
  const directory = path.resolve(
    import.meta.dirname,
    "../../../../../output/playwright/soft-precision-banking-2026-10-23",
    bankingCapturePhase,
  );
  await mkdir(directory, { recursive: true });
  if (anchor === "page")
    await page.getByRole("heading", { name: "Banking", exact: true }).evaluate((heading) => {
      for (let node = heading.parentElement; node; node = node.parentElement)
        node.scrollTo({ top: 0, left: 0, behavior: "instant" });
      document.scrollingElement?.scrollTo({ top: 0, left: 0, behavior: "instant" });
    });
  if (anchor === "import" || anchor === "match")
    await page
      .getByRole("form", {
        name: anchor === "import" ? "Statement import" : "Match review",
        exact: true,
      })
      .scrollIntoViewIfNeeded();
  await writeFile(
    path.join(directory, `${name}.png`),
    await page.screenshot({ animations: "disabled" }),
    { flag: "wx" },
  );
  if (name === "04-selected-match") {
    const measurements = await page.evaluate(() => {
      const table = document.querySelector('table[aria-label="Statement lines"]');
      const review = document.querySelector('form[aria-label="Match review"]');
      const workspace = document.querySelector('section[aria-label="Statement workspace"]');
      const rectangle = (element: Element | null) => {
        if (!element) return null;
        const { x, y, width, height } = element.getBoundingClientRect();
        return { x, y, width, height };
      };
      const controls = [
        ...(workspace?.querySelectorAll("button, input") ?? []),
        ...(review?.querySelectorAll("button, input") ?? []),
      ]
        .map((node) => node.getBoundingClientRect())
        .filter((r) => r.width > 0 && r.height > 0);
      const primary = review?.querySelector('button[type="submit"]');
      const summary = workspace?.querySelector('section[aria-label="Reconciliation summary"]');
      return {
        viewport: { width: innerWidth, height: innerHeight },
        table: rectangle(table),
        review: rectangle(review),
        rowHeights: [...(table?.querySelectorAll("tbody tr") ?? [])].map(
          (row) => row.getBoundingClientRect().height,
        ),
        minimumControlHeight: Math.min(...controls.map((control) => control.height)),
        primaryBackground: primary ? getComputedStyle(primary).backgroundColor : null,
        summaryBackground: summary ? getComputedStyle(summary).backgroundColor : null,
        summary: [
          ...document.querySelectorAll('section[aria-label="Reconciliation summary"] dd'),
        ].map((node) => node.textContent),
      };
    });
    await writeFile(
      path.join(directory, "measurements-1440.json"),
      `${JSON.stringify(measurements, null, 2)}\n`,
      { flag: "wx" },
    );
  }
}
