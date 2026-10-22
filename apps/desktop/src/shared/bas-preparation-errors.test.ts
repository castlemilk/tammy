// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  BasCoreCorrection,
  basPreparationCorrectionCodes,
  isBasPreparationCorrectionCode,
} from "./bas-preparation-errors";

describe("BAS preparation error codes", () => {
  it("exposes only the reviewed static codes", () => {
    expect(basPreparationCorrectionCodes).toEqual([
      "BAS_SOURCE_CAPACITY",
      "BAS_CONFLICT",
      "BAS_IDEMPOTENCY_CONFLICT",
      "BAS_PERMISSION_DENIED",
      "BAS_AUTHENTICATION_REQUIRED",
      "BAS_REQUEST_TIMEOUT",
    ]);
    for (const code of basPreparationCorrectionCodes)
      expect(isBasPreparationCorrectionCode(code)).toBe(true);
    for (const value of ["BAS_PRIVATE_DETAIL", "CORE_REQUEST_FAILED", 3, null, undefined])
      expect(isBasPreparationCorrectionCode(value)).toBe(false);
  });

  it("creates a correction whose only public message is its static code", () => {
    const correction = new BasCoreCorrection("BAS_IDEMPOTENCY_CONFLICT");
    expect(correction).toMatchObject({
      name: "BasCoreCorrection",
      code: "BAS_IDEMPOTENCY_CONFLICT",
      message: "BAS_IDEMPOTENCY_CONFLICT",
    });
    expect(JSON.stringify(correction)).not.toMatch(/private|password|secret|metadata|detail/i);
  });
});
