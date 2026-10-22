import { createHash } from "node:crypto";
import { copyFile, mkdir, readFile, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { create, toBinary } from "@bufbuild/protobuf";
import type { Page, TestInfo } from "@playwright/test";
import {
  BasDisputeOverlayState,
  BasExternalEventKind,
  BasExternalTrust,
  type BasFrozenBas,
  BasFrozenBasSchema,
  type BasFrozenBasSummary,
  BasLiveCurrentness,
  type BasPreparationSummary,
  GetBasPreparationRequestSchema,
  GetBasPreparationResponseSchema,
  GetFrozenBasRequestSchema,
  type GetFrozenBasResponse,
  GetFrozenBasResponseSchema,
  ListBasExternalObservationsRequestSchema,
  ListBasExternalObservationsResponseSchema,
  ListBasPreparationsRequestSchema,
  ListBasPreparationsResponseSchema,
  ListFrozenBasRequestSchema,
  ListFrozenBasResponseSchema,
} from "@tammy/connect-client/tammy/v1/bas_preparation_pb.js";
import {
  AuthenticationContextSchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { DocumentCandidateSchema } from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { InvoicePaymentSchema } from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { LegacyPurchaseDisposition } from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { BAS_PREPARATION_RESPONSE_FRAME_LIMIT } from "../../src/shared/bas-preparation-ipc";
import { createProtoMethodCodec } from "../../src/shared/proto-ipc";
import { expect, test } from "./fixtures";
import { invokeConveyorFrame } from "./support/conveyor";
import {
  authenticatedWorkspace,
  CURRENT_WORKFLOW_ADMIN_PASSWORD,
  CURRENT_WORKFLOW_PASSPHRASE,
  CURRENT_WORKFLOW_USERNAME,
} from "./support/current-accounting-workflow";
import { readInvoiceAccounting, recordInvoiceReceipt } from "./support/invoice-income-workflow";
import {
  expectPurchaseControls,
  openPurchase,
  postPurchaseThroughUi,
  readPurchasePayments,
  recordPurchasePaymentThroughUi,
  retainPurchaseThroughUi,
  setupPurchaseWorkspace,
} from "./support/purchase-payments-workflow";
import {
  parseReportCsv,
  readPdfPages,
  reportArtifactHash,
} from "./support/reporting-export-artifacts";
import { generateTotp } from "./support/totp";

const listFrozenCodec = createProtoMethodCodec({
  input: ListFrozenBasRequestSchema,
  output: ListFrozenBasResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: BAS_PREPARATION_RESPONSE_FRAME_LIMIT,
});
const getFrozenCodec = createProtoMethodCodec({
  input: GetFrozenBasRequestSchema,
  output: GetFrozenBasResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: BAS_PREPARATION_RESPONSE_FRAME_LIMIT,
});
const getPreparationCodec = createProtoMethodCodec({
  input: GetBasPreparationRequestSchema,
  output: GetBasPreparationResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: BAS_PREPARATION_RESPONSE_FRAME_LIMIT,
});
const listObservationsCodec = createProtoMethodCodec({
  input: ListBasExternalObservationsRequestSchema,
  output: ListBasExternalObservationsResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: BAS_PREPARATION_RESPONSE_FRAME_LIMIT,
});
const listPreparationsCodec = createProtoMethodCodec({
  input: ListBasPreparationsRequestSchema,
  output: ListBasPreparationsResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: BAS_PREPARATION_RESPONSE_FRAME_LIMIT,
});
type Workspace = Awaited<ReturnType<typeof authenticatedWorkspace>>;
type TotpClock = { counter: number; readonly secret: string };
type RetainedEvidence = {
  readonly type: string;
  readonly id: string;
  readonly revision: string;
  readonly hash: string;
  readonly originalContentHash: string;
};
type BasReference = {
  readonly type: string;
  readonly id: string;
  readonly revision: bigint;
  readonly contentHash: Uint8Array;
};
type SaleExpectation = {
  readonly invoiceId: string;
  readonly paymentId: string;
  readonly grossCents: string;
  readonly gstCents: string;
  readonly sourceHash: string;
};
type BasExportSources = {
  readonly sales: readonly SaleExpectation[];
  readonly purchase: {
    readonly postingId: string;
    readonly postingHash: string;
    readonly documentId: string;
    readonly paymentId: string;
    readonly paymentHash: string;
  };
  readonly paygEvidence: RetainedEvidence;
  readonly profileEvidence: RetainedEvidence;
  readonly actualDueDateEvidence: RetainedEvidence;
};

function hex(value: Uint8Array) {
  return [...value].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}
function refKey(value: BasReference) {
  return `${value.type}:${value.id}:${value.revision}:${hex(value.contentHash)}`;
}
function requiredPreparation(value: BasPreparationSummary["preparation"]) {
  if (!value) throw new Error("BAS_ACCEPTANCE_PREPARATION_MISSING");
  return value;
}
type FrozenRecord = NonNullable<GetFrozenBasResponse["frozenBas"]>;
type FrozenCalculationSnapshot = NonNullable<FrozenRecord["calculationSnapshot"]>;

function requiredFrozenRef(value: BasFrozenBasSummary["frozenBas"]) {
  if (!value) throw new Error("BAS_ACCEPTANCE_FROZEN_RECORD_MISSING");
  return value;
}
function requiredFrozenRecord(value: BasFrozenBas | undefined) {
  if (!value) throw new Error("BAS_ACCEPTANCE_FROZEN_RECORD_MISSING");
  return value;
}
function authentication(workspace: Workspace) {
  return create(AuthenticationContextSchema, {
    actorUserId: workspace.userId,
    sessionId: workspace.sessionId,
  });
}
function evidenceFor(
  document: Awaited<ReturnType<typeof retainPurchaseThroughUi>>,
): RetainedEvidence {
  if (
    !document.candidate ||
    !document.id ||
    document.version < 1n ||
    document.sha256.length !== 32
  ) {
    throw new Error("BAS_ACCEPTANCE_RETAINED_EVIDENCE_MISSING");
  }
  return {
    type: "document",
    id: document.id,
    revision: document.version.toString(),
    hash: createHash("sha256")
      .update(toBinary(DocumentCandidateSchema, document.candidate))
      .digest("hex"),
    originalContentHash: hex(document.sha256),
  };
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
  ] as const)
    await panel.getByLabel(label, { exact: true }).fill(date);
  await panel
    .getByRole("combobox", { name: "Legacy disposition", exact: true })
    .selectOption(String(LegacyPurchaseDisposition.NONCREDITABLE));
  // The retained source itself explicitly says it is supporting evidence only
  // and carries no purchase/payment value, so it is the direct evidence for this
  // reviewed noncreditable classification.
  await panel
    .getByRole("combobox", { name: "Legacy supporting evidence", exact: true })
    .selectOption(documentId);
  await panel
    .getByLabel("Legacy review reason", { exact: true })
    .fill("Supporting evidence only; zero purchase value and zero purchase credit.");
  await panel
    .getByLabel(
      "Reviewed legacy accounting, settlement and claimed status against retained records",
      {
        exact: true,
      },
    )
    .check();
  await panel.getByRole("button", { name: "Review legacy decision", exact: true }).click();
  await panel.getByRole("button", { name: "Confirm legacy purchase review", exact: true }).click();
  await expect(panel.getByRole("status")).toContainText("Legacy decision retained:");
}

async function allFrozen(page: Page, workspace: Workspace): Promise<BasFrozenBasSummary[]> {
  const values: BasFrozenBasSummary[] = [];
  let cursor = "";
  do {
    const frame = listFrozenCodec.encodeRequest(
      create(ListFrozenBasRequestSchema, {
        authentication: authentication(workspace),
        organisationId: workspace.organisationId,
        page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
      }),
    );
    const response = listFrozenCodec.decodeResponse(
      await invokeConveyorFrame(page, "basPreparation", "listFrozenBas", frame),
    );
    values.push(...response.frozenBas);
    const next = response.page?.nextCursor ?? "";
    if (next && next === cursor) throw new Error("BAS_ACCEPTANCE_FROZEN_CURSOR_REPEATED");
    cursor = next;
  } while (cursor);
  return values;
}

async function getFrozen(page: Page, workspace: Workspace, ref: BasReference) {
  const frame = getFrozenCodec.encodeRequest(
    create(GetFrozenBasRequestSchema, {
      authentication: authentication(workspace),
      organisationId: workspace.organisationId,
      frozenId: ref.id,
      frozenHash: ref.contentHash,
    }),
  );
  return getFrozenCodec.decodeResponse(
    await invokeConveyorFrame(page, "basPreparation", "getFrozenBas", frame),
  );
}

async function getPreparation(page: Page, workspace: Workspace, ref: BasReference) {
  const frame = getPreparationCodec.encodeRequest(
    create(GetBasPreparationRequestSchema, {
      authentication: authentication(workspace),
      organisationId: workspace.organisationId,
      preparationId: ref.id,
      preparationHash: ref.contentHash,
    }),
  );
  return getPreparationCodec.decodeResponse(
    await invokeConveyorFrame(page, "basPreparation", "getBasPreparation", frame),
  );
}

async function observationsForFrozen(page: Page, workspace: Workspace, ref: BasReference) {
  const frame = listObservationsCodec.encodeRequest(
    create(ListBasExternalObservationsRequestSchema, {
      authentication: authentication(workspace),
      organisationId: workspace.organisationId,
      frozenId: ref.id,
      frozenHash: ref.contentHash,
      page: create(PageRequestSchema, { pageSize: 200 }),
    }),
  );
  return listObservationsCodec.decodeResponse(
    await invokeConveyorFrame(page, "basPreparation", "listBasExternalObservations", frame),
  );
}

