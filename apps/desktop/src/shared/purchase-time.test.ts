import { describe, expect, it } from "vitest";
import { timestampUtcCivilDate } from "../../tests/e2e/support/purchase-time.js";

describe("purchase evidence review dates", () => {
  it("keeps the Core UTC civil date when Melbourne is already on the following day", () => {
    const millis = Date.parse("2026-10-22T13:00:00.000Z");
    const timestamp = {
      seconds: BigInt(Math.floor(millis / 1000)),
      nanos: (millis % 1000) * 1_000_000,
    };
    const local = new Intl.DateTimeFormat("en-AU", {
      day: "2-digit",
      month: "2-digit",
      timeZone: "Australia/Melbourne",
      year: "numeric",
    }).format(new Date(millis));
    expect(local).toBe("23/10/2026");
    expect(timestampUtcCivilDate(timestamp)).toBe("2026-10-22");
  });
});
