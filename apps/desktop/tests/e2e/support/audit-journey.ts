import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { expect, type Page } from "@playwright/test";

export const auditCapturePhase = process.env.TAMMY_AUDIT_CAPTURE;
if (auditCapturePhase !== undefined && !["before", "after"].includes(auditCapturePhase))
  throw new Error("AUDIT_CAPTURE_PHASE_INVALID");

export async function runAuditJourney(page: Page) {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("link", { name: "Audit trail", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Audit trail", exact: true })).toBeVisible();
  const list = page.locator("main ol");
  await expect(list).toHaveCount(1);
  await expect.poll(() => list.locator(":scope > li").count()).toBeGreaterThanOrEqual(4);
  await expect(
    page.getByText("Office supplies paid personally", { exact: true }).first(),
  ).toBeVisible();
  const initialCount = await list.locator(":scope > li").count();
  await capture(page, "01-retained-records");
  if (auditCapturePhase !== "before") {
    const filter = page.getByRole("combobox", {
      name: "Filter loaded activity by record type",
      exact: true,
    });
    const search = page.getByRole("searchbox", { name: "Search loaded activity", exact: true });
    await filter.selectOption("Journal");
    const summaries = list.getByRole("button", { name: /^Record details: Journal/ });
    await expect.poll(() => summaries.count()).toBeGreaterThan(0);
    const journalCount = await summaries.count();
    await filter.selectOption("Journal");
    await expect(summaries).toHaveCount(journalCount);
    const summary = summaries.first();
    await summary.focus();
    await page.keyboard.press("Enter");
    await expect(summary.locator("..")).toHaveAttribute("open", "");
    await expect(page.getByText("Record ID", { exact: true })).toBeVisible();
    await expect(page.getByText("Posting date", { exact: true })).toBeVisible();
    await capture(page, "02-journal-source-details");
    await page.keyboard.press("Enter");
    await expect(summary.locator("..")).not.toHaveAttribute("open", "");
    await search.fill("No synthetic activity matches this request");
    await expect(
      page.getByRole("heading", { name: "No matching loaded records", exact: true }),
    ).toBeVisible();
    await capture(page, "03-no-matching-records");
    await page.getByRole("button", { name: "Clear activity search", exact: true }).click();
    await expect(search).toBeFocused();
    await expect(summaries).toHaveCount(journalCount);
    await capture(page, "04-filtered-records");
  }
  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of [900, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    const focus =
      auditCapturePhase === "before"
        ? page.getByRole("link", { name: "Audit trail", exact: true })
        : list.getByRole("button", { name: /^Record details: Journal/ }).first();
    await focus.focus();
    await page.keyboard.press("Tab");
    await page.keyboard.press("Shift+Tab");
    await expect(focus).toBeFocused();
    await expect(focus).toHaveCSS("outline-style", "solid");
    if (auditCapturePhase !== "before") {
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      );
      await page.keyboard.press("Enter");
      await expect(focus.locator("..")).toHaveAttribute("open", "");
    }
    await capture(page, `05-keyboard-${width}`);
    if (auditCapturePhase !== "before") await page.keyboard.press("Enter");
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
  if (auditCapturePhase !== "before")
    await page
      .getByRole("combobox", { name: "Filter loaded activity by record type", exact: true })
      .selectOption("All");
  await page.getByRole("link", { name: "Journals", exact: true }).click();
  await page.goBack();
  await expect(page.getByRole("heading", { name: "Audit trail", exact: true })).toBeVisible();
  await expect(list.locator(":scope > li")).toHaveCount(initialCount);
  await capture(page, "06-returned-records");
  await page.goForward();
  await expect(page.getByRole("heading", { name: "Journals", exact: true })).toBeVisible();
}

async function capture(page: Page, name: string) {
  if (!auditCapturePhase) return;
  await page.getByRole("heading", { name: "Audit trail", exact: true }).evaluate((heading) => {
    for (let node = heading.parentElement; node; node = node.parentElement)
      node.scrollTo({ top: 0, left: 0, behavior: "instant" });
    document.scrollingElement?.scrollTo({ top: 0, left: 0, behavior: "instant" });
  });
  const directory = path.resolve(
    import.meta.dirname,
    "../../../../../output/playwright/soft-precision-audit-2026-10-24",
    auditCapturePhase,
  );
  await mkdir(directory, { recursive: true });
  await writeFile(
    path.join(directory, `${name}.png`),
    await page.screenshot({ animations: "disabled" }),
    { flag: "wx" },
  );
}
