import { createHash } from "node:crypto";
import { create, toBinary } from "@bufbuild/protobuf";
import type { Page } from "@playwright/test";
import {
  BasApplicability,
  BasCadence,
  BasEffectiveIntervalSchema,
  BasGstBasis,
  BasGstMethod,
  BasGstProfileInputSchema,
  BasObligationAnswerSchema,
  BasObligationsProfileInputSchema,
  BasOtherObligationInventorySchema,
  BasPaygiMethod,
  BasPaygiProfileInputSchema,
  BasPaygwCategory,
  BasPaygwProfileInputSchema,
  BasPrefillRequirement,
  BasRegistrationStatus,
  BasTurnoverCategory,
  ListBasObligationsProfilesRequestSchema,
  ListBasObligationsProfilesResponseSchema,
  SetBasObligationsProfileRequestSchema,
  SetBasObligationsProfileResponseSchema,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import {
  AuthenticationContextSchema,
  CommandContextSchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { DocumentCandidateSchema } from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { LegacyPurchaseDisposition } from "@tammy/connect-client/tammy/v1/purchases_pb.js";

import { BAS_PREPARATION_RESPONSE_FRAME_LIMIT } from "../../src/shared/bas-preparation-ipc";
import { createProtoMethodCodec } from "../../src/shared/proto-ipc";
import { expect, test } from "./fixtures";
import {
  openPurchase,
  purchaseKey,
  purchaseProto,
  retainPurchaseThroughUi,
  setupPurchaseWorkspace,
} from "./support/purchase-payments-workflow";

type Workspace = Awaited<ReturnType<typeof setupPurchaseWorkspace>>;
type RetainedEvidence = {
  readonly type: string;
  readonly id: string;
  readonly revision: string;
  readonly hash: string;
};

const setProfileCodec = createProtoMethodCodec({
  input: SetBasObligationsProfileRequestSchema,
  output: SetBasObligationsProfileResponseSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: BAS_PREPARATION_RESPONSE_FRAME_LIMIT,
});

function evidenceFor(
  document: Awaited<ReturnType<typeof retainPurchaseThroughUi>>,
): RetainedEvidence {
  if (
    !document.candidate ||
    !document.id ||
    document.version < 1n ||
    document.sha256.length !== 32
  ) {
    throw new Error("BAS_GUARD_RETAINED_EVIDENCE_MISSING");
  }
  return {
    type: "document",
    id: document.id,
    revision: document.version.toString(),
    // Evidence identities bind the retained candidate revision, not the source PDF bytes.
    hash: createHash("sha256")
      .update(toBinary(DocumentCandidateSchema, document.candidate))
      .digest("hex"),
  };
}

function unknownProfile(reason: string) {
  const unknown = () =>
    create(BasObligationAnswerSchema, { applicability: BasApplicability.UNKNOWN });
  return create(BasObligationsProfileInputSchema, {
    effectiveInterval: create(BasEffectiveIntervalSchema, {
      startDate: { year: 2026, month: 7, day: 1 },
    }),
    gst: create(BasGstProfileInputSchema, {
      applicability: BasApplicability.UNKNOWN,
      registrationStatus: BasRegistrationStatus.UNKNOWN,
      reportingMethod: BasGstMethod.UNKNOWN,
      turnoverCategory: BasTurnoverCategory.UNKNOWN,
      gstBasis: BasGstBasis.UNKNOWN,
      cadence: BasCadence.UNKNOWN,
    }),
    paygw: create(BasPaygwProfileInputSchema, {
      applicability: unknown(),
      category: BasPaygwCategory.UNKNOWN,
      cadence: BasCadence.UNKNOWN,
      prefillRequirement: BasPrefillRequirement.UNKNOWN,
    }),
    paygi: create(BasPaygiProfileInputSchema, {
      applicability: unknown(),
      method: BasPaygiMethod.UNKNOWN,
      cadence: BasCadence.UNKNOWN,
    }),
    otherObligations: create(BasOtherObligationInventorySchema, {
      fringeBenefitsTax: unknown(),
      wineEqualisationTax: unknown(),
      luxuryCarTax: unknown(),
      fuelTaxCredits: unknown(),
      deferredCompanyInstalments: unknown(),
      deferredOrImportGst: unknown(),
      specialGstSchemesOrAdjustments: unknown(),
      otherActivityStatementObligations: unknown(),
    }),
    reason,
  });
}

function profileCommand(workspace: Workspace, idempotencyKey: string, reason: string) {
  return create(SetBasObligationsProfileRequestSchema, {
    commandContext: create(CommandContextSchema, {
      authentication: create(AuthenticationContextSchema, {
        actorUserId: workspace.userId,
        sessionId: workspace.sessionId,
      }),
      idempotencyKey,
    }),
    organisationId: workspace.organisationId,
    expectedVersion: 0n,
    profile: unknownProfile(reason),
  });
}

function hex(bytes: Uint8Array) {
  return Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
}

async function markSupportingDocumentNoncreditable(page: Page, number: string, documentId: string) {
  await openPurchase(page, number);
  const panel = page.getByRole("region", { name: "Purchase accounting", exact: true });
  await panel
    .locator("summary")
    .filter({ hasText: /^Review legacy purchase \/ opening provenance$/ })
    .click();
  for (const [label, date] of [
    ["Legacy affected period start", "2026-07-01"],
    ["Legacy affected period end", "2026-09-30"],
    ["Legacy reporting balance date", "2026-09-30"],
  ] as const) {
    await panel.getByLabel(label, { exact: true }).fill(date);
  }
  await panel
    .getByRole("combobox", { name: "Legacy disposition", exact: true })
    .selectOption(String(LegacyPurchaseDisposition.NONCREDITABLE));
  await panel
    .getByRole("combobox", { name: "Legacy supporting evidence", exact: true })
    .selectOption(documentId);
  await panel
    .getByLabel("Legacy review reason", { exact: true })
    .fill("Supporting evidence only; zero purchase value and zero purchase credit.");
  await panel
    .getByLabel(
      "Reviewed legacy accounting, settlement and claimed status against retained records",
      { exact: true },
    )
    .check();
  await panel.getByRole("button", { name: "Review legacy decision", exact: true }).click();
  await panel.getByRole("button", { name: "Confirm legacy purchase review", exact: true }).click();
  await expect(panel.getByRole("status")).toContainText("Legacy decision retained:");
}

async function fillEvidence(page: Page, evidence: RetainedEvidence) {
  await page.getByLabel("Source type", { exact: true }).first().fill(evidence.type);
  await page.getByLabel("Source UUID", { exact: true }).first().fill(evidence.id);
  await page.getByLabel("Revision", { exact: true }).first().fill(evidence.revision);
  await page.getByLabel("SHA-256 hex", { exact: true }).first().fill(evidence.hash);
}

async function fillEvidenceGroup(page: Page, label: string, evidence: RetainedEvidence) {
  const group = page.getByRole("group", { name: label, exact: true });
  await group.getByLabel("Source type", { exact: true }).fill(evidence.type);
  await group.getByLabel("Source UUID", { exact: true }).fill(evidence.id);
  await group.getByLabel("Revision", { exact: true }).fill(evidence.revision);
  await group.getByLabel("SHA-256 hex", { exact: true }).fill(evidence.hash);
}

async function expectLifecycleMessage(page: Page, message: string) {
  const lifecycle = page.getByRole("region", {
    name: "BAS preparation lifecycle",
    exact: true,
  });
  await expect(lifecycle.getByRole("status").filter({ hasText: message })).toBeVisible();
}

test("profile replay after a lost response is stable and changed payloads conflict", async ({
  electronHarness,
}) => {
  const page = electronHarness.currentPage();
  const workspace = await setupPurchaseWorkspace(electronHarness, "cash");
  const request = profileCommand(workspace, purchaseKey(), "Retried unknown-profile baseline.");
  const requestBytes = setProfileCodec.encodeRequest(request);

  const replayProbe = await page.evaluate(
    async ({ bytes }) => {
      const invoke = async () =>
        (await window.conveyor.invoke(
          "conveyor:basPreparation",
          "setBasObligationsProfile",
          Uint8Array.from(bytes),
        )) as
          | { readonly ok: true; readonly data: Uint8Array }
          | { readonly ok: false; readonly error?: { readonly code?: string } };
      const first = await invoke();
      if (!first.ok || !(first.data instanceof Uint8Array)) {
        throw new Error(first.ok ? "BAS_GUARD_FIRST_RESPONSE_INVALID" : first.error?.code);
      }
      // Simulate a response lost after Core committed: do not return the first result
      // to the Playwright caller; retry the exact same framed command and key.
      const firstResultDiscardedAtCallerBoundary = first.data;
      const retry = await invoke();
      if (!retry.ok || !(retry.data instanceof Uint8Array)) {
        throw new Error(retry.ok ? "BAS_GUARD_REPLAY_RESPONSE_INVALID" : retry.error?.code);
      }
      const retryBytes = Array.from(retry.data);
      return {
        sameResult:
          firstResultDiscardedAtCallerBoundary.length === retryBytes.length &&
          firstResultDiscardedAtCallerBoundary.every((byte, index) => byte === retryBytes[index]),
        responseBytes: retryBytes,
      };
    },
    { bytes: Array.from(requestBytes) },
  );

  expect(replayProbe.sameResult).toBe(true);
  const replayResponse = setProfileCodec.decodeResponse(Uint8Array.from(replayProbe.responseBytes));
  const retainedProfile = replayResponse.profile;
  expect(retainedProfile?.profileId).toMatch(/^[0-9a-f-]{36}$/);
  expect(retainedProfile?.revision).toBe(1n);
  expect(retainedProfile?.organisationId).toBe(workspace.organisationId);

  const changedRequest = profileCommand(
    workspace,
    request.commandContext?.idempotencyKey ?? "",
    "Changed semantic payload under the already committed key.",
  );
  const conflict = await page.evaluate(
    async ({ bytes }) => {
      const envelope = (await window.conveyor.invoke(
        "conveyor:basPreparation",
        "setBasObligationsProfile",
        Uint8Array.from(bytes),
      )) as
        | { readonly ok: true; readonly data: Uint8Array }
        | { readonly ok: false; readonly error?: { readonly code?: string } };
      return { ok: envelope.ok, code: envelope.ok ? "" : (envelope.error?.code ?? "") };
    },
    { bytes: Array.from(setProfileCodec.encodeRequest(changedRequest)) },
  );
  // The packaged BAS transport exposes only the reviewed static status code.
  expect(conflict).toEqual({ ok: false, code: "BAS_IDEMPOTENCY_CONFLICT" });

  const list = await purchaseProto(
    page,
    "basPreparation",
    "listBasObligationsProfiles",
    ListBasObligationsProfilesRequestSchema,
    ListBasObligationsProfilesResponseSchema,
    create(ListBasObligationsProfilesRequestSchema, {
      authentication: create(AuthenticationContextSchema, {
        actorUserId: workspace.userId,
        sessionId: workspace.sessionId,
      }),
      organisationId: workspace.organisationId,
      page: create(PageRequestSchema, { pageSize: 200 }),
    }),
  );
  expect(list.profiles).toHaveLength(1);
  const sourceRef = list.profiles[0]?.profile;
  expect(sourceRef).toMatchObject({
    type: "bas_obligations_profile",
    id: retainedProfile?.profileId,
    revision: 1n,
  });
  expect(hex(sourceRef?.contentHash ?? new Uint8Array())).toBe(
    hex(retainedProfile?.contentHash ?? new Uint8Array()),
  );

  const afterConflict = await purchaseProto(
    page,
    "basPreparation",
    "listBasObligationsProfiles",
    ListBasObligationsProfilesRequestSchema,
    ListBasObligationsProfilesResponseSchema,
    create(ListBasObligationsProfilesRequestSchema, {
      authentication: create(AuthenticationContextSchema, {
        actorUserId: workspace.userId,
        sessionId: workspace.sessionId,
      }),
      organisationId: workspace.organisationId,
      page: create(PageRequestSchema, { pageSize: 200 }),
    }),
  );
  expect(afterConflict.profiles).toHaveLength(1);
  expect(afterConflict.profiles[0]?.profile?.id).toBe(retainedProfile?.profileId);
  expect(hex(afterConflict.profiles[0]?.profile?.contentHash ?? new Uint8Array())).toBe(
    hex(retainedProfile?.contentHash ?? new Uint8Array()),
  );
});

test("unsupported GST method is retained as a specific review finding and cannot be frozen", async ({
  electronHarness,
}) => {
  const page = electronHarness.currentPage();
  const workspace = await setupPurchaseWorkspace(electronHarness, "cash");
  const profileDocument = await retainPurchaseThroughUi(
    page,
    workspace,
    "BAS-GST-UNSUPPORTED-EVIDENCE-001",
    "2025-07-01",
    true,
    false,
    "profile-evidence",
  );
  const dueDateDocument = await retainPurchaseThroughUi(
    page,
    workspace,
    "BAS-GST-UNSUPPORTED-DUE-DATE-001",
    "2026-09-30",
    true,
    false,
    "actual-due-date-evidence",
  );
  await markSupportingDocumentNoncreditable(
    page,
    "BAS-GST-UNSUPPORTED-EVIDENCE-001",
    profileDocument.id,
  );
  await markSupportingDocumentNoncreditable(
    page,
    "BAS-GST-UNSUPPORTED-DUE-DATE-001",
    dueDateDocument.id,
  );
  const profileEvidence = evidenceFor(profileDocument);
  const dueDateEvidence = evidenceFor(dueDateDocument);

  await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  await expect(page.getByRole("heading", { name: "BAS preparation", exact: true })).toBeVisible();
  await page.getByLabel("Effective from", { exact: true }).fill("2026-07-01");
  for (const [label, value] of [
    ["GST obligation", "active"],
    ["GST registration status", "registered"],
    ["GST reporting method", "unsupported"],
    ["GST turnover category", "small"],
    ["GST basis", "cash"],
    ["GST cadence", "quarterly"],
    ["PAYGW obligation", "not applicable"],
    ["PAYGI obligation", "not applicable"],
    ["PAYGI method", "not applicable"],
  ] as const) {
    await page.getByRole("combobox", { name: label, exact: true }).selectOption({ label: value });
  }
  for (const label of [
    "Fringe benefits tax",
    "Wine equalisation tax",
    "Luxury car tax",
    "Fuel tax credits",
    "Deferred company instalments",
    "Deferred or import GST",
    "Special GST schemes or adjustments",
    "Other activity statement obligations",
  ]) {
    await page
      .getByRole("combobox", { name: label, exact: true })
      .selectOption({ label: "not applicable" });
  }
  await page
    .getByLabel("GST decision reason", { exact: true })
    .fill("Reviewed registration; this method is outside supported calculation scope.");
  await page.getByLabel("GST registration effective from", { exact: true }).fill("2025-07-01");
  await page.getByLabel("PAYGW reason", { exact: true }).fill("No PAYG withholding obligation.");
  await page.getByLabel("PAYGI reason", { exact: true }).fill("No PAYG instalment obligation.");
  await fillEvidence(page, profileEvidence);
  await page
    .getByLabel("Profile decision reason", { exact: true })
    .fill("Unsupported-method guard fixture.");
  await page.getByRole("button", { name: "Save immutable profile revision", exact: true }).click();
  await expectLifecycleMessage(page, "Saved BAS obligations profile revision");

  const periodSelect = page.getByRole("combobox", {
    name: "Confirmed period history",
    exact: true,
  });
  await expect
    .poll(async () =>
      (await periodSelect.locator("option").allTextContents()).some(
        (text) =>
          text.startsWith("Suggestion ·") &&
          text.includes("2026-07-01") &&
          text.includes("2026-09-30"),
      ),
    )
    .toBe(true);
  const options = await periodSelect.locator("option").evaluateAll((items) =>
    items.map((item) => ({
      value: (item as HTMLOptionElement).value,
      text: item.textContent ?? "",
    })),
  );
  const suggestion = options.find(
    (item) =>
      item.text.startsWith("Suggestion ·") &&
      item.text.includes("2026-07-01") &&
      item.text.includes("2026-09-30"),
  );
  if (!suggestion) throw new Error("BAS_GUARD_Q3_SUGGESTION_MISSING");
  await periodSelect.selectOption(suggestion.value);
  await page.getByRole("combobox", { name: "Due-date basis", exact: true }).selectOption({
    label: "user recorded external date",
  });
  await page.getByLabel("Confirmed due date", { exact: true }).fill("2026-10-28");
  await fillEvidenceGroup(page, "Actual statement due-date evidence identity", dueDateEvidence);
  await page
    .getByLabel("Period confirmation reason", { exact: true })
    .fill("Synthetic external due-date statement retained for the unsupported-method scope check.");
  await page.getByRole("button", { name: "Confirm selected period", exact: true }).click();
  await expectLifecycleMessage(page, "Confirmed BAS period");

  await page.getByRole("button", { name: "Prepare", exact: true }).click();
  await page.getByRole("button", { name: "Regenerate exact calculation", exact: true }).click();
  await expectLifecycleMessage(page, "Prepared exact BAS version");
  await page.getByRole("button", { name: "Review", exact: true }).click();
  await page
    .getByRole("button", { name: "Run validation on this exact calculation", exact: true })
    .click();
  await expectLifecycleMessage(page, "Created review validation");
  const findings = page
    .getByRole("region", { name: "BAS preparation lifecycle", exact: true })
    .getByRole("heading", { name: "Review findings", exact: true })
    .locator("xpath=..");
  await expect(findings.getByText(/GST_METHOD_UNSUPPORTED/)).toBeVisible();
  await expect(findings.getByText(/Unsupported · GST_METHOD_UNSUPPORTED/)).toBeVisible();

  await page.getByRole("button", { name: "Freeze", exact: true }).click();
  const freezeStage = page.getByRole("region", { name: "BAS preparation lifecycle", exact: true });
  await expect(freezeStage.getByText(/retained state Blocked/)).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Approve and freeze exact snapshot", exact: true }),
  ).toBeDisabled();
});
