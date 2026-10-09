import { create } from "@bufbuild/protobuf";
import {
  AccountDesignation,
  AccountSchema,
  AccountStatus,
  AccountType,
  ListAccountsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { MoneySchema, SourceRefSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentCandidateSchema,
  DocumentSchema,
  DocumentStatus,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { Role } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  AttributePurchaseGstResponseSchema,
  DeferPurchaseGstClaimResponseSchema,
  GetPurchaseAccountingResponseSchema,
  LegacyPurchaseDisposition,
  LegacyPurchaseReviewSchema,
  ListPurchasePaymentsResponseSchema,
  PostReviewedPurchaseResponseSchema,
  PurchaseAccountingSchema,
  PurchaseAllocationInputSchema,
  PurchaseBasClassification,
  PurchaseCashFlowClassification,
  PurchaseDuplicateMatchSchema,
  PurchaseEvidenceClassification,
  PurchaseEvidenceInputSchema,
  PurchaseGstAttributionSchema,
  PurchaseGstDeferralSchema,
  PurchasePaymentAllocationSchema,
  PurchasePaymentSchema,
  PurchasePostingSchema,
  PurchaseTaxEvidenceSchema,
  PurchaseTaxInvoiceExceptionKind,
  PurchaseTaxInvoiceExceptionReviewSchema,
  PurchaseUse,
  RecordPurchaseTaxEvidenceResponseSchema,
  ResolveLegacyPurchaseReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it } from "vitest";

import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
import {
  accountCodec,
  PurchaseAccountingPanel,
  purchaseCodecs,
  purchaseCorrectionGuidance,
} from "./purchase-accounting-panel";
import { retainedDocumentCodec } from "./purchase-codecs";
import { purchaseAttemptsFor } from "./purchase-command-state";

const org = "018f0000-0000-7000-8000-000000000004";
const doc = create(DocumentSchema, {
  id: "018f0000-0000-7000-8000-000000000010",
  organisationId: org,
  version: 2n,
  status: DocumentStatus.REVIEWED,
  sha256: new Uint8Array(32).fill(3),
  sourceDisplayName: "office.pdf",
  candidate: create(DocumentCandidateSchema, {
    supplierName: "Office",
    invoiceNumber: "INV1",
    documentDate: { year: 2026, month: 9, day: 25 },
    subtotal: { currencyCode: "AUD", minorUnits: 10000n },
    gst: { currencyCode: "AUD", minorUnits: 1000n },
    total: { currencyCode: "AUD", minorUnits: 11000n },
  }),
});
const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  organisationId: org,
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  roles: [Role.WORKSPACE_ADMIN],
};
function duplicateCandidate(id: string, documentId: string, invoiceNumber: string) {
  return create(PurchaseDuplicateMatchSchema, {
    posting: create(SourceRefSchema, {
      type: "purchase_posting",
      id,
      revision: 1n,
      contentHash: new Uint8Array(32).fill(12),
    }),
    documentId,
    supplierName: "Office Warehouse",
    invoiceNumber,
    invoiceDate: { year: 2026, month: 8, day: 10 },
    gross: { currencyCode: "AUD", minorUnits: 5500n },
  });
}
afterEach(() =>
  purchaseAttemptsFor(
    `${workspace.workspaceId}:${org}:${workspace.userId}:document:${doc.id}`,
  ).confirmed(),
);
function setup(posted = false) {
  const fixture = createConveyorFixture();
  const accounting = create(PurchaseAccountingSchema, {
    organisationId: org,
    documentId: doc.id,
    posted,
    accountingVersion: posted ? 1n : 0n,
    paymentCount: posted ? 201n : 0n,
    ...(posted
      ? {
          posting: create(PurchasePostingSchema, {
            id: "018f0000-0000-7000-8000-000000000020",
            documentId: doc.id,
            organisationId: org,
            invoiceDate: { year: 2026, month: 9, day: 25 },
            gstBasis: GstBasis.NON_CASH,
          }),
        }
      : {}),
    outstanding: { currencyCode: "AUD", minorUnits: 11000n },
    paid: { currencyCode: "AUD", minorUnits: 0n },
    eligibleGst: { currencyCode: "AUD", minorUnits: 1000n },
  });
  fixture.procedures.purchases.getPurchaseAccounting.mockResolvedValue(
    purchaseCodecs.get.encodeResponse(create(GetPurchaseAccountingResponseSchema, { accounting })),
  );
  fixture.procedures.purchases.listPurchasePayments.mockResolvedValue(
    purchaseCodecs.list.encodeResponse(
      create(ListPurchasePaymentsResponseSchema, { page: { returnedCount: 0 } }),
    ),
  );
  fixture.procedures.accounting.listAccounts.mockResolvedValue(
    accountCodec.encodeResponse(
      create(ListAccountsResponseSchema, {
        accounts: [
          create(AccountSchema, {
            id: "018f0000-0000-7000-8000-000000000030",
            organisationId: org,
            version: 1n,
            code: "6000",
            name: "Office expense",
            type: AccountType.EXPENSE,
            designation: AccountDesignation.ORDINARY,
            status: AccountStatus.ACTIVE,
          }),
          create(AccountSchema, {
            id: "018f0000-0000-7000-8000-000000000031",
            organisationId: org,
            version: 1n,
            code: "1000",
            name: "Bank",
            type: AccountType.ASSET,
            designation: AccountDesignation.SYSTEM,
            status: AccountStatus.ACTIVE,
            reportClassification: "balance_sheet.cash",
          }),
        ],
        page: { returnedCount: 2 },
      }),
    ),
  );
  return { fixture, accounting };
}
it("keeps extraction review separate from an explicit confirmed purchase posting", async () => {
  const { fixture, accounting } = setup();
  fixture.procedures.purchases.postReviewedPurchase.mockImplementation(
    async (frame: Uint8Array) => {
      const request = purchaseCodecs.post.decodeRequest(frame);
      expect(request.documentId).toBe(doc.id);
      expect(request.expectedReviewedVersion).toBe(2n);
      expect(request.allocations[0]?.eligibleGst?.minorUnits).toBe(1000n);
      return purchaseCodecs.post.encodeResponse(
        create(PostReviewedPurchaseResponseSchema, {
          posting: create(PurchasePostingSchema, { id: "018f0000-0000-7000-8000-000000000020" }),
          accounting: { ...accounting, posted: true },
        }),
      );
    },
  );
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  expect(await screen.findByRole("heading", { name: "Purchase accounting" })).toBeTruthy();
  expect(fixture.procedures.purchases.postReviewedPurchase).not.toHaveBeenCalled();
  await user.selectOptions(
    await screen.findByLabelText("Account 1"),
    "018f0000-0000-7000-8000-000000000030",
  );
  await user.type(screen.getByLabelText("Eligible GST 1 (AUD)"), "10.00");
  await user.type(
    screen.getByLabelText("Evidence review reason"),
    "Registration and invoice reviewed",
  );
  await user.type(
    screen.getByLabelText("Prior accounting review reason"),
    "Checked existing books",
  );
  for (const name of [
    "Eligibility and evidence reviewed for allocation 1",
    "Registration, business use and evidence reviewed",
    "Checked existing accounting and GST claims",
    "No duplicate transaction found",
  ])
    await user.click(screen.getByLabelText(name));
  await user.click(screen.getByRole("button", { name: "Add purchase to accounts" }));
  expect(screen.getByText(/Records the expense once/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Confirm purchase posting" }));
  await waitFor(() =>
    expect(fixture.procedures.purchases.postReviewedPurchase).toHaveBeenCalledOnce(),
  );
});

it("requires the selected retained source when evidence is changed from the acquisition", async () => {
  const { fixture } = setup();
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText("Reviewed source · not added to accounts");
  await user.selectOptions(
    screen.getByLabelText("Account 1"),
    "018f0000-0000-7000-8000-000000000030",
  );
  await user.type(screen.getByLabelText("Eligible GST 1 (AUD)"), "10.00");
  await user.selectOptions(screen.getByLabelText("GST evidence classification"), "2");
  await user.type(screen.getByLabelText("Actual evidence held date"), "2026-09-30");
  await user.type(
    screen.getByLabelText("Evidence review reason"),
    "Registration and invoice reviewed",
  );
  await user.type(
    screen.getByLabelText("Prior accounting review reason"),
    "Checked existing books",
  );
  for (const name of [
    "Eligibility and evidence reviewed for allocation 1",
    "Registration, business use and evidence reviewed",
    "Checked existing accounting and GST claims",
    "No duplicate transaction found",
    "Use another reviewed retained document as evidence",
  ])
    await user.click(screen.getByLabelText(name));
  await user.click(screen.getByRole("button", { name: "Add purchase to accounts" }));
  expect(
    await screen.findByText("Select the reviewed evidence source before confirming this review."),
  ).toBeTruthy();
  expect(screen.queryByRole("alertdialog", { name: "Confirm purchase action" })).toBeNull();
  expect(fixture.procedures.purchases.postReviewedPurchase).not.toHaveBeenCalled();
});

it("selects a verified core-discovered posting before recording a distinct transaction", async () => {
  const { fixture, accounting } = setup();
  const match = duplicateCandidate(
    "018f0000-0000-7000-8000-000000000050",
    "018f0000-0000-7000-8000-000000000051",
    "INV-42",
  );
  fixture.procedures.purchases.getPurchaseAccounting.mockResolvedValue(
    purchaseCodecs.get.encodeResponse(
      create(GetPurchaseAccountingResponseSchema, {
        accounting,
        duplicateCandidates: [match],
      }),
    ),
  );
  fixture.procedures.purchases.postReviewedPurchase.mockImplementation(
    async (frame: Uint8Array) => {
      const request = purchaseCodecs.post.decodeRequest(frame);
      expect(request.duplicateReview?.decision).toBe(2);
      expect(request.duplicateReview?.matchedPostingId).toBe(match.posting?.id);
      expect(request.duplicateReview?.reason).toBe("Checked the separate warehouse delivery");
      return purchaseCodecs.post.encodeResponse(
        create(PostReviewedPurchaseResponseSchema, {
          posting: create(PurchasePostingSchema, { id: "018f0000-0000-7000-8000-000000000020" }),
          accounting: { ...accounting, posted: true },
        }),
      );
    },
  );
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  expect(await screen.findByText(/Office Warehouse · INV-42/)).toBeTruthy();
  expect(screen.getByText(/018f0000-0000-7000-8000-000000000050 · revision 1/)).toBeTruthy();
  expect(document.body.textContent).toContain("0c".repeat(32));
  expect(screen.queryByLabelText("No duplicate transaction found")).toBeNull();
  await user.selectOptions(screen.getByLabelText("Matched purchase candidate"), match.posting!.id);
  await user.selectOptions(
    await screen.findByLabelText("Account 1"),
    "018f0000-0000-7000-8000-000000000030",
  );
  await user.type(screen.getByLabelText("Eligible GST 1 (AUD)"), "10.00");
  await user.type(screen.getByLabelText("Evidence review reason"), "Registration reviewed");
  await user.type(screen.getByLabelText("Prior accounting review reason"), "Books reviewed");
  await user.click(screen.getByLabelText("Eligibility and evidence reviewed for allocation 1"));
  await user.click(screen.getByLabelText("Registration, business use and evidence reviewed"));
  await user.click(screen.getByLabelText("Checked existing accounting and GST claims"));
  await user.click(screen.getByLabelText("Matched purchase reviewed as distinct"));
  await user.type(
    screen.getByLabelText("Distinct transaction reason"),
    "Checked the separate warehouse delivery",
  );
  await user.click(screen.getByRole("button", { name: "Add purchase to accounts" }));
  await user.click(await screen.findByRole("button", { name: "Confirm purchase posting" }));
  await waitFor(() =>
    expect(fixture.procedures.purchases.postReviewedPurchase).toHaveBeenCalledOnce(),
  );
});

it("blocks posting when more than one owned duplicate candidate needs selection", async () => {
  const { fixture, accounting } = setup();
  fixture.procedures.purchases.getPurchaseAccounting.mockResolvedValue(
    purchaseCodecs.get.encodeResponse(
      create(GetPurchaseAccountingResponseSchema, {
        accounting,
        duplicateCandidates: [
          duplicateCandidate(
            "018f0000-0000-7000-8000-000000000052",
            "018f0000-0000-7000-8000-000000000053",
            "INV-53",
          ),
          duplicateCandidate(
            "018f0000-0000-7000-8000-000000000054",
            "018f0000-0000-7000-8000-000000000055",
            "INV-55",
          ),
        ],
      }),
    ),
  );
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  expect(await screen.findByText(/Multiple possible duplicate purchases were found/)).toBeTruthy();
  expect(screen.queryByLabelText("No duplicate transaction found")).toBeNull();
  expect(
    (screen.getByRole("button", { name: "Add purchase to accounts" }) as HTMLButtonElement)
      .disabled,
  ).toBe(true);
  expect(fixture.procedures.purchases.postReviewedPurchase).not.toHaveBeenCalled();
});

it("restores a pending posting's allocation and review values after navigation and explicit retry", async () => {
  const { fixture } = setup();
  const frames: Uint8Array[] = [];
  fixture.procedures.purchases.postReviewedPurchase.mockImplementation(
    async (frame: Uint8Array) => {
      frames.push(frame.slice());
      throw new Error("response lost");
    },
  );
  const user = userEvent.setup();
  const first = render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText("Reviewed source · not added to accounts");
  await user.selectOptions(
    screen.getByLabelText("Account 1"),
    "018f0000-0000-7000-8000-000000000030",
  );
  await user.type(screen.getByLabelText("Eligible GST 1 (AUD)"), "10.00");
  await user.type(
    screen.getByLabelText("Evidence review reason"),
    "Registration and invoice reviewed",
  );
  await user.type(
    screen.getByLabelText("Prior accounting review reason"),
    "Checked books before posting",
  );
  for (const name of [
    "Eligibility and evidence reviewed for allocation 1",
    "Registration, business use and evidence reviewed",
    "Checked existing accounting and GST claims",
    "No duplicate transaction found",
  ])
    await user.click(screen.getByLabelText(name));
  await user.click(screen.getByRole("button", { name: "Add purchase to accounts" }));
  await user.click(screen.getByRole("button", { name: "Confirm purchase posting" }));
  await screen.findByRole("button", { name: "Retry exact request" });
  expect(fixture.procedures.purchases.postReviewedPurchase).toHaveBeenCalledOnce();
  const retained = frames[0]!.slice();
  first.unmount();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  expect(((await screen.findByLabelText("Account 1")) as HTMLSelectElement).value).toBe(
    "018f0000-0000-7000-8000-000000000030",
  );
  expect((screen.getByLabelText("Gross 1 (AUD)") as HTMLInputElement).value).toBe("110.00");
  expect((screen.getByLabelText("Eligible GST 1 (AUD)") as HTMLInputElement).value).toBe("10.00");
  expect((screen.getByLabelText("Prior accounting review reason") as HTMLInputElement).value).toBe(
    "Checked books before posting",
  );
  expect(fixture.procedures.purchases.postReviewedPurchase).toHaveBeenCalledOnce();
  await user.click(await screen.findByRole("button", { name: "Retry exact request" }));
  await waitFor(() => expect(frames).toHaveLength(2));
  expect([...frames[1]!]).toEqual([...retained]);
});

it("reaches the 201st actual payment through the rendered keyset history controls", async () => {
  const { fixture } = setup(true);
  fixture.procedures.purchases.listPurchasePayments.mockImplementation(
    async (frame: Uint8Array) => {
      const request = purchaseCodecs.list.decodeRequest(frame);
      const payments = request.page?.cursor
        ? [
            create(PurchasePaymentSchema, {
              id: "018f0000-0000-7000-8000-000000000201",
              organisationId: org,
              postingId: "018f0000-0000-7000-8000-000000000020",
              documentId: doc.id,
              accountingVersion: 201n,
              actualPaymentDate: { year: 2026, month: 10, day: 1 },
              amount: { currencyCode: "AUD", minorUnits: 100n },
              bankAccountId: "018f0000-0000-7000-8000-000000000031",
              reference: "Actual payment 201",
            }),
          ]
        : Array.from({ length: 200 }, (_, index) =>
            create(PurchasePaymentSchema, {
              id: `018f0000-0000-7000-8000-${String(index + 1).padStart(12, "0")}`,
              organisationId: org,
              postingId: "018f0000-0000-7000-8000-000000000020",
              documentId: doc.id,
              accountingVersion: BigInt(index + 1),
              actualPaymentDate: { year: 2026, month: 9, day: 30 },
              amount: { currencyCode: "AUD", minorUnits: 100n },
              bankAccountId: "018f0000-0000-7000-8000-000000000031",
              reference: `Actual payment ${index + 1}`,
            }),
          );
      return purchaseCodecs.list.encodeResponse(
        create(ListPurchasePaymentsResponseSchema, {
          payments,
          page: {
            returnedCount: payments.length,
            nextCursor: request.page?.cursor ? "" : "payments-201",
          },
        }),
      );
    },
  );
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  expect(await screen.findByText("Actual payment 200")).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Next payment page" }));
  expect(await screen.findByText("Actual payment 201")).toBeTruthy();
  expect(screen.getByText("01/10/2026")).toBeTruthy();
});

it("shows retained allocation account identity, name and cash-flow class to read-only roles", async () => {
  const { fixture, accounting } = setup(true);
  accounting.posting!.allocations = [
    create(PurchaseAllocationInputSchema, {
      accountId: "018f0000-0000-7000-8000-000000000030",
      accountVersion: 1n,
      sequence: 1,
      gross: { currencyCode: "AUD", minorUnits: 11000n },
      supplierGst: { currencyCode: "AUD", minorUnits: 1000n },
      eligibleGst: { currencyCode: "AUD", minorUnits: 1000n },
      use: PurchaseUse.BUSINESS,
      basClassification: PurchaseBasClassification.NON_CAPITAL,
      cashFlowClassification: PurchaseCashFlowClassification.INVESTING,
      eligibilityReviewConfirmed: true,
      evidenceReviewConfirmed: true,
    }),
  ];
  fixture.procedures.purchases.getPurchaseAccounting.mockResolvedValue(
    purchaseCodecs.get.encodeResponse(create(GetPurchaseAccountingResponseSchema, { accounting })),
  );
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={{ ...workspace, roles: [Role.AUDITOR] }}
    />,
  );
  const allocations = await screen.findByRole("table", { name: "Posted purchase allocations" });
  expect(within(allocations).getByText(/Office expense/)).toBeTruthy();
  expect(within(allocations).getByText(/018f0000-0000-7000-8000-000000000030/)).toBeTruthy();
  expect(within(allocations).getByText("Investing")).toBeTruthy();
  expect(fixture.procedures.accounting.listAccounts).toHaveBeenCalledOnce();
  expect(screen.queryByRole("button", { name: "Record payment" })).toBeNull();
});

it("discloses retained payment, posting and latest evidence provenance", async () => {
  const { fixture, accounting } = setup(true);
  accounting.paymentCount = 2n;
  accounting.paid = create(MoneySchema, { currencyCode: "AUD", minorUnits: 6500n });
  accounting.outstanding = create(MoneySchema, { currencyCode: "AUD", minorUnits: 4500n });
  accounting.posting = create(PurchasePostingSchema, {
    id: "018f0000-0000-7000-8000-000000000020",
    organisationId: org,
    documentId: doc.id,
    reviewedVersion: 2n,
    originalContentHash: new Uint8Array(32).fill(3),
    reviewedSnapshotHash: new Uint8Array(32).fill(4),
    supplierName: "Office",
    invoiceNumber: "INV1",
    invoiceDate: { year: 2026, month: 9, day: 25 },
    gstBasis: GstBasis.NON_CASH,
    gstProfileVersion: 2n,
    gstHistoryWatermark: 1n,
    gross: { currencyCode: "AUD", minorUnits: 11000n },
    supplierGst: { currencyCode: "AUD", minorUnits: 1000n },
    eligibleGst: { currencyCode: "AUD", minorUnits: 1000n },
    evidence: create(PurchaseEvidenceInputSchema, {
      classification: PurchaseEvidenceClassification.SUPPORTED_EXCEPTION,
      source: { sourceId: doc.id, sourceVersion: 2n, sourceHash: new Uint8Array(32).fill(5) },
      heldDate: { year: 2026, month: 9, day: 30 },
      registrationReviewConfirmed: true,
      businessUseReviewConfirmed: true,
      evidenceReviewConfirmed: true,
      reason: "Original low-value acquisition exception reviewed",
      exceptionReview: create(PurchaseTaxInvoiceExceptionReviewSchema, {
        kind: PurchaseTaxInvoiceExceptionKind.LOW_VALUE_ACQUISITION,
        rule: {
          sourceId: "018f0000-0000-7000-8000-000000000090",
          sourceVersion: 1n,
          sourceHash: new Uint8Array(32).fill(11),
        },
        supportingRecords: [
          {
            sourceId: "018f0000-0000-7000-8000-000000000091",
            sourceVersion: 3n,
            sourceHash: new Uint8Array(32).fill(12),
          },
        ],
        taxInvoiceNotRequiredReviewConfirmed: true,
        supportingRecordRequirementsReviewConfirmed: true,
        reason: "Retained low-value rule and record reviewed",
      }),
    }),
    journalId: "018f0000-0000-7000-8000-000000000021",
    financialRevision: 3n,
    sourceHash: new Uint8Array(32).fill(6),
    actorUserId: workspace.userId,
    postedAt: { seconds: 0n, nanos: 250_000_000 },
  });
  const latestEvidence = create(PurchaseTaxEvidenceSchema, {
    id: "018f0000-0000-7000-8000-000000000022",
    organisationId: org,
    documentId: doc.id,
    accountingVersion: 2n,
    review: create(PurchaseEvidenceInputSchema, {
      classification: PurchaseEvidenceClassification.TAX_INVOICE,
      source: { sourceId: doc.id, sourceVersion: 2n, sourceHash: new Uint8Array(32).fill(7) },
      heldDate: { year: 2026, month: 10, day: 1 },
      registrationReviewConfirmed: true,
      businessUseReviewConfirmed: true,
      evidenceReviewConfirmed: true,
      reason: "Updated held-date review",
    }),
    actorUserId: "018f0000-0000-7000-8000-000000000005",
    recordedAt: { seconds: 2n, nanos: 0 },
    sourceHash: new Uint8Array(32).fill(8),
  });
  fixture.procedures.purchases.getPurchaseAccounting.mockResolvedValue(
    purchaseCodecs.get.encodeResponse(
      create(GetPurchaseAccountingResponseSchema, { accounting, latestEvidence }),
    ),
  );
  fixture.procedures.purchases.listPurchasePayments.mockResolvedValue(
    purchaseCodecs.list.encodeResponse(
      create(ListPurchasePaymentsResponseSchema, {
        payments: [
          create(PurchasePaymentSchema, {
            id: "018f0000-0000-7000-8000-000000000030",
            organisationId: org,
            postingId: accounting.posting.id,
            documentId: doc.id,
            accountingVersion: 2n,
            actualPaymentDate: { year: 2026, month: 10, day: 2 },
            amount: { currencyCode: "AUD", minorUnits: 5500n },
            bankAccountId: "018f0000-0000-7000-8000-000000000031",
            reference: "transfer-to-supplier",
            journalId: "018f0000-0000-7000-8000-000000000032",
            sourceHash: new Uint8Array(32).fill(9),
            allocations: [
              create(PurchasePaymentAllocationSchema, {
                sequence: 2,
                accountId: "018f0000-0000-7000-8000-000000000041",
                accountVersion: 2n,
                gross: { currencyCode: "AUD", minorUnits: 3000n },
                supplierGst: { currencyCode: "AUD", minorUnits: 300n },
                eligibleGst: { currencyCode: "AUD", minorUnits: 300n },
                use: PurchaseUse.BUSINESS,
                basClassification: PurchaseBasClassification.NON_CAPITAL,
                cashFlowClassification: PurchaseCashFlowClassification.OPERATING,
              }),
              create(PurchasePaymentAllocationSchema, {
                sequence: 1,
                accountId: "018f0000-0000-7000-8000-000000000040",
                accountVersion: 4n,
                gross: { currencyCode: "AUD", minorUnits: 2500n },
                supplierGst: { currencyCode: "AUD", minorUnits: 200n },
                eligibleGst: { currencyCode: "AUD", minorUnits: 0n },
                use: PurchaseUse.PRIVATE,
                basClassification: PurchaseBasClassification.EXCLUDED,
                cashFlowClassification: PurchaseCashFlowClassification.FINANCING,
              }),
            ],
            financialRevision: 5n,
            actorUserId: workspace.userId,
            recordedAt: { seconds: 3n, nanos: 0 },
          }),
          create(PurchasePaymentSchema, {
            id: "018f0000-0000-7000-8000-000000000033",
            organisationId: org,
            postingId: accounting.posting.id,
            documentId: doc.id,
            accountingVersion: 3n,
            actualPaymentDate: { year: 2026, month: 10, day: 3 },
            amount: { currencyCode: "AUD", minorUnits: 1000n },
            bankAccountId: "018f0000-0000-7000-8000-000000000034",
            reference: "second-payment",
            journalId: "018f0000-0000-7000-8000-000000000035",
            sourceHash: new Uint8Array(32).fill(10),
            allocations: [
              create(PurchasePaymentAllocationSchema, {
                sequence: 1,
                accountId: "018f0000-0000-7000-8000-000000000040",
                accountVersion: 4n,
                gross: { currencyCode: "AUD", minorUnits: 1000n },
                supplierGst: { currencyCode: "AUD", minorUnits: 100n },
                eligibleGst: { currencyCode: "AUD", minorUnits: 100n },
                use: PurchaseUse.BUSINESS,
                basClassification: PurchaseBasClassification.NON_CAPITAL,
                cashFlowClassification: PurchaseCashFlowClassification.OPERATING,
              }),
            ],
            financialRevision: 6n,
            actorUserId: workspace.userId,
            recordedAt: { seconds: 4n, nanos: 0 },
          }),
        ],
        page: { returnedCount: 2 },
      }),
    ),
  );
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );

  const paymentRow = (await screen.findByText("transfer-to-supplier")).closest("tr");
  if (!paymentRow) throw new Error("Expected the first retained payment row.");
  await user.click(within(paymentRow).getByText("Retained payment details"));
  expect(within(paymentRow).getByText("018f0000-0000-7000-8000-000000000031")).toBeTruthy();
  expect(within(paymentRow).getByText("018f0000-0000-7000-8000-000000000032")).toBeTruthy();
  expect(within(paymentRow).getByText("09".repeat(32))).toBeTruthy();
  const revisionLabel = within(paymentRow).getByText("Financial revision");
  expect(revisionLabel.nextElementSibling?.textContent).toBe("5");
  expect(within(paymentRow).getByText(workspace.userId)).toBeTruthy();
  expect(within(paymentRow).getByText("1970-01-01T00:00:03Z")).toBeTruthy();
  const allocations = within(paymentRow).getAllByRole("listitem");
  expect(allocations).toHaveLength(2);
  expect(allocations[0]?.textContent).toContain("Allocation 1");
  expect(allocations[0]?.textContent).toContain("Private");
  expect(allocations[0]?.textContent).toContain("Excluded");
  expect(allocations[0]?.textContent).toContain("Financing");
  expect(allocations[0]?.textContent).toContain("$25.00 gross");
  expect(allocations[0]?.textContent).toContain("$2.00 supplier GST");
  expect(allocations[0]?.textContent).toContain("$0.00 eligible GST");
  expect(allocations[1]?.textContent).toContain("Allocation 2");
  expect(allocations[1]?.textContent).toContain("Business");
  expect(allocations[1]?.textContent).toContain("Non-capital");
  expect(allocations[1]?.textContent).toContain("Operating");
  expect(allocations[1]?.textContent).toContain("$30.00 gross");
  expect(allocations[1]?.textContent).toContain("$3.00 supplier GST");
  expect(allocations[1]?.textContent).toContain("$3.00 eligible GST");

  const secondPaymentRow = (await screen.findByText("second-payment")).closest("tr");
  if (!secondPaymentRow) throw new Error("Expected the second retained payment row.");
  await user.click(within(secondPaymentRow).getByText("Retained payment details"));
  expect(within(secondPaymentRow).getByText("018f0000-0000-7000-8000-000000000034")).toBeTruthy();
  expect(within(secondPaymentRow).getByText("018f0000-0000-7000-8000-000000000035")).toBeTruthy();
  expect(within(secondPaymentRow).getByText("0a".repeat(32))).toBeTruthy();
  expect(within(secondPaymentRow).getByText("6")).toBeTruthy();

  const disclosure = await screen.findByText("Retained posting and evidence details");
  const sourceDetails = disclosure.closest("details");
  if (!sourceDetails) throw new Error("Retained source details are missing");
  await user.click(within(sourceDetails).getByText("Retained posting and evidence details"));
  expect(within(sourceDetails).getByText("06".repeat(32))).toBeTruthy();
  expect(within(sourceDetails).getByText("04".repeat(32))).toBeTruthy();
  expect(within(sourceDetails).getByText(workspace.userId)).toBeTruthy();
  expect(within(sourceDetails).getByText("1970-01-01T00:00:00.25Z")).toBeTruthy();
  expect(within(sourceDetails).getByText("Latest evidence record")).toBeTruthy();
  expect(within(sourceDetails).getByText("018f0000-0000-7000-8000-000000000005")).toBeTruthy();
  expect(within(sourceDetails).getByText("1970-01-01T00:00:02Z")).toBeTruthy();
  expect(within(sourceDetails).getByText("Tax invoice · held 01/10/2026")).toBeTruthy();
  expect(within(sourceDetails).getByText("Updated held-date review")).toBeTruthy();
  expect(within(sourceDetails).getByText("08".repeat(32))).toBeTruthy();
  expect(sourceDetails.textContent).toContain("07".repeat(32));

  const originalHeading = within(sourceDetails).getByRole("heading", {
    name: "Original posting evidence · immutable snapshot",
  });
  const originalSection = originalHeading.closest("section");
  if (!originalSection) throw new Error("Original posting evidence details are missing");
  expect(originalSection.textContent).toContain("Supported exception · held 30/09/2026");
  expect(originalSection.textContent).toContain(
    "Original low-value acquisition exception reviewed",
  );
  expect(originalSection.textContent).toContain("05".repeat(32));
  expect(originalSection.textContent).toContain("Low-value acquisition");
  expect(originalSection.textContent).toContain("018f0000-0000-7000-8000-000000000090");
  expect(originalSection.textContent).toContain("0b".repeat(32));
  expect(originalSection.textContent).toContain("018f0000-0000-7000-8000-000000000091");
  expect(originalSection.textContent).toContain("0c".repeat(32));
  expect(originalSection.textContent).toContain("Retained low-value rule and record reviewed");

  const latestHeading = within(sourceDetails).getByRole("heading", {
    name: "Latest evidence review",
  });
  const latestSection = latestHeading.closest("section");
  if (!latestSection) throw new Error("Latest evidence review details are missing");
  expect(latestSection.textContent).toContain("Tax invoice · held 01/10/2026");
  expect(latestSection.textContent).toContain("Updated held-date review");
  expect(latestSection.textContent).toContain("07".repeat(32));
});

