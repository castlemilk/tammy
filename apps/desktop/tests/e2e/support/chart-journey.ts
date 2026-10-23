import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { create } from "@bufbuild/protobuf";
import { expect, type Page } from "@playwright/test";
import {
  AccountDesignation,
  AccountStatus,
  AccountType,
  ListAccountsRequestSchema,
  ListAccountsResponseSchema,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
  NormalBalance,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { PageRequestSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { createProtoMethodCodec } from "../../../src/shared/proto-ipc";
import { invokeConveyorFrame } from "./conveyor";
import type { AuthenticatedAccountingWorkspace } from "./current-accounting-workflow";

export const chartCapturePhase = process.env.TAMMY_CHART_CAPTURE;
if (
  chartCapturePhase !== undefined &&
  chartCapturePhase !== "before" &&
  chartCapturePhase !== "after"
)
  throw new Error("CHART_CAPTURE_PHASE_INVALID");
const listCodec = createProtoMethodCodec({
  input: ListAccountsRequestSchema,
  output: ListAccountsResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 131072,
});
const journalsCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  output: ListJournalsResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 262144,
});
const samples = [
  { code: "1200", name: "Synthetic studio equipment", type: AccountType.ASSET },
  { code: "2200", name: "Synthetic studio loan", type: AccountType.LIABILITY },
  { code: "4000", name: "Synthetic design services", type: AccountType.REVENUE },
  { code: "6200", name: "Synthetic software subscriptions", type: AccountType.EXPENSE },
];

