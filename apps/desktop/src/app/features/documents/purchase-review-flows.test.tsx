import { create } from "@bufbuild/protobuf";
import { MoneySchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentAccountingFilter,
  DocumentCandidateSchema,
  DocumentSchema,
  DocumentStatus,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { Role } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import * as p from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
import { PurchaseClaimForm } from "./purchase-claim-form";
import { purchaseCodecs, retainedDocumentCodec } from "./purchase-codecs";
import { initialEvidence, reviewedEvidence } from "./purchase-evidence-fields";
import { PurchaseLegacyReviewPanel } from "./purchase-legacy-review-panel";

const organisationId = "018f0000-0000-7000-8000-000000000004";
const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  organisationId,
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  roles: [Role.BUSINESS_PREPARER],
};
const acquisition = create(DocumentSchema, {
  id: "018f0000-0000-7000-8000-000000000010",
  organisationId,
  version: 2n,
  sha256: new Uint8Array(32).fill(1),
  status: DocumentStatus.REVIEWED,
  sourceDisplayName: "acquisition.pdf",
  candidate: { subtotal: { currencyCode: "AUD", minorUnits: 7500n } },
});
const proof = create(DocumentSchema, {
  id: "018f0000-0000-7000-8000-000000000011",
  organisationId,
  version: 2n,
  sha256: new Uint8Array(32).fill(2),
  status: DocumentStatus.REVIEWED,
  sourceDisplayName: "actual-required-due-date.pdf",
});
const postedDocument = create(DocumentSchema, {
  id: "018f0000-0000-7000-8000-000000000012",
  organisationId,
  version: 5n,
  sha256: new Uint8Array(32).fill(4),
  status: DocumentStatus.REVIEWED,
  sourceDisplayName: "posted-office-invoice.pdf",
});
function fixtureForRecords() {
  const fixture = createConveyorFixture();
  fixture.procedures.documents.listDocuments.mockResolvedValue(
    retainedDocumentCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents: [acquisition, proof],
        page: { returnedCount: 2 },
      }),
    ),
  );
  return fixture;
}
function claimFacts() {
  const accounting = create(p.PurchaseAccountingSchema, {
    documentId: acquisition.id,
    organisationId,
    posted: true,
    accountingVersion: 4n,
    suspenseGst: { currencyCode: "AUD", minorUnits: 750n },
    posting: {
      id: "018f0000-0000-7000-8000-000000000020",
      documentId: acquisition.id,
      sourceHash: new Uint8Array(32).fill(3),
      gstBasis: GstBasis.NON_CASH,
      invoiceDate: { year: 2026, month: 9, day: 25 },
      allocations: [{ sequence: 1, eligibleGst: { currencyCode: "AUD", minorUnits: 750n } }],
    },
  });
  const evidence = create(p.PurchaseTaxEvidenceSchema, {
    id: "018f0000-0000-7000-8000-000000000030",
    review: {
      classification: p.PurchaseEvidenceClassification.TAX_INVOICE,
      source: { sourceId: acquisition.id, sourceVersion: 2n, sourceHash: acquisition.sha256 },
      heldDate: { year: 2026, month: 10, day: 1 },
    },
  });
  return { accounting, evidence };
}
it("builds explicit claim and deferral reviews from owned source facts and distinct actual due-date proof", async () => {
  const fixture = fixtureForRecords();
  const facts = claimFacts();
  const propose = vi.fn();
  const onError = vi.fn();
  const user = userEvent.setup();
  const mounted = render(
    <PurchaseClaimForm
      document={acquisition}
      workspace={workspace}
      documentApi={fixture.conveyor.documents}
      accounting={facts.accounting}
      evidence={facts.evidence}
      payments={[]}
      propose={propose}
      onError={onError}
    />,
  );
  await user.click(screen.getByText("Review claim period or defer GST"));
  const values = {
    "Earliest eligible period start": "2026-07-01",
    "Earliest eligible period end": "2026-09-30",
    "Actual required return due date": "2026-11-03",
    "Ordinary last claim date": "2030-11-03",
    "Claim / affected period start": "2026-10-01",
    "Claim / affected period end": "2026-12-31",
    "GST release posting date (for claim)": "2026-10-07",
    "Timing review as-of date": "2026-10-07",
    "Claim / deferral review reason": "Actual required return due and retained invoice reviewed",
  };
  for (const [label, value] of Object.entries(values))
    await user.type(screen.getByLabelText(label), value);
  await user.selectOptions(await screen.findByLabelText("Due-date proof"), proof.id);
  expect(screen.queryByRole("option", { name: /acquisition.pdf/ })).toBeNull();
  await user.click(
    screen.getByLabelText("Reviewed allowable period, ordinary time limit and evidence held"),
  );
  await user.click(screen.getByRole("button", { name: "Attribute reviewed GST" }));
  expect(onError).not.toHaveBeenCalled();
  const request = purchaseCodecs.attribute.decodeRequest(propose.mock.calls[0]![1]);
  expect(request.expectedAccountingVersion).toBe(4n);
  expect(request.portions[0]?.source.case).toBe("posting");
  expect(request.portions[0]?.dueDateEvidence?.sourceId).toBe(proof.id);
  expect(request.portions[0]?.normalEventDate?.day).toBe(25);
  expect(request.portions[0]?.evidenceHeldDate?.month).toBe(10);
  expect(request.portions[0]?.requiredReturnDueDate?.day).toBe(3);
  expect(request.portions[0]?.ordinaryLastClaimDate?.year).toBe(2030);
  await user.click(screen.getByRole("button", { name: "Defer reviewed GST" }));
  const deferred = purchaseCodecs.defer.decodeRequest(propose.mock.calls[1]![1]);
  expect(deferred.portions).toEqual(request.portions);
  expect(deferred.reason).toBe(values["Claim / deferral review reason"]);
  mounted.unmount();
  render(
    <PurchaseClaimForm
      document={acquisition}
      workspace={workspace}
      documentApi={fixture.conveyor.documents}
      accounting={facts.accounting}
      evidence={facts.evidence}
      payments={[]}
      propose={propose}
      onError={onError}
      retainedRequest={request}
    />,
  );
  expect(
    ((await screen.findByLabelText("Earliest eligible period start")) as HTMLInputElement).value,
  ).toBe("2026-07-01");
  expect((screen.getByLabelText("Actual required return due date") as HTMLInputElement).value).toBe(
    "2026-11-03",
  );
  expect((screen.getByLabelText("Due-date proof") as HTMLSelectElement).value).toBe(proof.id);
  expect((screen.getByLabelText("Claim / affected period start") as HTMLInputElement).value).toBe(
    "2026-10-01",
  );
  expect(
    (screen.getByLabelText("GST release posting date (for claim)") as HTMLInputElement).value,
  ).toBe("2026-10-07");
  expect((screen.getByLabelText("Claim / deferral review reason") as HTMLInputElement).value).toBe(
    values["Claim / deferral review reason"],
  );
});