it("labels posting evidence as a snapshot when no latest evidence record is returned", async () => {
  const { fixture, accounting } = setup(true);
  accounting.posting = create(PurchasePostingSchema, {
    id: "018f0000-0000-7000-8000-000000000020",
    organisationId: org,
    documentId: doc.id,
    reviewedVersion: 2n,
    originalContentHash: new Uint8Array(32).fill(3),
    reviewedSnapshotHash: new Uint8Array(32).fill(4),
    supplierName: "Office",
    invoiceNumber: "INV1",
    invoiceDate: { year: 2026, month: 9, day: 25 },
    gstBasis: GstBasis.NON_CASH,
    gstProfileVersion: 1n,
    gross: { currencyCode: "AUD", minorUnits: 11000n },
    supplierGst: { currencyCode: "AUD", minorUnits: 1000n },
    eligibleGst: { currencyCode: "AUD", minorUnits: 1000n },
    evidence: create(PurchaseEvidenceInputSchema, {
      classification: PurchaseEvidenceClassification.TAX_INVOICE,
      source: { sourceId: doc.id, sourceVersion: 2n, sourceHash: new Uint8Array(32).fill(5) },
      heldDate: { year: 2026, month: 9, day: 30 },
      registrationReviewConfirmed: true,
      businessUseReviewConfirmed: true,
      evidenceReviewConfirmed: true,
      reason: "Evidence retained with the original posting",
    }),
    journalId: "018f0000-0000-7000-8000-000000000021",
    financialRevision: 3n,
    sourceHash: new Uint8Array(32).fill(6),
    actorUserId: workspace.userId,
    postedAt: { seconds: 0n, nanos: 0 },
  });
  fixture.procedures.purchases.getPurchaseAccounting.mockResolvedValue(
    purchaseCodecs.get.encodeResponse(create(GetPurchaseAccountingResponseSchema, { accounting })),
  );
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );

  const disclosure = await screen.findByText("Retained posting and evidence details");
  const sourceDetails = disclosure.closest("details");
  if (!sourceDetails) throw new Error("Retained source details are missing");
  await user.click(within(sourceDetails).getByText("Retained posting and evidence details"));
  expect(sourceDetails.textContent).toContain("No latest evidence record returned.");
  expect(sourceDetails.textContent).toContain("Tax invoice · held 30/09/2026");
  expect(sourceDetails.textContent).toContain("Evidence retained with the original posting");
  expect(sourceDetails.textContent).toContain("05".repeat(32));
});

