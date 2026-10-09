import { create } from "@bufbuild/protobuf";
import { RecordPurchasePaymentRequestSchema } from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { describe, expect, it } from "vitest";
import { authenticatedPurchaseRetry } from "./purchase-attempt-recovery";
import { purchaseCodecs } from "./purchase-codecs";
import {
  exactAud,
  PurchaseCommandState,
  purchaseAttemptsFor,
  uuidV7,
} from "./purchase-command-state";

describe("purchase command attempts", () => {
  it("retains exact request bytes after an uncertain response and prevents double submission", () => {
    const state = new PurchaseCommandState();
    const frame = new Uint8Array([1, 2, 3]);
    state.begin("postReviewedPurchase", frame);
    frame[0] = 99;
    expect(() => state.begin("recordPurchasePayment", frame)).toThrow();
    state.uncertain();
    const retry = state.retry();
    expect(retry.method).toBe("postReviewedPurchase");
    expect([...retry.frame]).toEqual([1, 2, 3]);
    retry.frame[0] = 88;
    state.uncertain();
    expect([...state.retry().frame]).toEqual([1, 2, 3]);
    state.confirmed();
    expect(state.pending).toBeUndefined();
  });

  it("requires explicit correction resolution after a definite rejection", () => {
    const state = new PurchaseCommandState();
    state.begin("recordPurchasePayment", new Uint8Array([4]));
    state.rejected();
    expect(() => state.begin("recordPurchasePayment", new Uint8Array([5]))).toThrow();
    state.resolveRejected();
    state.begin("recordPurchasePayment", new Uint8Array([5]));
    expect([...state.pending!.frame]).toEqual([5]);
  });

  it("does not turn uncertain transport failures into new-key corrections", () => {
    const state = new PurchaseCommandState();
    state.begin("recordPurchaseTaxEvidence", new Uint8Array([6]));
    state.uncertain();
    expect(() => state.resolveRejected()).toThrow();
  });
});

it("retains a pending request across navigation while isolating other actors and workspaces", () => {
  const state = purchaseAttemptsFor("workspace-a:org-a:actor-a:document-a");
  state.begin("recordPurchasePayment", new Uint8Array([9]));
  state.uncertain();
  expect(purchaseAttemptsFor("workspace-a:org-a:actor-a:document-a")).toBe(state);
  expect(purchaseAttemptsFor("workspace-a:org-a:actor-b:document-a").pending).toBeUndefined();
  expect(purchaseAttemptsFor("workspace-b:org-a:actor-a:document-a").pending).toBeUndefined();
  state.confirmed();
  expect(purchaseAttemptsFor("workspace-a:org-a:actor-a:document-a").pending).toBeUndefined();
});

it("explicit session recovery changes only authentication and preserves the key and business facts", () => {
  const workspace = {
    workspaceId: "018f0000-0000-7000-8000-000000000001",
    userId: "018f0000-0000-7000-8000-000000000002",
    sessionId: "018f0000-0000-7000-8000-000000000003",
    organisationId: "018f0000-0000-7000-8000-000000000004",
  };
  const request = create(RecordPurchasePaymentRequestSchema, {
    organisationId: workspace.organisationId,
    documentId: "018f0000-0000-7000-8000-000000000010",
    commandContext: {
      authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
      idempotencyKey: "018f0000-0000-7000-8000-000000000050",
    },
    expectedAccountingVersion: 8n,
    actualPaymentDate: { year: 2026, month: 9, day: 30 },
    amount: { currencyCode: "AUD", minorUnits: 5500n },
    bankAccountId: "018f0000-0000-7000-8000-000000000031",
    reference: "actual transfer",
  });
  const attempt = {
    method: "recordPurchasePayment" as const,
    frame: purchaseCodecs.payment.encodeRequest(request),
    status: "uncertain" as const,
  };
  const recovered = purchaseCodecs.payment.decodeRequest(
    authenticatedPurchaseRetry(attempt, {
      ...workspace,
      sessionId: "018f0000-0000-7000-8000-000000000053",
    }),
  );
  expect(recovered.commandContext?.authentication?.sessionId).toBe(
    "018f0000-0000-7000-8000-000000000053",
  );
  recovered.commandContext!.authentication = request.commandContext!.authentication;
  expect(recovered).toEqual(request);
  expect(() =>
    authenticatedPurchaseRetry(attempt, {
      ...workspace,
      userId: "018f0000-0000-7000-8000-000000000054",
    }),
  ).toThrow();
  expect(() =>
    authenticatedPurchaseRetry(attempt, {
      ...workspace,
      organisationId: "018f0000-0000-7000-8000-000000000055",
    }),
  ).toThrow();
});

it("parses AUD exactly without floating point or rounding", () => {
  expect(exactAud("90071992547409.93")).toBe(9007199254740993n);
  expect(exactAud("0.01")).toBe(1n);
  expect(exactAud("55")).toBe(5500n);
  for (const value of ["1.001", "-1", "1e3", "", ".5", "1,000", "92233720368547758.08"]) {
    expect(exactAud(value)).toBeUndefined();
  }
  expect(uuidV7()).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
});