it("closes a reopened retained deferral using its owned ID and source portion", async () => {
  const fixture = fixtureForRecords();
  const facts = claimFacts();
  const propose = vi.fn();
  const user = userEvent.setup();
  const deferral = create(p.PurchaseGstDeferralSchema, {
    id: "018f0000-0000-7000-8000-000000000040",
    portions: [
      {
        source: {
          case: "posting",
          value: {
            postingId: facts.accounting.posting!.id,
            postingHash: facts.accounting.posting!.sourceHash,
            allocationSequence: 1,
          },
        },
        normalEventDate: facts.accounting.posting!.invoiceDate,
        earliestOtherwiseEligiblePeriod: {
          periodStart: { year: 2026, month: 7, day: 1 },
          periodEnd: { year: 2026, month: 9, day: 30 },
        },
        requiredReturnDueDate: { year: 2026, month: 11, day: 3 },
        ordinaryLastClaimDate: { year: 2030, month: 11, day: 3 },
        dueDateEvidence: {
          sourceId: proof.id,
          sourceVersion: proof.version,
          sourceHash: proof.sha256,
        },
      },
    ],
  });
  render(
    <PurchaseClaimForm
      document={acquisition}
      workspace={workspace}
      documentApi={fixture.conveyor.documents}
      accounting={facts.accounting}
      evidence={facts.evidence}
      payments={[]}
      deferral={deferral}
      propose={propose}
      onError={vi.fn()}
    />,
  );
  await user.click(screen.getByText("Review claim period or defer GST"));
  await user.selectOptions(screen.getByLabelText("Retained deferral portion"), "0");
  for (const [label, value] of Object.entries({
    "Claim / affected period start": "2026-10-01",
    "Claim / affected period end": "2026-12-31",
    "GST release posting date (for claim)": "2026-10-07",
    "Timing review as-of date": "2026-10-07",
    "Claim / deferral review reason": "Retained deadline and source verified again",
  }))
    await user.type(screen.getByLabelText(label), value);
  await user.click(
    screen.getByLabelText("Reviewed allowable period, ordinary time limit and evidence held"),
  );
  await user.click(screen.getByRole("button", { name: "Attribute reviewed GST" }));
  const request = purchaseCodecs.attribute.decodeRequest(propose.mock.calls[0]![1]);
  expect(request.deferralId).toBe(deferral.id);
  expect(request.portions[0]?.source).toEqual(deferral.portions[0]?.source);
  expect(request.portions[0]?.evidenceId).toBe(facts.evidence.id);
});

