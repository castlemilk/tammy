import {
  createExternalHandoffEvent,
  type ExternalHandoffEvent,
} from "../../../../scripts/macos-runtime-egress.mjs";
import { expect, test } from "./fixtures";
import { setupAndRunCurrentAccountingWorkflow } from "./support/current-accounting-workflow";

const PRIVACY_URL = "https://tammy-accounting.castlemilk.chatgpt.site/privacy";
const SUPPORT_URL = "https://tammy-accounting.castlemilk.chatgpt.site/support";

test("runs one serial contained packaged privacy journey", async ({ electronHarness }) => {
  await setupAndRunCurrentAccountingWorkflow(electronHarness.page, electronHarness, {
    fixedRendererClock: true,
  });
  const page = electronHarness.currentPage();

  for (const target of [
    { heading: "Documents", link: "Documents" },
    { heading: "Banking", link: "Banking" },
    { heading: "GST & BAS", link: "GST & BAS" },
  ] as const) {
    await page.getByRole("link", { name: target.link }).click();
    await expect(page.getByRole("heading", { level: 1, name: target.heading })).toBeVisible();
    await expect(page.getByText(/^(?:Loading|Checking)\b/iu)).toHaveCount(0);
  }
  await page.waitForTimeout(250);

  await electronHarness.application.evaluate(({ shell }) => {
    const target = globalThis as typeof globalThis & {
      __tammyAppStoreHandoffs?: ExternalHandoffEvent[];
    };
    target.__tammyAppStoreHandoffs = [];
    shell.openExternal = async (url) => {
      target.__tammyAppStoreHandoffs?.push({
        occurredAt: new Date().toISOString(),
        url,
        userGesture: true,
      });
    };
  });

  await page.getByRole("link", { name: "Settings" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Settings" })).toBeVisible();
  await page.getByRole("link", { name: "Privacy policy" }).click();
  await page.waitForTimeout(10);
  await page.getByRole("link", { name: "Support" }).click();
  const recorded = await electronHarness.application.evaluate(() => {
    const target = globalThis as typeof globalThis & {
      __tammyAppStoreHandoffs?: ExternalHandoffEvent[];
    };
    return target.__tammyAppStoreHandoffs ?? [];
  });
  expect(recorded).toHaveLength(2);
  expect(
    recorded.map((event) =>
      createExternalHandoffEvent({
        allowedUrls: [PRIVACY_URL, SUPPORT_URL],
        occurredAt: event.occurredAt,
        url: event.url,
        userGesture: event.userGesture,
      }),
    ),
  ).toEqual(recorded);
  expect(recorded.map(({ url }) => url)).toEqual([PRIVACY_URL, SUPPORT_URL]);
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});
