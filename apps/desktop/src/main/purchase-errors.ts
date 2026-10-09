import { toBinary } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  ErrorCategory,
  RetryClassification,
  ValidationErrorDetailSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { isPurchaseCorrectionCode, PurchaseCoreCorrection } from "../shared/purchase-errors";

export function purchaseCoreCorrection(error: unknown): PurchaseCoreCorrection | undefined {
  if (!(error instanceof ConnectError) || error.details.length !== 1) return undefined;
  const details = error.findDetails(ValidationErrorDetailSchema);
  if (details.length !== 1) return undefined;
  const detail = details[0];
  if (!detail) return undefined;
  const context = detail.context;
  if (
    !context ||
    !isPurchaseCorrectionCode(context.code) ||
    context.retry !== RetryClassification.AFTER_USER_ACTION ||
    !/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(
      context.correlationId,
    ) ||
    toBinary(ValidationErrorDetailSchema, detail).byteLength > 1024
  )
    return undefined;
  const validation =
    context.code === "PURCHASE_PAYMENT_INVALID" || context.code === "PURCHASE_CLAIM_TIMING_INVALID";
  if (
    error.code !== (validation ? Code.InvalidArgument : Code.FailedPrecondition) ||
    context.category !== (validation ? ErrorCategory.VALIDATION : ErrorCategory.CONFLICT)
  )
    return undefined;
  return new PurchaseCoreCorrection(context.code);
}
