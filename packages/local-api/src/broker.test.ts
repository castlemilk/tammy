import assert from "node:assert/strict";
import { request } from "node:http";
import { connect } from "node:net";
import test from "node:test";

import * as api from "./index.ts";
import type { ValidatedLocalApiRequest } from "./request.ts";

const read = {
  arguments: {},
  name: "tammy.accounting.list_accounts",
  operation: "tool.call",
  version: "v1",
};

async function start(execute: (value: ValidatedLocalApiRequest) => Promise<unknown>) {
  assert.ok("startLocalApiBroker" in api, "the package exports its real HTTP broker");
  return (api as typeof import("./broker.ts")).startLocalApiBroker({ execute });
}

async function call(
  origin: string,
  capability: string,
  options: {
    body?: string;
    headers?: string[];
    method?: string;
    path?: string;
  } = {},
) {
  return await new Promise<{ body: unknown; status: number }>((resolve, reject) => {
    const url = new URL(origin);
    const body = options.body ?? (options.method === "GET" ? "" : JSON.stringify(read));
    const headers = options.headers ?? [
      "Host",
      url.host,
      "Authorization",
      `Bearer ${capability}`,
      "Content-Type",
      "application/json",
    ];
    const outgoing = request(
      {
        agent: false,
        headers: [...headers, "Content-Length", String(Buffer.byteLength(body))],
        hostname: url.hostname,
        method: options.method ?? "POST",
        path: options.path ?? "/v1/read",
        port: url.port,
      },
      (response) => {
        const chunks: Buffer[] = [];
        response.on("data", (chunk: Buffer) => chunks.push(chunk));
        response.on("end", () => {
          try {
            resolve({
              body: JSON.parse(Buffer.concat(chunks).toString("utf8")),
              status: response.statusCode ?? 0,
            });
          } catch (error) {
            reject(error);
          }
        });
      },
    );
    outgoing.on("error", reject);
    outgoing.end(body);
  });
}

async function rawHttp(origin: string, data: string, finish = true) {
  const url = new URL(origin);
  return await new Promise<{ response: string; timedOut: boolean }>((resolve, reject) => {
    let response = "";
    let timedOut = false;
    const socket = connect(Number(url.port), "127.0.0.1", () => {
      if (finish) socket.end(data);
      else socket.write(data);
    });
    const timer = setTimeout(() => {
      timedOut = true;
      socket.destroy();
    }, 6_000);
    socket.on("data", (chunk: Buffer) => {
      response += chunk.toString();
    });
    socket.on("error", reject);
    socket.on("close", () => {
      clearTimeout(timer);
      resolve({ response, timedOut });
    });
  });
}

test("broker binds ephemeral IPv4 loopback and serves only validated catalog reads", async (t) => {
  const received: ValidatedLocalApiRequest[] = [];
  const broker = await start(async (value) => {
    received.push(value);
    return [{ id: "account-1" }];
  });
  t.after(() => broker.close());
  assert.match(broker.connection.origin, /^http:\/\/127\.0\.0\.1:[1-9]\d{3,4}$/);
  assert.equal(broker.connection.version, "v1");
  assert.equal(Buffer.from(broker.connection.capability, "base64url").length, 32);
  const capabilities = await call(broker.connection.origin, broker.connection.capability, {
    body: "",
    method: "GET",
    path: "/v1/capabilities",
  });
  assert.deepEqual(capabilities, {
    body: {
      version: "v1",
      tools: api.LOCAL_API_READ_TOOLS,
      resources: api.LOCAL_API_READ_RESOURCES,
    },
    status: 200,
  });
  assert.deepEqual(await call(broker.connection.origin, broker.connection.capability), {
    body: { ok: true, data: [{ id: "account-1" }] },
    status: 200,
  });
  assert.equal(received[0]?.operation, "tool.call");
  assert.deepEqual(received[0]?.operation === "tool.call" && received[0].tool.target, {
    module: "accounting",
    procedure: "listAccounts",
  });
  assert.deepEqual(
    await call(broker.connection.origin, broker.connection.capability, {
      body: JSON.stringify({
        operation: "resource.read",
        uri: "tammy://current/accounts",
        version: "v1",
      }),
    }),
    { body: { ok: true, data: [{ id: "account-1" }] }, status: 200 },
  );
  assert.equal(received[1]?.operation, "resource.read");
});

