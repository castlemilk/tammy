import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { create } from "@bufbuild/protobuf";
import { expect, type Page } from "@playwright/test";
import {
  GetJournalRequestSchema,
  GetJournalResponseSchema,
  JournalSource,
  JournalState,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { PageRequestSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { createProtoMethodCodec } from "../../../src/shared/proto-ipc";
import { invokeConveyorFrame } from "./conveyor";
import type { AuthenticatedAccountingWorkspace } from "./current-accounting-workflow";

export const journalsCapturePhase = process.env.TAMMY_JOURNALS_CAPTURE;
if (
  journalsCapturePhase !== undefined &&
  journalsCapturePhase !== "before" &&
  journalsCapturePhase !== "after"
)
  throw new Error("JOURNALS_CAPTURE_PHASE_INVALID");
const listCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  output: ListJournalsResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 262144,
});
const detailCodec = createProtoMethodCodec({
  input: GetJournalRequestSchema,
  output: GetJournalResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 262144,
});
const samples = [
  { memo: "Synthetic studio materials", amount: "128.50" },
  { memo: "Synthetic software subscription", amount: "79.00" },
  { memo: "Synthetic stationery contribution", amount: "42.00" },
];

export async function runJournalsJourney(page: Page, workspace: AuthenticatedAccountingWorkspace) {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("link", { name: "Journals", exact: true }).click();
  const original = await list(page, workspace);
  await capture(page, "01-initial-list");
  for (const sample of samples) {
    await page.getByRole("button", { name: "New journal", exact: true }).click();
    await fillEntry(page, sample.memo, sample.amount);
    await page.getByRole("button", { name: "Post journal", exact: true }).click();
    await expect(page.getByRole("heading", { name: sample.memo, exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Journals", exact: true }).click();
  }
  const journals = await list(page, workspace);
  expect(journals).toHaveLength(original.length + 3);
  await capture(page, "02-loaded-history");
  if (journalsCapturePhase !== "before") {
    const search = page.getByRole("searchbox", {
      name: "Search journal descriptions",
      exact: true,
    });
    await search.fill("studio materials");
    const open = page.getByRole("button", {
      name: "Open journal: Synthetic studio materials",
      exact: true,
    });
    await open.focus();
    await page.keyboard.press("Enter");
    await expect(
      page.getByRole("heading", { name: "Synthetic studio materials", exact: true }),
    ).toBeFocused();
    await page.getByRole("button", { name: "Journals", exact: true }).click();
    await expect(search).toHaveValue("studio materials");
    await expect(open).toBeFocused();
    await capture(page, "09-retained-search");
    await search.fill("No synthetic journal matches this");
    await expect(
      page.getByRole("heading", { name: "No matching journals", exact: true }),
    ).toBeVisible();
    await capture(page, "10-no-results");
    await page.getByRole("button", { name: "Clear search", exact: true }).click();
    await expect(search).toBeFocused();
  }
  await page.getByRole("button", { name: "New journal", exact: true }).click();
  await fillEntry(page, "Synthetic owner-funded studio supplies", "200.00");
  await capture(page, "03-entry-review");
  await page
    .getByRole("combobox", { name: "Debit account", exact: true })
    .selectOption({ label: "3100 — Owner contributions" });
  await page.getByRole("button", { name: "Post journal", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Choose two different accounts");
  if (journalsCapturePhase !== "before")
    await expect(page.getByRole("region", { name: "Journal error", exact: true })).toBeFocused();
  await expect(page.getByLabel("Amount (AUD)", { exact: true })).toHaveValue("200.00");
  expect((await list(page, workspace)).map((j) => j.id)).toEqual(journals.map((j) => j.id));
  await capture(page, "04-invalid-accounts");
  await page
    .getByRole("combobox", { name: "Debit account", exact: true })
    .selectOption({ label: "6100 — Office expenses" });
  if (journalsCapturePhase !== "before") await expect(page.getByRole("alert")).toHaveCount(0);
  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of [900, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await page.getByLabel("Description", { exact: true }).focus();
    await page.keyboard.press("Tab");
    await page.keyboard.press("Shift+Tab");
    await expect(page.getByLabel("Description", { exact: true })).toBeFocused();
    await expect(page.getByLabel("Description", { exact: true })).toHaveCSS(
      "outline-style",
      "solid",
    );
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
    await capture(page, `05-entry-focus-${width}`, "focus");
    if (width === 320 && journalsCapturePhase !== "before") {
      const post = page.getByRole("button", { name: "Post journal", exact: true });
      await post.scrollIntoViewIfNeeded();
      await post.focus();
      await page.keyboard.press("Shift+Tab");
      await page.keyboard.press("Tab");
      await expect(post).toBeFocused();
      await expect(post).toHaveCSS("outline-style", "solid");
      await expect(post).toBeInViewport({ ratio: 1 });
      await capture(page, "11-post-action-320", "focus");
    }
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
  if (journalsCapturePhase === "before")
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
  else {
    await page.getByLabel("Description", { exact: true }).focus();
    await page.keyboard.press("Escape");
    await expect(page.getByRole("button", { name: "New journal", exact: true })).toBeFocused();
  }
  expect((await list(page, workspace)).map((j) => j.id)).toEqual(journals.map((j) => j.id));
  await page.getByRole("button", { name: "New journal", exact: true }).click();
  await expect(page.getByLabel("Description", { exact: true })).toHaveValue(
    "Synthetic owner-funded studio supplies",
  );
  await page.getByRole("button", { name: "Post journal", exact: true }).dblclick();
  await expect(
    page.getByRole("heading", { name: "Synthetic owner-funded studio supplies", exact: true }),
  ).toBeVisible();
  const final = await list(page, workspace);
  expect(final).toHaveLength(original.length + 4);
  const saved = final.find((j) => j.memo === "Synthetic owner-funded studio supplies");
  if (!saved) throw new Error("SYNTHETIC_JOURNAL_MISSING");
  const response = detailCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "accounting",
      "getJournal",
      detailCodec.encodeRequest(
        create(GetJournalRequestSchema, {
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
          journalId: saved.id,
        }),
      ),
    ),
  );
  expect(response.journal?.lines).toHaveLength(2);
  expect(response.journal).toMatchObject({
    source: JournalSource.MANUAL,
    state: JournalState.POSTED,
    version: 1n,
  });
  expect(response.journal?.totalDebits?.minorUnits).toBe(20000n);
  expect(response.journal?.totalCredits?.minorUnits).toBe(20000n);
  await expect(page.getByRole("button", { name: "Post journal", exact: true })).toHaveCount(0);
  if (journalsCapturePhase !== "before")
    await expect(page.getByRole("banner").getByText("Journals", { exact: true })).toBeVisible();
  await capture(page, "06-posted-detail");
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    const scroll = page.getByRole("region", { name: "Journal entry lines table", exact: true });
    await scroll.evaluate((n) => {
      n.scrollLeft = 0;
    });
    await scroll.focus();
    await page.keyboard.press("Shift+Tab");
    await page.keyboard.press("Tab");
    await expect(scroll).toBeFocused();
    await expect(scroll).toHaveCSS("outline-style", "solid");
    await page.keyboard.press("ArrowRight");
    await expect.poll(() => scroll.evaluate((n) => n.scrollLeft)).toBeGreaterThan(0);
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
    await capture(page, `07-detail-scroll-${width}`, "focus");
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("button", { name: "Journals", exact: true }).click();
  await expect(
    page.getByText("Synthetic owner-funded studio supplies", { exact: true }),
  ).toBeVisible();
  await page.goBack();
  await expect(
    page.getByRole("heading", { name: "Synthetic owner-funded studio supplies", exact: true }),
  ).toBeVisible();
  await page.goForward();
  await expect(page.getByRole("heading", { name: "Journals", exact: true })).toBeVisible();
  expect((await list(page, workspace)).map((j) => j.id)).toEqual(final.map((j) => j.id));
  await capture(page, "08-returned-history");
}
async function fillEntry(page: Page, memo: string, amount: string) {
  await page.getByLabel("Date", { exact: true }).fill("2024-05-10");
  await page.getByLabel("Description", { exact: true }).fill(memo);
  await page.getByLabel("Amount (AUD)", { exact: true }).fill(amount);
  await page
    .getByRole("combobox", { name: "Debit account", exact: true })
    .selectOption({ label: "6100 — Office expenses" });
  await page
    .getByRole("combobox", { name: "Credit account", exact: true })
    .selectOption({ label: "3100 — Owner contributions" });
}
async function list(page: Page, workspace: AuthenticatedAccountingWorkspace) {
  return listCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "accounting",
      "listJournals",
      listCodec.encodeRequest(
        create(ListJournalsRequestSchema, {
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
          organisationId: workspace.organisationId,
          page: create(PageRequestSchema, { pageSize: 200 }),
        }),
      ),
    ),
  ).journals;
}
async function capture(page: Page, name: string, anchor: "page" | "focus" = "page") {
  if (!journalsCapturePhase) return;
  const directory = path.resolve(
    import.meta.dirname,
    "../../../../../output/playwright/soft-precision-journals-2026-10-23",
    journalsCapturePhase,
  );
  await mkdir(directory, { recursive: true });
  if (anchor === "page")
    await page.getByRole("heading", { level: 1 }).evaluate((heading) => {
      for (let n = heading.parentElement; n; n = n.parentElement)
        n.scrollTo({ top: 0, left: 0, behavior: "instant" });
      document.scrollingElement?.scrollTo({ top: 0, left: 0, behavior: "instant" });
    });
  await writeFile(
    path.join(directory, `${name}.png`),
    await page.screenshot({ animations: "disabled" }),
    { flag: "wx" },
  );
  if (journalsCapturePhase === "after" && name === "03-entry-review") {
    const measurements = await page
      .getByRole("form", { name: "Manual journal entry", exact: true })
      .evaluate((form) => {
        const rect = (node: Element) => {
          const r = node.getBoundingClientRect();
          return { x: r.x, y: r.y, width: r.width, height: r.height };
        };
        const controls = [...form.querySelectorAll("input,select,button")].map(rect);
        const totals = form.querySelector("dl");
        return {
          viewport: { width: innerWidth, height: innerHeight },
          form: rect(form),
          minimumControlHeight: Math.min(...controls.map((r) => r.height)),
          totalsBackground: totals ? getComputedStyle(totals).backgroundColor : null,
        };
      });
    expect(measurements.minimumControlHeight).toBeGreaterThanOrEqual(44);
    expect(measurements.totalsBackground).toBe("rgb(231, 237, 226)");
    await writeFile(
      path.join(directory, "measurements-1440.json"),
      `${JSON.stringify(measurements, null, 2)}\n`,
      { flag: "wx" },
    );
  }
}