function assertCurrentConfirmedHead(
  detail: Awaited<ReturnType<typeof getPreparation>>,
  expectedFamily: BasReference,
  expectedHead: BasReference,
) {
  const chain = detail.currentChain;
  if (!chain?.family || !chain.latestHead) throw new Error("BAS_ACCEPTANCE_CURRENT_CHAIN_MISSING");
  expect(refKey(chain.family), "current chain family").toBe(refKey(expectedFamily));
  const head = chain.latestHead.expectedState;
  if (head.case !== "retained") throw new Error("BAS_ACCEPTANCE_CONFIRMED_HEAD_MISSING");
  expect(refKey(head.value), "current confirmed head identity").toBe(refKey(expectedHead));
  expect(chain.latestHead.expectedVersion, "current confirmed head version").toBe(
    expectedHead.revision,
  );
  expect(detail.currentDisputeOverlay).toBe(BasDisputeOverlayState.NONE);
}

async function captureScreenshot(page: Page, testInfo: TestInfo, name: string) {
  const file = testInfo.outputPath(name);
  await mkdir(path.dirname(file), { recursive: true });
  await page.screenshot({ path: file, fullPage: true });
  await testInfo.attach(name, { path: file, contentType: "image/png" });
  return file;
}

async function latestPreparationForFamily(page: Page, workspace: Workspace, family?: BasReference) {
  const values: BasPreparationSummary[] = [];
  let cursor = "";
  do {
    const frame = listPreparationsCodec.encodeRequest(
      create(ListBasPreparationsRequestSchema, {
        authentication: authentication(workspace),
        organisationId: workspace.organisationId,
        ...(family ? { familyId: family.id, familyHash: family.contentHash } : {}),
        page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
      }),
    );
    const response = listPreparationsCodec.decodeResponse(
      await invokeConveyorFrame(page, "basPreparation", "listBasPreparations", frame),
    );
    values.push(...response.preparations);
    const next = response.page?.nextCursor ?? "";
    if (next && next === cursor) throw new Error("BAS_ACCEPTANCE_PREPARATION_CURSOR_REPEATED");
    cursor = next;
  } while (cursor);
  const latest = values.sort((left, right) => {
    const timeDelta = (right.updatedAt?.seconds ?? 0n) - (left.updatedAt?.seconds ?? 0n);
    if (timeDelta !== 0n) return timeDelta > 0n ? 1 : -1;
    const revisionDelta = (right.preparation?.revision ?? 0n) - (left.preparation?.revision ?? 0n);
    return revisionDelta > 0n ? 1 : revisionDelta < 0n ? -1 : 0;
  })[0];
  if (!latest?.preparation) throw new Error("BAS_ACCEPTANCE_PREPARATION_MISSING");
  return latest;
}

async function latestPreparationForId(
  page: Page,
  workspace: Workspace,
  family: BasReference,
  preparationId: string,
) {
  const values: BasPreparationSummary[] = [];
  let cursor = "";
  do {
    const frame = listPreparationsCodec.encodeRequest(
      create(ListBasPreparationsRequestSchema, {
        authentication: authentication(workspace),
        organisationId: workspace.organisationId,
        familyId: family.id,
        familyHash: family.contentHash,
        page: create(PageRequestSchema, { pageSize: 200, ...(cursor ? { cursor } : {}) }),
      }),
    );
    const response = listPreparationsCodec.decodeResponse(
      await invokeConveyorFrame(page, "basPreparation", "listBasPreparations", frame),
    );
    values.push(
      ...response.preparations.filter((summary) => summary.preparation?.id === preparationId),
    );
    const next = response.page?.nextCursor ?? "";
    if (next && next === cursor) throw new Error("BAS_ACCEPTANCE_PREPARATION_CURSOR_REPEATED");
    cursor = next;
  } while (cursor);
  const latest = values.sort((left, right) => {
    const leftRevision = left.preparation?.revision ?? 0n;
    const rightRevision = right.preparation?.revision ?? 0n;
    return rightRevision > leftRevision ? 1 : rightRevision < leftRevision ? -1 : 0;
  })[0];
  const current = latest?.preparation;
  if (!current) throw new Error("BAS_ACCEPTANCE_EXPECTED_PREPARATION_IS_NOT_RETAINED");
  if (!latest.family || refKey(latest.family) !== refKey(family))
    throw new Error("BAS_ACCEPTANCE_EXPECTED_PREPARATION_FAMILY_CHANGED");
  return current;
}

