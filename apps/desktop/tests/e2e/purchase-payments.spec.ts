import { mkdir, readFile, stat, writeFile } from "node:fs/promises";
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
import { expect, test } from "./fixtures";
import { assertPurchaseExportArtifacts } from "./support/purchase-export-assertions";
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
  reviewPurchaseEvidenceThroughUi,
  savePurchaseExport,
  setupPurchaseWorkspace,
} from "./support/purchase-payments-workflow";
import { reportArtifactHash } from "./support/reporting-export-artifacts";

const acceptanceRoot = fileURLToPath(
  new URL("../../../../.tmp/purchase-payments-gst-e2e/", import.meta.url),
);

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
  await panel.getByLabel("Due-date proof", { exact: true }).selectOption(proof.id);
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
    ["Legacy affected period end", "2026-12-31"],
    ["Legacy reporting balance date", "2026-10-09"],
  ] as const)
    await legacyPanel.getByLabel(label, { exact: true }).fill(date);
  await legacyPanel
    .getByRole("combobox", { name: "Legacy disposition", exact: true })
    .selectOption(String(LegacyPurchaseDisposition.NONCREDITABLE));
  await legacyPanel
    .getByLabel("Legacy supporting evidence", { exact: true })
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
  await expect
    .poll(async () => (await readPurchase(page, workspace, proof.id)).accounting?.accountingVersion)
    .toBe(1n);
  expect((await readPurchase(page, workspace, proof.id)).accounting?.posted).toBe(false);
  await expectPurchaseControls(page, workspace, { ...controls, "1150": 1000n, "1170": 0n });
  const final = await regeneratePurchaseBas(page, workspace, "2026-10-01", "2026-12-31", 1000n);
  const finalCsv = path.join(directory, "claimed-and-reviewed.csv");
  const finalPdf = path.join(directory, "claimed-and-reviewed.pdf");
  await savePurchaseExport(electronHarness, "CSV", finalCsv, "2026-10-01", "2026-12-31");
  await savePurchaseExport(electronHarness, "PDF", finalPdf, "2026-10-01", "2026-12-31");
  await assertPurchaseExportArtifacts(finalCsv, finalPdf, final);
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
    const workspace = await setupPurchaseWorkspace(electronHarness, variant.basis);
    let page = electronHarness.currentPage();
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
    const screenshot = path.join(directory, "stale-retained-workpaper.png");
    await page.screenshot({ path: screenshot, animations: "disabled" });
    await testInfo.attach("stale-retained-workpaper", {
      path: screenshot,
      contentType: "image/png",
    });
    const reopened = await reopenPurchaseWorkspace(electronHarness);
    page = reopened.page;
    expect(reopened.workspace.workspaceId).toBe(workspace.workspaceId);
    expect(reopened.workspace.sessionId).not.toBe(workspace.sessionId);
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
    await page.getByRole("link", { name: "GST & BAS", exact: true }).click();
    expect((await readPurchaseBas(page, reopened.workspace)).id).toBe(secondSaved.id);
    const restartedCsv = path.join(directory, "restarted.csv");
    await savePurchaseExport(electronHarness, "CSV", restartedCsv, "2026-10-01", "2026-12-31");
    expect(await readFile(restartedCsv)).toEqual(await readFile(staleCsv));
    const artifactFiles = [
      firstCsv,
      firstPdf,
      secondCsv,
      secondPdf,
      staleCsv,
      stalePdf,
      restartedCsv,
    ];
    await writeFile(
      path.join(directory, "reconciliation.json"),
      `${JSON.stringify({ workspaceId: workspace.workspaceId, organisationId: workspace.organisationId, documentId: document.id, postingId: posting?.id, paymentIds: payments.map((payment) => payment.id), firstWorkpaperId: firstSaved.id, secondWorkpaperId: secondSaved.id, gstMinor: eligible.toString(), actualLedger: { expenseMinor: cost.toString(), bankMinor: "-11000", payableMinor: "0", inputGstMinor: eligible.toString() }, artifacts: await Promise.all(artifactFiles.map(async (file) => ({ file: path.basename(file), sha256: await reportArtifactHash(file) }))) }, null, 2)}\n`,
    );
    expect(electronHarness.consoleErrors).toEqual([]);
    expect(electronHarness.pageErrors).toEqual([]);
  });
}
