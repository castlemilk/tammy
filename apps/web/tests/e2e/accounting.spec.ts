import os from "node:os";
import path from "node:path";
import { expect, test } from "@playwright/test";

test("sets up a workspace, posts a balanced journal and reads the trial balance", async ({
  page,
}) => {
  const browserErrors: string[] = [];
  page.on("pageerror", (error) => browserErrors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") browserErrors.push(message.text());
  });
  await page.setViewportSize({ width: 1536, height: 1024 });
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();
  await page.getByLabel("Your name").fill("Tammy Admin");
  await page.getByLabel("Email or username").fill("admin@tammy.local");
  await page.getByLabel("Business legal name").fill("Tammy Business Pty Ltd");
  await page.getByLabel("Business display name").fill("Tammy Business");
  await page.getByLabel("ABN").fill("51824753556");
  await page.getByLabel("Workspace passphrase").fill("workspace-passphrase-long-enough");
  await page.getByLabel("Administrator password").fill("administrator-password-long-enough");
  await page.getByRole("button", { name: "Create local workspace" }).click();
  const recoveryCode = await page.locator(".recovery-code strong").innerText();
  await expect(
    page.getByText(
      /^Confirm by .+\. After that, or after 5 incorrect codes, this setup is discarded/,
    ),
  ).toBeVisible();
  await page.getByLabel("Enter your saved recovery code").fill(recoveryCode);
  await page.getByRole("button", { name: "Confirm recovery code" }).click();
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
  await expect(page.getByText("No journals yet")).toBeVisible();
  await page.screenshot({
    path: path.join(os.tmpdir(), "tammy-web-overview-e2e.png"),
    fullPage: true,
  });
  for (const width of [785, 390]) {
    await page.setViewportSize({ width, height: 900 });
    await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true);
    await page.screenshot({
      path: path.join(os.tmpdir(), `tammy-web-overview-${width}.png`),
      fullPage: true,
    });
  }
  await page.setViewportSize({ width: 1536, height: 1024 });

  await page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: "Chart of accounts" })
    .click();
  for (const account of [
    { code: "6100", name: "Office expenses", type: "expense" },
    { code: "3100", name: "Owner contributions", type: "equity" },
  ]) {
    await page.getByRole("button", { name: "Add account" }).click();
    await page.getByLabel("Account code").focus();
    await page.keyboard.type(account.code);
    await page.keyboard.press("Tab");
    await page.keyboard.type(account.name);
    await page.getByLabel("Account type").selectOption(account.type);
    await page.getByRole("button", { name: "Save account" }).click();
    await expect(page.getByRole("cell", { name: account.name })).toBeVisible();
  }
  await page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: "Journals" })
    .click();
  await page.getByRole("button", { name: "New journal" }).click();
  await page.getByLabel("Date").fill("2024-05-12");
  await page.getByLabel("Description").fill("Office supplies");
  await page.getByLabel("Debit account").selectOption({ label: "6100 — Office expenses" });
  await page.getByLabel("Credit account").selectOption({ label: "3100 — Owner contributions" });
  await page.getByLabel("Amount (AUD)").fill("319.00");
  const postedKeys: string[] = [];
  await page.route("**/api/journals", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    postedKeys.push(route.request().postDataJSON().idempotencyKey as string);
    if (postedKeys.length === 1) {
      await route.fetch();
      await route.abort("failed");
    } else await route.continue();
  });
  await page.getByRole("button", { name: "Post journal" }).click();
  await expect(page.getByText(/could not confirm whether this journal posted/i)).toBeVisible();
  await expect(page.getByLabel("Description")).toBeDisabled();
  await page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: "Chart of accounts" })
    .click();
  await page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: "Journals" })
    .click();
  await expect(page.getByLabel("Description")).toBeDisabled();
  await page.getByRole("button", { name: "Retry same journal" }).click();
  expect(postedKeys).toHaveLength(2);
  expect(postedKeys[0]).toBe(postedKeys[1]);
  await expect(page.getByText("Office supplies").first()).toBeVisible();
  await expect(page.getByRole("row", { name: /Office supplies/ })).toHaveCount(1);
  await page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: "Trial balance" })
    .click();
  await page.getByLabel("As of date").fill("2024-05-12");
  await page.getByRole("button", { name: "Show balance" }).click();
  await expect(page.getByRole("row", { name: /Office expenses/ })).toContainText("$319.00");
  await expect(page.getByRole("row", { name: /Owner contributions/ })).toContainText("$319.00");
  await expect(page.getByRole("row", { name: /Total/ })).toContainText("$319.00");
  await page.reload();
  await expect(page.getByRole("row", { name: /Office expenses/ })).toBeVisible();

  await page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: "Documents" })
    .click();
  await page.getByLabel("Upload document").setInputFiles({
    name: "receipt.png",
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aP1sAAAAASUVORK5CYII=",
      "base64",
    ),
  });
  await expect(page.getByRole("button", { name: /receipt.png/ })).toBeVisible();
  await page.getByLabel("Supplier").fill("Officeworks");
  await page.getByLabel("Invoice number").fill("INV-029847");
  await page.getByLabel("Document date").fill("2024-05-12");
  await page.getByLabel("Subtotal (AUD)").fill("290.00");
  await page.getByLabel("GST (AUD)").fill("29.00");
  await page.getByLabel("Total (AUD)", { exact: true }).fill("319.00");
  await page.getByRole("button", { name: "Save review" }).click();
  await expect(page.getByText("Reviewed", { exact: true }).first()).toBeVisible();

  await page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: "Banking" })
    .click();
  await page.getByLabel("Opening balance (AUD)").fill("1000.00");
  await page.getByLabel("Statement rows").fill("2024-05-12,Officeworks,-319.00");
  await page.getByRole("button", { name: "Import statement" }).click();
  await expect(page.getByRole("row", { name: /Officeworks/ })).toBeVisible();
  await page.getByRole("button", { name: "Match", exact: true }).click();
  await page.getByLabel("Match reference").fill("INV-029847");
  await page.getByRole("button", { name: "Mark matched" }).click();
  await expect(page.getByText("Matched", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Complete reconciliation" }).click();
  await expect(page.getByText("Reconciled", { exact: true })).toBeVisible();
  await expect(page.getByText("$681.00", { exact: true })).toBeVisible();

  await page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: "GST & BAS" })
    .click();
  await page.getByLabel("Period start").fill("2024-04-01");
  await page.getByLabel("Period end").fill("2024-06-30");
  await page.getByRole("button", { name: "Create local draft" }).click();
  await expect(page.getByText("Draft — not lodged")).toBeVisible();
  await expect(page.getByText(/calculated by the core but not shown/)).toBeVisible();
  // The reviewed receipt is a provisional candidate; only posted purchases count in 1B.
  await expect(page.getByText(/Their GST is not a claimed credit/)).toBeVisible();
  await expect(
    page.locator(".summary-list > div").filter({ hasText: "GST credits (1B)" }),
  ).toContainText("$0.00");
  await expect(page.getByRole("row", { name: /Officeworks/ })).toContainText("$29.00");
  await expect(page.getByRole("row", { name: /Total provisional candidates/ })).toContainText(
    "$29.00",
  );
  await page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: "Intelligence" })
    .click();
  await expect(page.getByText("GST workpaper is a draft")).toBeVisible();
  await page
    .getByRole("navigation", { name: "Primary navigation" })
    .getByRole("link", { name: "Connections" })
    .click();
  await expect(page.getByText(/Read-only tools/)).toBeVisible();
  await page.getByRole("button", { name: /Tammy Business/ }).click();
  await page.getByRole("button", { name: "Lock workspace" }).click();
  await expect(page.getByRole("heading", { name: "Unlock your workspace" })).toBeVisible();
  await page.getByLabel("Workspace passphrase").fill("workspace-passphrase-long-enough");
  await page.getByLabel("Email or username").fill("admin@tammy.local");
  await page.getByLabel("Administrator password").fill("administrator-password-long-enough");
  await page.getByRole("button", { name: "Unlock workspace" }).click();
  await expect(page.getByRole("heading", { name: "Connections" })).toBeVisible();
  expect(browserErrors.filter((message) => !message.includes("net::ERR_FAILED"))).toEqual([]);
  expect(
    browserErrors.filter((message) => message.includes("net::ERR_FAILED")).length,
  ).toBeLessThanOrEqual(1);
});
