import assert from "node:assert/strict";
import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { request as httpRequest } from "node:http";
import test from "node:test";
import { createReadinessMockServer, parsePort, validateFixtures } from "./sbr-local-mock.mjs";

const requestID = "018f0000-0000-7000-8000-000000000088";
const body = (scenario = "ACCEPTED") => ({
  request_id: requestID,
  fixture_id: "SIM-SBR-READINESS-V1",
  case: scenario,
});
async function withServer(t) {
  const server = await createReadinessMockServer();
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  t.after(
    () =>
      new Promise((resolve) => {
        server.close(resolve);
        server.closeAllConnections();
      }),
  );
  const root = `http://127.0.0.1:${server.address().port}`;
  return {
    server,
    root,
    select: (value, headers = {}) =>
      fetch(`${root}/v1/sbr/readiness/select`, {
        method: "POST",
        headers: { "content-type": "application/json", ...headers },
        body: JSON.stringify(value),
      }),
  };
}

test("serves actual loopback HTTP and deterministic adapter-conformant selection for every closed case", async (t) => {
  const { root, select } = await withServer(t);
  const health = await fetch(`${root}/healthz`);
  const healthBody = await health.json();
  assert.equal(healthBody.mode, "LOCAL_SIMULATOR_NOT_FOR_ATO_LODGMENT");
  assert.equal(healthBody.official_ato_response, false);
  assert.equal(healthBody.service_id, "SIM.READINESS.0001");
  for (const scenario of [
    "ACCEPTED",
    "NOT_STARTED",
    "MAYBE_SENT",
    "MALFORMED_RESPONSE",
    "HELPER_DEATH",
    "TIMEOUT",
  ]) {
    const response = await select(body(scenario));
    assert.equal(response.status, 200);
    const value = await response.json();
    assert.equal(value.simulator_outcome, scenario);
    assert.equal(value.request_id, requestID);
    assert.equal(value.official_ato_response, false);
    assert.equal(value.replay_policy, "STATELESS_FIXTURE_SELECTION");
    assert.equal(response.headers.get("cache-control"), "no-store");
  }
  const first = await (await select(body())).text();
  assert.equal(await (await select(body())).text(), first);
  // The existing adapter is stateless: changing case is selection, not a resend.
  assert.equal((await (await select(body("MAYBE_SENT"))).json()).simulator_outcome, "MAYBE_SENT");
});

test("rejects missing/invalid fields and unsupported pending/rejected business outcomes", async (t) => {
  const { select } = await withServer(t);
  for (const value of [
    {},
    [],
    { ...body(), request_id: "not-a-uuid" },
    { ...body(), fixture_id: "real-return" },
    body("PENDING"),
    body("REJECTED"),
    body("UNKNOWN"),
    { ...body(), endpoint_url: "https://example.invalid" },
    { ...body(), credential: "synthetic-secret-never-echo" },
    { ...body(), environment: "PRODUCTION" },
  ]) {
    const response = await select(value);
    assert.equal(response.status, 400);
    const text = await response.text();
    assert.ok(!text.includes("synthetic-secret-never-echo"));
    assert.ok(!text.includes("example.invalid"));
    assert.equal(JSON.parse(text).official_ato_response, false);
  }
});

test("rejects credentials, browser origins, non-loopback Host and unsupported routes", async (t) => {
  const { root, select } = await withServer(t);
  for (const headers of [
    { authorization: "Bearer synthetic-never-used" },
    { cookie: "synthetic-never-used" },
    { "x-api-key": "synthetic-never-used" },
    { origin: "https://example.invalid" },
    { origin: "null" },
  ]) {
    const response = await select(body(), headers);
    assert.equal(response.status, 403);
    assert.ok(!(await response.text()).includes("synthetic-never-used"));
  }
  const forbiddenHost = await new Promise((resolve, reject) => {
    const request = httpRequest(
      `${root}/v1/sbr/readiness/select`,
      { method: "POST", headers: { host: "example.invalid", "content-type": "application/json" } },
      (response) => {
        response.resume();
        resolve(response.statusCode);
      },
    );
    request.on("error", reject);
    request.end(JSON.stringify(body()));
  });
  assert.equal(forbiddenHost, 403);
  for (const route of [
    "/submit",
    "/lodge",
    "/evte",
    "/production",
    "/v1/sbr/readiness/select?endpoint=production",
  ])
    assert.equal((await fetch(`${root}${route}`)).status, 404);
  assert.equal((await fetch(`${root}/v1/sbr/readiness/select`)).status, 405);
  assert.equal((await select(body(), { "content-type": "text/plain" })).status, 415);
});

test("bounds declared and chunked bodies and validates launch arguments and fixture bytes", {
  timeout: 5000,
}, async (t) => {
  const { root } = await withServer(t);
  const response = await fetch(`${root}/v1/sbr/readiness/select`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: "x".repeat(4097),
  });
  assert.equal(response.status, 413);
  const chunkedStatus = await new Promise((resolve, reject) => {
    const request = httpRequest(
      `${root}/v1/sbr/readiness/select`,
      { method: "POST", headers: { "content-type": "application/json" } },
      (result) => {
        result.resume();
        resolve(result.statusCode);
      },
    );
    request.on("error", reject);
    request.write("x".repeat(2048));
    request.write("x".repeat(2048));
    request.end("x");
  });
  assert.equal(chunkedStatus, 413);
  assert.equal(
    (
      await fetch(`${root}/v1/sbr/readiness/select`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: "{",
      })
    ).status,
    400,
  );
  assert.equal(parsePort([]), 0);
  assert.equal(parsePort(["--port", "8765"]), 8765);
  for (const args of [
    ["--host", "0.0.0.0"],
    ["--upstream", "production"],
    ["--port", "65536"],
    ["--port", "-1"],
  ])
    assert.throws(() => parsePort(args));
  const canonical = await readFile(
    new URL("../test/fixtures/sbr/simulator-readiness-v1.json", import.meta.url),
  );
  const fixture = JSON.parse(
    await readFile(new URL("../test/fixtures/sbr/local-http-cases-v1.json", import.meta.url)),
  );
  assert.equal(validateFixtures(fixture, canonical), fixture);
  assert.throws(() => validateFixtures(fixture, Buffer.from("changed")));
  assert.throws(() => validateFixtures({ ...fixture, mode: "PRODUCTION" }, canonical));
});

test("mock runtime imports no network client, process launcher or credential component", async () => {
  const source = await readFile(new URL("./sbr-local-mock.mjs", import.meta.url), "utf8");
  const imports = [...source.matchAll(/from\s+"([^"]+)"/g)].map((match) => match[1]).sort();
  assert.deepEqual(imports, ["node:crypto", "node:fs/promises", "node:http", "node:url"]);
  assert.ok(source.includes('import { createServer } from "node:http"'));
  const runtime = source.replace(/\/\*[\s\S]*?\*\//g, "");
  for (const pattern of [
    /\bfetch\s*\(/,
    /\b(?:eval|Function)\s*\(/,
    /\bimport\s*\(/,
    /process\.env/,
    /node:(?:https|net|tls|child_process)/,
    /\.request\s*\(/,
    /\.connect\s*\(/,
  ])
    assert.ok(!pattern.test(runtime), String(pattern));
});
