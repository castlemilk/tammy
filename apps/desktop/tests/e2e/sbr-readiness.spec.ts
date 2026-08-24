import { createHash } from "node:crypto";
import { chmod, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

import { create } from "@bufbuild/protobuf";
import type { Page } from "@playwright/test";
import { AuthenticationContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  GetMachineCredentialStatusRequestSchema,
  GetMachineCredentialStatusResponseSchema,
  MachineCredentialState,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";

import { createProtoMethodCodec } from "../../src/shared/proto-ipc";
import { expect, test } from "./fixtures";
import {
  CURRENT_WORKFLOW_ADMIN_PASSWORD,
  CURRENT_WORKFLOW_PASSPHRASE,
  CURRENT_WORKFLOW_USERNAME,
  setupAndRunCurrentAccountingWorkflow,
} from "./support/current-accounting-workflow";
import { generateTotp } from "./support/totp";

const FIXTURE_BYTES =
  '{"fixture_id":"SIM-SBR-READINESS-V1","organisation_name":"Wattle & Co Test Pty Ltd","abn":"11 000 000 560","service_id":"SIM.READINESS.0001","clock":"2026-06-30T00:00:00Z","message_id":"SIM.MSG.0001","conversation_id":"SIM.CONV.0001","receipt":"SIM-READY-0001"}\n';
const SBR_OPERATION_UI_TIMEOUT_MS = 75_000;
const credentialStatusCodec = createProtoMethodCodec({
  input: GetMachineCredentialStatusRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetMachineCredentialStatusResponseSchema,
});

interface TotpClock {
  counter: number;
  readonly secret: string;
}

async function navigate(page: Page, destination: string): Promise<void> {
  await page.evaluate((target) => {
    window.history.replaceState(null, "", target);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, destination);
  await expect(page).toHaveURL(
    new RegExp(`${destination.replaceAll("/", "\\/").replace("?", "\\?")}$`),
  );
}

async function navigateToSbrThroughSettings(page: Page): Promise<void> {
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  await expect(page).toHaveURL(/\/settings$/);
  await page.getByRole("link", { name: "SBR readiness", exact: true }).click();
  await expect(page).toHaveURL(/\/settings\/sbr$/);
}

async function nextFreshCode(clock: TotpClock): Promise<string> {
  while (Math.floor(Date.now() / 30_000) <= clock.counter) {
    await new Promise<void>((resolve) => setTimeout(resolve, 250));
  }
  clock.counter = Math.floor(Date.now() / 30_000);
  return generateTotp(clock.secret);
}

async function unlockPrimary(page: Page): Promise<void> {
  await navigate(page, "/unlock");
  await page.getByLabel("Workspace passphrase").fill(CURRENT_WORKFLOW_PASSPHRASE);
  await page.getByLabel("Email or username").fill(CURRENT_WORKFLOW_USERNAME);
  await page.getByLabel("Administrator password").fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Unlock workspace" }).click();
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
}

async function runSimulatorCase(
  page: Page,
  clock: TotpClock,
  label: string,
  expected: string,
): Promise<void> {
  await page.getByLabel("Test-only diagnostic case").selectOption({ label });
  await page.getByLabel("Fresh six-digit code").fill(await nextFreshCode(clock));
  await page.getByRole("button", { name: "Run simulator fixture" }).click();
  await expect(page.getByText(expected, { exact: true })).toBeVisible({
    timeout: SBR_OPERATION_UI_TIMEOUT_MS,
  });
}

async function refreshAfterUncertain(page: Page): Promise<void> {
  await page.getByRole("button", { name: "Refresh authoritative status" }).click();
  await expect(page.getByRole("heading", { name: "SBR readiness", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Run simulator fixture" })).toBeVisible();
}

async function setupSecondaryWorkspace(page: Page) {
  await navigate(page, "/setup/workspace");
  await page.getByLabel("Your name").fill("Secondary Tammy Admin");
  await page.getByLabel("Email or username").fill("secondary@tammy.local");
  await page.getByLabel("Business legal name").fill("Wattle & Co Test Pty Ltd");
  await page.getByLabel("Business display name").fill("Wattle & Co Test Pty Ltd");
  await page.getByLabel("ABN").fill("11000000560");
  await page.getByLabel("Workspace passphrase").fill("secondary-workspace-passphrase");
  await page.getByLabel("Administrator password").fill("secondary-administrator-password");
  await page.getByRole("button", { name: "Create local workspace" }).click();
  await expect(page.getByRole("heading", { name: "Save your recovery code" })).toBeVisible();
  await page.getByRole("button", { name: "I saved my recovery code" }).click();
  await expect(page.getByRole("heading", { name: "Overview" })).toBeVisible();
  return page.evaluate(() => {
    const retained = window.sessionStorage.getItem("tammy.session.active");
    if (!retained) throw new Error("SECONDARY_WORKSPACE_MISSING");
    return JSON.parse(retained) as {
      organisationId: string;
      sessionId: string;
      userId: string;
      workspaceId: string;
    };
  });
}

async function credentialAction(
  page: Page,
  clock: TotpClock,
  credentialPassword: string,
  action: "Unlock for local use" | "Remove credential",
): Promise<void> {
  await page.getByRole("button", { name: action }).click();
  if (action === "Unlock for local use") {
    await page.getByLabel("Credential password").fill(credentialPassword);
  } else {
    await page.getByLabel(/Remove the credential for/).check();
  }
  await page.getByLabel("Fresh six-digit code").fill(await nextFreshCode(clock));
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(
    page.getByText(
      action === "Unlock for local use"
        ? "Credential unlocked for local use. No network request was started."
        : "Credential status updated.",
      { exact: true },
    ),
  ).toBeVisible({ timeout: SBR_OPERATION_UI_TIMEOUT_MS });
}

test("SBR readiness uses local RAM credential and deterministic simulator only", async ({
  electronHarness,
}, testInfo) => {
  test.setTimeout(480_000);
  expect(electronHarness.packagedLayout.target).toBe("darwin-arm64");
  expect(electronHarness.packagedLayout.releaseKind).toBe("ordinary-package");
  const profileSha256 = electronHarness.packagedLayout.profileSha256;
  if (!profileSha256) throw new Error("PACKAGED_PROFILE_HASH_MISSING");
  const profileFingerprint = electronHarness.packagedLayout.profileFingerprint;
  if (!profileFingerprint) throw new Error("PACKAGED_PROFILE_FINGERPRINT_MISSING");
  const installationAuthority = {
    appSha256: electronHarness.packagedLayout.appSha256,
    helperSha256: electronHarness.packagedLayout.helperSha256,
    profileSha256,
  };
  const assets = path.resolve(import.meta.dirname, "assets");
  const credentialPath = path.join(assets, "synthetic-machine-credential.p12");
  const credentialBytes = await readFile(credentialPath);
  const credentialSha256 = createHash("sha256").update(credentialBytes).digest("hex");
  const chooserAuthority = testInfo.outputPath("chooser-authority");
  const selectedCredentialPath = path.join(chooserAuthority, "synthetic-machine-credential.p12");
  await mkdir(chooserAuthority, { mode: 0o700, recursive: true });
  await chmod(chooserAuthority, 0o700);
  await writeFile(selectedCredentialPath, credentialBytes, { mode: 0o600 });
  await chmod(selectedCredentialPath, 0o600);
  const evidencePath = path.join(assets, "synthetic-abr-evidence.pdf");
  const metadata: unknown = JSON.parse(
    await readFile(path.join(assets, "synthetic-machine-credential.fixture.json"), "utf8"),
  );
  expect(metadata).toEqual(
    expect.objectContaining({
      password: expect.stringMatching(/^[a-z0-9-]{16,64}$/),
    }),
  );
  if (
    !metadata ||
    typeof metadata !== "object" ||
    !("password" in metadata) ||
    typeof metadata.password !== "string"
  ) {
    throw new Error("SYNTHETIC_CREDENTIAL_METADATA_INVALID");
  }
  const credentialPassword = metadata.password;
  expect({ ...metadata, password: "[test-only password redacted]" }).toEqual({
    schema: "tammy-sbr-synthetic-credential-fixture-v1",
    password: "[test-only password redacted]",
    canonical_abn: "11000000560",
    expires_at: "2030-06-30T00:00:00.000Z",
    component_version: "tammy-sbr-simulator-v1",
  });

  const workspace = await setupAndRunCurrentAccountingWorkflow(
    electronHarness.page,
    electronHarness,
  );
  let page = electronHarness.currentPage();

  await navigate(page, "/settings/organisation");
  await expect(page.getByRole("heading", { name: "Organisation", exact: true })).toBeVisible();
  await page.getByLabel("Independent evidence").setInputFiles(evidencePath);
  await page.getByRole("button", { name: "Record verification" }).click();
  await expect(
    page.getByText("Entity verification evidence recorded.", { exact: true }),
  ).toBeVisible();

  await navigateToSbrThroughSettings(page);
  await expect(page.getByRole("heading", { name: "SBR readiness", exact: true })).toBeVisible();
  await page.getByLabel("Current administrator password").fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Begin TOTP setup" }).click();
  const secret = await page.getByTestId("totp-provisioning-material").textContent();
  expect(secret).toMatch(/^[A-Z2-7]{32}$/);
  if (!secret) throw new Error("TOTP_PROVISIONING_MATERIAL_MISSING");
  const confirmationTime = Date.now() - 30_000;
  const confirmCounter = Math.floor(confirmationTime / 30_000);
  await page.getByLabel("Six-digit code").fill(generateTotp(secret, confirmationTime));
  await page.getByRole("button", { name: "Confirm security code" }).click();
  await expect(page.getByRole("button", { name: "Import credential" })).toBeVisible();
  const totp: TotpClock = { counter: confirmCounter, secret };
  await page.getByRole("button", { name: "Import credential" }).click();
  await electronHarness.injectMachineCredentialSelection(selectedCredentialPath);
  await page.getByRole("button", { name: "Choose credential in macOS" }).click();
  await expect(page.getByText(/filename is not retained or shown/i)).toBeVisible();
  await page.getByLabel("Credential password").fill(credentialPassword);
  await page.getByLabel("Fresh six-digit code").fill(await nextFreshCode(totp));
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByText("Credential status updated.", { exact: true })).toBeVisible({
    timeout: SBR_OPERATION_UI_TIMEOUT_MS,
  });
  await expect(page.getByText("Present", { exact: true }).first()).toBeVisible();
  await expect(page.getByText(credentialSha256, { exact: true })).toBeVisible();
  await expect(page.getByText(profileFingerprint, { exact: true })).toBeVisible();
  const rendered = await page.locator("body").textContent();
  expect(rendered).not.toContain(selectedCredentialPath);
  expect(rendered).not.toContain(path.basename(selectedCredentialPath));
  expect(rendered).not.toContain(credentialPassword);

  await navigate(page, "/settings/sbr?doctor=1");
  await expect(page.getByRole("heading", { name: "Ready for simulator" })).toBeVisible();
  await expect(page.getByRole("button", { name: /doctor/i })).toHaveCount(0);
  const simulator = page.getByRole("heading", { name: "SBR readiness simulator" }).locator("..");
  const simulatorText = await simulator.textContent();
  for (const forbidden of ["Officeworks", "$319.00", "G1", "1A", "1B"]) {
    expect(simulatorText).not.toContain(forbidden);
  }

  await runSimulatorCase(page, totp, "Accepted", "ACCEPTED");
  await page.getByRole("button", { name: "Replay exact request" }).click();
  await expect(page.getByText("EXACT_REPLAY", { exact: true })).toBeVisible();
  await page.getByLabel("Test-only diagnostic case").selectOption({ label: "Not started" });
  await page.getByRole("button", { name: "Check idempotency conflict" }).click();
  await expect(page.getByText("IDEMPOTENCY_CONFLICT", { exact: true })).toBeVisible();
  await runSimulatorCase(page, totp, "Not started", "NOT_STARTED");
  await runSimulatorCase(page, totp, "Maybe sent", "MAYBE_SENT");
  await refreshAfterUncertain(page);
  await runSimulatorCase(page, totp, "Malformed response", "MALFORMED_RESPONSE");
  await refreshAfterUncertain(page);
  await runSimulatorCase(page, totp, "Helper death", "HELPER_DEATH");

  page = await electronHarness.restart();
  await unlockPrimary(page);
  await navigate(page, "/settings/sbr");
  await expect(page.getByText("Present", { exact: true }).first()).toBeVisible();
  await runSimulatorCase(page, totp, "Helper death", "UNKNOWN");
  await refreshAfterUncertain(page);
  await runSimulatorCase(page, totp, "Timeout", "TIMEOUT");

  page = await electronHarness.switchUserDataRoot("secondary");
  expect({
    appSha256: electronHarness.packagedLayout.appSha256,
    helperSha256: electronHarness.packagedLayout.helperSha256,
    profileSha256: electronHarness.packagedLayout.profileSha256,
  }).toEqual(installationAuthority);
  const secondary = await setupSecondaryWorkspace(page);
  expect(secondary.workspaceId).not.toBe(workspace.workspaceId);
  const statusFrame = credentialStatusCodec.encodeRequest(
    create(GetMachineCredentialStatusRequestSchema, {
      authentication: create(AuthenticationContextSchema, {
        actorUserId: secondary.userId,
        sessionId: secondary.sessionId,
      }),
    }),
  );
  const secondaryStatusBytes = await page.evaluate(
    async (bytes) => {
      return [...(await window.tammy.getMachineCredentialStatus(Uint8Array.from(bytes)))];
    },
    [...statusFrame],
  );
  statusFrame.fill(0);
  const secondaryStatus = credentialStatusCodec.decodeResponse(
    Uint8Array.from(secondaryStatusBytes),
  );
  expect(secondaryStatus.credentialStatus?.state).toBe(MachineCredentialState.MISSING);
  await navigate(page, "/settings/sbr");
  await expect(page.getByRole("button", { name: "Run simulator fixture" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Unlock for local use" })).toHaveCount(0);
  await expect(page.getByText(/machine credential is required/i)).toBeVisible();

  page = await electronHarness.usePrimaryUserDataRoot();
  await unlockPrimary(page);
  await navigate(page, "/settings/sbr");
  await expect(page.getByText(credentialSha256, { exact: true })).toBeVisible();
  await credentialAction(page, totp, credentialPassword, "Unlock for local use");

  await page.getByRole("button", { name: "Replace credential" }).click();
  await electronHarness.injectMachineCredentialSelection(credentialPath);
  await page.getByRole("button", { name: "Choose credential in macOS" }).click();
  await page.getByLabel(/Replace the credential for/).check();
  await page.getByLabel("Credential password").fill(credentialPassword);
  await page.getByLabel("Fresh six-digit code").fill(await nextFreshCode(totp));
  await page.getByRole("button", { name: "Continue" }).click();
  await expect(page.getByText("Credential status updated.", { exact: true })).toBeVisible({
    timeout: SBR_OPERATION_UI_TIMEOUT_MS,
  });
  await credentialAction(page, totp, credentialPassword, "Remove credential");
  await expect(page.getByText("Missing", { exact: true }).first()).toBeVisible();

  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
  electronHarness.markSbrPassed(createHash("sha256").update(FIXTURE_BYTES).digest("hex"));
});