test("every route requires exactly one canonical Bearer capability", async (t) => {
  let executed = 0;
  const broker = await start(async () => {
    executed++;
    return {};
  });
  t.after(() => broker.close());
  const { origin, capability } = broker.connection;
  const host = new URL(origin).host;
  for (const authorization of [
    [],
    ["Authorization", "Bearer wrong"],
    ["Authorization", `bearer ${capability}`],
    ["Authorization", `Bearer ${capability}`, "Authorization", `Bearer ${capability}`],
    ["Authorization", `Bearer ${capability}, Bearer ${capability}`],
  ]) {
    for (const path of ["/v1/capabilities", "/v1/read", "/unknown"]) {
      const response = await call(origin, capability, {
        headers: ["Host", host, "Content-Type", "application/json", ...authorization],
        method: path === "/v1/capabilities" ? "GET" : "POST",
        path,
      });
      assert.deepEqual(response, {
        status: 401,
        body: { ok: false, error: { code: "UNAUTHORIZED" } },
      });
    }
  }
  assert.equal(executed, 0);
});

test("broker rejects browser origins, non-exact Host authorities, queries and generic RPC", async (t) => {
  let executed = 0;
  const broker = await start(async () => {
    executed++;
    return {};
  });
  t.after(() => broker.close());
  const { origin, capability } = broker.connection;
  const host = new URL(origin).host;
  for (const originValue of ["", "null", "https://malicious.invalid", origin]) {
    assert.deepEqual(
      await call(origin, capability, {
        headers: ["Host", host, "Authorization", `Bearer ${capability}`, "Origin", originValue],
      }),
      { status: 403, body: { ok: false, error: { code: "ORIGIN_NOT_ALLOWED" } } },
    );
  }
  for (const hostname of [
    "localhost",
    host.replace("127.0.0.1", "localhost"),
    "127.0.0.1",
    "evil.invalid",
    `${host}.evil.invalid`,
  ]) {
    assert.deepEqual(
      await call(origin, capability, {
        headers: ["Host", hostname, "Authorization", `Bearer ${capability}`],
      }),
      { status: 400, body: { ok: false, error: { code: "INVALID_HOST" } } },
    );
  }
  for (const path of [
    "/v1/read?tool=x",
    "/rpc",
    "/v1/read/",
    "/v1/%72ead",
    "/v1/capabilities?x=1",
  ]) {
    assert.deepEqual(await call(origin, capability, { path }), {
      status: 404,
      body: { ok: false, error: { code: "UNKNOWN_ROUTE" } },
    });
  }
  assert.deepEqual(await call(origin, capability, { method: "DELETE" }), {
    status: 405,
    body: { ok: false, error: { code: "METHOD_NOT_ALLOWED" } },
  });
  assert.equal(executed, 0);
});

test("broker requires JSON, rejects malformed/write envelopes and bounds request bytes", async (t) => {
  let executed = 0;
  const broker = await start(async () => {
    executed++;
    return {};
  });
  t.after(() => broker.close());
  const { origin, capability } = broker.connection;
  const host = new URL(origin).host;
  for (const contentType of [
    "text/plain",
    "application/x-www-form-urlencoded",
    "application/json, text/plain",
  ]) {
    assert.deepEqual(
      await call(origin, capability, {
        headers: [
          "Host",
          host,
          "Authorization",
          `Bearer ${capability}`,
          "Content-Type",
          contentType,
        ],
      }),
      { status: 415, body: { ok: false, error: { code: "UNSUPPORTED_MEDIA_TYPE" } } },
    );
  }
  for (const [body, code] of [
    ["{malformed-secret", "INVALID_REQUEST"],
    [JSON.stringify({ ...read, name: "tammy.accounting.create_journal" }), "UNKNOWN_TOOL"],
    [JSON.stringify({ ...read, arguments: { organisationId: "other" } }), "INVALID_ARGUMENTS"],
    [" ".repeat(16 * 1024 + 1), "PAYLOAD_TOO_LARGE"],
  ] as const) {
    const response = await call(origin, capability, { body });
    assert.deepEqual(response.body, { ok: false, error: { code } });
    assert.equal(response.status, code === "PAYLOAD_TOO_LARGE" ? 413 : 400);
  }
  assert.equal(executed, 0);
});

