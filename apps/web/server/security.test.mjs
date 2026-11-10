import assert from "node:assert/strict";
import { test } from "node:test";
import { isTrustedRequest } from "./security.mjs";

test("accepts the configured loopback host and browser origin for a JSON mutation", () => {
  assert.equal(
    isTrustedRequest(
      {
        method: "POST",
        host: "127.0.0.1:4174",
        origin: "http://127.0.0.1:5173",
        contentType: "application/json",
      },
      { host: "127.0.0.1:4174", origin: "http://127.0.0.1:5173" },
    ),
    true,
  );
});

test("rejects DNS rebinding, cross-origin, missing origin and non-JSON mutation requests", () => {
  const config = { host: "127.0.0.1:4174", origin: "http://127.0.0.1:5173" };
  const valid = {
    method: "POST",
    host: config.host,
    origin: config.origin,
    contentType: "application/json",
  };
  for (const patch of [
    { host: "evil.example:4174" },
    { origin: "https://evil.example" },
    { origin: undefined },
    { contentType: "text/plain" },
  ]) {
    assert.equal(isTrustedRequest({ ...valid, ...patch }, config), false);
  }
});

test("allows same-host safe reads but rejects foreign host even for reads", () => {
  const config = { host: "127.0.0.1:4174", origin: "http://127.0.0.1:5173" };
  assert.equal(isTrustedRequest({ method: "GET", host: config.host }, config), true);
  assert.equal(isTrustedRequest({ method: "GET", host: "evil.example:4174" }, config), false);
});
