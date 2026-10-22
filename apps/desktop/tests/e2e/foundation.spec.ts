import { create, isFieldSet } from "@bufbuild/protobuf";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
  ReportingCapabilityMode,
  ReportingCapabilityStatus,
  ReportingEntityType,
  ReportingModeAvailability,
  ReportingModeCapabilitySchema,
  ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import { createProtoMethodCodec } from "../../src/shared/proto-ipc";
import { consumeExpectedCspViolations } from "./csp-console";
import { expect, test } from "./fixtures";
import { invokeConveyorFrame, invokeConveyorValue } from "./support/conveyor";

const expectedTarget = `${process.platform}-${process.arch}`;
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
    page.getByText("Tammy supports a local invoice and reconciled purchase GST workpaper."),
  ).toBeVisible();
  await expect(
    page.getByText(
      "Local BAS preparation requires confirmed supported obligations and source evidence. Tammy does not declare or lodge a BAS with the ATO.",
    ),
  ).toBeVisible();

  const gstWorkpaper = await windowReportingCapability(
    page,
    ReportKind.GST_WORKPAPER,
    ReportingEntityType.AU_BUSINESS,
    2024,
  );
  expect(gstWorkpaper.capability).toMatchObject({
    report: ReportKind.GST_WORKPAPER,
    taxYear: 2024,
    entityType: ReportingEntityType.AU_BUSINESS,
    status: ReportingCapabilityStatus.AVAILABLE,
  });
  expect(gstWorkpaper.capability?.appVersion).toBeTruthy();
  const bas = await windowReportingCapability(
    page,
    ReportKind.BAS,
    ReportingEntityType.AU_BUSINESS,
    2024,
  );
  expect(bas.capability).toMatchObject({
    report: ReportKind.BAS,
    taxYear: 2024,
    entityType: ReportingEntityType.AU_BUSINESS,
    status: ReportingCapabilityStatus.PREPARATION_ONLY,
  });
  expect(bas.capability?.appVersion).toBe(gstWorkpaper.capability?.appVersion);

  const companyReturn = await windowReportingCapability(
    page,
    ReportKind.COMPANY_TAX_RETURN,
    ReportingEntityType.AU_PRIVATE_COMPANY,
    2026,
  );
  expect(companyReturn.capability).toMatchObject({
    report: ReportKind.COMPANY_TAX_RETURN,
    taxYear: 2026,
    entityType: ReportingEntityType.AU_PRIVATE_COMPANY,
    status: ReportingCapabilityStatus.UNSUPPORTED,
  });
  expect(companyReturn.capability?.appVersion).toBe(gstWorkpaper.capability?.appVersion);
  expect(companyReturn.capability?.modes.map(({ mode }) => mode)).toEqual([
    ReportingCapabilityMode.PREPARATION,
    ReportingCapabilityMode.SIMULATOR,
    ReportingCapabilityMode.EVTE,
    ReportingCapabilityMode.PRODUCTION,
  ]);
  expect(companyReturn.capability?.modes.map(({ availability }) => availability)).toEqual([
    ReportingModeAvailability.NOT_IMPLEMENTED,
    ReportingModeAvailability.NOT_IMPLEMENTED,
    ReportingModeAvailability.NOT_IMPLEMENTED,
    ReportingModeAvailability.NOT_IMPLEMENTED,
  ]);
  expect(companyReturn.capability?.modes.map(({ blockers }) => blockers)).toEqual([
    ["COMPANY_RETURN_PREPARATION_NOT_IMPLEMENTED"],
    ["COMPANY_RETURN_SIMULATOR_NOT_IMPLEMENTED"],
    [
      "COMPANY_RETURN_DELIVERY_NOT_IMPLEMENTED",
      "DSP_REGISTRATION_REQUIRED",
      "OFFICIAL_SERVICE_ARTEFACTS_REQUIRED",
      "EVTE_ACCESS_REQUIRED",
      "CONFORMANCE_REQUIRED",
    ],
    [
      "COMPANY_RETURN_DELIVERY_NOT_IMPLEMENTED",
      "DSP_REGISTRATION_REQUIRED",
      "OFFICIAL_SERVICE_ARTEFACTS_REQUIRED",
      "EVTE_ACCESS_REQUIRED",
      "CONFORMANCE_REQUIRED",
      "PRODUCT_ID_REQUIRED",
      "PRODUCTION_ACCESS_REQUIRED",
      "RAM_MACHINE_CREDENTIAL_REQUIRED",
      "RELEASE_APPROVAL_REQUIRED",
    ],
  ]);
  expect(companyReturn.capability?.modes[0]?.requiredBundleId).toBe(
    "au-company-return-2026-preparation-v1",
  );
  expect(companyReturn.capability?.modes[1]?.requiredBundleId).toBe(
    "au-company-return-2026-preparation-v1",
  );
  expect(companyReturn.capability?.modes[2]?.requiredServiceName).toBe("Company return 2026");
  expect(companyReturn.capability?.modes[3]?.requiredServiceName).toBe("Company return 2026");
  for (const mode of companyReturn.capability?.modes ?? []) {
    expect(isFieldSet(mode, ReportingModeCapabilitySchema.field.activatedBundleFingerprint)).toBe(
      false,
    );
  }

  const bridgeEvidence = await page.evaluate(async () => {
    const bridgeKeys = Object.keys(window.conveyor).sort();
    const manifest = window.conveyor.manifest();
    let unknownCallError = "";
    try {
      await window.conveyor.invoke("conveyor:unknown", "unknown");
    } catch (error) {
      unknownCallError = error instanceof Error ? error.message : String(error);
    }
    let subscriptionError = "";
    try {
      window.conveyor.subscribe("conveyor:system", () => undefined);
    } catch (error) {
      subscriptionError = error instanceof Error ? error.message : String(error);
    }
    return {
      bridgeKeys,
      manifest,
      subscriptionError,
      legacyFacadeType: typeof (window as unknown as Record<string, unknown>)["tammy"],
      unknownCallError,
    };
  });
  expect(bridgeEvidence.bridgeKeys).toEqual(["invoke", "manifest", "subscribe"]);
  expect(Object.keys(bridgeEvidence.manifest).sort()).toEqual([
    "accounting",
    "banking",
    "basPreparation",
    "documents",
    "organisation",
    "purchases",
    "reporting",
    "sbr",
    "system",
    "workspace",
  ]);
  expect(bridgeEvidence.legacyFacadeType).toBe("undefined");
  expect(bridgeEvidence.unknownCallError).toBe("INVALID_CONVEYOR_REQUEST");
  expect(bridgeEvidence.subscriptionError).toBe("IPC_SUBSCRIPTIONS_DISABLED");
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
  const diagnostics = (await invokeConveyorValue(page, "system", "getSystemDiagnostics")) as {
    apiVersion: string;
    networkRequired: boolean;
    runtimeMode: string;
  };
  return {
    apiVersion: diagnostics.apiVersion,
    networkRequired: diagnostics.networkRequired,
    runtimeMode: diagnostics.runtimeMode,
  };
}

async function windowReportingCapability(
  page: import("@playwright/test").Page,
  report: ReportKind,
  entityType: ReportingEntityType,
  taxYear: number,
) {
  const request = reportingCodec.encodeRequest(
    create(GetReportingCapabilityRequestSchema, {
      report,
      taxYear,
      entityType,
    }),
  );
  const response = await invokeConveyorFrame(page, "reporting", "getReportingCapability", request);
  return reportingCodec.decodeResponse(response);
}