it("supports an explicit opening-source legacy review without fabricating a document or payment", async () => {
  const fixture = fixtureForRecords();
  const propose = vi.fn();
  const user = userEvent.setup();
  const source = create(p.LegacyPurchaseSourceRefSchema, {
    source: {
      case: "openingPayable",
      value: {
        openingPayableId: "018f0000-0000-7000-8000-000000000050",
        sourceVersion: 3n,
        sourceHash: new Uint8Array(32).fill(7),
      },
    },
  });
  const mounted = render(
    <PurchaseLegacyReviewPanel
      source={source}
      workspace={workspace}
      documentApi={fixture.conveyor.documents}
      accountingApi={fixture.conveyor.accounting}
      propose={propose}
      onError={vi.fn()}
    />,
  );
  await user.click(screen.getByText("Review legacy purchase / opening provenance"));
  expect(screen.getByText(/has retained provenance without a document/)).toBeTruthy();
  for (const [label, value] of Object.entries({
    "Legacy affected period start": "2026-07-01",
    "Legacy affected period end": "2026-09-30",
    "Legacy reporting balance date": "2026-09-30",
    "Legacy review reason": "Noncreditable opening reviewed against retained records",
  }))
    await user.type(screen.getByLabelText(label), value);
  await user.selectOptions(await screen.findByLabelText("Legacy supporting evidence"), proof.id);
  await user.click(
    screen.getByLabelText(
      "Reviewed legacy accounting, settlement and claimed status against retained records",
    ),
  );
  await user.click(screen.getByRole("button", { name: "Review legacy decision" }));
  await waitFor(() => expect(propose).toHaveBeenCalledOnce());
  const request = purchaseCodecs.legacy.decodeRequest(propose.mock.calls[0]![1]);
  expect(request.review?.source).toEqual(source);
  expect(request.review?.supportingPayments).toEqual([]);
  expect(request.review?.supportingEvidence[0]?.sourceId).toBe(proof.id);
  mounted.unmount();
  render(
    <PurchaseLegacyReviewPanel
      source={source}
      workspace={workspace}
      documentApi={fixture.conveyor.documents}
      accountingApi={fixture.conveyor.accounting}
      propose={propose}
      onError={vi.fn()}
      retainedRequest={request}
    />,
  );
  expect(
    ((await screen.findByLabelText("Legacy affected period start")) as HTMLInputElement).value,
  ).toBe("2026-07-01");
  expect((screen.getByLabelText("Legacy reporting balance date") as HTMLInputElement).value).toBe(
    "2026-09-30",
  );
  expect((screen.getByLabelText("Legacy supporting evidence") as HTMLSelectElement).value).toBe(
    proof.id,
  );
  expect((screen.getByLabelText("Legacy review reason") as HTMLInputElement).value).toBe(
    "Noncreditable opening reviewed against retained records",
  );
});

