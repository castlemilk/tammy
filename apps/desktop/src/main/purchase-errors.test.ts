// @vitest-environment node
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  ErrorCategory,
  RetryClassification,
  ValidationErrorDetailSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { ConveyorError } from "electron-conveyor/define";
import { expect, it } from "vitest";
import { sanitizeHandlerError } from "../conveyor/error";
import { purchaseCoreCorrection } from "./purchase-errors";

function detail() {
  return create(ValidationErrorDetailSchema, {
    context: {
      code: "PURCHASE_STALE",
      category: ErrorCategory.CONFLICT,
      retry: RetryClassification.AFTER_USER_ACTION,
      correlationId: "01900000-0000-7000-8000-000000000001",
      safeSummary: "private password=secret",
      remediation: "/private/evidence.pdf",
    },
  });
}

it("accepts only bounded known corrections with matching status and reviewed retry semantics", () => {
  const valid = detail();
  const outgoing = { desc: ValidationErrorDetailSchema, value: valid };
  expect(
    purchaseCoreCorrection(
      new ConnectError("private password=secret", Code.FailedPrecondition, undefined, [outgoing]),
    ),
  ).toMatchObject({ code: "PURCHASE_STALE", message: "PURCHASE_STALE" });
  for (const mutate of [
    (value: ReturnType<typeof detail>) => {
      value.context!.code = "/private/sql";
    },
    (value: ReturnType<typeof detail>) => {
      value.context!.category = ErrorCategory.INTERNAL;
    },
    (value: ReturnType<typeof detail>) => {
      value.context!.retry = RetryClassification.SAFE;
    },
    (value: ReturnType<typeof detail>) => {
      value.context!.correlationId = "invalid";
    },
    (value: ReturnType<typeof detail>) => {
      value.context!.safeSummary = "secret".repeat(1000);
    },
  ]) {
    const invalid = detail();
    mutate(invalid);
    expect(
      purchaseCoreCorrection(
        new ConnectError("private", Code.FailedPrecondition, undefined, [
          { desc: ValidationErrorDetailSchema, value: invalid },
        ]),
      ),
    ).toBeUndefined();
  }
  expect(
    purchaseCoreCorrection(
      new ConnectError("private", Code.InvalidArgument, undefined, [outgoing]),
    ),
  ).toBeUndefined();
  expect(
    purchaseCoreCorrection(
      new ConnectError("private", Code.FailedPrecondition, undefined, [outgoing, outgoing]),
    ),
  ).toBeUndefined();
  expect(purchaseCoreCorrection(new Error("PURCHASE_STALE"))).toBeUndefined();
});

it("handler sanitization accepts static Conveyor codes only, discarding arbitrary details", () => {
  for (const error of [
    new ConveyorError("PURCHASE_STALE", "private password=secret"),
    new ConveyorError("UNKNOWN", "UNKNOWN"),
    { code: "PURCHASE_STALE", message: "PURCHASE_STALE", path: "/private" },
  ])
    expect(sanitizeHandlerError(error)).toMatchObject({
      code: "CORE_REQUEST_FAILED",
      message: "CORE_REQUEST_FAILED",
    });
  const allowed = new ConveyorError("PURCHASE_STALE", "PURCHASE_STALE");
  Object.assign(allowed, { path: "/private", metadata: { password: "secret" } });
  const sanitized = sanitizeHandlerError(allowed);
  expect(sanitized).toMatchObject({ code: "PURCHASE_STALE", message: "PURCHASE_STALE" });
  expect(JSON.stringify(sanitized)).not.toMatch(/private|password|secret/);
});
