import { readFileSync } from "node:fs";
import { create } from "@bufbuild/protobuf";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
  ReportingCapabilityStatus,
  ReportingEntityType,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import { createProtoMethodCodec } from "../../src/shared/proto-ipc";
import { consumeExpectedCspViolations } from "./csp-console";
import { expect, test } from "./fixtures";

const expectedTarget = `${process.platform}-${process.arch}`;
const preloadMethods = JSON.parse(
  readFileSync(new URL("../../src/shared/preload-methods.json", import.meta.url), "utf8"),
) as readonly string[];
const probeUrls = [
  "https://example.com/tammy-csp-probe",
  "http://example.com/tammy-csp-probe",
] as const;
const reportingCodec = createProtoMethodCodec({
  input: GetReportingCapabilityRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetReportingCapabilityResponseSchema,
});

test("runs the packaged first-run journey offline and exits cleanly", async ({
  electronHarness,
}) => {
  expect(test.info().project.name).toBe(expectedTarget);

  const { consoleErrors, page, pageErrors } = electronHarness;
  await expect(page).toHaveTitle("Tammy");
  await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();
  await expect(
    page.getByText("Your accounting data stays encrypted on this device."),
  ).toBeVisible();
  await expect(page.getByLabel("Your name")).toBeVisible();
  await expect(page.getByLabel("Email or username")).toBeVisible();
  await expect(page.getByLabel("Business legal name")).toBeVisible();
  await expect(page.getByLabel("Business display name")).toBeVisible();
  await expect(page.getByLabel("ABN")).toBeVisible();
  await expect(page.getByLabel("Workspace passphrase")).toHaveAttribute("type", "password");
  await expect(page.getByLabel("Administrator password")).toHaveAttribute("type", "password");
  await expect(
    page.getByText("Tammy supports a local reviewed-document GST workpaper only."),
  ).toBeVisible();
  await expect(
    page.getByText("Tammy does not prepare, declare, or lodge a complete BAS."),
  ).toBeVisible();

  const gstWorkpaper = await windowReportingCapability(page, ReportKind.GST_WORKPAPER);
  expect(gstWorkpaper.capability).toMatchObject({
    report: ReportKind.GST_WORKPAPER,
    taxYear: 2024,
    entityType: ReportingEntityType.AU_BUSINESS,
    status: ReportingCapabilityStatus.AVAILABLE,
  });
  expect(gstWorkpaper.capability?.appVersion).toBeTruthy();
  const bas = await windowReportingCapability(page, ReportKind.BAS);
  expect(bas.capability).toMatchObject({
    report: ReportKind.BAS,
    taxYear: 2024,
    entityType: ReportingEntityType.AU_BUSINESS,
    status: ReportingCapabilityStatus.UNSUPPORTED,
  });
  expect(bas.capability?.appVersion).toBe(gstWorkpaper.capability?.appVersion);

  expect(await page.evaluate(() => Object.keys(window.tammy))).toEqual(preloadMethods);
  expect(
    await page.evaluate(() => ({
      Buffer: typeof (globalThis as { Buffer?: unknown }).Buffer,
      global: typeof (globalThis as { global?: unknown }).global,
      module: typeof (globalThis as { module?: unknown }).module,
      process: typeof (globalThis as { process?: unknown }).process,
      require: typeof (globalThis as { require?: unknown }).require,
    })),
  ).toEqual({
    Buffer: "undefined",
    global: "undefined",
    module: "undefined",
    process: "undefined",
    require: "undefined",
  });

  expect(await page.evaluate(() => navigator.onLine)).toBe(false);
  const fetchResults = await page.evaluate(async (urls) => {
    const attempt = async (url: string) => {
      try {
        await fetch(url);
        return "resolved";
      } catch {
        return "rejected";
      }
    };
    return Promise.all(urls.map(attempt));
  }, probeUrls);
  expect(fetchResults).toEqual(["rejected", "rejected"]);
  await expect
    .poll(() => {
      try {
        consumeExpectedCspViolations(consoleErrors, probeUrls);
        return true;
      } catch {
        return false;
      }
    })
    .toBe(true);

  await expect
    .poll(async () => windowDiagnostics(page))
    .toEqual({
      apiVersion: "tammy.v1",
      networkRequired: false,
      runtimeMode: "offline",
    });

  await page.getByRole("button", { name: "Privacy and support" }).click();
  await expect(page.getByRole("heading", { name: "Privacy and support" })).toBeVisible();
  await page.getByRole("button", { name: "Back to Tammy" }).click();
  await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();
  await page.keyboard.press("Tab");
  await expect(page.getByLabel("Your name")).toBeFocused();

  expect(consumeExpectedCspViolations(consoleErrors, probeUrls)).toEqual([]);
  expect(pageErrors).toEqual([]);
});

async function windowDiagnostics(page: import("@playwright/test").Page) {
  const diagnostics = await page.evaluate(() => window.tammy.getSystemDiagnostics());
  return {
    apiVersion: diagnostics.apiVersion,
    networkRequired: diagnostics.networkRequired,
    runtimeMode: diagnostics.runtimeMode,
  };
}

async function windowReportingCapability(
  page: import("@playwright/test").Page,
  report: ReportKind,
) {
  const request = reportingCodec.encodeRequest(
    create(GetReportingCapabilityRequestSchema, {
      report,
      taxYear: 2024,
      entityType: ReportingEntityType.AU_BUSINESS,
    }),
  );
  const response = await page.evaluate(
    async (requestBytes) => {
      const frame = await window.tammy.getReportingCapability(Uint8Array.from(requestBytes));
      return [...frame];
    },
    [...request],
  );
  return reportingCodec.decodeResponse(Uint8Array.from(response));
}