it("retains a reviewed evidence decision through the panel's confirmed command", async () => {
  const { fixture, accounting } = setup(true);
  fixture.procedures.purchases.recordPurchaseTaxEvidence.mockImplementation(
    async (frame: Uint8Array) => {
      const request = purchaseCodecs.evidence.decodeRequest(frame);
      expect(request.evidence?.heldDate).toEqual({ year: 2026, month: 9, day: 30 });
      expect(request.evidence?.source?.sourceId).toBe(doc.id);
      return purchaseCodecs.evidence.encodeResponse(
        create(RecordPurchaseTaxEvidenceResponseSchema, {
          evidence: create(PurchaseTaxEvidenceSchema, {
            id: "018f0000-0000-7000-8000-000000000081",
            organisationId: org,
            documentId: doc.id,
            accountingVersion: 2n,
            review: request.evidence,
            sourceHash: new Uint8Array(32),
          }),
          accounting: { ...accounting, accountingVersion: 2n },
        }),
      );
    },
  );
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText(/Added to accounts/);
  await user.click(screen.getByText("Review GST evidence"));
  await user.selectOptions(screen.getByLabelText("GST evidence classification"), "2");
  await user.type(screen.getByLabelText("Actual evidence held date"), "2026-09-30");
  await user.type(
    screen.getByLabelText("Evidence review reason"),
    "Tax invoice and actual held date reviewed",
  );
  await user.click(screen.getByLabelText("Registration, business use and evidence reviewed"));
  await user.click(screen.getByRole("button", { name: "Retain GST evidence review" }));
  expect(screen.getByText(/Existing purchase and payment amounts are unchanged/)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Confirm GST evidence review" }));
  await waitFor(() =>
    expect(fixture.procedures.purchases.recordPurchaseTaxEvidence).toHaveBeenCalledOnce(),
  );
});

it("confirms an explicit claim attribution from the retained source and timing evidence", async () => {
  const { fixture } = setup(true);
  const proof = create(DocumentSchema, {
    id: "018f0000-0000-7000-8000-000000000012",
    organisationId: org,
    version: 4n,
    status: DocumentStatus.REVIEWED,
    sha256: new Uint8Array(32).fill(4),
    sourceDisplayName: "actual-due-date.pdf",
  });
  fixture.procedures.documents.listDocuments.mockResolvedValue(
    retainedDocumentCodec.encodeResponse(
      create(ListDocumentsResponseSchema, { documents: [doc, proof], page: { returnedCount: 2 } }),
    ),
  );
  const posting = create(PurchasePostingSchema, {
    id: "018f0000-0000-7000-8000-000000000020",
    documentId: doc.id,
    organisationId: org,
    gstBasis: GstBasis.NON_CASH,
    invoiceDate: { year: 2026, month: 9, day: 25 },
    sourceHash: new Uint8Array(32).fill(5),
    allocations: [
      create(PurchaseAllocationInputSchema, {
        sequence: 1,
        eligibleGst: { currencyCode: "AUD", minorUnits: 1000n },
        use: PurchaseUse.BUSINESS,
        basClassification: PurchaseBasClassification.NON_CAPITAL,
      }),
    ],
  });
  const accounting = create(PurchaseAccountingSchema, {
    organisationId: org,
    documentId: doc.id,
    posted: true,
    accountingVersion: 4n,
    paymentCount: 0n,
    posting,
    outstanding: { currencyCode: "AUD", minorUnits: 11000n },
    paid: { currencyCode: "AUD", minorUnits: 0n },
    eligibleGst: { currencyCode: "AUD", minorUnits: 1000n },
    suspenseGst: { currencyCode: "AUD", minorUnits: 1000n },
  });
  const evidence = create(PurchaseTaxEvidenceSchema, {
    id: "018f0000-0000-7000-8000-000000000080",
    organisationId: org,
    documentId: doc.id,
    accountingVersion: 4n,
    review: create(PurchaseEvidenceInputSchema, {
      source: { sourceId: doc.id, sourceVersion: doc.version, sourceHash: doc.sha256 },
      classification: PurchaseEvidenceClassification.TAX_INVOICE,
      heldDate: { year: 2026, month: 9, day: 25 },
      registrationReviewConfirmed: true,
      businessUseReviewConfirmed: true,
      evidenceReviewConfirmed: true,
      reason: "Tax invoice and actual held date reviewed",
    }),
  });
  fixture.procedures.purchases.getPurchaseAccounting.mockResolvedValue(
    purchaseCodecs.get.encodeResponse(
      create(GetPurchaseAccountingResponseSchema, { accounting, latestEvidence: evidence }),
    ),
  );
  fixture.procedures.purchases.attributePurchaseGst.mockImplementation(
    async (frame: Uint8Array) => {
      const request = purchaseCodecs.attribute.decodeRequest(frame);
      expect(request.expectedAccountingVersion).toBe(4n);
      expect(request.portions[0]?.source.case).toBe("posting");
      expect(
        request.portions[0]?.source.case === "posting" &&
          request.portions[0].source.value.allocationSequence,
      ).toBe(1);
      expect(request.portions[0]?.dueDateEvidence?.sourceId).toBe(proof.id);
      expect(request.portions[0]?.evidenceId).toBe(evidence.id);
      expect(request.releasePostingDate?.year).toBe(2026);
      expect(request.releasePostingDate?.month).toBe(10);
      expect(request.releasePostingDate?.day).toBe(7);
      return purchaseCodecs.attribute.encodeResponse(
        create(AttributePurchaseGstResponseSchema, {
          attributions: [
            create(PurchaseGstAttributionSchema, { id: "018f0000-0000-7000-8000-000000000081" }),
          ],
          accounting: { ...accounting, accountingVersion: 5n },
        }),
      );
    },
  );
  fixture.procedures.purchases.deferPurchaseGstClaim.mockImplementation(
    async (frame: Uint8Array) => {
      const request = purchaseCodecs.defer.decodeRequest(frame);
      expect(request.expectedAccountingVersion).toBe(5n);
      expect(request.portions[0]?.dueDateEvidence?.sourceId).toBe(proof.id);
      expect(request.affectedPeriod?.periodEnd?.year).toBe(2026);
      expect(request.affectedPeriod?.periodEnd?.month).toBe(12);
      expect(request.affectedPeriod?.periodEnd?.day).toBe(31);
      return purchaseCodecs.defer.encodeResponse(
        create(DeferPurchaseGstClaimResponseSchema, {
          deferral: create(PurchaseGstDeferralSchema, {
            id: "018f0000-0000-7000-8000-000000000083",
            organisationId: org,
            postingId: posting.id,
            documentId: doc.id,
            portions: request.portions,
          }),
          accounting: { ...accounting, accountingVersion: 6n },
        }),
      );
    },
  );
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await user.click(await screen.findByText("Review claim period or defer GST"));
  for (const [label, value] of Object.entries({
    "Earliest eligible period start": "2026-07-01",
    "Earliest eligible period end": "2026-09-30",
    "Actual required return due date": "2026-11-03",
    "Ordinary last claim date": "2030-11-03",
    "Claim / affected period start": "2026-10-01",
    "Claim / affected period end": "2026-12-31",
    "GST release posting date (for claim)": "2026-10-07",
    "Timing review as-of date": "2026-10-07",
    "Claim / deferral review reason": "Actual due date and owned invoice reviewed",
  }))
    await user.type(screen.getByLabelText(label), value);
  await user.selectOptions(await screen.findByLabelText("Due-date proof"), proof.id);
  await user.click(
    screen.getByLabelText("Reviewed allowable period, ordinary time limit and evidence held"),
  );
  await user.click(screen.getByRole("button", { name: "Attribute reviewed GST" }));
  await user.click(await screen.findByRole("button", { name: "Confirm GST attribution" }));
  await waitFor(() =>
    expect(fixture.procedures.purchases.attributePurchaseGst).toHaveBeenCalledOnce(),
  );
  await screen.findByText("Purchase action confirmed and retained locally.");
  await user.click(screen.getByRole("button", { name: "Defer reviewed GST" }));
  await user.click(await screen.findByRole("button", { name: "Confirm GST deferral" }));
  await waitFor(() =>
    expect(fixture.procedures.purchases.deferPurchaseGstClaim).toHaveBeenCalledOnce(),
  );
});

it("confirms a period-specific legacy decision without posting or inferring a settlement", async () => {
  const { fixture } = setup();
  const support = create(DocumentSchema, {
    id: "018f0000-0000-7000-8000-000000000013",
    organisationId: org,
    version: 3n,
    status: DocumentStatus.REVIEWED,
    sha256: new Uint8Array(32).fill(6),
    sourceDisplayName: "legacy-support.pdf",
  });
  fixture.procedures.documents.listDocuments.mockResolvedValue(
    retainedDocumentCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents: [doc, support],
        page: { returnedCount: 2 },
      }),
    ),
  );
  fixture.procedures.purchases.resolveLegacyPurchaseReview.mockImplementation(
    async (frame: Uint8Array) => {
      const request = purchaseCodecs.legacy.decodeRequest(frame);
      expect(request.review?.source?.source.case).toBe("document");
      expect(
        request.review?.source?.source.case === "document" &&
          request.review.source.source.value.documentId,
      ).toBe(doc.id);
      expect(request.review?.supportingEvidence[0]?.sourceId).toBe(support.id);
      expect(request.review?.supportingPayments).toEqual([]);
      expect(request.review?.disposition).toBe(LegacyPurchaseDisposition.NONCREDITABLE);
      expect(request.review?.reason).toBe("Prior records reviewed; no credit taken");
      return purchaseCodecs.legacy.encodeResponse(
        create(ResolveLegacyPurchaseReviewResponseSchema, {
          review: create(LegacyPurchaseReviewSchema, {
            id: "018f0000-0000-7000-8000-000000000082",
          }),
        }),
      );
    },
  );
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await user.click(await screen.findByText("Review legacy purchase / opening provenance"));
  for (const [label, value] of Object.entries({
    "Legacy affected period start": "2026-07-01",
    "Legacy affected period end": "2026-09-30",
    "Legacy reporting balance date": "2026-09-30",
    "Legacy review reason": "Prior records reviewed; no credit taken",
  }))
    await user.type(screen.getByLabelText(label), value);
  await user.selectOptions(await screen.findByLabelText("Legacy supporting evidence"), support.id);
  await user.click(
    screen.getByLabelText(
      "Reviewed legacy accounting, settlement and claimed status against retained records",
    ),
  );
  await user.click(screen.getByRole("button", { name: "Review legacy decision" }));
  await user.click(await screen.findByRole("button", { name: "Confirm legacy purchase review" }));
  await waitFor(() =>
    expect(fixture.procedures.purchases.resolveLegacyPurchaseReview).toHaveBeenCalledOnce(),
  );
  expect(fixture.procedures.purchases.postReviewedPurchase).not.toHaveBeenCalled();
  expect(fixture.procedures.purchases.recordPurchasePayment).not.toHaveBeenCalled();
});

