import { mkdir, stat, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { create, toBinary } from "@bufbuild/protobuf";
import {
  JournalSource,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  LegacyPurchaseDisposition,
  PurchaseReviewStatus,
  RecordPurchasePaymentRequestSchema,
  RecordPurchasePaymentResponseSchema,
} from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { BasWorkpaperSchema } from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { validateBasSnapshot } from "@tammy/reporting";
import { expect, test } from "./fixtures";
import { assertPurchaseExportArtifacts } from "./support/purchase-export-assertions";
import {
  capturePurchaseBankingSnapshot,
  seedPurchaseBankingReconciliationBaseline,
} from "./support/purchase-non-effects";
import {
  buildPurchaseOpeningFixture,
  seedPackagedOpeningWithoutDocument,
} from "./support/purchase-opening-fixture";
import {
  expectPurchaseControls,
  expectPurchaseJournal,
  openPurchase,
  postPurchaseThroughUi,
  purchaseAuth,
  purchaseCommand,
  purchaseDate,
  purchaseProto,
  readPurchase,
  readPurchaseBas,
  readPurchasePayments,
  recordPurchasePaymentThroughUi,
  regeneratePurchaseBas,
  reopenPurchaseWorkspace,
  retainPurchaseThroughUi,
  retainPurchaseVolumeProof,
  reviewPurchaseEvidenceThroughUi,
  savePurchaseExport,
  setupPurchaseWorkspace,
} from "./support/purchase-payments-workflow";
import {
  buildPurchaseEvidenceInspector,
  inspectPurchasePersistedEvidence,
  type PurchasePersistedEvidence,
} from "./support/purchase-persisted-evidence";
import { parseReportCsv, reportArtifactHash } from "./support/reporting-export-artifacts";

const acceptanceRoot = fileURLToPath(
  new URL("../../../../.tmp/purchase-payments-gst-e2e/", import.meta.url),
);

test.beforeAll(async () => {
  await buildPurchaseEvidenceInspector();
  await buildPurchaseOpeningFixture();
});

test("purchases/payments-gst: opening without a document and an earlier-period legacy payment retain distinct review outcomes", async ({
  electronHarness,
}, testInfo) => {
  test.setTimeout(180_000);
  const directory = path.join(acceptanceRoot, "opening-legacy");
  await mkdir(directory, { recursive: true });
  let workspace = await setupPurchaseWorkspace(electronHarness, "cash");
  let page = electronHarness.currentPage();
  const proof = await retainPurchaseThroughUi(
    page,
    workspace,
    "OPENING-SETTLEMENT-PROOF",
    "2026-10-09",
    true,
    true,
  );
  const legacyDocument = await retainPurchaseThroughUi(
    page,
    workspace,
    "EARLIER-LEGACY-CASH-PAYMENT",
    "2026-06-10",
  );
  let seed: Awaited<ReturnType<typeof seedPackagedOpeningWithoutDocument>> | undefined;
  const reopened = await reopenPurchaseWorkspace(electronHarness, async (userDataPath) => {
    seed = await seedPackagedOpeningWithoutDocument(
      electronHarness.packagedLayout,
      userDataPath,
      workspace,
      legacyDocument,
    );
  });
  if (!seed) throw new Error("PURCHASE_OPENING_FIXTURE_MISSING");
  workspace = { ...workspace, ...reopened.workspace };
  page = reopened.page;
  await expectPurchaseJournal(
    page,
    workspace,
    seed.opening.journal?.id,
    JournalSource.OPENING_CONVERSION,
    "2026-08-31",
    [
      { code: "2000", debit: 0n, credit: 11000n },
      { code: seed.openingEquityCode, debit: 11000n, credit: 0n },
    ],
  );
  await expectPurchaseJournal(
    page,
    workspace,
    seed.settlement.id,
    JournalSource.MANUAL,
    "2026-10-02",
    [
      { code: "6101", debit: 11000n, credit: 0n },
      { code: seed.legacyCashCode, debit: 0n, credit: 11000n },
    ],
  );
  const controls = {
    "1000": 0n,
    [seed.legacyCashCode]: -11000n,
    "2000": -11000n,
    "1150": 0n,
    "1160": 0n,
    "1170": 0n,
    "6101": 11000n,
    [seed.openingEquityCode]: 11000n,
  };
  await expectPurchaseControls(page, workspace, controls);
  const initial = await regeneratePurchaseBas(page, workspace, "2026-10-01", "2026-10-09", 0n);
  expect(initial.purchaseSources).toEqual([]);
  expect(initial.legacyCandidateGst?.minorUnits).toBe(2000n);
  const openingRow = initial.purchaseReviews.find(
    (review) => review.legacySource?.source.case === "openingPayable",
  );
  expect(openingRow?.legacySource?.source).toMatchObject({
    case: "openingPayable",
    value: { openingPayableId: seed.openingPayableId },
  });
  expect(openingRow?.documentId).toBeUndefined();
  expect(openingRow?.unclaimedGst?.minorUnits).toBe(1000n);
  const reviewRegion = page.getByRole("region", {
    name: "Purchase GST review outcomes",
    exact: true,
  });
  const row = reviewRegion
    .locator("div.grid.gap-2.border-b")
    .filter({ hasText: seed.openingPayableId });
  await row
    .locator("summary")
    .filter({ hasText: /^Review legacy purchase \/ opening provenance$/ })
    .click();
  await expect(
    row.getByText(/Its settlement and GST credit allocation are unsupported/),
  ).toBeVisible();
  await row
    .getByRole("combobox", { name: "Legacy disposition", exact: true })
    .selectOption(String(LegacyPurchaseDisposition.CONFIRMED_UNPAID_THROUGH_BALANCE));
  await expect(
    row
      .getByRole("combobox", { name: "Legacy supporting evidence", exact: true })
      .locator(`option[value="${proof.id}"]`),
  ).toBeAttached();
  await row
    .getByRole("combobox", { name: "Legacy supporting evidence", exact: true })
    .selectOption(proof.id);
  await row
    .getByLabel("Legacy review reason", { exact: true })
    .fill(
      "Retained conversion source has no document and remains unpaid through 9 October; no new expense, settlement or GST credit is posted",
    );
  await row
    .getByLabel(
      "Reviewed legacy accounting, settlement and claimed status against retained records",
      { exact: true },
    )
    .check();
  await row.getByRole("button", { name: "Review legacy decision", exact: true }).click();
  await row.getByRole("button", { name: "Confirm legacy purchase review", exact: true }).click();
  await expect(row.getByRole("status")).toContainText("Decision retained:");
  const legacyRow = reviewRegion
    .locator("div.grid.gap-2.border-b")
    .filter({ hasText: "Invoice 10/06/2026 · payment" });
  await legacyRow
    .locator("summary")
    .filter({ hasText: /^Review legacy purchase \/ opening provenance$/ })
    .click();
  await legacyRow
    .getByRole("combobox", { name: "Legacy disposition", exact: true })
    .selectOption(String(LegacyPurchaseDisposition.UNSUPPORTED_EXISTING_SETTLEMENT));
  const legacyEvidence = legacyRow.getByRole("combobox", {
    name: "Legacy supporting evidence",
    exact: true,
  });
  await expect(legacyEvidence.locator(`option[value="${proof.id}"]`)).toBeAttached();
  await legacyEvidence.selectOption(proof.id);
  const paymentChoice = legacyRow.getByRole("combobox", {
    name: "Retained legacy settlement journal",
    exact: true,
  });
  await expect(paymentChoice.locator(`option[value="${seed.settlement.id}"]`)).toBeAttached();
  await paymentChoice.selectOption(seed.settlement.id);
  await legacyRow
    .getByLabel("Legacy review reason", { exact: true })
    .fill(
      "Reviewed actual 2 October cash payment of a prior-quarter legacy invoice; settlement allocation and any GST claim remain unsupported in this workpaper",
    );
  await legacyRow
    .getByLabel(
      "Reviewed legacy accounting, settlement and claimed status against retained records",
      { exact: true },
    )
    .check();
  await legacyRow.getByRole("button", { name: "Review legacy decision", exact: true }).click();
  await legacyRow
    .getByRole("button", { name: "Confirm legacy purchase review", exact: true })
    .click();
  await expect(legacyRow.getByRole("status")).toContainText("Decision retained:");
  expect((await readPurchaseBas(page, workspace)).id).toBe(initial.id);
  await expectPurchaseControls(page, workspace, controls);
  const saved = await regeneratePurchaseBas(page, workspace, "2026-10-01", "2026-10-09", 0n);
  const retained = saved.purchaseReviews.find(
    (review) => review.legacySource?.source.case === "openingPayable",
  );
  expect(retained?.status).toBe(PurchaseReviewStatus.CLEAR);
  expect(retained?.legacyReview?.disposition).toBe(
    LegacyPurchaseDisposition.CONFIRMED_UNPAID_THROUGH_BALANCE,
  );
  const retainedLegacy = saved.purchaseReviews.find(
    (review) =>
      review.legacySource?.source.case === "document" &&
      review.legacySource.source.value.documentId === legacyDocument.id,
  );
  const settlementId = seed.settlement.id;
  expect(retainedLegacy?.status).toBe(PurchaseReviewStatus.UNSUPPORTED);
  expect(retainedLegacy?.legacyReview?.disposition).toBe(
    LegacyPurchaseDisposition.UNSUPPORTED_EXISTING_SETTLEMENT,
  );
  expect(
    saved.legacyPurchaseSupport.find(
      (support) =>
        support.reviewId === retainedLegacy?.legacyReview?.id &&
        support.source?.sourceId === settlementId,
    ),
  ).toBeDefined();
  expect(saved.legacyCandidateGst?.minorUnits).toBe(1000n);
  expect(saved.legacyPurchaseSupport.map((support) => support.source?.sourceId).sort()).toEqual(
    [seed.settlement.id, proof.id, proof.id].sort(),
  );
  expect(saved.purchaseSources).toEqual([]);
  const csv = path.join(directory, "opening-reviewed.csv");
  const pdf = path.join(directory, "opening-reviewed.pdf");
  await savePurchaseExport(electronHarness, "CSV", csv, "2026-10-01", "2026-10-09");
  await savePurchaseExport(electronHarness, "PDF", pdf, "2026-10-01", "2026-10-09");
  await assertPurchaseExportArtifacts(csv, pdf, saved);
  await writeFile(
    path.join(directory, "opening-reviewed.core.pb"),
    toBinary(BasWorkpaperSchema, saved),
  );
  const screenshot = path.join(directory, "opening-review.png");
  await page.screenshot({ path: screenshot, animations: "disabled" });
  await testInfo.attach("opening-review", { path: screenshot, contentType: "image/png" });
  const restarted = await reopenPurchaseWorkspace(electronHarness);
  await restarted.page.getByRole("link", { name: "GST & BAS", exact: true }).click();
  expect(await readPurchaseBas(restarted.page, restarted.workspace)).toEqual(saved);
  await expectPurchaseControls(restarted.page, restarted.workspace, controls);
  await writeFile(
    path.join(directory, "reconciliation.json"),
    `${JSON.stringify(
      {
        sourceRevision: electronHarness.packagedLayout.sourceRevision,
        workspaceId: workspace.workspaceId,
        organisationId: workspace.organisationId,
        openingPayableId: seed.openingPayableId,
        openingJournalId: seed.opening.journal?.id,
        settlementJournalId: seed.settlement.id,
        legacyDocumentId: legacyDocument.id,
        actualSettlementDate: "2026-10-02",
        workpaperId: saved.id,
        claimedGstMinor: "0",
        candidateGstMinor: "1000",
        artifacts: await Promise.all(
          [csv, pdf].map(async (file) => ({
            file: path.basename(file),
            sha256: await reportArtifactHash(file),
          })),
        ),
      },
      null,
      2,
    )}\n`,
  );
  expect(electronHarness.consoleErrors).toEqual([]);
  expect(electronHarness.pageErrors).toEqual([]);
});

test("purchases/payments-gst: missing evidence, deferral, explicit claim and legacy review retain their separate effects", async ({
  electronHarness,
}, testInfo) => {
  test.setTimeout(240_000);
  const directory = path.join(acceptanceRoot, "evidence-deferral");
  await mkdir(directory, { recursive: true });
  let workspace = await setupPurchaseWorkspace(electronHarness, "non-cash");
  let page = electronHarness.currentPage();
  const number = "PURCHASE-SUSPENSE-001";
  const document = await retainPurchaseThroughUi(page, workspace, number);
  const posted = await postPurchaseThroughUi(page, workspace, document, { evidence: "missing" });
  expect(posted.accounting?.suspenseGst?.minorUnits).toBe(1000n);
  await expectPurchaseJournal(
    page,
    workspace,
    posted.accounting?.posting?.journalId,
    JournalSource.PURCHASE,
    "2026-08-31",
    [
      { code: "6101", debit: 10000n, credit: 0n },
      { code: "1170", debit: 1000n, credit: 0n },
      { code: "2000", debit: 0n, credit: 11000n },
    ],
  );
  const paid = await recordPurchasePaymentThroughUi(
    page,
    workspace,
    document.id,
    "2026-09-30",
    "110.00",
    "Actual paid purchase with missing invoice evidence",
  );
  expect(paid.accounting?.suspenseGst?.minorUnits).toBe(1000n);
  const controls = {
    "6101": 10000n,
    "1000": -11000n,
    "2000": 0n,
    "1150": 0n,
    "1160": 0n,
    "1170": 1000n,
  };
  await expectPurchaseControls(page, workspace, controls);
  const held = await reviewPurchaseEvidenceThroughUi(
    page,
    workspace,
    document.id,
    "invoice",
    "2026-10-01",
  );
  expect(held.accounting?.suspenseGst?.minorUnits).toBe(1000n);
  expect(held.accounting?.attributedGst?.minorUnits).toBe(0n);
  await expectPurchaseControls(page, workspace, controls);
  // The synthetic due-date notice is a distinct retained reviewed original;
  // its zero money facts cannot become an expense or claimed purchase source.
  const proof = await retainPurchaseThroughUi(
    page,
    workspace,
    "REQUIRED-RETURN-NOTICE",
    "2026-10-01",
    true,
  );
  await openPurchase(page, number);
  const panel = page.getByRole("region", { name: "Purchase accounting", exact: true });
  await panel
    .locator("summary")
    .filter({ hasText: /^Review claim period or defer GST$/ })
    .click();
  for (const [label, date] of [
    ["Earliest eligible period start", "2026-07-01"],
    ["Earliest eligible period end", "2026-09-30"],
    ["Actual required return due date", "2026-11-03"],
    ["Ordinary last claim date", "2030-11-03"],
    ["Claim / affected period start", "2026-10-01"],
    ["Claim / affected period end", "2026-12-31"],
    ["Timing review as-of date", "2026-10-09"],
  ] as const)
    await panel.getByLabel(label, { exact: true }).fill(date);
  await panel.getByRole("combobox", { name: "Due-date proof", exact: true }).selectOption(proof.id);
  await panel
    .getByLabel("Claim / deferral review reason", { exact: true })
    .fill(
      "Reviewed the actual required return notice, current evidence and supported ordinary claim deadline",
    );
  await panel
    .getByLabel("Reviewed allowable period, ordinary time limit and evidence held", { exact: true })
    .check();
  await panel.getByRole("button", { name: "Defer reviewed GST", exact: true }).click();
  await panel.getByRole("button", { name: "Confirm GST deferral", exact: true }).click();
  await expect
    .poll(
      async () => (await readPurchase(page, workspace, document.id)).accounting?.accountingVersion,
    )
    .toBe(4n);
  const deferred = await readPurchase(page, workspace, document.id);
  expect(deferred.latestDeferral?.deferredGst?.minorUnits).toBe(1000n);
  expect(deferred.latestDeferral?.ordinaryLastClaimDate).toMatchObject(purchaseDate("2030-11-03"));
  await expectPurchaseControls(page, workspace, controls);
  const future = await regeneratePurchaseBas(page, workspace, "2026-10-01", "2026-12-31", 0n);
  const futureRow = future.purchaseReviews.find(
    (review) => review.futureClaimObligation?.documentId === document.id,
  );
  expect(futureRow?.status).toBe(PurchaseReviewStatus.CLEAR);
  expect(futureRow?.unclaimedGst?.minorUnits).toBe(1000n);
  expect(futureRow?.futureClaimObligation?.portion?.dueDateEvidence?.sourceId).toBe(proof.id);
  const futureCsv = path.join(directory, "future-claim.csv");
  const futurePdf = path.join(directory, "future-claim.pdf");
  await savePurchaseExport(electronHarness, "CSV", futureCsv, "2026-10-01", "2026-12-31");
  await savePurchaseExport(electronHarness, "PDF", futurePdf, "2026-10-01", "2026-12-31");
  await assertPurchaseExportArtifacts(futureCsv, futurePdf, future);
  await writeFile(
    path.join(directory, "future-claim.core.pb"),
    toBinary(BasWorkpaperSchema, future),
  );
  const reopened = await reopenPurchaseWorkspace(electronHarness);
  page = reopened.page;
  workspace = { ...workspace, ...reopened.workspace };
  await openPurchase(page, number);
  expect(await readPurchase(page, workspace, document.id)).toEqual(deferred);
  const reopenedPanel = page.getByRole("region", { name: "Purchase accounting", exact: true });
  await reopenedPanel
    .locator("summary")
    .filter({ hasText: /^Review claim period or defer GST$/ })
    .click();
  await reopenedPanel
    .getByRole("combobox", { name: "Retained deferral portion", exact: true })
    .selectOption("0");
  for (const [label, date] of [
    ["Claim / affected period start", "2026-10-01"],
    ["Claim / affected period end", "2026-12-31"],
    ["GST release posting date (for claim)", "2026-10-09"],
    ["Timing review as-of date", "2026-10-09"],
  ] as const)
    await reopenedPanel.getByLabel(label, { exact: true }).fill(date);
  await reopenedPanel
    .getByLabel("Claim / deferral review reason", { exact: true })
    .fill(
      "Explicit claim of the retained deferral with current supported evidence and actual due-date proof",
    );
  await reopenedPanel
    .getByLabel("Reviewed allowable period, ordinary time limit and evidence held", { exact: true })
    .check();
  await reopenedPanel.getByRole("button", { name: "Attribute reviewed GST", exact: true }).click();
  await reopenedPanel.getByRole("button", { name: "Confirm GST attribution", exact: true }).click();
  await expect
    .poll(
      async () => (await readPurchase(page, workspace, document.id)).accounting?.accountingVersion,
    )
    .toBe(5n);
  const claimed = await readPurchase(page, workspace, document.id);
  expect(claimed.accounting?.suspenseGst?.minorUnits).toBe(0n);
  expect(claimed.accounting?.attributedGst?.minorUnits).toBe(1000n);
  await expectPurchaseControls(page, workspace, { ...controls, "1150": 1000n, "1170": 0n });
  const journals = await purchaseProto(
    page,
    "accounting",
    "listJournals",
    ListJournalsRequestSchema,
    ListJournalsResponseSchema,
    create(ListJournalsRequestSchema, {
      authentication: purchaseAuth(workspace),
      organisationId: workspace.organisationId,
      page: { pageSize: 200 },
    }),
  );
  expect(journals.page?.nextCursor ?? "").toBe("");
  expect(journals.journals).toHaveLength(3);
  const release = journals.journals.find(
    (journal) => journal.source === JournalSource.PURCHASE_GST_ATTRIBUTION,
  );
  await expectPurchaseJournal(
    page,
    workspace,
    release?.id,
    JournalSource.PURCHASE_GST_ATTRIBUTION,
    "2026-10-09",
    [
      { code: "1150", debit: 1000n, credit: 0n },
      { code: "1170", debit: 0n, credit: 1000n },
    ],
  );
  const saved = await regeneratePurchaseBas(page, workspace, "2026-10-01", "2026-12-31", 1000n);
  expect(saved.purchaseSources).toHaveLength(1);
  const claim = saved.purchaseSources[0];
  expect(claim?.releasePostingDate).toMatchObject(purchaseDate("2026-10-09"));
  expect(claim?.attributionId).toBeTruthy();
  expect(
    saved.purchaseReviews.find((review) => review.deferralId === deferred.latestDeferral?.id)
      ?.unclaimedGst?.minorUnits,
  ).toBe(0n);
  // A rejected new payment version is corrective and has no financial effect.
  await expect(
    purchaseProto(
      page,
      "purchases",
      "recordPurchasePayment",
      RecordPurchasePaymentRequestSchema,
      RecordPurchasePaymentResponseSchema,
      create(RecordPurchasePaymentRequestSchema, {
        commandContext: purchaseCommand(workspace),
        organisationId: workspace.organisationId,
        documentId: document.id,
        expectedAccountingVersion: 1n,
        actualPaymentDate: purchaseDate("2026-10-09"),
        amount: { currencyCode: "AUD", minorUnits: 1n },
        bankAccountId: workspace.bank.id,
        reference: "Rejected stale request",
      }),
      131_072,
    ),
  ).rejects.toThrow();
  expect(await readPurchase(page, workspace, document.id)).toEqual(claimed);
  await expectPurchaseControls(page, workspace, { ...controls, "1150": 1000n, "1170": 0n });
  // Retaining a legacy decision on the zero-value notice is separate again.
  await openPurchase(page, "REQUIRED-RETURN-NOTICE");
  const legacyPanel = page.getByRole("region", { name: "Purchase accounting", exact: true });
  await legacyPanel
    .locator("summary")
    .filter({ hasText: /^Review legacy purchase \/ opening provenance$/ })
    .click();
  for (const [label, date] of [
    ["Legacy affected period start", "2026-10-01"],
    ["Legacy affected period end", "2026-10-09"],
    ["Legacy reporting balance date", "2026-10-09"],
  ] as const)
    await legacyPanel.getByLabel(label, { exact: true }).fill(date);
  await legacyPanel
    .getByRole("combobox", { name: "Legacy disposition", exact: true })
    .selectOption(String(LegacyPurchaseDisposition.NONCREDITABLE));
  await legacyPanel
    .getByRole("combobox", { name: "Legacy supporting evidence", exact: true })
    .selectOption(document.id);
  await legacyPanel
    .getByLabel("Legacy review reason", { exact: true })
    .fill(
      "Reviewed notice is supporting evidence with zero purchase value and no credit entitlement",
    );
  await legacyPanel
    .getByLabel(
      "Reviewed legacy accounting, settlement and claimed status against retained records",
      { exact: true },
    )
    .check();
  await legacyPanel.getByRole("button", { name: "Review legacy decision", exact: true }).click();
  await legacyPanel
    .getByRole("button", { name: "Confirm legacy purchase review", exact: true })
    .click();
  await expect(legacyPanel.getByRole("status")).toContainText("Legacy decision retained:");
  expect((await readPurchase(page, workspace, proof.id)).accounting?.accountingVersion).toBe(0n);
  expect((await readPurchase(page, workspace, proof.id)).accounting?.posted).toBe(false);
  await expectPurchaseControls(page, workspace, { ...controls, "1150": 1000n, "1170": 0n });
  const final = await regeneratePurchaseBas(page, workspace, "2026-10-01", "2026-12-31", 1000n);
  await writeFile(
    path.join(directory, "claimed-and-reviewed.pb"),
    toBinary(BasWorkpaperSchema, final),
  );
  validateBasSnapshot({
    organisation: {
      id: workspace.organisationId,
      version: 1n,
      legalName: "Synthetic Purchase Acceptance Pty Ltd",
      displayName: "Purchase Acceptance",
      abn: "51824753556",
    },
    workpaper: final,
    acquiredAt: new Date().toISOString(),
  });
  const finalCsv = path.join(directory, "claimed-and-reviewed.csv");
  const finalPdf = path.join(directory, "claimed-and-reviewed.pdf");
  await savePurchaseExport(electronHarness, "CSV", finalCsv, "2026-10-01", "2026-12-31");
  await savePurchaseExport(electronHarness, "PDF", finalPdf, "2026-10-01", "2026-12-31");
  await assertPurchaseExportArtifacts(finalCsv, finalPdf, final);
  await writeFile(
    path.join(directory, "claimed-and-reviewed.core.pb"),
    toBinary(BasWorkpaperSchema, final),
  );
  // A legacy decision can cover only dates already reviewed. It cannot waive
  // a whole future quarter; the complete Q4 draft above retains that limitation.
  const scopedLegacy = await regeneratePurchaseBas(page, workspace, "2026-10-01", "2026-10-09", 0n);
  expect(
    scopedLegacy.purchaseReviews.some(
      (review) => review.legacyReview?.disposition === LegacyPurchaseDisposition.NONCREDITABLE,
    ),
  ).toBe(true);
  const legacyCsv = path.join(directory, "reviewed-through-today.csv");
  const legacyPdf = path.join(directory, "reviewed-through-today.pdf");
  await savePurchaseExport(electronHarness, "CSV", legacyCsv, "2026-10-01", "2026-10-09");
  await savePurchaseExport(electronHarness, "PDF", legacyPdf, "2026-10-01", "2026-10-09");
  await assertPurchaseExportArtifacts(legacyCsv, legacyPdf, scopedLegacy);
  await writeFile(
    path.join(directory, "reviewed-through-today.core.pb"),
    toBinary(BasWorkpaperSchema, scopedLegacy),
  );
  await page.screenshot({
    path: path.join(directory, "claim-and-legacy-review.png"),
    animations: "disabled",
  });
  await testInfo.attach("claimed-and-reviewed-pdf", {
    path: finalPdf,
    contentType: "application/pdf",
  });
  expect(electronHarness.pageErrors).toEqual([]);
});

test("purchases/payments-gst: document and payment page 201 remain reachable and source overflow retains the saved draft", async ({
  electronHarness,
}, testInfo) => {
  test.setTimeout(420_000);
  const directory = path.join(acceptanceRoot, "pagination-capacity");
  await mkdir(directory, { recursive: true });
  let page = electronHarness.currentPage();
  let workspace = await setupPurchaseWorkspace(electronHarness, "cash");
  const number = "PURCHASE-PAGE-201";
  const document = await retainPurchaseThroughUi(page, workspace, number);
  const posted = await postPurchaseThroughUi(page, workspace, document);
  expect(posted.accounting?.accountingVersion).toBe(1n);
  const paymentIds: string[] = [];
  for (let sequence = 1; sequence <= 200; sequence++) {
    const payment = await purchaseProto(
      page,
      "purchases",
      "recordPurchasePayment",
      RecordPurchasePaymentRequestSchema,
      RecordPurchasePaymentResponseSchema,
      create(RecordPurchasePaymentRequestSchema, {
        commandContext: purchaseCommand(workspace),
        organisationId: workspace.organisationId,
        documentId: document.id,
        expectedAccountingVersion: BigInt(sequence),
        actualPaymentDate: purchaseDate("2026-09-30"),
        amount: { currencyCode: "AUD", minorUnits: 1n },
        bankAccountId: workspace.bank.id,
        reference: `Seed payment ${String(sequence).padStart(3, "0")}`,
      }),
      131_072,
    );
    expect(payment.accounting?.accountingVersion).toBe(BigInt(sequence + 1));
    if (!payment.payment) throw new Error("PURCHASE_VOLUME_PAYMENT_MISSING");
    paymentIds.push(payment.payment.id);
  }
  const proofIds: string[] = [];
  for (let sequence = 1; sequence <= 200; sequence++) {
    proofIds.push((await retainPurchaseVolumeProof(page, workspace, sequence)).id);
  }
  await page.getByRole("link", { name: "Overview", exact: true }).click();
  await page.getByRole("link", { name: "Documents", exact: true }).click();
  await expect(page.getByRole("button").filter({ hasText: number })).toHaveCount(0);
  await page.getByRole("button", { name: "Next document page", exact: true }).click();
  await page.getByRole("button").filter({ hasText: number }).click();
  const panel = page.getByRole("region", { name: "Purchase accounting", exact: true });
  await expect(panel).toBeVisible();
  await expect(page.getByRole("heading", { name: `${number}.pdf`, exact: true })).toBeVisible();
  await recordPurchasePaymentThroughUi(
    page,
    workspace,
    document.id,
    "2026-09-30",
    "108.00",
    "Final payment 201 through UI",
  );
  await panel.getByRole("button", { name: "Next payment page", exact: true }).click();
  const table = panel.getByRole("table", { name: "Purchase payments", exact: true });
  await expect(table).toContainText("Seed payment 001");
  await expect(table).not.toContainText("Seed payment 002");
  const payments = await readPurchasePayments(page, workspace, document.id);
  expect(payments).toHaveLength(201);
  expect(new Set(payments.map((payment) => payment.id)).size).toBe(201);
  expect(payments.map((payment) => payment.id).slice(1)).toEqual(paymentIds.toReversed());
  expect(payments.reduce((sum, payment) => sum + (payment.amount?.minorUnits ?? 0n), 0n)).toBe(
    11000n,
  );
  const controls = {
    "6101": 10000n,
    "1000": -11000n,
    "2000": 0n,
    "1150": 1000n,
    "1160": 0n,
    "1170": 0n,
  };
  await expectPurchaseControls(page, workspace, controls);
  // Changing filters resets the cursor while the owned selected document stays reachable.
  await page.getByRole("combobox", { name: "Accounting filter", exact: true }).selectOption("2");
  await expect(
    page.getByRole("button", { name: "Previous document page", exact: true }),
  ).toBeDisabled();
  await expect(page.getByRole("heading", { name: `${number}.pdf`, exact: true })).toBeVisible();
  await page.getByRole("combobox", { name: "Accounting filter", exact: true }).selectOption("0");
  await expect(page.getByRole("heading", { name: `${number}.pdf`, exact: true })).toBeVisible();
  const saved = await regeneratePurchaseBas(page, workspace, "2026-07-01", "2026-09-30", 1000n);
  expect(saved.purchaseSources).toHaveLength(201);
  expect(saved.purchaseReviews).toHaveLength(200);
  expect(
    saved.purchaseSources.reduce(
      (sum, source) => sum + (source.allocatedGross?.minorUnits ?? 0n),
      0n,
    ),
  ).toBe(11000n);
  expect(
    saved.purchaseReviews
      .map((review) =>
        review.legacySource?.source.case === "document"
          ? review.legacySource.source.value.documentId
          : undefined,
      )
      .sort(),
  ).toEqual(proofIds.toSorted());
  const csv = path.join(directory, "complete.csv");
  const pdf = path.join(directory, "complete.pdf");
  await savePurchaseExport(electronHarness, "CSV", csv, "2026-07-01", "2026-09-30");
  await savePurchaseExport(electronHarness, "PDF", pdf, "2026-07-01", "2026-09-30");
  await assertPurchaseExportArtifacts(csv, pdf, saved);
  await writeFile(path.join(directory, "complete.core.pb"), toBinary(BasWorkpaperSchema, saved));
  for (let sequence = 201; sequence <= 1001; sequence++) {
    proofIds.push((await retainPurchaseVolumeProof(page, workspace, sequence)).id);
  }
  await page.getByRole("button", { name: "Regenerate local draft", exact: true }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  const retained = await readPurchaseBas(page, workspace);
  expect(retained.id).toBe(saved.id);
  expect(retained.version).toBe(saved.version);
  expect(retained.purchaseSources).toEqual(saved.purchaseSources);
  expect(retained.purchaseReviews).toEqual(saved.purchaseReviews);
  expect(retained.gstCredits1b).toEqual(saved.gstCredits1b);
  expect(retained.sourcesChanged).toBe(true);
  expect(retained.reviewWarnings).toContain(
    "The complete current source set exceeds supported workpaper capacity. Saved figures have been retained; complete preparation requires supported volume.",
  );
  const overflowCsv = path.join(directory, "overflow-retained.csv");
  const overflowPdf = path.join(directory, "overflow-retained.pdf");
  await savePurchaseExport(electronHarness, "CSV", overflowCsv, "2026-07-01", "2026-09-30");
  await savePurchaseExport(electronHarness, "PDF", overflowPdf, "2026-07-01", "2026-09-30");
  await assertPurchaseExportArtifacts(overflowCsv, overflowPdf, retained);
  await writeFile(
    path.join(directory, "overflow-retained.core.pb"),
    toBinary(BasWorkpaperSchema, retained),
  );
  await page.screenshot({
    path: path.join(directory, "source-capacity-retained.png"),
    animations: "disabled",
  });
  const reopened = await reopenPurchaseWorkspace(electronHarness);
  page = reopened.page;
  workspace = { ...workspace, ...reopened.workspace };
  expect(await readPurchasePayments(page, workspace, document.id)).toEqual(payments);
  expect((await readPurchaseBas(page, workspace)).id).toBe(saved.id);
  await expectPurchaseControls(page, workspace, controls);
  await writeFile(
    path.join(directory, "reconciliation.json"),
    `${JSON.stringify({ documentId: document.id, retainedWorkpaperId: saved.id, paymentIds: payments.map((payment) => payment.id), reviewedProofIds: proofIds, sourceCount: saved.purchaseSources.length, retainedReviewCount: saved.purchaseReviews.length, rejectedCurrentCandidateCount: proofIds.length, gstMinor: "1000", actualLedger: Object.fromEntries(Object.entries(controls).map(([code, minor]) => [code, minor.toString()])), artifacts: await Promise.all([csv, pdf, overflowCsv, overflowPdf].map(async (file) => ({ file: path.basename(file), sha256: await reportArtifactHash(file) }))) }, null, 2)}\n`,
  );
  await testInfo.attach("pagination-capacity-reconciliation", {
    path: path.join(directory, "reconciliation.json"),
    contentType: "application/json",
  });
  expect(electronHarness.pageErrors).toEqual([]);
  expect(electronHarness.consoleErrors).toEqual([]);
});

for (const variant of [
  { basis: "cash", mixed: false },
  { basis: "non-cash", mixed: false },
  { basis: "cash", mixed: true },
] as const) {
  test(`purchases/payments-gst: ${variant.basis}${variant.mixed ? " mixed/private/capital" : ""} purchase, exact saved files and evidence survive restart`, async ({
    electronHarness,
  }, testInfo) => {
    test.setTimeout(240_000);
    const directory = path.join(acceptanceRoot, `${variant.basis}${variant.mixed ? "-mixed" : ""}`);
    await mkdir(directory, { recursive: true });
    let workspace = await setupPurchaseWorkspace(electronHarness, variant.basis);
    let page = electronHarness.currentPage();
    const bankingBaseline = await seedPurchaseBankingReconciliationBaseline(page, workspace);
    let preActionsPersisted: PurchasePersistedEvidence | undefined;
    const beforeActionsReopened = await reopenPurchaseWorkspace(
      electronHarness,
      async (userDataPath) => {
        preActionsPersisted = await inspectPurchasePersistedEvidence(userDataPath, workspace);
      },
    );
    if (!preActionsPersisted) throw new Error("PURCHASE_PRE_ACTIONS_EVIDENCE_MISSING");
    expect(preActionsPersisted.facts).toEqual([]);
    expect(preActionsPersisted.projection).toEqual({});
    expect(preActionsPersisted.sbrSimulatorTransportCount).toBe(0);
    workspace = { ...workspace, ...beforeActionsReopened.workspace };
    page = beforeActionsReopened.page;
    const bankingBefore = await capturePurchaseBankingSnapshot(page, workspace);
    expect(bankingBefore).toEqual(bankingBaseline);
    const number = `PURCHASE-${variant.basis === "cash" ? "CASH" : "ACCRUAL"}${variant.mixed ? "-MIXED" : ""}-001`;
    const document = await retainPurchaseThroughUi(page, workspace, number);
    const reviewed = await readPurchase(page, workspace, document.id);
    expect(reviewed.accounting?.posted).toBe(false);
    await expectPurchaseControls(page, workspace, {
      "6101": 0n,
      "1000": 0n,
      "2000": 0n,
      "1150": 0n,
      "1160": 0n,
      "1170": 0n,
    });
    const posted = await postPurchaseThroughUi(page, workspace, document, { mixed: variant.mixed });
    const eligible = variant.mixed ? 800n : 1000n;
    const cost = variant.mixed ? 6000n : 10000n;
    const capital = variant.mixed ? 2000n : 0n;
    const privateUse = variant.mixed ? 2200n : 0n;
    const posting = posted.accounting?.posting;
    expect(posting?.supplierGst?.minorUnits).toBe(1000n);
    expect(posted.accounting?.eligibleGst?.minorUnits).toBe(eligible);
    const postingJournal = await expectPurchaseJournal(
      page,
      workspace,
      posting?.journalId,
      JournalSource.PURCHASE,
      "2026-08-31",
      [
        { code: "6101", debit: cost, credit: 0n },
        ...(variant.mixed
          ? [
              { code: "1501", debit: capital, credit: 0n },
              { code: "3101", debit: privateUse, credit: 0n },
            ]
          : []),
        { code: variant.basis === "cash" ? "1160" : "1150", debit: eligible, credit: 0n },
        { code: "2000", debit: 0n, credit: 11000n },
      ],
    );
    await expectPurchaseControls(page, workspace, {
      "6101": cost,
      "1501": capital,
      "3101": privateUse,
      "1000": 0n,
      "2000": -11000n,
      "1150": variant.basis === "cash" ? 0n : eligible,
      "1160": variant.basis === "cash" ? eligible : 0n,
      "1170": 0n,
    });
    const initial = await regeneratePurchaseBas(
      page,
      workspace,
      "2026-07-01",
      "2026-09-30",
      variant.basis === "cash" ? 0n : eligible,
    );
    expect(initial.purchaseSources.every((source) => source.postingId === posting?.id)).toBe(true);
    await openPurchase(page, number);
    const first = await recordPurchasePaymentThroughUi(
      page,
      workspace,
      document.id,
      "2026-09-30",
      "55.00",
      "First actual half",
    );
    expect(first.accounting?.accountingVersion).toBe(2n);
    expect(first.accounting?.paid?.minorUnits).toBe(5500n);
    expect(first.accounting?.outstanding?.minorUnits).toBe(5500n);
    const firstPayments = await readPurchasePayments(page, workspace, document.id);
    expect(firstPayments).toHaveLength(1);
    const firstJournal = await expectPurchaseJournal(
      page,
      workspace,
      firstPayments[0]?.journalId,
      JournalSource.PURCHASE_PAYMENT,
      "2026-09-30",
      [
        { code: "2000", debit: 5500n, credit: 0n },
        { code: "1000", debit: 0n, credit: 5500n },
        ...(variant.basis === "cash"
          ? [
              { code: "1150", debit: eligible / 2n, credit: 0n },
              { code: "1160", debit: 0n, credit: eligible / 2n },
            ]
          : []),
      ],
    );
    await expectPurchaseControls(page, workspace, {
      "6101": cost,
      "1501": capital,
      "3101": privateUse,
      "1000": -5500n,
      "2000": -5500n,
      "1150": variant.basis === "cash" ? eligible / 2n : eligible,
      "1160": variant.basis === "cash" ? eligible / 2n : 0n,
      "1170": 0n,
    });
    const firstSaved = await regeneratePurchaseBas(
      page,
      workspace,
      "2026-07-01",
      "2026-09-30",
      variant.basis === "cash" ? eligible / 2n : eligible,
    );
    const firstCsv = path.join(directory, "first-quarter.csv");
    const firstPdf = path.join(directory, "first-quarter.pdf");
    await savePurchaseExport(electronHarness, "CSV", firstCsv, "2026-07-01", "2026-09-30");
    await savePurchaseExport(electronHarness, "PDF", firstPdf, "2026-07-01", "2026-09-30");
    await assertPurchaseExportArtifacts(firstCsv, firstPdf, firstSaved);
    await writeFile(
      path.join(directory, "first-quarter.core.pb"),
      toBinary(BasWorkpaperSchema, firstSaved),
    );
    await openPurchase(page, number);
    const final = await recordPurchasePaymentThroughUi(
      page,
      workspace,
      document.id,
      "2026-10-02",
      "55.00",
      "Final actual half",
    );
    expect(final.accounting?.accountingVersion).toBe(3n);
    expect(final.accounting?.paid?.minorUnits).toBe(11000n);
    expect(final.accounting?.outstanding?.minorUnits).toBe(0n);
    expect(final.accounting?.attributedGst?.minorUnits).toBe(eligible);
    expect(final.accounting?.deferredGst?.minorUnits).toBe(0n);
    expect(final.accounting?.suspenseGst?.minorUnits).toBe(0n);
    await expectPurchaseControls(page, workspace, {
      "6101": cost,
      "1501": capital,
      "3101": privateUse,
      "1000": -11000n,
      "2000": 0n,
      "1150": eligible,
      "1160": 0n,
      "1170": 0n,
    });
    const payments = await readPurchasePayments(page, workspace, document.id);
    expect(payments).toHaveLength(2);
    expect(payments[1]).toEqual(firstPayments[0]);
    const finalJournal = await expectPurchaseJournal(
      page,
      workspace,
      payments[0]?.journalId,
      JournalSource.PURCHASE_PAYMENT,
      "2026-10-02",
      [
        { code: "2000", debit: 5500n, credit: 0n },
        { code: "1000", debit: 0n, credit: 5500n },
        ...(variant.basis === "cash"
          ? [
              { code: "1150", debit: eligible / 2n, credit: 0n },
              { code: "1160", debit: 0n, credit: eligible / 2n },
            ]
          : []),
      ],
    );
    await writeFile(
      path.join(directory, "journal-vectors.json"),
      JSON.stringify(
        { postingJournal, firstJournal, finalJournal },
        (_key, value: unknown) => (typeof value === "bigint" ? value.toString() : value),
        2,
      ),
    );
    expect(payments.map((payment) => payment.actualPaymentDate)).toMatchObject([
      { year: 2026, month: 10, day: 2 },
      { year: 2026, month: 9, day: 30 },
    ]);
    expect(
      payments.reduce(
        (sum, payment) =>
          sum +
          payment.allocations.reduce(
            (amount, allocation) => amount + (allocation.eligibleGst?.minorUnits ?? 0n),
            0n,
          ),
        0n,
      ),
    ).toBe(eligible);
    for (const payment of payments) {
      expect(payment.sourceHash).toHaveLength(32);
      expect(payment.journalId).toBeTruthy();
      expect(
        payment.allocations.reduce(
          (sum, allocation) => sum + (allocation.gross?.minorUnits ?? 0n),
          0n,
        ),
      ).toBe(payment.amount?.minorUnits);
    }
    const secondSaved = await regeneratePurchaseBas(
      page,
      workspace,
      "2026-10-01",
      "2026-12-31",
      variant.basis === "cash" ? eligible / 2n : 0n,
    );
    expect(secondSaved.sources).toHaveLength(0);
    expect(secondSaved.legacyCandidateGst?.minorUnits).toBe(0n);
    if (variant.basis === "cash") {
      expect(secondSaved.purchaseSources.map((source) => source.paymentId)).toEqual(
        payments[0]!.allocations.map(() => payments[0]!.id),
      );
      for (const source of secondSaved.purchaseSources) {
        expect(source.invoiceDate).toMatchObject({ year: 2026, month: 8, day: 31 });
        expect(source.actualPaymentDate).toMatchObject({ year: 2026, month: 10, day: 2 });
        expect(source.postingHash).toEqual(posting?.sourceHash);
        expect(source.accountId).toBeTruthy();
      }
    }
    const secondCsv = path.join(directory, "second-quarter.csv");
    const secondPdf = path.join(directory, "second-quarter.pdf");
    await savePurchaseExport(electronHarness, "CSV", secondCsv, "2026-10-01", "2026-12-31");
    await savePurchaseExport(electronHarness, "PDF", secondPdf, "2026-10-01", "2026-12-31");
    await assertPurchaseExportArtifacts(secondCsv, secondPdf, secondSaved);
    await writeFile(
      path.join(directory, "second-quarter.core.pb"),
      toBinary(BasWorkpaperSchema, secondSaved),
    );
    const canceled = path.join(directory, "canceled.csv");
    await electronHarness.application.evaluate(({ dialog }, filePath) => {
      dialog.showSaveDialog = async () => ({ canceled: true, filePath });
    }, canceled);
    await page.getByRole("button", { name: "Export CSV", exact: true }).click();
    await expect(page.getByText(/export cancelled\./)).toBeVisible();
    expect(
      await stat(canceled).then(
        () => true,
        () => false,
      ),
    ).toBe(false);
    expect((await readPurchaseBas(page, workspace)).id).toBe(secondSaved.id);
    await openPurchase(page, number);
    const withdrawn = await reviewPurchaseEvidenceThroughUi(
      page,
      workspace,
      document.id,
      "missing",
      "",
    );
    expect(withdrawn.accounting?.attributedGst?.minorUnits).toBe(eligible);
    expect(withdrawn.accounting?.reviewOutcomes?.evidence).toBe(
      PurchaseReviewStatus.NEEDS_EVIDENCE,
    );
    await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
    await expect(page.getByText(/Sources changed since this workpaper was saved/)).toBeVisible();
    const stale = await readPurchaseBas(page, workspace);
    expect(stale.id).toBe(secondSaved.id);
    expect(stale.version).toBe(secondSaved.version);
    expect(stale.gstCredits1b).toEqual(secondSaved.gstCredits1b);
    expect(stale.purchaseSources).toEqual(secondSaved.purchaseSources);
    expect(stale.purchaseReviews).toEqual(secondSaved.purchaseReviews);
    const staleCsv = path.join(directory, "retained-stale.csv");
    const stalePdf = path.join(directory, "retained-stale.pdf");
    await savePurchaseExport(electronHarness, "CSV", staleCsv, "2026-10-01", "2026-12-31");
    await savePurchaseExport(electronHarness, "PDF", stalePdf, "2026-10-01", "2026-12-31");
    await assertPurchaseExportArtifacts(staleCsv, stalePdf, stale);
    await writeFile(
      path.join(directory, "retained-stale.core.pb"),
      toBinary(BasWorkpaperSchema, stale),
    );
    const screenshot = path.join(directory, "stale-retained-workpaper.png");
    await page.screenshot({ path: screenshot, animations: "disabled" });
    await testInfo.attach("stale-retained-workpaper", {
      path: screenshot,
      contentType: "image/png",
    });
    let persisted: PurchasePersistedEvidence | undefined;
    const reopened = await reopenPurchaseWorkspace(electronHarness, async (userDataPath) => {
      persisted = await inspectPurchasePersistedEvidence(userDataPath, workspace);
    });
    if (!persisted) throw new Error("PURCHASE_PERSISTED_EVIDENCE_MISSING");
    expect(persisted.sbrSimulatorTransportCount).toBe(
      preActionsPersisted.sbrSimulatorTransportCount,
    );
    expect(persisted.projection).toEqual(
      variant.mixed
        ? { OPERATING: "-6600", INVESTING: "-2200", FINANCING: "-2200" }
        : { OPERATING: "-11000" },
    );
    expect(persisted.facts).toHaveLength(variant.mixed ? 6 : 2);
    expect(new Set(persisted.facts.map((fact) => fact.id)).size).toBe(persisted.facts.length);
    for (const payment of payments) {
      const facts = persisted.facts.filter((fact) => fact.paymentId === payment.id);
      expect(facts.map((fact) => [fact.category, fact.amountMinor])).toEqual(
        variant.mixed
          ? [
              ["OPERATING", "-3300"],
              ["INVESTING", "-1100"],
              ["FINANCING", "-1100"],
            ]
          : [["OPERATING", "-5500"]],
      );
      for (const fact of facts) {
        expect(fact.journalId).toBe(payment.journalId);
        // Each retained payment is the first revision of its own source journal.
        expect(fact.sourceRevision).toBe("1");
        expect(fact.paymentDate).toBe(
          payment.actualPaymentDate?.month === 9 ? "2026-09-30" : "2026-10-02",
        );
      }
    }
    await writeFile(
      path.join(directory, "actual-cash-flow-facts.json"),
      `${JSON.stringify(persisted, null, 2)}\n`,
    );
    page = reopened.page;
    expect(reopened.workspace.workspaceId).toBe(workspace.workspaceId);
    expect(reopened.workspace.sessionId).not.toBe(workspace.sessionId);
    const bankingAfter = await capturePurchaseBankingSnapshot(page, {
      ...workspace,
      ...reopened.workspace,
    });
    expect(bankingAfter).toEqual(bankingBefore);
    await openPurchase(page, number);
    const retained = await readPurchase(page, reopened.workspace, document.id);
    expect(retained).toEqual(withdrawn);
    expect(await readPurchasePayments(page, reopened.workspace, document.id)).toEqual(payments);
    await expectPurchaseControls(page, reopened.workspace, {
      "6101": cost,
      "1501": capital,
      "3101": privateUse,
      "1000": -11000n,
      "2000": 0n,
      "1150": eligible,
      "1160": 0n,
      "1170": 0n,
    });
    await openPurchase(page, number);
    const postingDetails = page
      .locator("summary")
      .filter({ hasText: /^Retained posting and evidence details$/ });
    await postingDetails.focus();
    await expect(postingDetails).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(
      postingDetails
        .locator("..")
        .getByText(Buffer.from(posting?.sourceHash ?? []).toString("hex"), { exact: true }),
    ).toBeVisible();
    const latestPayment = payments[0];
    if (!latestPayment) throw new Error("PURCHASE_RETAINED_PAYMENT_MISSING");
    const paymentDetails = page
      .locator("summary")
      .filter({ hasText: /^Retained payment details$/ })
      .first();
    await paymentDetails.focus();
    await expect(paymentDetails).toBeFocused();
    await page.keyboard.press("Enter");
    await expect(
      paymentDetails
        .locator("..")
        .getByText(Buffer.from(latestPayment.sourceHash).toString("hex"), { exact: true }),
    ).toBeVisible();
    const purchaseScreenshot = path.join(directory, "retained-purchase-details.png");
    await page.screenshot({ path: purchaseScreenshot, animations: "disabled", fullPage: true });
    await testInfo.attach("retained-purchase-details", {
      path: purchaseScreenshot,
      contentType: "image/png",
    });
    await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
    const restartedWorkpaper = await readPurchaseBas(page, reopened.workspace);
    expect(restartedWorkpaper.id).toBe(secondSaved.id);
    const restartedCsv = path.join(directory, "restarted.csv");
    await savePurchaseExport(electronHarness, "CSV", restartedCsv, "2026-10-01", "2026-12-31");
    await assertPurchaseExportArtifacts(restartedCsv, stalePdf, restartedWorkpaper);
    await writeFile(
      path.join(directory, "restarted.core.pb"),
      toBinary(BasWorkpaperSchema, restartedWorkpaper),
    );
    const [beforeRestart, afterRestart] = await Promise.all([
      parseReportCsv(staleCsv),
      parseReportCsv(restartedCsv),
    ]);
    const acquisition = (rows: typeof beforeRestart.rows) => {
      const matches = rows.filter((row) => row.kind === "metadata" && row.metric === "acquired_at");
      expect(matches).toHaveLength(1);
      const value = matches[0]?.value;
      if (value === undefined) throw new Error("PURCHASE_EXPORT_ACQUISITION_MISSING");
      expect(value).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
      expect(new Date(value).toISOString()).toBe(value);
      return value;
    };
    const acquiredBefore = acquisition(beforeRestart.rows);
    const acquiredAfter = acquisition(afterRestart.rows);
    expect(Date.parse(acquiredAfter)).toBeGreaterThanOrEqual(Date.parse(acquiredBefore));
    expect(afterRestart.header).toEqual(beforeRestart.header);
    expect(afterRestart.rows).toEqual(
      beforeRestart.rows.map((row) =>
        row.kind === "metadata" && row.metric === "acquired_at"
          ? { ...row, value: acquiredAfter }
          : row,
      ),
    );
    const artifactFiles = [
      firstCsv,
      firstPdf,
      path.join(directory, "first-quarter.core.pb"),
      secondCsv,
      secondPdf,
      path.join(directory, "second-quarter.core.pb"),
      staleCsv,
      stalePdf,
      path.join(directory, "retained-stale.core.pb"),
      restartedCsv,
      path.join(directory, "restarted.core.pb"),
      purchaseScreenshot,
    ];
    await writeFile(
      path.join(directory, "reconciliation.json"),
      `${JSON.stringify({ workspaceId: workspace.workspaceId, organisationId: workspace.organisationId, documentId: document.id, postingId: posting?.id, paymentIds: payments.map((payment) => payment.id), firstWorkpaperId: firstSaved.id, secondWorkpaperId: secondSaved.id, gstMinor: eligible.toString(), actualLedger: { expenseMinor: cost.toString(), bankMinor: "-11000", payableMinor: "0", inputGstMinor: eligible.toString() }, bankingBefore, bankingAfter, sbrSimulatorTransportCount: persisted.sbrSimulatorTransportCount, artifacts: await Promise.all(artifactFiles.map(async (file) => ({ file: path.basename(file), sha256: await reportArtifactHash(file) }))) }, null, 2)}\n`,
    );
    expect(electronHarness.consoleErrors).toEqual([]);
    expect(electronHarness.pageErrors).toEqual([]);
  });
}
