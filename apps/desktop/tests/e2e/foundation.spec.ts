import { createHash } from "node:crypto";
import { chmod, mkdir, stat, writeFile } from "node:fs/promises";
import path from "node:path";
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
  await expect(page.getByLabel("Workspace passphrase", { exact: true })).toHaveAttribute(
    "type",
    "password",
  );
  await expect(page.getByLabel("Administrator password", { exact: true })).toHaveAttribute(
    "type",
    "password",
  );
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
  await expect(
    page.getByRole("button", { name: "Privacy and support", exact: true }),
  ).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByLabel("Your name")).toBeFocused();

  expect(consumeExpectedCspViolations(consoleErrors, probeUrls)).toEqual([]);
  expect(pageErrors).toEqual([]);
});

test("foundation/onboarding-feedback: validates the first-run form and captures private setup guidance", async ({
  electronHarness,
}) => {
  const { application, page, packagedLayout } = electronHarness;
  expect(packagedLayout.sourceRevision).toMatch(/^[0-9a-f]{40}$/);
  await page.setViewportSize({ width: 1440, height: 900 });
  await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();

  const secretInputs = page.locator('input[autocomplete="new-password"]');
  await expect(secretInputs).toHaveCount(4);
  expect(await newCredentialInputTypes(page)).toEqual([
    "password",
    "password",
    "password",
    "password",
  ]);
  expect(
    await secretInputs.evaluateAll((inputs) =>
      inputs.some((input) => (input as HTMLInputElement).value.length > 0),
    ),
  ).toBe(false);

  const evidenceDirectory = path.resolve(
    import.meta.dirname,
    "../../../../.tmp/ui-feedback-2026-10-23",
    packagedLayout.sourceRevision,
  );
  const evidenceParent = path.dirname(evidenceDirectory);
  await mkdir(evidenceParent, { recursive: true, mode: 0o700 });
  await chmod(evidenceParent, 0o700);
  await mkdir(evidenceDirectory, { recursive: true, mode: 0o700 });
  await chmod(evidenceDirectory, 0o700);
  const captures = [await savePrivateOnboardingCapture(page, evidenceDirectory, "setup-empty.png")];

  for (const width of [1440, 900, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    const documentSize = await page.evaluate(() => ({
      bodyScrollWidth: document.body.scrollWidth,
      clientWidth: document.documentElement.clientWidth,
      documentScrollWidth: document.documentElement.scrollWidth,
    }));
    expect(documentSize.documentScrollWidth, `document overflow at ${width}px`).toBeLessThanOrEqual(
      documentSize.clientWidth,
    );
    expect(documentSize.bodyScrollWidth, `body overflow at ${width}px`).toBeLessThanOrEqual(
      documentSize.clientWidth,
    );
    expect(await newCredentialInputTypes(page), `unmasked input at ${width}px`).toEqual([
      "password",
      "password",
      "password",
      "password",
    ]);
  }

  await page.emulateMedia({ reducedMotion: "reduce" });
  expect(await page.evaluate(() => matchMedia("(prefers-reduced-motion: reduce)").matches)).toBe(
    true,
  );
  expect(await page.evaluate(() => getComputedStyle(document.documentElement).scrollBehavior)).toBe(
    "auto",
  );

  const workspacePassphrase = page.getByLabel("Workspace passphrase", { exact: true });
  const workspacePassphraseConfirmation = page.getByLabel("Confirm workspace passphrase", {
    exact: true,
  });
  await page.getByRole("button", { name: "Show workspace passphrase" }).focus();
  await page.keyboard.press("Enter");
  await expect(workspacePassphrase).toHaveAttribute("type", "text");
  await expect(workspacePassphraseConfirmation).toHaveAttribute("type", "password");
  await page.getByRole("button", { name: "Hide workspace passphrase" }).focus();
  await page.keyboard.press("Enter");
  await expect(workspacePassphrase).toHaveAttribute("type", "password");
  await page.getByRole("button", { name: "Show confirm workspace passphrase" }).focus();
  await page.keyboard.press("Enter");
  await expect(workspacePassphraseConfirmation).toHaveAttribute("type", "text");
  await expect(workspacePassphrase).toHaveAttribute("type", "password");
  await page.getByRole("button", { name: "Hide confirm workspace passphrase" }).focus();
  await page.keyboard.press("Enter");
  await expect(workspacePassphraseConfirmation).toHaveAttribute("type", "password");

  await page.getByRole("button", { name: "Privacy and support" }).click();
  await expect(page.getByRole("heading", { name: "Privacy and support", level: 1 })).toBeVisible();
  await expect(secretInputs).toHaveCount(0);
  const privacyContents = page.getByRole("navigation", { name: "Privacy policy contents" });
  await expect(privacyContents.getByRole("link", { name: "ATO, RAM, and SBR" })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Files, reports, and local integrations" }),
  ).toBeVisible();
  await expect(
    page.getByRole("heading", { name: "Access, correction, and privacy complaints" }),
  ).toBeVisible();
  captures.push(await savePrivateOnboardingCapture(page, evidenceDirectory, "privacy-policy.png"));

  const routeBeforeContents = page.url();
  const ramHeading = page.getByRole("heading", { name: "ATO, RAM, and SBR", level: 2 });
  await privacyContents.getByRole("link", { name: "ATO, RAM, and SBR" }).click();
  await expect(ramHeading).toBeFocused();
  await expect(ramHeading).toBeInViewport();
  await expect(page.locator("#privacy-ato-ram-and-sbr")).toContainText(
    "Relationship Authorisation Manager",
  );
  const integrationsHeading = page.getByRole("heading", {
    name: "Files, reports, and local integrations",
    level: 2,
  });
  await privacyContents
    .getByRole("link", { name: "Files, reports, and local integrations" })
    .click();
  await expect(integrationsHeading).toBeFocused();
  await expect(integrationsHeading).toBeInViewport();
  await expect(page.locator("#privacy-files-reports-and-local-integrations")).toContainText(
    "Local API and MCP access is off",
  );
  const complaintsHeading = page.getByRole("heading", {
    name: "Access, correction, and privacy complaints",
    level: 2,
  });
  await privacyContents
    .getByRole("link", { name: "Access, correction, and privacy complaints" })
    .click();
  await expect(complaintsHeading).toBeFocused();
  await expect(complaintsHeading).toBeInViewport();
  expect(await page.evaluate(() => window.scrollY)).toBeGreaterThan(0);
  await expect(page.locator("#privacy-access-correction-and-privacy-complaints")).toContainText(
    "privacy complaints",
  );
  expect(page.url()).toBe(routeBeforeContents);
  await page.getByRole("button", { name: "Back to Tammy" }).click();
  await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();
  await expect(secretInputs).toHaveCount(4);
  expect(await newCredentialInputTypes(page)).toEqual([
    "password",
    "password",
    "password",
    "password",
  ]);
  expect(
    await secretInputs.evaluateAll((inputs) =>
      inputs.some((input) => (input as HTMLInputElement).value.length > 0),
    ),
  ).toBe(false);
  await page.evaluate(() => window.scrollTo(0, 0));

  await installAbnExternalOpenProbe(application);
  try {
    await page.getByLabel("Your name").fill("Tammy Setup Owner");
    await page.getByLabel("Email or username").fill("setup-owner@tammy.invalid");
    await page.getByLabel("Business legal name").fill("Tammy Example Pty Ltd");
    await page.getByLabel("Business display name").fill("Tammy Example");
    await page.getByLabel("Workspace passphrase", { exact: true }).fill("short");
    await page.getByLabel("Confirm workspace passphrase", { exact: true }).fill("different");
    await page.getByLabel("Administrator password", { exact: true }).fill("Local-admin-2026!");
    await page
      .getByLabel("Confirm administrator password", { exact: true })
      .fill("Local-admin-2026!");
    await page.getByLabel("ABN").fill("51824753556");

    const createButton = page.getByRole("button", { name: "Create local workspace" });
    await expect(page.getByText("Use 15–128 characters.")).toBeVisible();
    await expect(page.getByText("Passwords do not match yet.")).toBeVisible();
    await expect(createButton).toBeDisabled();
    await expect(
      page.getByRole("link", { name: "Look up this ABN on the Australian Business Register" }),
    ).toBeVisible();
    expect(await readAbnExternalCalls(application)).toEqual([]);

    const decomposedPassphrase = "e\u0301".repeat(15);
    await page.getByLabel("Workspace passphrase", { exact: true }).fill(decomposedPassphrase);
    await page.getByLabel("Confirm workspace passphrase", { exact: true }).fill("é".repeat(15));
    await expect(page.getByText("Passwords match.").first()).toBeVisible();
    await expect(page.getByText("Format checks pass.").first()).toBeVisible();
    await expect(createButton).toBeEnabled();

    await page.getByLabel("ABN").fill("51824753557");
    await expect(
      page.getByText("This number does not pass the ABN checksum. Check it against the register."),
    ).toBeVisible();
    await expect(
      page.getByRole("link", { name: "Look up this ABN on the Australian Business Register" }),
    ).toHaveCount(0);
    await expect(createButton).toBeDisabled();
    expect(await readAbnExternalCalls(application)).toEqual([]);

    const abnInput = page.getByLabel("ABN");
    await abnInput.fill("");
    await abnInput.focus();
    await page.keyboard.insertText("52 682 863 690");
    await expect(abnInput).toHaveValue("52682863690");
    const lookupLink = page.getByRole("link", {
      name: "Look up this ABN on the Australian Business Register",
    });
    await expect(lookupLink).toHaveAttribute(
      "href",
      "https://abr.business.gov.au/ABN/View?abn=52682863690",
    );
    await expect(lookupLink).toHaveAttribute("target", "_blank");
    await expect(lookupLink).toHaveAttribute("rel", "noopener noreferrer");
    await expect(createButton).toBeEnabled();
    expect(await readAbnExternalCalls(application)).toEqual([]);

    await lookupLink.click();
    await expect
      .poll(async () => readAbnExternalCalls(application))
      .toEqual(["https://abr.business.gov.au/ABN/View?abn=52682863690"]);
    await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();
  } finally {
    await restoreAbnExternalOpen(application);
  }

  const manifest = {
    captures,
    sourceRevision: packagedLayout.sourceRevision,
    target: packagedLayout.target,
  };
  const manifestPath = path.join(evidenceDirectory, "manifest.json");
  await writePrivateFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
  if (process.platform !== "win32") {
    expect((await stat(evidenceParent)).mode & 0o777).toBe(0o700);
    expect((await stat(evidenceDirectory)).mode & 0o777).toBe(0o700);
    expect((await stat(manifestPath)).mode & 0o777).toBe(0o600);
    for (const capture of captures) {
      expect((await stat(path.join(evidenceDirectory, capture.fileName))).mode & 0o777).toBe(0o600);
    }
  }

  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});

async function newCredentialInputTypes(page: import("@playwright/test").Page) {
  return page
    .locator('input[autocomplete="new-password"]')
    .evaluateAll((inputs) => inputs.map((input) => (input as HTMLInputElement).type));
}

async function savePrivateOnboardingCapture(
  page: import("@playwright/test").Page,
  directory: string,
  fileName: string,
) {
  const bytes = await page.screenshot({ animations: "disabled", fullPage: true });
  await writePrivateFile(path.join(directory, fileName), bytes);
  const dimensions = await page.evaluate(() => ({
    height: document.documentElement.scrollHeight,
    width: document.documentElement.scrollWidth,
  }));
  return {
    bytes: bytes.length,
    fileName,
    height: dimensions.height,
    sha256: createHash("sha256").update(bytes).digest("hex"),
    width: dimensions.width,
  };
}

async function writePrivateFile(filePath: string, content: string | Buffer) {
  await writeFile(filePath, content, { mode: 0o600 });
  await chmod(filePath, 0o600);
}

async function installAbnExternalOpenProbe(
  application: import("@playwright/test").ElectronApplication,
) {
  await application.evaluate(({ shell }) => {
    const target = globalThis as typeof globalThis & {
      __tammyOnboardingAbnHandoffs?: string[];
      __tammyOriginalOpenExternal?: typeof shell.openExternal;
    };
    if (target.__tammyOriginalOpenExternal) throw new Error("ABN_HANDOFF_PROBE_ALREADY_ACTIVE");
    target.__tammyOriginalOpenExternal = shell.openExternal;
    target.__tammyOnboardingAbnHandoffs = [];
    shell.openExternal = async (url) => {
      target.__tammyOnboardingAbnHandoffs?.push(url);
    };
  });
}

async function readAbnExternalCalls(
  application: import("@playwright/test").ElectronApplication,
): Promise<string[]> {
  return application.evaluate(() => {
    const target = globalThis as typeof globalThis & {
      __tammyOnboardingAbnHandoffs?: string[];
    };
    return [...(target.__tammyOnboardingAbnHandoffs ?? [])];
  });
}

async function restoreAbnExternalOpen(application: import("@playwright/test").ElectronApplication) {
  await application.evaluate(({ shell }) => {
    const target = globalThis as typeof globalThis & {
      __tammyOnboardingAbnHandoffs?: string[];
      __tammyOriginalOpenExternal?: typeof shell.openExternal;
    };
    if (target.__tammyOriginalOpenExternal) shell.openExternal = target.__tammyOriginalOpenExternal;
    delete target.__tammyOnboardingAbnHandoffs;
    delete target.__tammyOriginalOpenExternal;
  });
}

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