export async function runChartJourney(page: Page, workspace: AuthenticatedAccountingWorkspace) {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("link", { name: "Chart of accounts", exact: true }).click();
  const initial = await accounts(page, workspace);
  const originalJournalIds = await journalIds(page, workspace);
  await capture(page, "01-installed-chart");
  for (const sample of samples) {
    await page.getByRole("button", { name: "Add account", exact: true }).click();
    await fill(page, sample);
    await page.getByRole("button", { name: "Save account", exact: true }).click();
    await expect(page.getByRole("cell", { name: sample.name, exact: true })).toBeVisible();
  }
  const seeded = await accounts(page, workspace);
  expect(seeded).toHaveLength(initial.length + samples.length);
  for (const sample of samples) {
    expect(seeded.find((a) => a.code === sample.code)).toMatchObject({
      name: sample.name,
      type: sample.type,
      designation: AccountDesignation.ORDINARY,
      status: AccountStatus.ACTIVE,
      normalBalance: [AccountType.LIABILITY, AccountType.REVENUE].includes(sample.type)
        ? NormalBalance.CREDIT
        : NormalBalance.DEBIT,
      version: 1n,
    });
  }
  await capture(page, "02-grouped-chart");
  if (chartCapturePhase !== "before") {
    const assets = page.getByRole("button", { name: /^Assets \(/ });
    await assets.focus();
    await page.keyboard.press("Enter");
    await expect(assets).toHaveAttribute("aria-expanded", "false");
    await expect(
      page.getByRole("cell", { name: "Synthetic studio equipment", exact: true }),
    ).toHaveCount(0);
    await capture(page, "03-collapsed-assets", true);
    await page.keyboard.press("Space");
    await expect(assets).toHaveAttribute("aria-expanded", "true");
    const search = page.getByRole("searchbox", {
      name: "Search accounts by code or name",
      exact: true,
    });
    await search.fill("software");
    await expect(
      page.getByRole("cell", { name: "Synthetic software subscriptions", exact: true }),
    ).toBeVisible();
    await expect(page.getByRole("cell", { name: "Business bank", exact: true })).toHaveCount(0);
    await capture(page, "04-search");
    await search.fill("No synthetic account matches this");
    await expect(
      page.getByRole("heading", { name: "No matching accounts", exact: true }),
    ).toBeVisible();
    await capture(page, "05-no-results");
    await page.getByRole("button", { name: "Clear search", exact: true }).click();
    await expect(search).toBeFocused();
    for (const name of ["Business bank", "Retained earnings"]) {
      const row = page
        .getByRole("row")
        .filter({ has: page.getByRole("cell", { name, exact: true }) });
      await expect(row).toContainText("Protected");
      await expect(row.getByRole("button")).toHaveCount(0);
    }
  }
  await page.getByRole("button", { name: "Add account", exact: true }).click();
  await fill(page, { code: "6300", name: "Synthetic studio insurance", type: AccountType.EXPENSE });
  await capture(page, "06-ordinary-account-entry");
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  expect(await accounts(page, workspace)).toEqual(seeded);
  if (chartCapturePhase !== "before")
    await expect(page.getByRole("button", { name: "Add account", exact: true })).toBeFocused();
  await page.getByRole("button", { name: "Add account", exact: true }).click();
  await expect(page.getByLabel("Code", { exact: true })).toHaveValue("6300");
  if (chartCapturePhase !== "before") {
    await page.getByLabel("Code", { exact: true }).focus();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("button", { name: "Add account", exact: true })).toBeFocused();
    await page.getByRole("button", { name: "Add account", exact: true }).click();
    await expect(page.getByLabel("Account name", { exact: true })).toHaveValue(
      "Synthetic studio insurance",
    );
  }
  await fill(page, {
    code: "1000",
    name: "Synthetic duplicate control code",
    type: AccountType.EXPENSE,
  });
  await page.getByRole("button", { name: "Save account", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("could not be created");
  await expect(page.getByLabel("Account name", { exact: true })).toHaveValue(
    "Synthetic duplicate control code",
  );
  expect(await accounts(page, workspace)).toEqual(seeded);
  await capture(page, "07-rejected-duplicate");
  await fill(page, { code: "6300", name: "Synthetic studio insurance", type: AccountType.EXPENSE });
  if (chartCapturePhase !== "before") await expect(page.getByRole("alert")).toHaveCount(0);
  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of [900, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    const field = page.getByLabel("Account name", { exact: true });
    await field.focus();
    await page.keyboard.press("Tab");
    await page.keyboard.press("Shift+Tab");
    await expect(field).toBeFocused();
    await expect(field).toHaveCSS("outline-style", "solid");
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await capture(page, `08-entry-focus-${width}`, true);
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("button", { name: "Save account", exact: true }).dblclick();
  await expect(
    page.getByRole("cell", { name: "Synthetic studio insurance", exact: true }),
  ).toBeVisible();
  const final = await accounts(page, workspace);
  expect(final).toHaveLength(seeded.length + 1);
  expect(final.filter((a) => a.code === "6300")).toHaveLength(1);
  for (const prior of initial.filter((a) => a.designation !== AccountDesignation.ORDINARY))
    expect(final.find((a) => a.id === prior.id)).toEqual(prior);
  await capture(page, "09-account-saved");
  await page
    .getByRole("cell", { name: "Synthetic studio insurance", exact: true })
    .scrollIntoViewIfNeeded();
  await capture(page, "09b-new-ordinary-row", true);
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    const scroll = page.getByRole("region", { name: "Accounts table", exact: true });
    await scroll.evaluate((element) => {
      element.scrollLeft = 0;
    });
    await scroll.focus();
    await page.keyboard.press("Shift+Tab");
    await page.keyboard.press("Tab");
    await expect(scroll).toBeFocused();
    await expect(scroll).toHaveCSS("outline-style", "solid");
    await page.keyboard.press("ArrowRight");
    await expect.poll(() => scroll.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
      true,
    );
    await capture(page, `10-table-focus-${width}`, true);
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("link", { name: "Overview", exact: true }).click();
  await page.goBack();
  await expect(page.getByRole("heading", { name: "Chart of accounts", exact: true })).toBeVisible();
  await page.goForward();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  expect(await accounts(page, workspace)).toEqual(final);
  expect(await journalIds(page, workspace)).toEqual(originalJournalIds);
}

async function fill(page: Page, sample: { code: string; name: string; type: AccountType }) {
  await page.getByLabel("Code", { exact: true }).fill(sample.code);
  await page.getByLabel("Account name", { exact: true }).fill(sample.name);
  await page.getByRole("combobox", { name: "Type", exact: true }).selectOption(String(sample.type));
}
async function accounts(page: Page, workspace: AuthenticatedAccountingWorkspace) {
  return listCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "accounting",
      "listAccounts",
      listCodec.encodeRequest(
        create(ListAccountsRequestSchema, {
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
          organisationId: workspace.organisationId,
          page: create(PageRequestSchema, { pageSize: 200 }),
        }),
      ),
    ),
  ).accounts;
}
async function journalIds(page: Page, workspace: AuthenticatedAccountingWorkspace) {
  return journalsCodec
    .decodeResponse(
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
    )
    .journals.map((journal) => journal.id);
}
async function capture(page: Page, name: string, focused = false) {
  if (!chartCapturePhase) return;
  const directory = path.resolve(
    import.meta.dirname,
    "../../../../../output/playwright/soft-precision-chart-2026-10-23",
    chartCapturePhase,
  );
  await mkdir(directory, { recursive: true });
  if (!focused)
    await page.getByRole("heading", { level: 1 }).evaluate((heading) => {
      for (let node = heading.parentElement; node; node = node.parentElement)
        node.scrollTo({ top: 0, left: 0, behavior: "instant" });
      document.scrollingElement?.scrollTo({ top: 0, left: 0, behavior: "instant" });
    });
  await writeFile(
    path.join(directory, `${name}.png`),
    await page.screenshot({ animations: "disabled" }),
    { flag: "wx" },
  );
  if (chartCapturePhase === "after" && name === "02-grouped-chart") {
    const measurements = await page.locator(".chart-accounts-table").evaluate((table) => {
      const height = (node: Element) => node.getBoundingClientRect().height;
      return {
        viewport: { width: innerWidth, height: innerHeight },
        dataRowHeights: [...table.querySelectorAll("tbody tr:has(td)")].map(height),
        groupControlHeights: [...table.querySelectorAll("button")].map(height),
        headerHeight: table.querySelector("thead")?.getBoundingClientRect().height,
        groups: [...table.querySelectorAll("button")].map((button) => button.textContent),
        pageContainsHorizontalOverflow: document.documentElement.scrollWidth > innerWidth,
      };
    });
    expect(Math.min(...measurements.groupControlHeights)).toBeGreaterThanOrEqual(44);
    expect(measurements.pageContainsHorizontalOverflow).toBe(false);
    await writeFile(
      path.join(directory, "measurements-1440.json"),
      `${JSON.stringify(measurements, null, 2)}\n`,
      { flag: "wx" },
    );
  }
}
