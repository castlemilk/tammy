import type { Page } from "@playwright/test";
import { expect } from "../fixtures";

const SUMMARY = "Saved workpapers and exports";

export async function openSavedBasWorkpaperPanel(page: Page) {
  const panel = page.locator("details").filter({
    has: page.getByText(SUMMARY, { exact: true }),
  });
  await expect(panel).toHaveCount(1);
  const isOpen = await panel.evaluate((element) =>
    element instanceof HTMLDetailsElement ? element.open : false,
  );
  if (!isOpen) await panel.locator("summary").click();
  await expect(panel).toHaveAttribute("open", "");
  return panel;
}
