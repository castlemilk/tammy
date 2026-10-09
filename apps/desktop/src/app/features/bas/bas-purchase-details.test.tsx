import { create } from "@bufbuild/protobuf";
import { CommandContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { Role } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import * as purchases from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { PurchaseReviewStatus } from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import {
  BasPurchaseReviewKind,
  BasPurchaseReviewSchema,
  BasPurchaseSourceFormat,
  BasWorkpaperSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
import { purchaseCodecs } from "../documents/purchase-codecs";
import { purchaseAttemptsFor } from "../documents/purchase-command-state";
import { BasPurchaseDetails } from "./bas-purchase-details";

const organisationId = "018f0000-0000-7000-8000-000000000004";
const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  organisationId,
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  roles: [Role.BUSINESS_PREPARER],
};
const openingId = "018f0000-0000-7000-8000-000000000031";
const legacyScope = `${workspace.workspaceId}:${organisationId}:${workspace.userId}:legacy:${openingId}`;
const documentId = "018f0000-0000-7000-8000-000000000010";
const documentScope = `${workspace.workspaceId}:${organisationId}:${workspace.userId}:document:${documentId}`;
afterEach(() => {
  purchaseAttemptsFor(legacyScope).confirmed();
  purchaseAttemptsFor(documentScope).confirmed();
});

it("drills into saved purchase dates and keeps unresolved opening candidates outside credits", async () => {
  createConveyorFixture();
  const onReview = vi.fn();
  const value = create(BasWorkpaperSchema, {
    id: "018f0000-0000-7000-8000-000000000401",
    purchaseSourceFormat: BasPurchaseSourceFormat.RECONCILED_V1,
    legacyCandidateGst: { currencyCode: "AUD", minorUnits: 700n },
    purchaseSources: [
      {
        documentId: "018f0000-0000-7000-8000-000000000010",
        postingId: "018f0000-0000-7000-8000-000000000020",
        paymentId: "018f0000-0000-7000-8000-000000000021",
        invoiceDate: { year: 2026, month: 9, day: 25 },
        actualPaymentDate: { year: 2026, month: 10, day: 2 },
        claimPeriod: {
          periodStart: { year: 2026, month: 10, day: 1 },
          periodEnd: { year: 2026, month: 12, day: 31 },
        },
        allocatedGross: { currencyCode: "AUD", minorUnits: 5500n },
        claimedGst: { currencyCode: "AUD", minorUnits: 500n },
      },
    ],
    purchaseReviews: [
      {
        legacySource: {
          source: {
            case: "openingPayable",
            value: {
              openingPayableId: "018f0000-0000-7000-8000-000000000030",
              sourceVersion: 1n,
              sourceHash: new Uint8Array(32),
            },
          },
        },
        status: PurchaseReviewStatus.UNRESOLVED_LEGACY,
        reason: "Opening source without a linked document",
        unclaimedGst: { currencyCode: "AUD", minorUnits: 700n },
      },
    ],
  });
  render(<BasPurchaseDetails value={value} onReviewDocument={onReview} />);
  const sources = screen.getByRole("table", { name: "Saved BAS purchase sources" });
  expect(within(sources).getByText("25/09/2026")).toBeTruthy();
  expect(within(sources).getByText("02/10/2026")).toBeTruthy();
  expect(screen.getByText(/Provisional legacy candidate GST: \$7.00/)).toBeTruthy();
  expect(screen.getByText(/Opening source without a linked document/)).toBeTruthy();
  await userEvent.setup().click(screen.getByRole("button", { name: "Open purchase review" }));
  expect(onReview).toHaveBeenCalledWith("018f0000-0000-7000-8000-000000000010");
});

it("routes unresolved BAS reviews from their retained posting document ID", async () => {
  createConveyorFixture();
  const documentId = "018f0000-0000-7000-8000-000000000041";
  const value = create(BasWorkpaperSchema, {
    id: "018f0000-0000-7000-8000-000000000042",
    purchaseSourceFormat: BasPurchaseSourceFormat.RECONCILED_V1,
    purchaseReviews: [
      create(BasPurchaseReviewSchema, {
        documentId,
        status: purchases.PurchaseReviewStatus.NEEDS_EVIDENCE,
        reason: "The source is unresolved but has no eligible credit row",
        sourceRevision: 2n,
        sourceHash: new Uint8Array(32).fill(8),
        kind: BasPurchaseReviewKind.MISSING_EVIDENCE,
      }),
    ],
  });
  const onReview = vi.fn();
  render(<BasPurchaseDetails value={value} onReviewDocument={onReview} />);
  await userEvent.setup().click(screen.getByRole("button", { name: "Review purchase source" }));
  expect(onReview).toHaveBeenCalledWith(documentId);
});

it("restores a document legacy attempt across surfaces and refreshes only authentication on explicit retry", async () => {
  const fixture = createConveyorFixture();
  const source = create(purchases.LegacyPurchaseSourceRefSchema, {
    source: {
      case: "document",
      value: {
        documentId,
        reviewedVersion: 3n,
        originalContentHash: new Uint8Array(32).fill(7),
        reviewedSnapshotHash: new Uint8Array(32).fill(6),
      },
    },
  });
  const review = create(purchases.LegacyPurchaseReviewInputSchema, {
    source,
    affectedPeriod: {
      periodStart: { year: 2026, month: 7, day: 1 },
      periodEnd: { year: 2026, month: 9, day: 30 },
    },
    reportingBalanceDate: { year: 2026, month: 9, day: 30 },
    disposition: purchases.LegacyPurchaseDisposition.NONCREDITABLE,
    supportingEvidence: [
      create(purchases.PurchaseRetainedSourceRefSchema, {
        sourceId: "018f0000-0000-7000-8000-000000000032",
        sourceVersion: 4n,
        sourceHash: new Uint8Array(32).fill(8),
      }),
    ],
    reason: "Document provenance reviewed before session lock",
  });
  const request = create(purchases.ResolveLegacyPurchaseReviewRequestSchema, {
    commandContext: create(CommandContextSchema, {
      authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
      idempotencyKey: "018f0000-0000-7000-8000-000000000033",
    }),
    organisationId,
    review,
  });
  const originalFrame = purchaseCodecs.legacy.encodeRequest(request);
  const attempt = purchaseAttemptsFor(documentScope);
  attempt.begin("resolveLegacyPurchaseReview", originalFrame);
  attempt.uncertain();
  const savedDecision = create(purchases.LegacyPurchaseReviewSchema, {
    id: "018f0000-0000-7000-8000-000000000034",
  });
  const saved = create(BasWorkpaperSchema, {
    id: "018f0000-0000-7000-8000-000000000035",
    purchaseSourceFormat: BasPurchaseSourceFormat.RECONCILED_V1,
    purchaseReviews: [
      create(BasPurchaseReviewSchema, {
        legacySource: source,
        status: purchases.PurchaseReviewStatus.UNRESOLVED_LEGACY,
        reason: "Saved snapshot still requires explicit review",
        sourceRevision: 1n,
        sourceHash: new Uint8Array(32).fill(9),
        kind: BasPurchaseReviewKind.OLD_LEGACY_DOCUMENT,
      }),
    ],
  });
  const original = render(
    <BasPurchaseDetails
      value={saved}
      purchaseApi={fixture.conveyor.purchases}
      documentApi={fixture.conveyor.documents}
      accountingApi={fixture.conveyor.accounting}
      workspace={workspace}
    />,
  );
  await screen.findByRole("button", { name: "Retry exact legacy request" });
  expect((screen.getByLabelText("Legacy affected period start") as HTMLInputElement).value).toBe(
    "2026-07-01",
  );
  expect((screen.getByLabelText("Legacy reporting balance date") as HTMLInputElement).value).toBe(
    "2026-09-30",
  );
  expect((screen.getByLabelText("Legacy supporting evidence") as HTMLSelectElement).value).toBe(
    "018f0000-0000-7000-8000-000000000032",
  );
  expect((screen.getByLabelText("Legacy review reason") as HTMLInputElement).value).toBe(
    "Document provenance reviewed before session lock",
  );
  original.unmount();

  const resumedWorkspace = { ...workspace, sessionId: "018f0000-0000-7000-8000-000000000099" };
  let retried: purchases.ResolveLegacyPurchaseReviewRequest | undefined;
  fixture.procedures.purchases.resolveLegacyPurchaseReview.mockImplementation(
    async (frame: Uint8Array) => {
      retried = purchaseCodecs.legacy.decodeRequest(frame);
      return purchaseCodecs.legacy.encodeResponse(
        create(purchases.ResolveLegacyPurchaseReviewResponseSchema, { review: savedDecision }),
      );
    },
  );
  const resumed = render(
    <BasPurchaseDetails
      value={saved}
      purchaseApi={fixture.conveyor.purchases}
      documentApi={fixture.conveyor.documents}
      accountingApi={fixture.conveyor.accounting}
      workspace={resumedWorkspace}
    />,
  );
  const user = userEvent.setup();
  expect(((await screen.findByLabelText("Legacy review reason")) as HTMLInputElement).value).toBe(
    "Document provenance reviewed before session lock",
  );
  await user.click(await screen.findByRole("button", { name: "Retry exact legacy request" }));
  await waitFor(() =>
    expect(fixture.procedures.purchases.resolveLegacyPurchaseReview).toHaveBeenCalledOnce(),
  );
  expect(retried?.commandContext?.authentication?.sessionId).toBe(resumedWorkspace.sessionId);
  expect(retried?.commandContext?.idempotencyKey).toBe(request.commandContext?.idempotencyKey);
  expect(retried?.review).toEqual(request.review);
  resumed.unmount();
});

it("blocks a different pending document action and offers only the owned document", async () => {
  const fixture = createConveyorFixture();
  const attempt = purchaseAttemptsFor(documentScope);
  attempt.begin("recordPurchasePayment", Uint8Array.of(1, 2, 3));
  attempt.uncertain();
  const source = create(purchases.LegacyPurchaseSourceRefSchema, {
    source: {
      case: "document",
      value: {
        documentId,
        reviewedVersion: 3n,
        originalContentHash: new Uint8Array(32).fill(7),
        reviewedSnapshotHash: new Uint8Array(32).fill(6),
      },
    },
  });
  const saved = create(BasWorkpaperSchema, {
    id: "018f0000-0000-7000-8000-000000000035",
    purchaseSourceFormat: BasPurchaseSourceFormat.RECONCILED_V1,
    purchaseReviews: [
      create(BasPurchaseReviewSchema, {
        legacySource: source,
        status: purchases.PurchaseReviewStatus.UNRESOLVED_LEGACY,
        reason: "Saved snapshot still requires explicit review",
        sourceRevision: 1n,
        sourceHash: new Uint8Array(32).fill(9),
        kind: BasPurchaseReviewKind.OLD_LEGACY_DOCUMENT,
      }),
    ],
  });
  const onReview = vi.fn();
  render(
    <BasPurchaseDetails
      value={saved}
      purchaseApi={fixture.conveyor.purchases}
      documentApi={fixture.conveyor.documents}
      accountingApi={fixture.conveyor.accounting}
      workspace={workspace}
      onReviewDocument={onReview}
    />,
  );
  expect(
    await screen.findByText(/A different purchase action is pending for this document/),
  ).toBeTruthy();
  expect(screen.queryByRole("button", { name: "Retry exact legacy request" })).toBeNull();
  await userEvent.setup().click(screen.getByRole("button", { name: "Open source document" }));
  expect(onReview).toHaveBeenCalledWith(documentId);
  expect(fixture.procedures.purchases.resolveLegacyPurchaseReview).not.toHaveBeenCalled();
});
