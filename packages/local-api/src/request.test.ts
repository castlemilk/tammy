import assert from "node:assert/strict";
import test from "node:test";

import { validateLocalApiRequest } from "./request.ts";

test("validates and resolves a protocol-neutral read-only tool call", () => {
  const result = validateLocalApiRequest({
    arguments: {},
    name: "tammy.accounting.list_accounts",
    operation: "tool.call",
    version: "v1",
  });

  assert.equal(result.ok, true);
  if (!result.ok) return;
  assert.equal(result.value.operation, "tool.call");
  assert.equal(result.value.tool.name, "tammy.accounting.list_accounts");
  assert.deepEqual(result.value.arguments, {});
  assert.equal(result.value.tool.target.module, "accounting");
  assert.equal(result.value.tool.target.procedure, "listAccounts");
});

test("validates and resolves a protocol-neutral resource read", () => {
  const result = validateLocalApiRequest({
    operation: "resource.read",
    uri: "tammy://current/organisation",
    version: "v1",
  });

  assert.equal(result.ok, true);
  if (!result.ok) return;
  assert.equal(result.value.operation, "resource.read");
  assert.equal(result.value.resource.uri, "tammy://current/organisation");
  assert.equal(result.value.resource.target.module, "organisation");
  assert.equal(result.value.resource.target.procedure, "getOrganisation");
});

test("canonicalizes an omitted tool argument object to an empty object", () => {
  const result = validateLocalApiRequest({
    name: "tammy.system.get_diagnostics",
    operation: "tool.call",
    version: "v1",
  });

  assert.equal(result.ok, true);
  if (result.ok && result.value.operation === "tool.call") {
    assert.deepEqual(result.value.arguments, {});
    assert.equal(Object.isFrozen(result.value.arguments), true);
  }
});

test("rejects unsupported versions and operations with stable error codes", () => {
  assert.deepEqual(
    validateLocalApiRequest({
      name: "tammy.system.get_diagnostics",
      operation: "tool.call",
      version: "v2",
    }),
    { error: { code: "UNSUPPORTED_VERSION" }, ok: false },
  );
  assert.deepEqual(validateLocalApiRequest({ operation: "rpc.call", version: "v1" }), {
    error: { code: "UNKNOWN_OPERATION" },
    ok: false,
  });
});

test("rejects unknown, write, credential, and unfinished tool names", () => {
  for (const name of [
    "tammy.accounting.create_account",
    "tammy.sbr.import_machine_credential",
    "tammy.workspace.unlock",
    "tammy.company_tax.get_return",
    "call_rpc",
  ]) {
    assert.deepEqual(
      validateLocalApiRequest({ arguments: {}, name, operation: "tool.call", version: "v1" }),
      { error: { code: "UNKNOWN_TOOL" }, ok: false },
    );
  }
});

test("rejects unknown resources and every non-empty tool argument object", () => {
  assert.deepEqual(
    validateLocalApiRequest({
      operation: "resource.read",
      uri: "tammy://current/machine-credential",
      version: "v1",
    }),
    { error: { code: "UNKNOWN_RESOURCE" }, ok: false },
  );

  for (const argumentsValue of [
    { authentication: { actorUserId: "user", sessionId: "secret" } },
    { filePath: "/tmp/credential.p12" },
    { productId: "secret" },
    [],
    null,
    "",
  ]) {
    assert.deepEqual(
      validateLocalApiRequest({
        arguments: argumentsValue,
        name: "tammy.system.get_diagnostics",
        operation: "tool.call",
        version: "v1",
      }),
      { error: { code: "INVALID_ARGUMENTS" }, ok: false },
    );
  }
});

test("rejects malformed envelopes and unexpected fields", () => {
  for (const request of [
    null,
    [],
    {},
    { name: "tammy.system.get_diagnostics", operation: "tool.call" },
    {
      name: "tammy.system.get_diagnostics",
      operation: "tool.call",
      password: "secret",
      version: "v1",
    },
    {
      operation: "resource.read",
      uri: "tammy://system/diagnostics",
      version: "v1",
      workspacePath: "/tmp/private",
    },
  ]) {
    assert.deepEqual(validateLocalApiRequest(request), {
      error: { code: "INVALID_REQUEST" },
      ok: false,
    });
  }
});