it("links legacy evidence only to a selected owned posted purchase with returned posting facts", async () => {
  const fixture = fixtureForRecords();
  fixture.procedures.documents.listDocuments.mockImplementation(async (frame: Uint8Array) => {
    const request = retainedDocumentCodec.decodeRequest(frame);
    if (request.accounting === DocumentAccountingFilter.POSTED) {
      expect(request.status).toBe(DocumentStatus.REVIEWED);
      return retainedDocumentCodec.encodeResponse(
        create(ListDocumentsResponseSchema, {
          documents: [postedDocument],
          page: { returnedCount: 1 },
        }),
      );
    }
    return retainedDocumentCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents: [acquisition, postedDocument, proof],
        page: { returnedCount: 3 },
      }),
    );
  });
  const postingId = "018f0000-0000-7000-8000-000000000060";
  const posting = create(p.PurchasePostingSchema, {
    id: postingId,
    organisationId,
    documentId: postedDocument.id,
    reviewedVersion: postedDocument.version,
    originalContentHash: postedDocument.sha256,
    reviewedSnapshotHash: new Uint8Array(32).fill(5),
    supplierName: "Verified Office Supplier",
    invoiceNumber: "PO-42",
    invoiceDate: { year: 2026, month: 8, day: 15 },
    gstBasis: GstBasis.NON_CASH,
    gross: { currencyCode: "AUD", minorUnits: 7700n },
    supplierGst: { currencyCode: "AUD", minorUnits: 700n },
    eligibleGst: { currencyCode: "AUD", minorUnits: 700n },
    allocations: [
      create(p.PurchaseAllocationInputSchema, {
        accountId: "018f0000-0000-7000-8000-000000000061",
        accountVersion: 1n,
        sequence: 1,
        gross: { currencyCode: "AUD", minorUnits: 7700n },
        supplierGst: { currencyCode: "AUD", minorUnits: 700n },
        eligibleGst: { currencyCode: "AUD", minorUnits: 700n },
        use: p.PurchaseUse.BUSINESS,
        basClassification: p.PurchaseBasClassification.NON_CAPITAL,
        cashFlowClassification: p.PurchaseCashFlowClassification.OPERATING,
        eligibilityReviewConfirmed: true,
        evidenceReviewConfirmed: true,
      }),
    ],
    sourceHash: new Uint8Array(32).fill(9),
  });
  const queriedDocumentIds: string[] = [];
  fixture.procedures.purchases.getPurchaseAccounting.mockImplementation(
    async (frame: Uint8Array) => {
      const request = purchaseCodecs.get.decodeRequest(frame);
      queriedDocumentIds.push(request.documentId);
      return purchaseCodecs.get.encodeResponse(
        create(p.GetPurchaseAccountingResponseSchema, {
          accounting: create(p.PurchaseAccountingSchema, {
            documentId: postedDocument.id,
            organisationId,
            posted: true,
            posting,
          }),
        }),
      );
    },
  );
  const source = create(p.LegacyPurchaseSourceRefSchema, {
    source: {
      case: "document",
      value: {
        documentId: acquisition.id,
        reviewedVersion: acquisition.version,
        originalContentHash: acquisition.sha256,
        reviewedSnapshotHash: new Uint8Array(32).fill(6),
      },
    },
  });
  const propose = vi.fn();
  const user = userEvent.setup();
  render(
    <PurchaseLegacyReviewPanel
      source={source}
      workspace={workspace}
      documentApi={fixture.conveyor.documents}
      purchaseApi={fixture.conveyor.purchases}
      accountingApi={fixture.conveyor.accounting}
      propose={propose}
      onError={vi.fn()}
    />,
  );
  await user.click(screen.getByText("Review legacy purchase / opening provenance"));
  await user.selectOptions(
    screen.getByLabelText("Legacy disposition"),
    String(p.LegacyPurchaseDisposition.EVIDENCE_ONLY_LINKED_PURCHASE),
  );
  await user.selectOptions(
    await screen.findByLabelText("Legacy supporting evidence"),
    acquisition.id,
  );
  await user.selectOptions(
    await screen.findByLabelText("Existing posted purchase"),
    postedDocument.id,
  );
  expect(await screen.findByText(/Verified Office Supplier · PO-42/)).toBeTruthy();
  expect(screen.getByText(/15\/08\/2026 · \$77\.00/)).toBeTruthy();
  expect(screen.queryByLabelText("Existing linked purchase posting ID")).toBeNull();
  expect(queriedDocumentIds).toEqual([postedDocument.id]);
  for (const [label, value] of Object.entries({
    "Legacy affected period start": "2026-07-01",
    "Legacy affected period end": "2026-09-30",
    "Legacy reporting balance date": "2026-09-30",
    "Legacy review reason": "Verified evidence against the existing posted purchase",
  }))
    await user.type(screen.getByLabelText(label), value);
  await user.click(
    screen.getByLabelText(
      "Reviewed legacy accounting, settlement and claimed status against retained records",
    ),
  );
  await user.click(screen.getByRole("button", { name: "Review legacy decision" }));
  await waitFor(() => expect(propose).toHaveBeenCalledOnce());
  const request = purchaseCodecs.legacy.decodeRequest(propose.mock.calls[0]![1]);
  expect(request.review?.disposition).toBe(p.LegacyPurchaseDisposition.EVIDENCE_ONLY_LINKED_PURCHASE);
  expect(request.review?.linkedPostingId).toBe(postingId);
  await user.selectOptions(
    screen.getByLabelText("Legacy disposition"),
    String(p.LegacyPurchaseDisposition.DUPLICATE),
  );
  await user.click(screen.getByRole("button", { name: "Review legacy decision" }));
  await waitFor(() => expect(propose).toHaveBeenCalledTimes(2));
  const duplicateRequest = purchaseCodecs.legacy.decodeRequest(propose.mock.calls[1]![1]);
  expect(duplicateRequest.review?.disposition).toBe(p.LegacyPurchaseDisposition.DUPLICATE);
  expect(duplicateRequest.review?.linkedPostingId).toBe(postingId);
});

it("requires positive human review for low-value evidence and never applies a rule from an allocation amount", () => {
  const fields = {
    ...initialEvidence(),
    classification: p.PurchaseEvidenceClassification.SUPPORTED_EXCEPTION,
    held: "2026-09-25",
    reason: "Whole transaction and supplier ABN/items/date/price reviewed",
    confirmed: true,
    exceptionConfirmed: true,
    supportConfirmed: true,
  };
  const review = reviewedEvidence(fields, acquisition);
  expect(review.exceptionReview?.supportingRecords[0]?.sourceId).toBe(acquisition.id);
  expect(review.exceptionReview?.rule?.sourceHash).toHaveLength(32);
  expect(() => reviewedEvidence({ ...fields, supportConfirmed: false }, acquisition)).toThrow();
  expect(() =>
    reviewedEvidence(
      fields,
      create(DocumentSchema, {
        ...acquisition,
        candidate: create(DocumentCandidateSchema, {
          subtotal: create(MoneySchema, { currencyCode: "AUD", minorUnits: 7501n }),
        }),
      }),
    ),
  ).toThrow();
});
