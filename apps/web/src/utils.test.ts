import { expect, it } from "vitest";
import { uuidV7 } from "./utils";

it("creates UUIDv7 identifiers for idempotent accounting commands", () => {
  expect(uuidV7()).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  expect(uuidV7()).not.toEqual(uuidV7());
});