async function navigate(page: Page, destination: string) {
  await page.evaluate((target) => {
    window.history.replaceState(null, "", target);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, destination);
  await expect(page).toHaveURL(
    new RegExp(`${destination.replaceAll("/", "\\/").replace("?", "\\?")}$`),
  );
}

async function createAndIssueInvoice(
  page: Page,
  number: string,
  price: string,
  taxTreatment: "taxable" | "gst-free",
) {
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  const invoiceNumber = page.getByLabel("Invoice number", { exact: true });
  if (await invoiceNumber.isDisabled()) {
    await page.getByRole("button", { name: "New invoice", exact: true }).click();
  }
  await expect(invoiceNumber).toBeEnabled();
  await invoiceNumber.fill(number);
  await page.getByLabel("Issue date", { exact: true }).fill("2026-09-30");
  await page.getByLabel("Due date", { exact: true }).fill("2026-10-14");
  await page.getByLabel("Customer name", { exact: true }).fill("Synthetic BAS acceptance customer");
  await page
    .getByRole("textbox", { name: "Customer address", exact: true })
    .fill("1 Receipt Road, Melbourne VIC 3000");
  await page
    .getByRole("textbox", { name: "Seller address", exact: true })
    .fill("27 Cambridge Road, Mooroolbark VIC 3138");
  await page.getByLabel("Registered for GST", { exact: true }).check();
  await page
    .getByLabel("Description 1", { exact: true })
    .fill(taxTreatment === "taxable" ? "Taxable synthetic service" : "GST-free synthetic service");
  await page.getByLabel("Unit price 1", { exact: true }).fill(price);
  await page.getByLabel("Unit cost 1", { exact: true }).fill("10.00");
  await page
    .getByRole("combobox", { name: "GST treatment 1", exact: true })
    .selectOption(taxTreatment);
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Issue invoice", exact: true }).click();
  await page.getByRole("button", { name: "Confirm issue", exact: true }).click();
  await expect(page.getByText("Invoice issued.", { exact: true })).toBeVisible();
}

async function fillEvidence(page: Page, evidence: RetainedEvidence) {
  await page.getByLabel("Source type", { exact: true }).first().fill(evidence.type);
  await page.getByLabel("Source UUID", { exact: true }).first().fill(evidence.id);
  await page.getByLabel("Revision", { exact: true }).first().fill(evidence.revision);
  await page.getByLabel("SHA-256 hex", { exact: true }).first().fill(evidence.hash);
}

async function configureProfileAndPeriod(
  page: Page,
  evidence: RetainedEvidence,
  dueDateEvidenceIdentity: RetainedEvidence,
) {
  await page.getByLabel("Effective from", { exact: true }).fill("2026-07-01");
  for (const [label, value] of [
    ["GST obligation", "active"],
    ["GST registration status", "registered"],
    ["GST reporting method", "simpler"],
    ["GST turnover category", "small"],
    ["GST basis", "cash"],
    ["GST cadence", "quarterly"],
    ["PAYGW obligation", "active"],
    ["PAYGW category", "small"],
    ["PAYGW cadence", "quarterly"],
    ["PAYGW prefill requirement", "not required"],
    ["PAYGI obligation", "active"],
    ["PAYGI method", "rate"],
    ["PAYGI cadence", "quarterly"],
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
    .fill("Synthetic business is GST registered and reports quarterly on a cash basis.");
  await page.getByLabel("GST registration effective from", { exact: true }).fill("2025-07-01");
  await page
    .getByLabel("PAYGW reason", { exact: true })
    .fill("Synthetic payroll statement reviewed for this acceptance fixture.");
  await page
    .getByLabel("PAYGI reason", { exact: true })
    .fill("Synthetic operative PAYGI notice and prior-year instalment reviewed.");
  await page
    .getByLabel("Confirmed income year UUID", { exact: true })
    .fill("018f2f5e-7b5e-7a11-8f00-000000000501");
  await page.getByLabel("Income year start", { exact: true }).fill("2026-07-01");
  await page.getByLabel("Income year end", { exact: true }).fill("2027-06-30");
  await fillEvidence(page, evidence);
  const dueDateEvidence = page.getByRole("group", {
    name: "Actual statement due-date evidence identity",
    exact: true,
  });
  await dueDateEvidence
    .getByLabel("Source type", { exact: true })
    .fill(dueDateEvidenceIdentity.type);
  await dueDateEvidence.getByLabel("Source UUID", { exact: true }).fill(dueDateEvidenceIdentity.id);
  await dueDateEvidence
    .getByLabel("Revision", { exact: true })
    .fill(dueDateEvidenceIdentity.revision);
  await dueDateEvidence
    .getByLabel("SHA-256 hex", { exact: true })
    .fill(dueDateEvidenceIdentity.hash);
  await page
    .getByLabel("Profile decision reason", { exact: true })
    .fill("Synthetic quarterly cash BAS obligations confirmed from retained fixture evidence.");
  await page.getByRole("button", { name: "Save immutable profile revision", exact: true }).click();
  await expectLifecycleMessage(page, "Saved BAS obligations profile revision");

  const periodSelect = page.getByRole("combobox", {
    name: "Confirmed period history",
    exact: true,
  });
  await expect
    .poll(async () =>
      (await periodSelect.locator("option").allTextContents()).some(
        (value) => value.includes("2026-07-01") && value.includes("2026-09-30"),
      ),
    )
    .toBe(true);
  const options = await periodSelect.locator("option").evaluateAll((items) =>
    items.map((item) => ({
      value: (item as HTMLOptionElement).value,
      text: item.textContent ?? "",
    })),
  );
  const q3 = options.find(
    (item) =>
      item.text.startsWith("Suggestion ·") &&
      item.text.includes("2026-07-01") &&
      item.text.includes("2026-09-30"),
  );
  if (!q3) throw new Error("BAS_ACCEPTANCE_Q3_SUGGESTION_MISSING");
  await periodSelect.selectOption(q3.value);
  await page
    .getByLabel("Period confirmation reason", { exact: true })
    .fill(
      "Synthetic supported quarter confirmed from the BAS calendar and retained GST registration evidence.",
    );
  await page.getByRole("button", { name: "Confirm selected period", exact: true }).click();
  await expectLifecycleMessage(page, "Confirmed BAS period");
}

async function expectLifecycleMessage(page: Page, message: string) {
  const lifecycle = page.getByRole("region", {
    name: "BAS preparation lifecycle",
    exact: true,
  });
  await expect(lifecycle.getByRole("status").filter({ hasText: message })).toBeVisible();
}

async function expectAuthorityMessage(page: Page, message: string) {
  const authority = page.getByRole("region", { name: "BAS approval authority", exact: true });
  await expect(authority.getByRole("status").filter({ hasText: message })).toBeVisible();
}

function reviewFindings(page: Page) {
  return page
    .getByRole("region", { name: "BAS preparation lifecycle", exact: true })
    .getByRole("heading", { name: "Review findings", exact: true })
    .locator("xpath=..");
}

async function expectNoReviewBlockers(page: Page) {
  await expect(reviewFindings(page).getByText(/Blocker ·/)).toHaveCount(0);
}

async function regenerateSaveAndValidate(
  page: Page,
  evidence: RetainedEvidence,
  noticeStatus: "unknown" | "operative",
) {
  await page.getByRole("button", { name: "Prepare", exact: true }).click();
  await page.getByRole("button", { name: "Regenerate exact calculation", exact: true }).click();
  await expectLifecycleMessage(page, "Prepared exact BAS version");
  await page
    .getByRole("combobox", { name: "W1 total payments state", exact: true })
    .selectOption({ label: "known" });
  await page.getByLabel("W1 total payments exact AUD", { exact: true }).fill("1000.00");
  await page
    .getByRole("combobox", { name: "W2 amount withheld state", exact: true })
    .selectOption({ label: "known" });
  await page.getByLabel("W2 amount withheld exact AUD", { exact: true }).fill("50.00");
  await page
    .getByRole("combobox", { name: "W3 other withholding state", exact: true })
    .selectOption({ label: "known zero" });
  await page.getByLabel("W3 other withholding exact AUD", { exact: true }).fill("0.00");
  await page
    .getByRole("combobox", { name: "W4 no-ABN withholding state", exact: true })
    .selectOption({ label: "known zero" });
  await page.getByLabel("W4 no-ABN withholding exact AUD", { exact: true }).fill("0.00");
  await page
    .getByRole("combobox", { name: "PAYGI method for this input", exact: true })
    .selectOption({ label: "rate" });
  await page
    .getByRole("combobox", { name: "PAYGI amount state", exact: true })
    .selectOption({ label: "known" });
  await page.getByLabel("PAYGI exact AUD (T7 or T1)", { exact: true }).fill("2010.99");
  await page.getByLabel("PAYGI exact T2 rate (e.g. 15.5)", { exact: true }).fill("1.7");
  await page
    .getByLabel("Confirmed income year UUID", { exact: true })
    .fill("018f2f5e-7b5e-7a11-8f00-000000000501");
  await page.getByLabel("Income year start", { exact: true }).fill("2026-07-01");
  await page.getByLabel("Income year end", { exact: true }).fill("2027-06-30");
  await page
    .getByRole("combobox", { name: "PAYGI notice status", exact: true })
    .selectOption({ label: noticeStatus });
  await page
    .getByRole("combobox", { name: "PAYGI prior variation", exact: true })
    .selectOption({ label: noticeStatus === "unknown" ? "unknown" : "none" });
  await page
    .getByLabel("PAYG review reason", { exact: true })
    .fill("Synthetic payroll statement, ATO PAYGI notice, and prior-year T1 reviewed.");
  await fillEvidence(page, evidence);
  await page.getByRole("button", { name: "Save exact PAYG inputs", exact: true }).click();
  await expectLifecycleMessage(
    page,
    "Saved PAYG inputs against the displayed preparation revision",
  );
  // Saving inputs creates a new immutable preparation revision and clears its calculation pointer.
  await page.getByRole("button", { name: "Regenerate exact calculation", exact: true }).click();
  await expectLifecycleMessage(page, "Prepared exact BAS version");
  await page.getByRole("button", { name: "Review", exact: true }).click();
  await page.getByRole("button", { name: "Run validation on this exact calculation" }).click();
  await expectLifecycleMessage(page, "Created review validation");
}

async function selectExactPreparation(page: Page, workspace: Workspace, family: BasReference) {
  const summary = await latestPreparationForFamily(page, workspace, family);
  const preparation = requiredPreparation(summary.preparation);
  await page.getByRole("button", { name: "Review", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Preparation history", exact: true })
    .selectOption(refKey(preparation));
  return summary;
}

async function selectLatestPreparation(page: Page, workspace: Workspace) {
  const summary = await latestPreparationForFamily(page, workspace);
  const preparation = requiredPreparation(summary.preparation);
  await page.getByRole("button", { name: "Review", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Preparation history", exact: true })
    .selectOption(refKey(preparation));
  return summary;
}

async function setupTotp(page: Page): Promise<TotpClock> {
  await page.getByRole("link", { name: "Settings", exact: true }).click();
  await page.getByRole("link", { name: "SBR readiness", exact: true }).click();
  await expect(page.getByRole("heading", { name: "SBR readiness", exact: true })).toBeVisible();
  await page
    .getByLabel("Current administrator password", { exact: true })
    .fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Begin TOTP setup", exact: true }).click();
  const secret = await page.getByTestId("totp-provisioning-material").textContent();
  if (!secret || !/^[A-Z2-7]{32}$/.test(secret))
    throw new Error("BAS_ACCEPTANCE_TOTP_SECRET_INVALID");
  const confirmationTime = Date.now() - 30_000;
  await page
    .getByLabel("Six-digit code", { exact: true })
    .fill(generateTotp(secret, confirmationTime));
  await page.getByRole("button", { name: "Confirm security code", exact: true }).click();
  await expect(page.getByRole("button", { name: "Import credential", exact: true })).toBeVisible();
  return { counter: Math.floor(confirmationTime / 30_000), secret };
}

async function nextFreshCode(clock: TotpClock) {
  while (Math.floor(Date.now() / 30_000) <= clock.counter) {
    await new Promise<void>((resolve) => setTimeout(resolve, 250));
  }
  clock.counter = Math.floor(Date.now() / 30_000);
  return generateTotp(clock.secret);
}

async function freezeCurrent(page: Page, clock: TotpClock, reason: string) {
  await page.getByRole("button", { name: "Freeze", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: "Freeze for manual handoff", exact: true }),
  ).toBeVisible();
  await page.getByLabel("Approval reason", { exact: true }).fill(reason);
  await page.getByLabel("Authenticator code", { exact: true }).fill(await nextFreshCode(clock));
  await page
    .getByRole("button", { name: "Approve and freeze exact snapshot", exact: true })
    .click();
  await expectLifecycleMessage(page, "Frozen exact handoff");
}

async function recordConfirmation(
  page: Page,
  clock: TotpClock,
  evidence: RetainedEvidence,
  reference: string,
) {
  await page.getByRole("button", { name: "External record", exact: true }).click();
  await page
    .getByRole("combobox", { name: "User-recorded event", exact: true })
    .selectOption({ label: "completed lodgement" });
  await page
    .getByRole("combobox", { name: "Channel", exact: true })
    .selectOption({ label: "online portal" });
  await page.getByLabel("Claimed event date", { exact: true }).fill("2026-10-22");
  await page.getByLabel("External reference (optional)", { exact: true }).fill(reference);
  await page
    .getByLabel("User-recorded reason", { exact: true })
    .fill(
      "Synthetic acceptance record: a user reports completion; Tammy did not transmit or verify anything with the ATO.",
    );
  await fillEvidence(page, evidence);
  await page
    .getByLabel("Fresh authenticator code for external confirmation", { exact: true })
    .fill(await nextFreshCode(clock));
  await page
    .getByRole("button", { name: "Record user-reported event as unverified", exact: true })
    .click();
  await expectLifecycleMessage(page, "USER_RECORDED_UNVERIFIED. This is not ATO verification.");
}

async function latestFrozen(page: Page, workspace: Workspace) {
  const values = await allFrozen(page, workspace);
  const summary = values.sort((left, right) =>
    Number((right.approvedAt?.seconds ?? 0n) - (left.approvedAt?.seconds ?? 0n)),
  )[0];
  if (!summary?.frozenBas) throw new Error("BAS_ACCEPTANCE_FROZEN_RECORD_MISSING");
  return summary;
}

function jsonRecord(row: Readonly<Record<string, string>>) {
  if (!row.record_json) throw new Error("BAS_ACCEPTANCE_RECORD_JSON_MISSING");
  return JSON.parse(row.record_json) as Record<string, unknown>;
}

function collectStrings(value: unknown): string[] {
  if (typeof value === "string") return [value];
  if (Array.isArray(value)) return value.flatMap(collectStrings);
  if (value && typeof value === "object") return Object.values(value).flatMap(collectStrings);
  return [];
}

type ExpectedLabel = {
  state: string;
  cents?: readonly [string, string];
  rate?: readonly [string, string];
  whole?: string;
  direction?: string;
};

function expectedLabels(revision: boolean): Record<string, ExpectedLabel> {
  return {
    G1: {
      state: "KNOWN",
      cents: [revision ? "18200" : "16000", "1"],
      whole: revision ? "182" : "160",
    },
    "1A": {
      state: "KNOWN",
      cents: [revision ? "1200" : "1000", "1"],
      whole: revision ? "12" : "10",
    },
    "1B": { state: "KNOWN", cents: ["500", "1"], whole: "5" },
    W1: { state: "KNOWN", cents: ["100000", "1"], whole: "1000" },
    W2: { state: "KNOWN", cents: ["5000", "1"], whole: "50" },
    W3: { state: "KNOWN_ZERO", cents: ["0", "1"], whole: "0" },
    W4: { state: "KNOWN_ZERO", cents: ["0", "1"], whole: "0" },
    W5: { state: "KNOWN", cents: ["5000", "1"], whole: "50" },
    "4": { state: "KNOWN", cents: ["5000", "1"], whole: "50" },
    T1: { state: "KNOWN", cents: ["201099", "1"], whole: "2010" },
    T2: { state: "KNOWN", rate: ["17", "10"] },
    T7: { state: "UNSET" },
    T11: { state: "KNOWN", cents: ["3417", "1"], whole: "34" },
    "5A": { state: "KNOWN", cents: ["3417", "1"], whole: "34" },
    "5B": { state: "NOT_APPLICABLE" },
    "8A": {
      state: "KNOWN",
      cents: [revision ? "9617" : "9417", "1"],
      whole: revision ? "96" : "94",
    },
    "8B": { state: "KNOWN", cents: ["500", "1"], whole: "5" },
    "9": {
      state: "KNOWN",
      cents: [revision ? "9117" : "8917", "1"],
      whole: revision ? "91" : "89",
      direction: "PAYABLE",
    },
  };
}

function assertCsvLabels(csv: Awaited<ReturnType<typeof parseReportCsv>>, revision: boolean) {
  const rows = csv.rows.filter((row) => row.record_type === "label");
  const expected = expectedLabels(revision);
  expect(rows).toHaveLength(18);
  const actual = new Map(rows.map((row) => [row.label, row]));
  expect(actual.size).toBe(18);
  expect([...actual.keys()].sort()).toEqual(Object.keys(expected).sort());
  for (const [label, want] of Object.entries(expected)) {
    const row = actual.get(label);
    expect(row, `missing ${label} label`).toBeDefined();
    expect(row?.state, `${label} state`).toBe(want.state);
    expect(row?.exact_cents_numerator, `${label} exact cents numerator`).toBe(
      want.cents?.[0] ?? "",
    );
    expect(row?.exact_cents_denominator, `${label} exact cents denominator`).toBe(
      want.cents?.[1] ?? "",
    );
    expect(row?.exact_rate_numerator, `${label} exact rate numerator`).toBe(want.rate?.[0] ?? "");
    expect(row?.exact_rate_denominator, `${label} exact rate denominator`).toBe(
      want.rate?.[1] ?? "",
    );
    expect(row?.reported_whole_dollars, `${label} whole-dollar amount`).toBe(want.whole ?? "");
    expect(row?.direction, `${label} direction`).toBe(want.direction ?? "");
  }
}

function assertCsvRevisionDeltas(csv: Awaited<ReturnType<typeof parseReportCsv>>) {
  const expected: Record<string, string> = {
    G1: "2200/1 cents",
    "1A": "200/1 cents",
    "1B": "0/1 cents",
    W1: "0/1 cents",
    W2: "0/1 cents",
    W3: "0/1 cents",
    W4: "0/1 cents",
    W5: "0/1 cents",
    "4": "0/1 cents",
    T1: "0/1 cents",
    T2: "0/1 rate units",
    T7: "state-only; exact value unavailable",
    T11: "0/1 cents",
    "5A": "0/1 cents",
    "5B": "state-only; exact value unavailable",
    "8A": "200/1 cents",
    "8B": "0/1 cents",
    "9": "200/1 cents",
  };
  const rows = csv.rows.filter((row) => row.record_type === "label_delta");
  expect(rows).toHaveLength(18);
  const actual = new Map(rows.map((row) => [row.label, row]));
  expect(actual.size).toBe(18);
  for (const [label, delta] of Object.entries(expected)) {
    expect(actual.get(label)?.value, `${label} exact revision delta`).toBe(delta);
  }
  expect(actual.get("G1")?.reason).toContain(
    "predecessor=KNOWN: 16000/1 cents; current=KNOWN: 18200/1 cents",
  );
  expect(actual.get("1A")?.reason).toContain(
    "predecessor=KNOWN: 1000/1 cents; current=KNOWN: 1200/1 cents",
  );
  expect(actual.get("T2")?.reason).toContain("predecessor=KNOWN: 17/10; current=KNOWN: 17/10");
}

function assertCsvSourceManifest(
  csv: Awaited<ReturnType<typeof parseReportCsv>>,
  ref: BasReference,
  snapshot: FrozenCalculationSnapshot,
  revision: boolean,
  sources: BasExportSources,
) {
  const metadata = new Map(
    csv.rows.filter((row) => row.record_type === "metadata").map((row) => [row.field, row.value]),
  );
  expect(metadata.get("report_kind")).toBe("FROZEN_MANUAL_HANDOFF_NOT_LODGED");
  expect(metadata.get("target_kind")).toBe("frozen_bas");
  expect(metadata.get("target_id")).toBe(ref.id);
  expect(metadata.get("target_revision")).toBe(ref.revision.toString());
  expect(metadata.get("target_hash")).toBe(hex(ref.contentHash));
  expect(metadata.get("calculation_id")).toBe(snapshot.calculationId);
  expect(metadata.get("calculation_revision")).toBe(snapshot.version.toString());
  expect(metadata.get("calculation_hash")).toBe(hex(snapshot.contentHash));
  expect(metadata.get("calculation_outcome")).toBe("COMPLETE");
  expect(metadata.get("label_9_direction")).toBe("PAYABLE");
  expect(metadata.get("source_snapshot_hash")).toBe(hex(snapshot.sourceSnapshotHash));
  expect(metadata.get("coverage_manifest_hash")).toBe(hex(snapshot.coverageManifestHash));
  expect(metadata.get("revision_vector_hash")).toBe(hex(snapshot.revisionVectorHash));
  for (const field of [
    "source_snapshot_hash",
    "coverage_manifest_hash",
    "revision_vector_hash",
    "rule_hash",
    "calculator_hash",
    "form_hash",
    "schema_hash",
    "descriptor_hash",
  ]) {
    expect(metadata.get(field), `${field} present and SHA-256 shaped`).toMatch(/^[0-9a-f]{64}$/);
  }
  if (revision) {
    expect(metadata.get("preparation_creation_reason")).toContain(
      "Linked revision after a new Q3 taxable receipt",
    );
  }

  const sales = csv.rows.filter((row) => row.record_type === "sales_source");
  expect(sales).toHaveLength(sources.sales.length);
  for (const expected of sources.sales) {
    const row = sales.find(
      (candidate) =>
        candidate.record_id === expected.invoiceId && candidate.related_id === expected.paymentId,
    );
    expect(row, `sales source ${expected.invoiceId}/${expected.paymentId}`).toBeDefined();
    if (!row) throw new Error("BAS_ACCEPTANCE_SALES_SOURCE_MISSING");
    const source = jsonRecord(row);
    expect(source.invoiceId).toBe(expected.invoiceId);
    expect(source.paymentId).toBe(expected.paymentId);
    expect((source.gross as { minorUnits?: string } | undefined)?.minorUnits).toBe(
      expected.grossCents,
    );
    expect((source.gst as { minorUnits?: string } | undefined)?.minorUnits).toBe(expected.gstCents);
    expect(source.sourceRevision).toBe("1");
    expect(source.sourceHash).toBe(expected.sourceHash);
  }

  const purchaseRows = csv.rows.filter((row) => row.record_type === "purchase_source");
  expect(purchaseRows).toHaveLength(1);
  const purchaseRow = purchaseRows.at(0);
  if (!purchaseRow) throw new Error("BAS_ACCEPTANCE_PURCHASE_SOURCE_MISSING");
  expect(purchaseRow.record_id).toBe(sources.purchase.postingId);
  expect(purchaseRow.source_id).toBe(sources.purchase.documentId);
  expect(purchaseRow.source_hash).toBe(sources.purchase.postingHash);
  const purchase = jsonRecord(purchaseRow);
  expect(purchase.documentId).toBe(sources.purchase.documentId);
  expect(purchase.postingId).toBe(sources.purchase.postingId);
  expect(purchase.postingHash).toBe(sources.purchase.postingHash);
  expect(purchase.paymentId).toBe(sources.purchase.paymentId);
  expect(purchase.paymentHash).toBe(sources.purchase.paymentHash);
  expect((purchase.claimedGst as { minorUnits?: string } | undefined)?.minorUnits).toBe("500");
  expect((purchase.eligibleGst as { minorUnits?: string } | undefined)?.minorUnits).toBe("500");

  const periodSnapshot = csv.rows.find(
    (row) => row.record_type === "pinned_snapshot" && row.field === "period_snapshot",
  );
  if (!periodSnapshot) throw new Error("BAS_ACCEPTANCE_PERIOD_SNAPSHOT_MISSING");
  const periodEvidenceStrings = collectStrings(jsonRecord(periodSnapshot));
  expect(periodEvidenceStrings).toContain(sources.actualDueDateEvidence.id);
  expect(periodEvidenceStrings).toContain(sources.actualDueDateEvidence.hash);

  const profileSnapshot = csv.rows.find(
    (row) => row.record_type === "pinned_snapshot" && row.field === "profile_snapshot",
  );
  if (!profileSnapshot) throw new Error("BAS_ACCEPTANCE_PROFILE_SNAPSHOT_MISSING");
  const profileSnapshotStrings = collectStrings(jsonRecord(profileSnapshot));
  expect(profileSnapshotStrings).toContain(sources.profileEvidence.id);
  expect(profileSnapshotStrings).toContain(sources.profileEvidence.hash);

  const payrollEvidenceRows = csv.rows.filter((row) => row.record_type === "trace_evidence");
  for (const label of ["W1", "W2", "T1"]) {
    expect(
      payrollEvidenceRows.some(
        (row) =>
          row.label === label &&
          row.source_type === sources.paygEvidence.type &&
          row.source_id === sources.paygEvidence.id &&
          row.source_revision === sources.paygEvidence.revision &&
          row.source_hash === sources.paygEvidence.hash,
      ),
      `${label} trace retains the reviewed PAYG evidence`,
    ).toBe(true);
  }

  const coverage = csv.rows.filter((row) => row.record_type === "coverage");
  expect(coverage.length).toBeGreaterThan(0);
  for (const kind of ["INVOICE_POSTING", "PURCHASE_SETTLEMENT", "PURCHASE_CREDIT"]) {
    expect(
      coverage.some((row) => row.candidate_kind === kind && row.disposition === "ELIGIBLE"),
      `${kind} source disposition`,
    ).toBe(true);
  }
  const coverageStrings = coverage.flatMap((row) => collectStrings(jsonRecord(row)));
  for (const sourceId of [
    ...sources.sales.flatMap((sale) => [sale.invoiceId, sale.paymentId]),
    sources.purchase.documentId,
    sources.purchase.postingId,
    sources.purchase.paymentId,
  ]) {
    expect(coverageStrings, `coverage manifest includes ${sourceId}`).toContain(sourceId);
  }
  for (const source of [
    sources.paygEvidence,
    sources.actualDueDateEvidence,
    sources.profileEvidence,
  ]) {
    const sourceRows = coverage.filter(
      (row) => row.source_type === "documents" && row.source_id === source.id,
    );
    expect(sourceRows, `one complete coverage candidate for ${source.id}`).toHaveLength(1);
    const sourceRow = sourceRows[0];
    if (!sourceRow) throw new Error("BAS_ACCEPTANCE_SUPPORT_COVERAGE_MISSING");
    expect(sourceRow).toMatchObject({
      source_revision: source.revision,
      source_hash: source.originalContentHash,
      candidate_kind: "OPENING_OR_LEGACY_FACT",
      disposition: "EXCLUDED_WITH_REASON",
      classification_code: "LEGACY_EXCLUDED_BY_RETAINED_DECISION",
      amount_currency: "",
      amount_minor_units: "",
    });
    expect(coverageStrings).toContain(source.id);
    expect(coverageStrings).toContain(source.originalContentHash);
  }
}

function assertPdfReport(
  pages: readonly string[],
  ref: BasReference,
  snapshot: FrozenCalculationSnapshot,
  revision: boolean,
  sources: BasExportSources,
) {
  const text = pages.join(" ");
  expect(text).toContain("FROZEN MANUAL HANDOFF");
  expect(text).toContain("NOT LODGED");
  expect(text).toContain(ref.id);
  expect(text).toContain(snapshot.calculationId);
  expect(text).toContain(hex(snapshot.sourceSnapshotHash));
  expect(text).toContain(hex(snapshot.coverageManifestHash));
  expect(text).toContain("Label 9 direction PAYABLE");
  const tableEnd = text.indexOf(
    revision
      ? "Exact predecessor calculation comparison"
      : "All 18 calculation traces and evidence",
  );
  const tableStart = text.indexOf("G1 KNOWN");
  expect(tableStart).toBeGreaterThanOrEqual(0);
  expect(tableEnd).toBeGreaterThan(tableStart);
  const labelTable = text.slice(tableStart, tableEnd);
  const labelRows = Object.entries(expectedLabels(revision)).map(([label, want]) => {
    const start = labelTable.indexOf(`${label} ${want.state}`);
    expect(start, `${label} table row`).toBeGreaterThanOrEqual(0);
    return { label, want, start };
  });
  for (const [index, row] of labelRows.entries()) {
    const next = labelRows[index + 1];
    const rowText = labelTable.slice(row.start, next?.start ?? labelTable.length);
    if (row.want.cents)
      expect(rowText, `${row.label} PDF exact amount`).toContain(
        `${row.want.cents[0]}/${row.want.cents[1]} cents`,
      );
    if (row.want.rate)
      expect(rowText, `${row.label} PDF exact rate`).toContain(
        `${row.want.rate[0]}/${row.want.rate[1]}`,
      );
    if (row.want.whole !== undefined)
      expect(rowText, `${row.label} PDF reported amount`).toContain(`${row.want.whole} dollars`);
    if (row.want.direction)
      expect(rowText, `${row.label} PDF direction`).toContain(row.want.direction);
  }
  for (const source of sources.sales) {
    expect(text).toContain(source.invoiceId);
    expect(text).toContain(source.paymentId);
  }
  expect(text).toContain(sources.purchase.documentId);
  expect(text).toContain(sources.purchase.postingId);
  expect(text).toContain(sources.purchase.paymentId);
  expect(text).toContain(sources.paygEvidence.id);
  expect(text).toContain(sources.paygEvidence.hash);
  expect(text).toContain(sources.profileEvidence.id);
  expect(text).toContain(sources.profileEvidence.hash);
  expect(text).toContain(sources.actualDueDateEvidence.id);
  expect(text).toContain(sources.actualDueDateEvidence.hash);
  for (const source of sources.sales) expect(text).toContain(source.sourceHash);
  expect(text).toContain(sources.purchase.postingHash);
  expect(text).toContain(sources.purchase.paymentHash);
  if (revision) {
    const comparisonStart = text.indexOf("Exact predecessor calculation comparison");
    const comparisonEnd = text.indexOf("Frozen approval", comparisonStart);
    expect(comparisonStart).toBeGreaterThanOrEqual(0);
    expect(comparisonEnd).toBeGreaterThan(comparisonStart);
    const comparison = text.slice(comparisonStart, comparisonEnd).replace(/\s+/gu, " ");
    expect(text).toContain("Linked revision after a new Q3 taxable receipt");
    const beforeLabels = expectedLabels(false);
    const currentLabels = expectedLabels(true);
    const deltas: Record<string, string> = {
      G1: "2200/1 cents",
      "1A": "200/1 cents",
      "1B": "0/1 cents",
      W1: "0/1 cents",
      W2: "0/1 cents",
      W3: "0/1 cents",
      W4: "0/1 cents",
      W5: "0/1 cents",
      "4": "0/1 cents",
      T1: "0/1 cents",
      T2: "0/1 rate units",
      T7: "state-only; exact value unavailable",
      T11: "0/1 cents",
      "5A": "0/1 cents",
      "5B": "state-only; exact value unavailable",
      "8A": "200/1 cents",
      "8B": "0/1 cents",
      "9": "200/1 cents",
    };
    for (const [label, delta] of Object.entries(deltas)) {
      const before = beforeLabels[label];
      const current = currentLabels[label];
      if (!before || !current) throw new Error("BAS_ACCEPTANCE_REVISION_LABEL_MISSING");
      const beforeValue = before.rate
        ? `${before.rate[0]}/${before.rate[1]}`
        : before.cents
          ? `${before.cents[0]}/${before.cents[1]} cents`
          : "not applicable";
      const currentValue = current.rate
        ? `${current.rate[0]}/${current.rate[1]}`
        : current.cents
          ? `${current.cents[0]}/${current.cents[1]} cents`
          : "not applicable";
      expect(comparison, `${label} delta binds both exact values`).toContain(
        `${label} ${before.state}: ${beforeValue} ${current.state}: ${currentValue} ${delta}`,
      );
    }
  }
}

async function exportFrozen(
  page: Page,
  electronHarness: { readonly application: import("@playwright/test").ElectronApplication },
  testInfo: import("@playwright/test").TestInfo,
  summary: BasFrozenBasSummary,
  format: "PDF" | "CSV",
  name: string,
  sources: BasExportSources,
) {
  const ref = summary.frozenBas;
  if (!ref) throw new Error("BAS_ACCEPTANCE_EXPORT_TARGET_MISSING");
  const file = testInfo.outputPath(name);
  await mkdir(path.dirname(file), { recursive: true });
  await page.getByRole("button", { name: "Export", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Exact report source", exact: true })
    .selectOption("frozen_bas");
  await page
    .getByRole("combobox", { name: "Frozen BAS identity", exact: true })
    .selectOption(refKey(ref));
  await page
    .getByRole("combobox", { name: "Format", exact: true })
    .selectOption(format.toLowerCase());
  await electronHarness.application.evaluate(({ dialog }, filePath) => {
    dialog.showSaveDialog = async () => ({ canceled: false, filePath });
  }, file);
  await page.getByRole("button", { name: `Export ${format}`, exact: true }).click();
  await expect(page.getByText(new RegExp(`Exact frozen BAS ${format} export saved`))).toBeVisible();
  await expect
    .poll(() =>
      stat(file).then(
        (value) => value.size > 0,
        () => false,
      ),
    )
    .toBe(true);
  const hash = await reportArtifactHash(file);
  const history = page.getByRole("region", { name: "Exact-target export history", exact: true });
  await expect(history).toContainText(format);
  await expect(history).toContainText("Succeeded");
  const frozenResponse = await getFrozen(page, await authenticatedWorkspace(page), ref);
  const frozen = frozenResponse.frozenBas;
  const snapshot = frozen?.calculationSnapshot;
  if (!frozen || !snapshot) throw new Error("BAS_ACCEPTANCE_FROZEN_SNAPSHOT_MISSING");
  const revision = name.includes("revision");
  if (format === "CSV") {
    const csv = await parseReportCsv(file);
    assertCsvLabels(csv, revision);
    assertCsvSourceManifest(csv, ref, snapshot, revision, sources);
    if (revision) assertCsvRevisionDeltas(csv);
    else expect(csv.rows.filter((row) => row.record_type === "label_delta")).toHaveLength(0);
  } else {
    assertPdfReport(await readPdfPages(file), ref, snapshot, revision, sources);
  }
  return {
    file,
    hash,
    format,
    frozenRef: ref,
    calculationRef: {
      type: "bas_calculation",
      id: snapshot.calculationId,
      revision: snapshot.version,
      contentHash: snapshot.contentHash,
    },
  };
}

async function attachBasSuccessEvidence(
  testInfo: TestInfo,
  artifacts: readonly Awaited<ReturnType<typeof exportFrozen>>[],
  screenshots: readonly string[],
  sourceRevision: string,
) {
  // Successful Playwright output is removed by preserveOutput=failures-only.
  // Keep inspectable evidence outside that disposable directory.
  const evidenceDirectory = path.resolve(
    testInfo.config.rootDir,
    "../../../../.tmp/bas-acceptance",
    sourceRevision,
    `${createHash("sha256").update(testInfo.testId).digest("hex").slice(0, 16)}-${Date.now()}-${testInfo.retry}`,
  );
  await mkdir(evidenceDirectory, { recursive: true });
  const manifestArtifacts = await Promise.all(
    artifacts.map(async (artifact) => {
      const fileInfo = await stat(artifact.file);
      const actualHash = await reportArtifactHash(artifact.file);
      expect(actualHash, `${artifact.format} artifact hash remains stable`).toBe(artifact.hash);
      const retainedFile = path.join(evidenceDirectory, path.basename(artifact.file));
      await copyFile(artifact.file, retainedFile);
      expect(
        await reportArtifactHash(retainedFile),
        "retained export matches inspected bytes",
      ).toBe(actualHash);
      return {
        name: path.basename(artifact.file),
        format: artifact.format,
        sizeBytes: fileInfo.size,
        sha256: actualHash,
        frozenBas: {
          id: artifact.frozenRef.id,
          revision: artifact.frozenRef.revision.toString(),
          sha256: hex(artifact.frozenRef.contentHash),
        },
        calculation: {
          id: artifact.calculationRef.id,
          revision: artifact.calculationRef.revision.toString(),
          sha256: hex(artifact.calculationRef.contentHash),
        },
      };
    }),
  );
  const manifestScreenshots = await Promise.all(
    screenshots.map(async (file) => {
      const retainedFile = path.join(evidenceDirectory, path.basename(file));
      await copyFile(file, retainedFile);
      return {
        name: path.basename(retainedFile),
        sha256: await reportArtifactHash(retainedFile),
        sizeBytes: (await stat(retainedFile)).size,
      };
    }),
  );
  const manifest = {
    schemaVersion: 1,
    sourceRevision,
    artifacts: manifestArtifacts,
    screenshots: manifestScreenshots,
  };
  const manifestPath = path.join(evidenceDirectory, "bas-success-artifact-manifest.json");
  await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, "utf8");
  expect(JSON.parse(await readFile(manifestPath, "utf8"))).toEqual(manifest);
  for (const artifact of artifacts) {
    await testInfo.attach(
      `bas-${artifact.format.toLowerCase()}-${artifact.frozenRef.id}.${artifact.format.toLowerCase()}`,
      {
        path: path.join(evidenceDirectory, path.basename(artifact.file)),
        contentType: artifact.format === "PDF" ? "application/pdf" : "text/csv",
      },
    );
  }
  await testInfo.attach("bas-success-artifact-manifest.json", {
    path: manifestPath,
    contentType: "application/json",
  });
}

async function cancelFrozenExport(
  page: Page,
  electronHarness: { readonly application: import("@playwright/test").ElectronApplication },
  ref: BasReference,
) {
  await page.getByRole("button", { name: "Export", exact: true }).click();
  await page
    .getByRole("combobox", { name: "Exact report source", exact: true })
    .selectOption("frozen_bas");
  await page
    .getByRole("combobox", { name: "Frozen BAS identity", exact: true })
    .selectOption(refKey(ref));
  await page.getByRole("combobox", { name: "Format", exact: true }).selectOption("pdf");
  await electronHarness.application.evaluate(({ dialog }) => {
    dialog.showSaveDialog = async () => ({ canceled: true, filePath: "" });
  });
  await page.getByRole("button", { name: "Export PDF", exact: true }).click();
  await expect(page.getByText("BAS export cancelled.", { exact: true })).toBeVisible();
  const history = page.getByRole("region", { name: "Exact-target export history", exact: true });
  await expect(history).toContainText("PDF");
  await expect(history).toContainText("Cancelled");
  await expect(history).toContainText(ref.id);
}

test("BAS lifecycle: clean cash sources, blocked PAYGI resolution, frozen handoffs, linked revision and immutable exports", async ({
  electronHarness,
}, testInfo) => {
  test.setTimeout(900_000);
  const workspace = await setupPurchaseWorkspace(electronHarness, "cash");
  let page = electronHarness.currentPage();
  const screenshots: string[] = [];

  await createAndIssueInvoice(page, "BAS-CASH-TAXABLE-001", "200.00", "taxable");
  await recordInvoiceReceipt(
    page,
    "BAS-CASH-TAXABLE-001",
    "2026-09-30",
    "110.00",
    "Q3 half receipt",
  );
  await recordInvoiceReceipt(
    page,
    "BAS-CASH-TAXABLE-001",
    "2026-10-01",
    "110.00",
    "Q4 half receipt",
  );
  await createAndIssueInvoice(page, "BAS-CASH-GSTFREE-001", "50.00", "gst-free");
  await recordInvoiceReceipt(
    page,
    "BAS-CASH-GSTFREE-001",
    "2026-09-30",
    "50.00",
    "Q3 GST-free receipt",
  );

  const purchase = await retainPurchaseThroughUi(
    page,
    workspace,
    "BAS-CASH-PURCHASE-001",
    "2026-08-31",
  );
  const postedPurchase = await postPurchaseThroughUi(page, workspace, purchase);
  expect(postedPurchase.accounting?.paid?.minorUnits).toBe(0n);
  expect(postedPurchase.accounting?.outstanding?.minorUnits).toBe(11000n);
  expect(postedPurchase.accounting?.deferredGst?.minorUnits).toBe(1000n);
  expect(postedPurchase.accounting?.attributedGst?.minorUnits).toBe(0n);
  expect(postedPurchase.accounting?.suspenseGst?.minorUnits).toBe(0n);
  await expectPurchaseControls(page, workspace, {
    "1000": 27000n,
    "2000": -11000n,
    "1150": 0n,
    "1160": 1000n,
    "1170": 0n,
    "2100": -2000n,
    "2110": 0n,
  });
  const firstPurchasePayment = await recordPurchasePaymentThroughUi(
    page,
    workspace,
    purchase.id,
    "2026-09-30",
    "55.00",
    "Q3 half payment",
  );
  expect(firstPurchasePayment.accounting?.paid?.minorUnits).toBe(5500n);
  expect(firstPurchasePayment.accounting?.outstanding?.minorUnits).toBe(5500n);
  expect(firstPurchasePayment.accounting?.deferredGst?.minorUnits).toBe(500n);
  expect(firstPurchasePayment.accounting?.attributedGst?.minorUnits).toBe(500n);
  expect(firstPurchasePayment.accounting?.suspenseGst?.minorUnits).toBe(0n);
  await expectPurchaseControls(page, workspace, {
    "1000": 21500n,
    "2000": -5500n,
    "1150": 500n,
    "1160": 500n,
    "1170": 0n,
    "2100": -2000n,
    "2110": 0n,
  });
  const secondPurchasePayment = await recordPurchasePaymentThroughUi(
    page,
    workspace,
    purchase.id,
    "2026-10-01",
    "55.00",
    "Q4 half payment",
  );
  expect(secondPurchasePayment.accounting?.paid?.minorUnits).toBe(11000n);
  expect(secondPurchasePayment.accounting?.outstanding?.minorUnits).toBe(0n);
  expect(secondPurchasePayment.accounting?.deferredGst?.minorUnits).toBe(0n);
  expect(secondPurchasePayment.accounting?.attributedGst?.minorUnits).toBe(1000n);
  expect(secondPurchasePayment.accounting?.suspenseGst?.minorUnits).toBe(0n);
  await expectPurchaseControls(page, workspace, {
    "1000": 16000n,
    "2000": 0n,
    "1150": 1000n,
    "1160": 0n,
    "1170": 0n,
    "2100": -2000n,
    "2110": 0n,
  });
  const purchasePosting = postedPurchase.accounting?.posting;
  const purchaseQ3Payment = (await readPurchasePayments(page, workspace, purchase.id)).find(
    (payment) => payment.reference === "Q3 half payment",
  );
  if (!purchasePosting || !purchaseQ3Payment)
    throw new Error("BAS_ACCEPTANCE_PURCHASE_SOURCE_MISSING");
  const purchaseSource = {
    postingId: purchasePosting.id,
    postingHash: hex(purchasePosting.sourceHash),
    documentId: purchase.id,
    paymentId: purchaseQ3Payment.id,
    paymentHash: hex(purchaseQ3Payment.sourceHash),
  };

  const taxableInvoice = await readInvoiceAccounting(page, workspace, "BAS-CASH-TAXABLE-001");
  const taxableQ3Payment = taxableInvoice.accounting.payments.find(
    (payment) => payment.reference === "Q3 half receipt",
  );
  const gstFreeInvoice = await readInvoiceAccounting(page, workspace, "BAS-CASH-GSTFREE-001");
  const gstFreeQ3Payment = gstFreeInvoice.accounting.payments.find(
    (payment) => payment.reference === "Q3 GST-free receipt",
  );
  if (!taxableQ3Payment || !gstFreeQ3Payment)
    throw new Error("BAS_ACCEPTANCE_SALES_SOURCE_MISSING");
  expect(taxableQ3Payment.basSalesGross?.minorUnits).toBe(11000n);
  expect(taxableQ3Payment.allocatedGst?.minorUnits).toBe(1000n);
  expect(gstFreeQ3Payment.basSalesGross?.minorUnits).toBe(5000n);
  expect(gstFreeQ3Payment.allocatedGst?.minorUnits).toBe(0n);
  const initialSales: SaleExpectation[] = [
    {
      invoiceId: taxableInvoice.invoice.id,
      paymentId: taxableQ3Payment.id,
      grossCents: "11000",
      gstCents: "1000",
      sourceHash: createHash("sha256")
        .update(toBinary(InvoicePaymentSchema, taxableQ3Payment))
        .digest("hex"),
    },
    {
      invoiceId: gstFreeInvoice.invoice.id,
      paymentId: gstFreeQ3Payment.id,
      grossCents: "5000",
      gstCents: "0",
      sourceHash: createHash("sha256")
        .update(toBinary(InvoicePaymentSchema, gstFreeQ3Payment))
        .digest("hex"),
    },
  ];
  const support = await retainPurchaseThroughUi(
    page,
    workspace,
    "BAS-PAYG-SUPPORT-001",
    "2026-08-31",
    true,
    false,
    "payg-evidence",
  );
  const retainedEvidence = evidenceFor(support);
  const dueDateStatement = await retainPurchaseThroughUi(
    page,
    workspace,
    "BAS-ACTUAL-DUE-DATE-STATEMENT-001",
    "2026-09-30",
    true,
    false,
    "actual-due-date-evidence",
  );
  const dueDateEvidence = evidenceFor(dueDateStatement);
  const profileEvidenceDocument = await retainPurchaseThroughUi(
    page,
    workspace,
    "BAS-PROFILE-REGISTRATION-EVIDENCE-001",
    "2025-07-01",
    true,
    false,
    "profile-evidence",
  );
  const profileEvidence = evidenceFor(profileEvidenceDocument);
  for (const source of [
    ["BAS-PAYG-SUPPORT-001", support.id],
    ["BAS-ACTUAL-DUE-DATE-STATEMENT-001", dueDateStatement.id],
    ["BAS-PROFILE-REGISTRATION-EVIDENCE-001", profileEvidenceDocument.id],
  ] as const)
    await markSupportingDocumentNoncreditable(page, source[0], source[1]);
  const initialExportSources: BasExportSources = {
    sales: initialSales,
    purchase: purchaseSource,
    paygEvidence: retainedEvidence,
    profileEvidence,
    actualDueDateEvidence: dueDateEvidence,
  };

  await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  await expect(page.getByRole("heading", { name: "BAS preparation", exact: true })).toBeVisible();
  const stages = page.getByRole("navigation", { name: "BAS lifecycle stages" });
  for (const stage of ["Prepare", "Review", "Freeze", "Export", "External record"] as const) {
    await expect(stages.getByRole("button", { name: stage, exact: true })).toBeVisible();
  }

  await configureProfileAndPeriod(page, profileEvidence, dueDateEvidence);
  await regenerateSaveAndValidate(page, retainedEvidence, "unknown");
  await expect(page.getByText("PAYGI_NOTICE_STATUS_UNKNOWN", { exact: true })).toBeVisible();
  await expect(page.getByText("PAYGI_PRIOR_VARIATION_UNKNOWN", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Acknowledge selected exact warnings" }),
  ).toBeDisabled();

  await page.getByRole("button", { name: "Prepare", exact: true }).click();
  await page
    .getByRole("combobox", { name: "PAYGI notice status", exact: true })
    .selectOption({ label: "operative" });
  await page
    .getByRole("combobox", { name: "PAYGI prior variation", exact: true })
    .selectOption({ label: "none" });
  await page.getByRole("button", { name: "Save exact PAYG inputs", exact: true }).click();
  await expectLifecycleMessage(
    page,
    "Saved PAYG inputs against the displayed preparation revision",
  );
  await page.getByRole("button", { name: "Regenerate exact calculation", exact: true }).click();
  await expectLifecycleMessage(page, "Prepared exact BAS version");
  await page.getByRole("button", { name: "Review", exact: true }).click();
  await page.getByRole("button", { name: "Run validation on this exact calculation" }).click();
  await expectLifecycleMessage(page, "Created review validation");
  await expectNoReviewBlockers(page);
  screenshots.push(await captureScreenshot(page, testInfo, "bas-reviewed-labels.png"));

  await page.getByRole("button", { name: "Freeze", exact: true }).click();
  await page
    .getByLabel("Approval reason", { exact: true })
    .fill("Approval is unavailable until a Business Lodger is assigned.");
  await page.getByLabel("Authenticator code", { exact: true }).fill("000000");
  await expect(
    page.getByRole("button", { name: "Approve and freeze exact snapshot" }),
  ).toBeDisabled();
  await page
    .getByLabel("Grant business lodger approval authority for BAS actions", { exact: true })
    .check();
  await page.getByRole("button", { name: "Review role change", exact: true }).click();
  await page.getByRole("button", { name: "Confirm and save", exact: true }).click();
  await expectAuthorityMessage(page, "BAS approval authority updated");

  const totp = await setupTotp(page);
  await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  await expect(page.getByRole("heading", { name: "BAS preparation", exact: true })).toBeVisible();
  // Route changes preserve exact Core history; select the returned identity by its immutable ref.
  await selectLatestPreparation(page, workspace);
  await freezeCurrent(
    page,
    totp,
    "Reviewed exact Q3 sources and PAYG evidence; freeze this manual handoff.",
  );

  const frozenOneSummary = await latestFrozen(page, workspace);
  const frozenOneRef = requiredFrozenRef(frozenOneSummary.frozenBas);
  const frozenOne = await getFrozen(page, workspace, frozenOneRef);
  const frozenOneRecord = requiredFrozenRecord(frozenOne.frozenBas);
  expect(frozenOneRecord.calculationSnapshot?.result?.labels).toHaveLength(18);
  screenshots.push(await captureScreenshot(page, testInfo, "bas-frozen-identity-currentness.png"));
  await cancelFrozenExport(page, electronHarness, frozenOneRef);
  const firstPdf = await exportFrozen(
    page,
    electronHarness,
    testInfo,
    frozenOneSummary,
    "PDF",
    "bas-original.pdf",
    initialExportSources,
  );
  const firstCsv = await exportFrozen(
    page,
    electronHarness,
    testInfo,
    frozenOneSummary,
    "CSV",
    "bas-original.csv",
    initialExportSources,
  );
  const firstExports = [firstPdf, firstCsv];
  expect(firstExports.map((item) => item.hash)).toHaveLength(2);
  await recordConfirmation(page, totp, retainedEvidence, "SYNTHETIC-LODGE-001");
  screenshots.push(await captureScreenshot(page, testInfo, "bas-user-recorded-unverified.png"));

  const observationsFrame = listObservationsCodec.encodeRequest(
    create(ListBasExternalObservationsRequestSchema, {
      authentication: authentication(workspace),
      organisationId: workspace.organisationId,
      frozenId: frozenOneRef.id,
      frozenHash: frozenOneRef.contentHash,
      page: create(PageRequestSchema, { pageSize: 200 }),
    }),
  );
  const observations = listObservationsCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "basPreparation",
      "listBasExternalObservations",
      observationsFrame,
    ),
  );
  expect(observations.observations).toHaveLength(1);
  expect(observations.observations[0]?.eventKind).toBe(BasExternalEventKind.COMPLETED_LODGEMENT);
  expect(observations.observations[0]?.trust).toBe(BasExternalTrust.USER_RECORDED_UNVERIFIED);
  expect(observations.observations[0]?.frozenBas?.id).toBe(frozenOneRef.id);

  await createAndIssueInvoice(page, "BAS-REVISION-TAXABLE-001", "20.00", "taxable");
  await recordInvoiceReceipt(
    page,
    "BAS-REVISION-TAXABLE-001",
    "2026-09-30",
    "22.00",
    "Q3 revision source receipt",
  );
  const revisionInvoice = await readInvoiceAccounting(page, workspace, "BAS-REVISION-TAXABLE-001");
  const revisionPayment = revisionInvoice.accounting.payments.find(
    (payment) => payment.reference === "Q3 revision source receipt",
  );
  if (!revisionPayment) throw new Error("BAS_ACCEPTANCE_REVISION_SOURCE_MISSING");
  expect(revisionPayment.basSalesGross?.minorUnits).toBe(2200n);
  expect(revisionPayment.allocatedGst?.minorUnits).toBe(200n);
  const revisionExportSources: BasExportSources = {
    ...initialExportSources,
    sales: [
      ...initialSales,
      {
        invoiceId: revisionInvoice.invoice.id,
        paymentId: revisionPayment.id,
        grossCents: "2200",
        gstCents: "200",
        sourceHash: createHash("sha256")
          .update(toBinary(InvoicePaymentSchema, revisionPayment))
          .digest("hex"),
      },
    ],
  };
  await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  await expect(page.getByRole("heading", { name: "BAS preparation", exact: true })).toBeVisible();
  const firstFrozen = await getFrozen(page, await authenticatedWorkspace(page), frozenOneRef);
  const firstFamily = firstFrozen.frozenBas?.family;
  if (!firstFamily) throw new Error("BAS_ACCEPTANCE_FAMILY_MISSING");
  await selectExactPreparation(page, await authenticatedWorkspace(page), firstFamily);
  await page.getByRole("button", { name: "External record", exact: true }).click();
  await page
    .getByLabel("Revision reason", { exact: true })
    .fill(
      "Linked revision after a new Q3 taxable receipt; preserve the confirmed original handoff.",
    );
  await page
    .getByRole("button", { name: "Create revision from exact confirmed head", exact: true })
    .click();
  await expectLifecycleMessage(page, "Created a BAS revision");
  await page.getByRole("button", { name: "Prepare", exact: true }).click();
  await page.getByRole("button", { name: "Regenerate exact calculation", exact: true }).click();
  await expectLifecycleMessage(page, "Prepared exact BAS version");
  await page
    .getByRole("combobox", { name: "W1 total payments state", exact: true })
    .selectOption({ label: "known" });
  await page.getByLabel("W1 total payments exact AUD", { exact: true }).fill("1000.00");
  await page
    .getByRole("combobox", { name: "W2 amount withheld state", exact: true })
    .selectOption({ label: "known" });
  await page.getByLabel("W2 amount withheld exact AUD", { exact: true }).fill("50.00");
  await page
    .getByRole("combobox", { name: "W3 other withholding state", exact: true })
    .selectOption({ label: "known zero" });
  await page.getByLabel("W3 other withholding exact AUD", { exact: true }).fill("0.00");
  await page
    .getByRole("combobox", { name: "W4 no-ABN withholding state", exact: true })
    .selectOption({ label: "known zero" });
  await page.getByLabel("W4 no-ABN withholding exact AUD", { exact: true }).fill("0.00");
  await page
    .getByRole("combobox", { name: "PAYGI method for this input", exact: true })
    .selectOption({ label: "rate" });
  await page
    .getByRole("combobox", { name: "PAYGI amount state", exact: true })
    .selectOption({ label: "known" });
  await page.getByLabel("PAYGI exact AUD (T7 or T1)", { exact: true }).fill("2010.99");
  await page.getByLabel("PAYGI exact T2 rate (e.g. 15.5)", { exact: true }).fill("1.7");
  await page
    .getByLabel("Confirmed income year UUID", { exact: true })
    .fill("018f2f5e-7b5e-7a11-8f00-000000000501");
  await page.getByLabel("Income year start", { exact: true }).fill("2026-07-01");
  await page.getByLabel("Income year end", { exact: true }).fill("2027-06-30");
  await page
    .getByRole("combobox", { name: "PAYGI notice status", exact: true })
    .selectOption({ label: "operative" });
  await page
    .getByRole("combobox", { name: "PAYGI prior variation", exact: true })
    .selectOption({ label: "none" });
  await page
    .getByLabel("PAYG review reason", { exact: true })
    .fill(
      "Synthetic payroll statement, ATO PAYGI notice, and prior-year T1 reviewed for linked revision.",
    );
  await fillEvidence(page, retainedEvidence);
  await page.getByRole("button", { name: "Save exact PAYG inputs", exact: true }).click();
  await expectLifecycleMessage(
    page,
    "Saved PAYG inputs against the displayed preparation revision",
  );
  await page.getByRole("button", { name: "Regenerate exact calculation", exact: true }).click();
  await expectLifecycleMessage(page, "Prepared exact BAS version");
  await page.getByRole("button", { name: "Review", exact: true }).click();
  await page.getByRole("button", { name: "Run validation on this exact calculation" }).click();
  await expectLifecycleMessage(page, "Created review validation");
  await expectNoReviewBlockers(page);
  const warnings = reviewFindings(page).locator('input[type="checkbox"]:not([disabled])');
  const warningCount = await warnings.count();
  if (warningCount > 0) {
    for (let index = 0; index < warningCount; index += 1) await warnings.nth(index).check();
    await page.getByRole("button", { name: "Acknowledge selected exact warnings" }).click();
    await expectLifecycleMessage(page, "Recorded exact warning acknowledgement");
  }
  screenshots.push(await captureScreenshot(page, testInfo, "bas-linked-revision-review.png"));
  await freezeCurrent(
    page,
    totp,
    "Linked Q3 revision reviewed with new source coverage and exact PAYG evidence.",
  );
  const frozenTwoSummary = await latestFrozen(page, workspace);
  const frozenTwoRef = requiredFrozenRef(frozenTwoSummary.frozenBas);
  const frozenTwo = await getFrozen(page, workspace, frozenTwoRef);
  const frozenTwoRecord = requiredFrozenRecord(frozenTwo.frozenBas);
  expect(frozenTwoRecord.calculationSnapshot?.result?.labels).toHaveLength(18);
  screenshots.push(await captureScreenshot(page, testInfo, "bas-linked-revision-frozen.png"));
  const secondPdf = await exportFrozen(
    page,
    electronHarness,
    testInfo,
    frozenTwoSummary,
    "PDF",
    "bas-revision.pdf",
    revisionExportSources,
  );
  const secondCsv = await exportFrozen(
    page,
    electronHarness,
    testInfo,
    frozenTwoSummary,
    "CSV",
    "bas-revision.csv",
    revisionExportSources,
  );
  const secondExports = [secondPdf, secondCsv];
  expect(secondExports.map((item) => item.hash)).toHaveLength(2);
  await recordConfirmation(page, totp, retainedEvidence, "SYNTHETIC-LODGE-002");
  await expect(page.getByText("USER_RECORDED_UNVERIFIED", { exact: false })).toBeVisible();
  const secondObservations = await observationsForFrozen(page, workspace, frozenTwoRef);
  expect(secondObservations.observations).toHaveLength(1);
  expect(secondObservations.observations[0]?.eventKind).toBe(
    BasExternalEventKind.COMPLETED_LODGEMENT,
  );
  expect(secondObservations.observations[0]?.trust).toBe(BasExternalTrust.USER_RECORDED_UNVERIFIED);
  const secondObservationRef = secondObservations.observations[0]?.frozenBas;
  if (!secondObservationRef) throw new Error("BAS_ACCEPTANCE_SECOND_OBSERVATION_TARGET_MISSING");
  expect(refKey(secondObservationRef), "second confirmation exact frozen target").toBe(
    refKey(frozenTwoRef),
  );
  const frozenTwoFamily = frozenTwoRecord.family;
  const frozenTwoPreparation = frozenTwoRecord.preparation;
  if (!frozenTwoFamily || !frozenTwoPreparation)
    throw new Error("BAS_ACCEPTANCE_REVISION_CHAIN_IDENTITY_MISSING");
  const currentConfirmedHead = await latestPreparationForId(
    page,
    workspace,
    frozenTwoFamily,
    frozenTwoPreparation.id,
  );
  expect(currentConfirmedHead.revision, "freeze and confirmation append versions").toBe(
    frozenTwoPreparation.revision + 2n,
  );
  assertCurrentConfirmedHead(
    await getPreparation(page, workspace, currentConfirmedHead),
    frozenTwoFamily,
    currentConfirmedHead,
  );

  const firstFrozenBytes = Buffer.from(toBinary(BasFrozenBasSchema, frozenOneRecord));
  const secondFrozenBytes = Buffer.from(toBinary(BasFrozenBasSchema, frozenTwoRecord));
  page = await electronHarness.restart("accounting-workflow-restart");
  await navigate(page, "/unlock");
  await page.getByLabel("Workspace passphrase", { exact: true }).fill(CURRENT_WORKFLOW_PASSPHRASE);
  await page.getByLabel("Email or username", { exact: true }).fill(CURRENT_WORKFLOW_USERNAME);
  await page
    .getByLabel("Administrator password", { exact: true })
    .fill(CURRENT_WORKFLOW_ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Unlock workspace", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  const restartedWorkspace = await authenticatedWorkspace(page);
  const retainedAfterRestartOne = await getFrozen(page, restartedWorkspace, frozenOneRef);
  const retainedAfterRestartTwo = await getFrozen(page, restartedWorkspace, frozenTwoRef);
  expect(
    Buffer.from(
      toBinary(BasFrozenBasSchema, requiredFrozenRecord(retainedAfterRestartOne.frozenBas)),
    ),
  ).toEqual(firstFrozenBytes);
  expect(
    Buffer.from(
      toBinary(BasFrozenBasSchema, requiredFrozenRecord(retainedAfterRestartTwo.frozenBas)),
    ),
  ).toEqual(secondFrozenBytes);

  const currentConfirmedHeadAfterRestart = await latestPreparationForId(
    page,
    restartedWorkspace,
    frozenTwoFamily,
    frozenTwoPreparation.id,
  );
  expect(refKey(currentConfirmedHeadAfterRestart), "restart retains confirmed chain head").toBe(
    refKey(currentConfirmedHead),
  );
  assertCurrentConfirmedHead(
    await getPreparation(page, restartedWorkspace, currentConfirmedHeadAfterRestart),
    frozenTwoFamily,
    currentConfirmedHeadAfterRestart,
  );
  const secondObservationsAfterRestart = await observationsForFrozen(
    page,
    restartedWorkspace,
    frozenTwoRef,
  );
  expect(secondObservationsAfterRestart.observations).toHaveLength(1);
  expect(secondObservationsAfterRestart.observations[0]?.trust).toBe(
    BasExternalTrust.USER_RECORDED_UNVERIFIED,
  );
  const restartedObservationRef = secondObservationsAfterRestart.observations[0]?.frozenBas;
  if (!restartedObservationRef)
    throw new Error("BAS_ACCEPTANCE_RESTARTED_OBSERVATION_TARGET_MISSING");
  expect(refKey(restartedObservationRef), "restarted confirmation exact target").toBe(
    refKey(frozenTwoRef),
  );

  await createAndIssueInvoice(page, "BAS-AFTER-FREEZE-TAXABLE-001", "10.00", "taxable");
  await recordInvoiceReceipt(
    page,
    "BAS-AFTER-FREEZE-TAXABLE-001",
    "2026-09-30",
    "11.00",
    "Post-freeze source mutation",
  );
  const changedOne = await getFrozen(page, restartedWorkspace, frozenOneRef);
  const changedTwo = await getFrozen(page, restartedWorkspace, frozenTwoRef);
  expect(changedOne.liveCurrentness).toBe(BasLiveCurrentness.SOURCE_CHANGED);
  expect(changedTwo.liveCurrentness).toBe(BasLiveCurrentness.SOURCE_CHANGED);
  expect(
    Buffer.from(toBinary(BasFrozenBasSchema, requiredFrozenRecord(changedOne.frozenBas))),
  ).toEqual(firstFrozenBytes);
  expect(
    Buffer.from(toBinary(BasFrozenBasSchema, requiredFrozenRecord(changedTwo.frozenBas))),
  ).toEqual(secondFrozenBytes);
  expect(
    requiredFrozenRecord(changedOne.frozenBas).calculationSnapshot?.result?.labels,
  ).toHaveLength(18);
  expect(
    requiredFrozenRecord(changedTwo.frozenBas).calculationSnapshot?.result?.labels,
  ).toHaveLength(18);
  expect(await allFrozen(page, restartedWorkspace)).toHaveLength(2);
  await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  await expect(page.getByRole("heading", { name: "BAS preparation", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Freeze", exact: true }).click();
  await expect(page.getByText(/Source changed/)).toBeVisible();
  screenshots.push(await captureScreenshot(page, testInfo, "bas-frozen-source-changed.png"));
  await attachBasSuccessEvidence(
    testInfo,
    [...firstExports, ...secondExports],
    screenshots,
    electronHarness.packagedLayout.sourceRevision,
  );
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});