it("restores a pending evidence review after remount and waits for an explicit retry", async () => {
  const { fixture } = setup(true);
  const frames: Uint8Array[] = [];
  fixture.procedures.purchases.recordPurchaseTaxEvidence.mockImplementation(
    async (frame: Uint8Array) => {
      frames.push(frame.slice());
      throw new Error("response lost");
    },
  );
  const user = userEvent.setup();
  const first = render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText(/Added to accounts/);
  await user.click(screen.getByText("Review GST evidence"));
  await user.selectOptions(screen.getByLabelText("GST evidence classification"), "2");
  await user.type(screen.getByLabelText("Actual evidence held date"), "2026-09-30");
  await user.type(
    screen.getByLabelText("Evidence review reason"),
    "Tax invoice checked before lock",
  );
  await user.click(screen.getByLabelText("Registration, business use and evidence reviewed"));
  await user.click(screen.getByRole("button", { name: "Retain GST evidence review" }));
  await user.click(screen.getByRole("button", { name: "Confirm GST evidence review" }));
  await screen.findByRole("button", { name: "Retry exact request" });
  const retained = frames[0]!.slice();
  first.unmount();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  expect(
    ((await screen.findByLabelText("GST evidence classification")) as HTMLSelectElement).value,
  ).toBe("2");
  expect((screen.getByLabelText("Actual evidence held date") as HTMLInputElement).value).toBe(
    "2026-09-30",
  );
  expect((screen.getByLabelText("Evidence review reason") as HTMLInputElement).value).toBe(
    "Tax invoice checked before lock",
  );
  expect(fixture.procedures.purchases.recordPurchaseTaxEvidence).toHaveBeenCalledOnce();
  await user.click(await screen.findByRole("button", { name: "Retry exact request" }));
  await waitFor(() => expect(frames).toHaveLength(2));
  expect([...frames[1]!]).toEqual([...retained]);
});

