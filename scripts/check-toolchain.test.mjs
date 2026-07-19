import assert from "node:assert/strict";
import test from "node:test";

import { validateToolVersions } from "./check-toolchain.mjs";

test("accepts the exact pinned toolchain versions", () => {
  assert.deepEqual(
    validateToolVersions({
      node: "v24.18.0",
      pnpm: "11.15.0",
      go: "go version go1.26.4 darwin/arm64",
      buf: "1.72.0",
    }),
    [],
  );
});

test("reports every mismatched toolchain version", () => {
  assert.deepEqual(
    validateToolVersions({
      node: "v22.20.0",
      pnpm: "10.9.3",
      go: "go version go1.26.3 darwin/arm64",
      buf: "1.71.0",
    }),
    [
      "Node must be v24.18.0 (received v22.20.0)",
      "pnpm must be 11.15.0 (received 10.9.3)",
      "Go must be go1.26.4 (received go1.26.3)",
      "Buf must be 1.72.0 (received 1.71.0)",
    ],
  );
});
