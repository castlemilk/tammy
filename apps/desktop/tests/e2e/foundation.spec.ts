import { create, isFieldSet } from "@bufbuild/protobuf";
import {
  ApprovedFileRefSchema,
  SecretInputSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
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
import {
  ConfirmRecoveryRequestSchema,
  ConfirmRecoveryResponseSchema,
  CreateWorkspaceRequestSchema,
  CreateWorkspaceResponseSchema,
  RecoveryGroupConfirmationSchema,
  WorkspaceState,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
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

  await page.emulateMedia({ forcedColors: "none" });
  const scrollbars = await page.evaluate(() => {
    const region = document.createElement("div");
    region.style.overflow = "auto";
    region.style.width = "20px";
    region.style.height = "20px";
    const content = document.createElement("div");
    content.style.width = "100px";
    content.style.height = "100px";
    region.append(content);
    document.body.append(region);
    const colors = {
      root: getComputedStyle(document.documentElement).scrollbarColor,
      region: getComputedStyle(region).scrollbarColor,
    };
    region.remove();
    return colors;
  });
  expect(scrollbars.root).not.toBe("auto");
  expect(scrollbars.region).toBe(scrollbars.root);
  await page.emulateMedia({ forcedColors: "active" });
  expect(await page.evaluate(() => getComputedStyle(document.documentElement).scrollbarColor)).toBe(
    "auto",
  );
  await page.emulateMedia({ forcedColors: "none" });

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

const createWorkspaceCodec = createProtoMethodCodec({
  input: CreateWorkspaceRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 65_536,
  output: CreateWorkspaceResponseSchema,
});
const confirmRecoveryCodec = createProtoMethodCodec({
  input: ConfirmRecoveryRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: ConfirmRecoveryResponseSchema,
});

test.describe("supported workspace contract qualification", () => {
  // The fixture traces only retry 1. This secret-bearing synthetic contract case
  // must fail directly, without recording response frames in a retry trace.
  test.describe.configure({ retries: 0 });

  test("foundation/workspace-recovery-confirmation: qualifies pending recovery to unauthenticated through the packaged IPC boundary", async ({
    electronHarness,
  }) => {
    const { page } = electronHarness;
    await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();
    const setupId = "018f0000-0000-7000-8000-000000000f01";
    const request = createWorkspaceCodec.encodeRequest(
      create(CreateWorkspaceRequestSchema, {
        setupId,
        destination: create(ApprovedFileRefSchema, { capabilityId: "local-workspace-directory" }),
        workspacePassphrase: create(SecretInputSchema, {
          utf8: new TextEncoder().encode("synthetic-qualification-workspace-passphrase"),
        }),
        administratorUsername: "qualification-admin@tammy.invalid",
        administratorDisplayName: "Synthetic qualification owner",
        administratorPassword: create(SecretInputSchema, {
          utf8: new TextEncoder().encode("synthetic-qualification-administrator-password"),
        }),
      }),
    );
    const invoke = (method: "createWorkspace" | "confirmRecovery", frame: Uint8Array) =>
      invokeConveyorFrame(page, "workspace", method, frame);
    const created = createWorkspaceCodec.decodeResponse(await invoke("createWorkspace", request));
    const secret = created.recoverySecret?.utf8;
    const confirmationValues: Uint8Array[] = [];
    let replaySecret: Uint8Array | undefined;
    let confirmation: Uint8Array | undefined;
    let incorrect: Uint8Array | undefined;
    try {
      expect(created.workspace?.state).toBe(WorkspaceState.PENDING_RECOVERY);
      const workspaceId = created.workspace?.id;
      if (!workspaceId || !secret?.byteLength)
        throw new Error("SYNTHETIC_SETUP_PROJECTION_MISSING");
      const groups = new TextDecoder()
        .decode(secret)
        .replaceAll("-", "")
        .toUpperCase()
        .match(/.{1,4}/g);
      if (!groups || groups.length < 2) throw new Error("SYNTHETIC_RECOVERY_FORMAT_INVALID");
      confirmationValues.push(
        ...groups.slice(0, 2).map((value) => new TextEncoder().encode(value)),
      );
      const confirmationRequest = create(ConfirmRecoveryRequestSchema, {
        setupId,
        confirmations: confirmationValues.map((value, groupIndex) =>
          create(RecoveryGroupConfirmationSchema, { groupIndex, value }),
        ),
      });
      confirmation = confirmRecoveryCodec.encodeRequest(confirmationRequest);
      const firstGroup = groups[0];
      if (!firstGroup) throw new Error("SYNTHETIC_RECOVERY_GROUP_MISSING");
      const wrongGroup = `${firstGroup.startsWith("A") ? "B" : "A"}${firstGroup.slice(1)}`;
      incorrect = confirmRecoveryCodec.encodeRequest(
        create(ConfirmRecoveryRequestSchema, {
          setupId,
          confirmations: [
            create(RecoveryGroupConfirmationSchema, {
              groupIndex: 0,
              value: new TextEncoder().encode(wrongGroup),
            }),
            ...confirmationRequest.confirmations.slice(1),
          ],
        }),
      );
      await expect(invoke("confirmRecovery", incorrect)).rejects.toThrow("CORE_REQUEST_FAILED");
      const replay = createWorkspaceCodec.decodeResponse(await invoke("createWorkspace", request));
      replaySecret = replay.recoverySecret?.utf8;
      expect(replay.workspace?.id).toBe(workspaceId);
      expect(replay.workspace?.state).toBe(WorkspaceState.PENDING_RECOVERY);
      expect(replay.workspace?.version).toBe(created.workspace?.version);
      const confirmed = confirmRecoveryCodec.decodeResponse(
        await invoke("confirmRecovery", confirmation),
      );
      expect(confirmed.workspace?.id).toBe(workspaceId);
      expect(confirmed.workspace?.state).toBe(WorkspaceState.UNAUTHENTICATED);
      const confirmedReplay = confirmRecoveryCodec.decodeResponse(
        await invoke("confirmRecovery", confirmation),
      );
      expect(confirmedReplay.workspace?.id).toBe(workspaceId);
      expect(confirmedReplay.workspace?.state).toBe(WorkspaceState.UNAUTHENTICATED);
      expect(confirmedReplay.workspace?.version).toBe(confirmed.workspace?.version);
      expect(electronHarness.consoleErrors).toEqual([]);
      expect(electronHarness.pageErrors).toEqual([]);
    } finally {
      for (const value of confirmationValues) value.fill(0);
      request.fill(0);
      secret?.fill(0);
      replaySecret?.fill(0);
      confirmation?.fill(0);
      incorrect?.fill(0);
    }
  });
});
