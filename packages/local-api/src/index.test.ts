import assert from "node:assert/strict";
import test from "node:test";

import {
  LOCAL_API_READ_RESOURCES,
  LOCAL_API_READ_TOOLS,
  LOCAL_API_VERSION,
  validateLocalApiRequest,
} from "./index.ts";

test("exports the complete local API contract from the package entrypoint", () => {
  assert.equal(LOCAL_API_VERSION, "v1");
  assert.equal(LOCAL_API_READ_TOOLS.length, 12);
  assert.equal(LOCAL_API_READ_RESOURCES.length, 9);
  assert.equal(
    validateLocalApiRequest({
      name: "tammy.system.get_diagnostics",
      operation: "tool.call",
      version: "v1",
    }).ok,
    true,
  );
});