test("broker suppresses execution details and returns only whitelisted session errors", async (t) => {
  let message = "credential-secret internal path /private.json";
  const broker = await start(async () => {
    throw new Error(message);
  });
  t.after(() => broker.close());
  for (const code of ["LOCAL_READ_FAILED", "LOCAL_SESSION_REQUIRED", "LOCAL_PAIRING_REVOKED"]) {
    assert.deepEqual((await call(broker.connection.origin, broker.connection.capability)).body, {
      ok: false,
      error: { code },
    });
    message = code === "LOCAL_READ_FAILED" ? "LOCAL_SESSION_REQUIRED" : "LOCAL_PAIRING_REVOKED";
  }
});

test("broker rejects non-JSON and oversized results without reflecting their values", async (t) => {
  let data: unknown = "secret".repeat(200_000);
  const broker = await start(async () => data);
  t.after(() => broker.close());
  assert.deepEqual(await call(broker.connection.origin, broker.connection.capability), {
    status: 502,
    body: { ok: false, error: { code: "RESPONSE_TOO_LARGE" } },
  });
  for (const invalid of [
    undefined,
    1n,
    () => "private",
    Symbol("private"),
    { toJSON: () => undefined },
    { cycle: null },
  ]) {
    data = invalid;
    if (typeof data === "object" && data !== null && "cycle" in data)
      (data as { cycle: unknown }).cycle = data;
    assert.deepEqual((await call(broker.connection.origin, broker.connection.capability)).body, {
      ok: false,
      error: { code: "LOCAL_READ_FAILED" },
    });
  }
});

test("broker times out a stalled read after five seconds", async (t) => {
  const broker = await start(async () => await new Promise(() => {}));
  t.after(() => broker.close());
  const started = performance.now();
  assert.deepEqual(await call(broker.connection.origin, broker.connection.capability), {
    status: 504,
    body: { ok: false, error: { code: "REQUEST_TIMEOUT" } },
  });
  assert.ok(performance.now() - started < 6_000);
});

test("closing the broker revokes its connection and is idempotent", async () => {
  const broker = await start(async () => ({}));
  await broker.close();
  await broker.close();
  await assert.rejects(call(broker.connection.origin, broker.connection.capability));
});

test("broker bounds chunked bodies and rejects missing or duplicate Host headers", async (t) => {
  let executions = 0;
  const broker = await start(async () => {
    executions++;
    return {};
  });
  t.after(() => broker.close());
  const { origin, capability } = broker.connection;
  const host = new URL(origin).host;
  const chunk = " ".repeat(16 * 1024 + 1);
  const oversized = await rawHttp(
    origin,
    `POST /v1/read HTTP/1.1\r\nHost: ${host}\r\nAuthorization: Bearer ${capability}\r\nContent-Type: application/json\r\nTransfer-Encoding: chunked\r\n\r\n${chunk.length.toString(16)}\r\n${chunk}\r\n0\r\n\r\n`,
  );
  assert.equal(oversized.timedOut, false);
  assert.match(oversized.response, /^HTTP\/1\.1 413 /);
  assert.match(oversized.response, /"code":"PAYLOAD_TOO_LARGE"/);
  for (const hosts of ["", `Host: ${host}\r\nHost: ${host}\r\n`]) {
    const response = await rawHttp(
      origin,
      `POST /v1/read HTTP/1.1\r\n${hosts}Authorization: Bearer ${capability}\r\nContent-Type: application/json\r\nContent-Length: 0\r\n\r\n`,
    );
    assert.match(response.response, /^HTTP\/1\.1 400 /);
  }
  assert.equal(executions, 0);
});

test("broker enforces five-second deadlines on unfinished headers and request bodies", async (t) => {
  let executions = 0;
  const broker = await start(async () => {
    executions++;
    return {};
  });
  t.after(() => broker.close());
  const { origin, capability } = broker.connection;
  const host = new URL(origin).host;
  const started = performance.now();
  const [headers, body] = await Promise.all([
    rawHttp(origin, "POST /v1/read HTTP/1.1\r\n", false),
    rawHttp(
      origin,
      `POST /v1/read HTTP/1.1\r\nHost: ${host}\r\nAuthorization: Bearer ${capability}\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{`,
      false,
    ),
  ]);
  assert.equal(
    headers.timedOut,
    false,
    "incomplete headers must not remain open beyond the deadline",
  );
  assert.equal(body.timedOut, false, "incomplete bodies must not remain open beyond the deadline");
  assert.match(headers.response, /^HTTP\/1\.1 400 /);
  assert.match(body.response, /"code":"REQUEST_TIMEOUT"/);
  assert.ok(performance.now() - started < 6_000);
  assert.equal(executions, 0);
});
