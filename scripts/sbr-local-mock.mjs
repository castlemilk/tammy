import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import { createServer } from "node:http";
import { pathToFileURL } from "node:url";

const MODE = "LOCAL_SIMULATOR_NOT_FOR_ATO_LODGMENT";
const CASES = [
  "ACCEPTED",
  "NOT_STARTED",
  "MAYBE_SENT",
  "MALFORMED_RESPONSE",
  "HELPER_DEATH",
  "TIMEOUT",
];
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const MAX_BODY = 4096;
const CREDENTIAL_HEADERS = [
  "authorization",
  "proxy-authorization",
  "cookie",
  "x-api-key",
  "x-machine-credential",
  "x-product-id",
];

/** @typedef {{case: string, helper_outcome: number, simulator_state: number, redacted_result: number, stable_error_code: string, fatal: boolean, malformed_payload_hex: string}} MockCase */
/** @typedef {{schema_version: number, contract: string, mode: string, fixture_id: string, service_id: string, fixture_sha256: string, cases: MockCase[]}} MockFixtures */
/** @param {MockFixtures} value @param {Uint8Array} canonicalBytes */
export function validateFixtures(value, canonicalBytes) {
  if (
    Object.keys(value ?? {})
      .sort()
      .join(",") !== "cases,contract,fixture_id,fixture_sha256,mode,schema_version,service_id" ||
    value?.schema_version !== 1 ||
    value.contract !== "TAMMY_SBR_READINESS_SIMULATOR_V1" ||
    value.mode !== MODE ||
    value.fixture_id !== "SIM-SBR-READINESS-V1" ||
    value.service_id !== "SIM.READINESS.0001" ||
    value.fixture_sha256 !== createHash("sha256").update(canonicalBytes).digest("hex") ||
    !Array.isArray(value.cases) ||
    value.cases.length !== CASES.length ||
    value.cases.some(
      (row, index) =>
        Object.keys(row).sort().join(",") !==
          "case,fatal,helper_outcome,malformed_payload_hex,redacted_result,simulator_state,stable_error_code" ||
        row.case !== CASES[index] ||
        ![0, 1, 2].includes(row.helper_outcome) ||
        !Number.isInteger(row.simulator_state) ||
        row.simulator_state < 0 ||
        row.simulator_state > 5 ||
        !Number.isInteger(row.redacted_result) ||
        row.redacted_result < 0 ||
        row.redacted_result > 8 ||
        typeof row.stable_error_code !== "string" ||
        typeof row.fatal !== "boolean" ||
        typeof row.malformed_payload_hex !== "string",
    )
  )
    throw new Error("LOCAL_MOCK_FIXTURE_INVALID");
  return value;
}

/** @param {import("node:http").ServerResponse} response @param {number} status @param {Record<string, unknown>} body */
function reply(response, status, body) {
  response.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "X-Content-Type-Options": "nosniff",
    Connection: "close",
  });
  response.end(`${JSON.stringify({ mode: MODE, official_ato_response: false, ...body })}\n`);
}

/** @param {import("node:http").IncomingMessage} request @returns {Promise<string>} */
function readBody(request) {
  return new Promise((resolve, reject) => {
    let bytes = 0;
    let failed = false;
    /** @type {Buffer[]} */
    let chunks = [];
    request.on("data", (chunk) => {
      if (failed) return;
      bytes += chunk.length;
      if (bytes > MAX_BODY) {
        failed = true;
        chunks = [];
        reject(new Error("BODY_LIMIT"));
      } else chunks.push(chunk);
    });
    request.once("end", () => {
      if (!failed) resolve(Buffer.concat(chunks).toString("utf8"));
    });
    request.once("error", () => reject(new Error("BODY_READ")));
    request.once("aborted", () => reject(new Error("BODY_READ")));
  });
}

