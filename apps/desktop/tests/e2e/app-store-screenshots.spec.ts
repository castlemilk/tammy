import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

import {
  hashAppBundle,
  validateScreenshotCaptureContract,
} from "../../../../scripts/capture-app-store-screenshots.mjs";
import {
  normalizeAccessibilitySnapshot,
  scanScreenshotInputs,
  validateScreenshotFixture,
} from "../../../../scripts/check-app-store-screenshots.mjs";
import { expect, test } from "./fixtures";
import { setupAndRunCurrentAccountingWorkflow } from "./support/current-accounting-workflow";

const sha256 = (value: Uint8Array | string) => createHash("sha256").update(value).digest("hex");

test("captures one serial five-image real packaged UI journey", async ({ electronHarness }) => {
  const contractPath = process.env.TAMMY_APP_STORE_SCREENSHOT_CONTRACT;
  if (!contractPath) throw new Error("APP_STORE_SCREENSHOT_CAPTURE_CONTRACT_MISSING");
  const contract = validateScreenshotCaptureContract(
    JSON.parse(await readFile(contractPath, "utf8")),
  );
  const fixtureBytes = await readFile(contract.fixturePath);
  const fixture = validateScreenshotFixture(JSON.parse(fixtureBytes.toString("utf8")));
  expect(sha256(fixtureBytes)).toBe(contract.fixtureSha256);

  await setupAndRunCurrentAccountingWorkflow(electronHarness.page, electronHarness, {
    fixedRendererClock: true,
  });
  const page = electronHarness.currentPage();
  const contentBounds = await electronHarness.application.evaluate(({ BrowserWindow }) => {
    const window = BrowserWindow.getAllWindows()[0];
    if (!window) throw new Error("APP_STORE_SCREENSHOT_WINDOW_MISSING");
    window.setResizable(false);
    window.setContentSize(1440, 900, false);
    return window.getContentBounds();
  });
  expect(contentBounds).toEqual(expect.objectContaining({ height: 900, width: 1440 }));
  await page.emulateMedia({ colorScheme: "light", reducedMotion: "reduce" });
  const renderingEnvironment = await page.evaluate(() => ({
    colorScheme: matchMedia("(prefers-color-scheme: light)").matches,
    devicePixelRatio,
    height: innerHeight,
    locale: navigator.language,
    reducedMotion: matchMedia("(prefers-reduced-motion: reduce)").matches,
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    width: innerWidth,
  }));
  expect(renderingEnvironment).toEqual({
    colorScheme: true,
    devicePixelRatio: 1,
    height: 900,
    locale: contract.locale,
    reducedMotion: true,
    timezone: contract.timezone,
    width: 1440,
  });
  await mkdir(contract.captureDirectory, { mode: 0o700 });

  const targets = [
    { heading: "Overview", link: "Overview", ready: "Nothing needs review" },
    {
      heading: "Documents",
      link: "Documents",
      ready: fixture.sourceDocument.sourceDisplayName,
    },
    { heading: "Trial balance", link: "Trial balance", ready: fixture.accounts[0]?.name },
    { heading: "Banking", link: "Banking", ready: "Reconciled" },
    { heading: "GST & BAS", link: "GST & BAS", ready: fixture.sourceDocument.supplierName },
  ] as const;
  const images = [];
  for (const [index, target] of targets.entries()) {
    await page.getByRole("link", { name: target.link }).click();
    await expect(page.getByRole("heading", { level: 1, name: target.heading })).toBeVisible();
    if (!target.ready) throw new Error("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
    await expect(page.getByText(target.ready, { exact: true }).first()).toBeVisible();
    if (target.heading === "Banking") {
      await page.getByLabel("Opening balance").fill("1000.00");
      await page
        .getByLabel("CSV rows")
        .fill("2024-05-12,HARBOUR OFFICE SUPPLIES WCS-2024Q4-001,-319.00");
    }
    await expect(page.getByText(/^(?:Loading|Checking)\b/iu)).toHaveCount(0);
    await expect(
      page.getByText(/^(?:Documents|Trial balance|Banking data) (?:is )?unavailable\.?$/iu),
    ).toHaveCount(0);
    await page.evaluate(async () => {
      await document.fonts.ready;
    });
    const stableLayout = async () =>
      page.evaluate(() => ({
        height: document.documentElement.scrollHeight,
        text: document.body.innerText,
        width: document.documentElement.scrollWidth,
      }));
    let before = await stableLayout();
    await page.waitForTimeout(50);
    const after = await stableLayout();
    expect(after).toEqual(before);
    before = after;
    expect(before.width).toBeLessThanOrEqual(1440);
    expect(before.height).toBeLessThanOrEqual(900);

    const definition = fixture.screenshots[index];
    if (!definition) throw new Error("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
    const accessibility = await page.locator("body").ariaSnapshot();
    scanScreenshotInputs(accessibility);
    const completeAccessibilityText = normalizeAccessibilitySnapshot(accessibility);
    scanScreenshotInputs(completeAccessibilityText);
    expect(completeAccessibilityText).toEqual(definition.completeAccessibilityText);
    const accessibilitySnapshot = `${completeAccessibilityText.join("\n")}\n`;
    const accessibilityName = definition.filename.replace(/\.png$/u, ".accessibility.txt");
    const imagePath = path.join(contract.captureDirectory, definition.filename);
    await Promise.all([
      page.screenshot({ animations: "disabled", path: imagePath, scale: "css" }),
      writeFile(path.join(contract.captureDirectory, accessibilityName), accessibilitySnapshot, {
        mode: 0o600,
      }),
    ]);
    const imageBytes = await readFile(imagePath);
    images.push({
      accessibilitySnapshot: accessibilityName,
      accessibilitySnapshotSha256: sha256(accessibilitySnapshot),
      caption: definition.caption,
      filename: definition.filename,
      sha256: sha256(imageBytes),
    });
  }

  const manifest = {
    buildNumber: contract.buildNumber,
    candidateEventPath: null,
    candidateEventSha256: null,
    captureArtifactKind: contract.captureArtifactKind,
    capturedAt: contract.capturedAt,
    developmentSignedAppSha256: contract.developmentSignedAppSha256,
    dimensions: contract.dimensions,
    distributionPackageSha256: null,
    fixturePath: "apps/desktop/release/macos/screenshots/fixture.json",
    fixtureSha256: contract.fixtureSha256,
    images,
    locale: contract.locale,
    marketingVersion: contract.marketingVersion,
    productSourceCommit: contract.productSourceCommit,
    productSourceTree: contract.productSourceTree,
    schemaVersion: 1,
    unsignedContentManifestSha256: contract.unsignedContentManifestSha256,
  };
  await writeFile(
    path.join(contract.captureDirectory, "manifest.json"),
    `${JSON.stringify(manifest, null, 2)}\n`,
    { mode: 0o600 },
  );
  expect(await hashAppBundle(contract.developmentApp)).toBe(contract.developmentSignedAppSha256);
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});