it("retains the exact payment key and payload after a lost response, without allowing an edited retry", async () => {
  const { fixture, accounting } = setup(true);
  const frames: Uint8Array[] = [];
  fixture.procedures.purchases.recordPurchasePayment.mockImplementation(
    async (frame: Uint8Array) => {
      frames.push(frame.slice());
      throw new Error("transport lost");
    },
  );
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText(/Added to accounts/);
  await user.click(screen.getByText("Record payment"));
  await user.type(screen.getByLabelText("Actual payment date"), "2026-09-30");
  await user.type(screen.getByLabelText("Payment amount (AUD)"), "55.00");
  await user.selectOptions(
    screen.getByLabelText("Payment bank account"),
    "018f0000-0000-7000-8000-000000000031",
  );
  await user.type(screen.getByLabelText("Payment reference"), "Actual transfer");
  await user.click(screen.getByRole("button", { name: "Review recorded payment" }));
  await user.click(screen.getByRole("button", { name: "Confirm recorded payment" }));
  await screen.findByRole("button", { name: "Retry exact request" });
  expect(screen.getByLabelText("Payment amount (AUD)").matches(":disabled")).toBe(true);
  expect((screen.getByLabelText("Payment amount (AUD)") as HTMLInputElement).value).toBe("55.00");
  expect(screen.queryByRole("button", { name: "Review and correct retained entries" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Retry exact request" }));
  await waitFor(() => expect(frames).toHaveLength(2));
  expect([...frames[1]!]).toEqual([...frames[0]!]);
  expect(purchaseCodecs.payment.decodeRequest(frames[1]!).expectedAccountingVersion).toBe(
    accounting.accountingVersion,
  );
});

it("restores a pending payment after remount and refreshes only authentication for explicit retry", async () => {
  const { fixture, accounting } = setup(true);
  const frames: Uint8Array[] = [];
  fixture.procedures.purchases.recordPurchasePayment.mockImplementation(
    async (frame: Uint8Array) => {
      frames.push(frame.slice());
      throw new Error("response lost");
    },
  );
  const user = userEvent.setup();
  const first = render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText(/Added to accounts/);
  await user.click(screen.getByText("Record payment"));
  await user.type(screen.getByLabelText("Actual payment date"), "2026-09-30");
  await user.type(screen.getByLabelText("Payment amount (AUD)"), "55.00");
  await user.selectOptions(
    screen.getByLabelText("Payment bank account"),
    "018f0000-0000-7000-8000-000000000031",
  );
  await user.type(screen.getByLabelText("Payment reference"), "Retained before lock");
  await user.click(screen.getByRole("button", { name: "Review recorded payment" }));
  await user.click(screen.getByRole("button", { name: "Confirm recorded payment" }));
  await screen.findByRole("button", { name: "Retry exact request" });
  const original = purchaseCodecs.payment.decodeRequest(frames[0]!);
  first.unmount();
  const refreshedWorkspace = { ...workspace, sessionId: "018f0000-0000-7000-8000-000000000099" };
  const second = render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={refreshedWorkspace}
    />,
  );
  expect(((await screen.findByLabelText("Payment amount (AUD)")) as HTMLInputElement).value).toBe(
    "55.00",
  );
  expect((screen.getByLabelText("Payment reference") as HTMLInputElement).value).toBe(
    "Retained before lock",
  );
  await user.click(await screen.findByRole("button", { name: "Retry exact request" }));
  await waitFor(() => expect(frames).toHaveLength(2));
  const retried = purchaseCodecs.payment.decodeRequest(frames[1]!);
  expect(retried.commandContext?.authentication?.sessionId).toBe(refreshedWorkspace.sessionId);
  expect(retried.commandContext?.idempotencyKey).toBe(original.commandContext?.idempotencyKey);
  expect(retried.expectedAccountingVersion).toBe(accounting.accountingVersion);
  expect(retried.actualPaymentDate).toEqual(original.actualPaymentDate);
  expect(retried.amount).toEqual(original.amount);
  expect(retried.bankAccountId).toBe(original.bankAccountId);
  expect(retried.reference).toBe(original.reference);
  const otherActor = { ...refreshedWorkspace, userId: "018f0000-0000-7000-8000-000000000088" };
  second.unmount();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={otherActor}
    />,
  );
  await screen.findByText(/Added to accounts/);
  expect(screen.queryByRole("button", { name: "Retry exact request" })).toBeNull();
  await user.click(screen.getByText("Record payment"));
  expect((screen.getByLabelText("Payment amount (AUD)") as HTMLInputElement).value).toBe("");
});

it("ignores a second payment confirmation while the first request is in flight", async () => {
  const { fixture } = setup(true);
  fixture.procedures.purchases.recordPurchasePayment.mockImplementation(async () => {
    await new Promise((resolve) => setTimeout(resolve, 5));
    throw new Error("response lost");
  });
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText(/Added to accounts/);
  await user.click(screen.getByText("Record payment"));
  await user.type(screen.getByLabelText("Actual payment date"), "2026-09-30");
  await user.type(screen.getByLabelText("Payment amount (AUD)"), "55.00");
  await user.selectOptions(
    screen.getByLabelText("Payment bank account"),
    "018f0000-0000-7000-8000-000000000031",
  );
  await user.click(screen.getByRole("button", { name: "Review recorded payment" }));
  await user.dblClick(screen.getByRole("button", { name: "Confirm recorded payment" }));
  await screen.findByRole("button", { name: "Retry exact request" });
  expect(fixture.procedures.purchases.recordPurchasePayment).toHaveBeenCalledOnce();
});

it("keeps entered payment values on a definite closed-period rejection", async () => {
  const { fixture } = setup(true);
  fixture.procedures.purchases.recordPurchasePayment.mockRejectedValue({
    code: "PURCHASE_PERIOD_CLOSED",
    message: "private internal detail",
  });
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText(/Added to accounts/);
  await user.click(screen.getByText("Record payment"));
  await user.type(screen.getByLabelText("Actual payment date"), "2026-09-30");
  await user.type(screen.getByLabelText("Payment amount (AUD)"), "55.00");
  await user.selectOptions(
    screen.getByLabelText("Payment bank account"),
    "018f0000-0000-7000-8000-000000000031",
  );
  await user.click(screen.getByRole("button", { name: "Review recorded payment" }));
  await user.click(screen.getByRole("button", { name: "Confirm recorded payment" }));
  expect(await screen.findByText(/The accounting period is closed/)).toBeTruthy();
  expect(screen.queryByText("private internal detail")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Review and correct retained entries" }));
  expect((screen.getByLabelText("Payment amount (AUD)") as HTMLInputElement).value).toBe("55.00");
});

it.each([
  ["stale source", "PURCHASE_STALE"],
  ["changed GST basis history", "PURCHASE_GST_BASIS_CHANGED"],
  ["possible duplicate", "PURCHASE_DUPLICATE"],
  ["unsupported evidence", "PURCHASE_EVIDENCE_UNSUPPORTED"],
] as const)("retains posting entries after a definite %s rejection", async (_label, code) => {
  const { fixture } = setup();
  fixture.procedures.purchases.postReviewedPurchase.mockRejectedValue({
    code,
    message: "private backend detail",
  });
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText("Reviewed source · not added to accounts");
  await user.selectOptions(
    screen.getByLabelText("Account 1"),
    "018f0000-0000-7000-8000-000000000030",
  );
  await user.type(screen.getByLabelText("Eligible GST 1 (AUD)"), "10.00");
  await user.type(
    screen.getByLabelText("Evidence review reason"),
    "Registration and invoice reviewed",
  );
  await user.type(
    screen.getByLabelText("Prior accounting review reason"),
    "Checked existing books",
  );
  for (const name of [
    "Eligibility and evidence reviewed for allocation 1",
    "Registration, business use and evidence reviewed",
    "Checked existing accounting and GST claims",
    "No duplicate transaction found",
  ])
    await user.click(screen.getByLabelText(name));
  await user.click(screen.getByRole("button", { name: "Add purchase to accounts" }));
  await user.click(await screen.findByRole("button", { name: "Confirm purchase posting" }));
  expect(await screen.findByText(purchaseCorrectionGuidance[code])).toBeTruthy();
  expect(screen.queryByText("private backend detail")).toBeNull();
  await user.click(screen.getByRole("button", { name: "Review and correct retained entries" }));
  expect((screen.getByLabelText("Eligible GST 1 (AUD)") as HTMLInputElement).value).toBe("10.00");
  expect((screen.getByLabelText("Evidence review reason") as HTMLInputElement).value).toBe(
    "Registration and invoice reviewed",
  );
});

it("rejects overpayment before confirmation and preserves the entered amount", async () => {
  const { fixture } = setup(true);
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText(/Added to accounts/);
  await user.click(screen.getByText("Record payment"));
  await user.type(screen.getByLabelText("Actual payment date"), "2026-09-30");
  await user.type(screen.getByLabelText("Payment amount (AUD)"), "110.01");
  await user.selectOptions(
    screen.getByLabelText("Payment bank account"),
    "018f0000-0000-7000-8000-000000000031",
  );
  await user.click(screen.getByRole("button", { name: "Review recorded payment" }));
  expect(
    await screen.findByText(
      "Choose an actual payment date and bank account, and a positive AUD amount within the outstanding balance.",
    ),
  ).toBeTruthy();
  expect(screen.queryByRole("alertdialog", { name: "Confirm purchase action" })).toBeNull();
  expect(fixture.procedures.purchases.recordPurchasePayment).not.toHaveBeenCalled();
  expect((screen.getByLabelText("Payment amount (AUD)") as HTMLInputElement).value).toBe("110.01");
});

it("rejects an advance-dated payment before confirmation", async () => {
  const { fixture } = setup(true);
  const user = userEvent.setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={workspace}
    />,
  );
  await screen.findByText(/Added to accounts/);
  await user.click(screen.getByText("Record payment"));
  await user.type(screen.getByLabelText("Actual payment date"), "2026-09-24");
  await user.type(screen.getByLabelText("Payment amount (AUD)"), "55.00");
  await user.selectOptions(
    screen.getByLabelText("Payment bank account"),
    "018f0000-0000-7000-8000-000000000031",
  );
  await user.click(screen.getByRole("button", { name: "Review recorded payment" }));
  expect(
    await screen.findByText("Advance payments before the invoice date are unsupported."),
  ).toBeTruthy();
  expect(screen.queryByRole("alertdialog", { name: "Confirm purchase action" })).toBeNull();
  expect(fixture.procedures.purchases.recordPurchasePayment).not.toHaveBeenCalled();
});
it("does not offer write controls to an auditor or an unreviewed document", async () => {
  const { fixture } = setup();
  render(
    <PurchaseAccountingPanel
      api={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      documentApi={fixture.conveyor.documents}
      document={doc}
      workspace={{ ...workspace, roles: [Role.AUDITOR] }}
    />,
  );
  expect(await screen.findByText(/Read-only role/)).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Add purchase to accounts" })).toBeNull();
});