export async function createReadinessMockServer() {
  const [bytes, canonical] = await Promise.all([
    readFile(new URL("../test/fixtures/sbr/local-http-cases-v1.json", import.meta.url)),
    readFile(new URL("../test/fixtures/sbr/simulator-readiness-v1.json", import.meta.url)),
  ]);
  const fixture = validateFixtures(JSON.parse(bytes.toString("utf8")), canonical);
  const server = createServer(
    {
      requestTimeout: 5000,
      connectionsCheckingInterval: 1000,
      headersTimeout: 5000,
      maxHeaderSize: 8192,
      keepAliveTimeout: 1000,
    },
    async (request, response) => {
      const host = request.headers.host ?? "";
      if (
        !/^127\.0\.0\.1(?::[0-9]{1,5})?$/.test(host) ||
        request.socket.remoteAddress !== "127.0.0.1" ||
        request.headers.origin !== undefined ||
        CREDENTIAL_HEADERS.some((key) => request.headers[key] !== undefined)
      ) {
        request.resume();
        reply(response, 403, { code: "LOCAL_MOCK_REQUEST_FORBIDDEN" });
        return;
      }
      if (request.url === "/healthz" && request.method === "GET") {
        request.resume();
        reply(response, 200, {
          contract: fixture.contract,
          fixture_id: fixture.fixture_id,
          service_id: fixture.service_id,
          fixture_sha256: fixture.fixture_sha256,
        });
        return;
      }
      if (request.url !== "/v1/sbr/readiness/select") {
        request.resume();
        reply(response, 404, { code: "LOCAL_MOCK_ROUTE_UNSUPPORTED" });
        return;
      }
      if (request.method !== "POST") {
        request.resume();
        reply(response, 405, { code: "LOCAL_MOCK_METHOD_UNSUPPORTED" });
        return;
      }
      if (
        !/^application\/json(?:;\s*charset=utf-8)?$/i.test(request.headers["content-type"] ?? "")
      ) {
        request.resume();
        reply(response, 415, { code: "LOCAL_MOCK_JSON_REQUIRED" });
        return;
      }
      if (Number(request.headers["content-length"] ?? 0) > MAX_BODY) {
        request.resume();
        reply(response, 413, { code: "LOCAL_MOCK_BODY_LIMIT" });
        return;
      }
      try {
        const body = JSON.parse(await readBody(request));
        if (
          !body ||
          typeof body !== "object" ||
          Array.isArray(body) ||
          Object.keys(body).sort().join(",") !== "case,fixture_id,request_id" ||
          typeof body.request_id !== "string" ||
          !UUID.test(body.request_id) ||
          body.fixture_id !== fixture.fixture_id
        ) {
          reply(response, 400, { code: "LOCAL_MOCK_REQUEST_INVALID" });
          return;
        }
        const selected = fixture.cases.find((row) => row.case === body.case);
        if (!selected) {
          reply(response, 400, { code: "SBR_SIMULATOR_CASE_INVALID" });
          return;
        }
        reply(response, 200, {
          contract: fixture.contract,
          fixture_id: fixture.fixture_id,
          service_id: fixture.service_id,
          fixture_sha256: fixture.fixture_sha256,
          request_id: body.request_id,
          replay_policy: "STATELESS_FIXTURE_SELECTION",
          simulator_outcome: selected.case,
          ...selected,
        });
      } catch (error) {
        const bodyLimit = error instanceof Error && error.message === "BODY_LIMIT";
        reply(response, bodyLimit ? 413 : 400, {
          code: bodyLimit ? "LOCAL_MOCK_BODY_LIMIT" : "LOCAL_MOCK_REQUEST_INVALID",
        });
      }
    },
  );
  server.maxConnections = 16;
  server.maxRequestsPerSocket = 1;
  return server;
}

/** @param {string[]} args */
export function parsePort(args) {
  if (args.length === 0) return 0;
  if (
    args.length !== 2 ||
    args[0] !== "--port" ||
    !/^[0-9]{1,5}$/.test(args[1]) ||
    Number(args[1]) > 65535
  )
    throw new Error("LOCAL_MOCK_ARGUMENTS_INVALID");
  return Number(args[1]);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const port = parsePort(process.argv.slice(2));
    const server = await createReadinessMockServer();
    server.listen(port, "127.0.0.1", () => {
      const address = server.address();
      if (address && typeof address === "object")
        console.log(`${MODE} http://127.0.0.1:${address.port}`);
    });
    server.on("error", () => {
      console.error("LOCAL_MOCK_LISTEN_FAILED");
      process.exitCode = 1;
    });
    for (const signal of ["SIGINT", "SIGTERM"])
      process.once(signal, () => {
        server.close();
        server.closeAllConnections();
      });
  } catch {
    console.error("LOCAL_MOCK_START_FAILED");
    process.exitCode = 1;
  }
}
