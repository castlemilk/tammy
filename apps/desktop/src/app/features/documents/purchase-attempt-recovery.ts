import { create } from "@bufbuild/protobuf";
import {
  AuthenticationContextSchema,
  type CommandContext,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { purchaseCodecs } from "./purchase-codecs";
import type { PurchaseAttempt } from "./purchase-command-state";

/** Recovery changes transport authentication only. Key and business facts remain exact. */
export function authenticatedPurchaseRetry(
  attempt: PurchaseAttempt,
  workspace: AuthenticatedWorkspace,
): Uint8Array {
  const authentication = { actorUserId: workspace.userId, sessionId: workspace.sessionId };
  function recover<
    Request extends { organisationId: string; commandContext?: CommandContext | undefined },
  >(request: Request): Request {
    if (
      request.organisationId !== workspace.organisationId ||
      request.commandContext?.authentication?.actorUserId !== workspace.userId
    )
      throw new Error("This purchase attempt belongs to a different authenticated context.");
    request.commandContext.authentication = create(AuthenticationContextSchema, authentication);
    return request;
  }
  switch (attempt.method) {
    case "postReviewedPurchase":
      return purchaseCodecs.post.encodeRequest(
        recover(purchaseCodecs.post.decodeRequest(attempt.frame)),
      );
    case "recordPurchasePayment":
      return purchaseCodecs.payment.encodeRequest(
        recover(purchaseCodecs.payment.decodeRequest(attempt.frame)),
      );
    case "recordPurchaseTaxEvidence":
      return purchaseCodecs.evidence.encodeRequest(
        recover(purchaseCodecs.evidence.decodeRequest(attempt.frame)),
      );
    case "attributePurchaseGst":
      return purchaseCodecs.attribute.encodeRequest(
        recover(purchaseCodecs.attribute.decodeRequest(attempt.frame)),
      );
    case "deferPurchaseGstClaim":
      return purchaseCodecs.defer.encodeRequest(
        recover(purchaseCodecs.defer.decodeRequest(attempt.frame)),
      );
    case "resolveLegacyPurchaseReview":
      return purchaseCodecs.legacy.encodeRequest(
        recover(purchaseCodecs.legacy.decodeRequest(attempt.frame)),
      );
  }
}
