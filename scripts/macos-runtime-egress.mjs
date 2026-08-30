import { execFile as nodeExecFile } from "node:child_process";
import { access } from "node:fs/promises";
import { createServer } from "node:net";
import path from "node:path";
import { promisify } from "node:util";

const execFile = promisify(nodeExecFile);
const SHA256 = /^[0-9a-f]{64}$/u;
const SHA40 = /^[0-9a-f]{40}$/u;
const VERSION = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u;
const BUILD = /^[1-9]\d*$/u;
const UTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/u;
const PRIVACY_URL = "https://tammy-accounting.castlemilk.chatgpt.site/privacy";
const SUPPORT_URL = "https://tammy-accounting.castlemilk.chatgpt.site/support";

export const MACOS_RUNTIME_EGRESS_SANDBOX_PROFILE = [
  "(version 1)",
  "(allow default)",
  "(deny network* (with report))",
  '(allow network-inbound (local ip "localhost:*"))',
  '(allow network-outbound (remote ip "localhost:*"))',
].join("\n");

function fail(code = "MACOS_RUNTIME_EGRESS_EVIDENCE_INVALID") {
  throw new Error(code);
}

function record(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function exactKeys(value, keys) {
  return (
    record(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}

function sortedUniqueStrings(value, { maximum = 256, pattern } = {}) {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.length <= maximum &&
    new Set(value).size === value.length &&
    value.every(
      (item, index) =>
        typeof item === "string" &&
        item.length > 0 &&
        (!pattern || pattern.test(item)) &&
        (index === 0 || Buffer.compare(Buffer.from(value[index - 1]), Buffer.from(item)) < 0),
    )
  );
}

export async function probeMacOSSandboxExec() {
  let server;
  try {
    await access("/usr/bin/sandbox-exec");
    server = createServer((socket) => socket.end("ok"));
    await new Promise((resolve, reject) => {
      server.once("error", reject);
      server.listen(0, "127.0.0.1", resolve);
    });
    const address = server.address();
    if (!record(address) || typeof address.port !== "number") {
      fail("MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE");
    }
    const probeScript = `
const { spawnSync } = require("node:child_process");
const dns = require("node:dns");
const net = require("node:net");
const denied = new Set(["EACCES", "EPERM"]);
const connect = (host, port) => new Promise((resolve) => {
  const socket = net.connect({ host, port });
  const done = (value) => { socket.destroy(); resolve(value); };
  socket.setTimeout(1500, () => done("TIMEOUT"));
  socket.once("connect", () => done("CONNECTED"));
  socket.once("error", (error) => done(error.code || "ERROR"));
});
const lookup = () => new Promise((resolve) => {
  dns.lookup("tammy-egress-probe.invalid", (error) => resolve(error?.code || "RESOLVED"));
});
(async () => {
  const loopback = await connect("127.0.0.1", Number(process.argv[1]));
  const nonLoopback = await connect("203.0.113.1", 9);
  const dnsResult = await lookup();
  const child = spawnSync(process.execPath, ["-e", ${JSON.stringify(
    'const net=require("node:net");const s=net.connect({host:"203.0.113.1",port:9});s.setTimeout(1500,()=>{s.destroy();process.exit(2)});s.once("connect",()=>process.exit(3));s.once("error",e=>process.exit(new Set(["EACCES","EPERM"]).has(e.code)?0:4));',
  )}], { timeout: 3000 });
  process.stdout.write(JSON.stringify({
    childInheritanceDenied: child.status === 0,
    dnsDenied: dnsResult !== "RESOLVED",
    loopbackAllowed: loopback === "CONNECTED",
    nonLoopbackDenied: denied.has(nonLoopback),
  }));
})().catch(() => process.exit(5));
`;
    const { stdout } = await execFile(
      "/usr/bin/sandbox-exec",
      [
        "-p",
        MACOS_RUNTIME_EGRESS_SANDBOX_PROFILE,
        process.execPath,
        "-e",
        probeScript,
        String(address.port),
      ],
      {
        encoding: "utf8",
        env: {
          HOME: process.env.HOME ?? "/var/empty",
          PATH: "/usr/bin:/bin",
          TMPDIR: process.env.TMPDIR ?? "/private/tmp",
        },
        killSignal: "SIGKILL",
        maxBuffer: 16 * 1024,
        timeout: 10_000,
      },
    );
    let result;
    try {
      result = JSON.parse(stdout);
    } catch {
      fail("MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE");
    }
    if (
      !exactKeys(result, [
        "childInheritanceDenied",
        "dnsDenied",
        "loopbackAllowed",
        "nonLoopbackDenied",
      ])
    ) {
      fail("MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE");
    }
    return {
      auditSamples: [
        result.childInheritanceDenied,
        result.dnsDenied,
        result.nonLoopbackDenied,
      ].filter(Boolean).length,
      ...result,
    };
  } catch {
    fail("MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE");
  } finally {
    if (server) {
      await new Promise((resolve) => server.close(resolve)).catch(() => undefined);
    }
  }
}

export async function detectMacOSEgressEnforcer({
  platform = process.platform,
  probe = probeMacOSSandboxExec,
} = {}) {
  if (platform !== "darwin" || typeof probe !== "function") {
    fail("MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE");
  }
  let preflight;
  try {
    preflight = await probe();
  } catch {
    fail("MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE");
  }
  if (
    !exactKeys(preflight, [
      "auditSamples",
      "childInheritanceDenied",
      "dnsDenied",
      "loopbackAllowed",
      "nonLoopbackDenied",
    ]) ||
    !Number.isSafeInteger(preflight.auditSamples) ||
    preflight.auditSamples <= 0 ||
    preflight.childInheritanceDenied !== true ||
    preflight.dnsDenied !== true ||
    preflight.loopbackAllowed !== true ||
    preflight.nonLoopbackDenied !== true
  ) {
    fail("MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE");
  }
  return { kind: "sandbox-exec", preflight };
}

function parseProcessId(value) {
  if (!/^[1-9]\d*$/u.test(value)) fail("MACOS_RUNTIME_PROCESS_EVIDENCE_INVALID");
  const number = Number(value);
  if (!Number.isSafeInteger(number)) fail("MACOS_RUNTIME_PROCESS_EVIDENCE_INVALID");
  return number;
}

export function parseMacOSProcessTreeSnapshot(stdout, rootProcessId) {
  if (
    typeof stdout !== "string" ||
    stdout.length === 0 ||
    stdout.length > 1024 * 1024 ||
    !Number.isSafeInteger(rootProcessId) ||
    rootProcessId <= 1
  ) {
    fail("MACOS_RUNTIME_PROCESS_EVIDENCE_INVALID");
  }
  const rows = stdout
    .split(/\r?\n/u)
    .filter(Boolean)
    .map((line) => {
      const fields = line.split("\t");
      if (fields.length !== 3) fail("MACOS_RUNTIME_PROCESS_EVIDENCE_INVALID");
      const [pidText, parentText, executablePath] = fields;
      if (
        !executablePath ||
        !path.posix.isAbsolute(executablePath) ||
        path.posix.normalize(executablePath) !== executablePath
      ) {
        fail("MACOS_RUNTIME_PROCESS_EVIDENCE_INVALID");
      }
      return {
        executablePath,
        parentProcessId: parseProcessId(parentText),
        processId: parseProcessId(pidText),
      };
    });
  if (new Set(rows.map(({ processId }) => processId)).size !== rows.length) {
    fail("MACOS_RUNTIME_PROCESS_EVIDENCE_INVALID");
  }
  const byProcess = new Map(rows.map((row) => [row.processId, row]));
  const root = byProcess.get(rootProcessId);
  if (!root?.executablePath.includes(".app/Contents/")) {
    fail("MACOS_RUNTIME_PROCESS_EVIDENCE_INVALID");
  }
  const appRoot = `${root.executablePath.slice(0, root.executablePath.indexOf(".app/Contents/") + 4)}${path.posix.sep}`;
  for (const row of rows) {
    if (!row.executablePath.startsWith(appRoot)) {
      fail("MACOS_RUNTIME_PROCESS_EVIDENCE_INVALID");
    }
    if (row.processId === rootProcessId) continue;
    const visited = new Set([row.processId]);
    let current = row;
    while (current.processId !== rootProcessId) {
      const parent = byProcess.get(current.parentProcessId);
      if (!parent || visited.has(parent.processId)) {
        fail("MACOS_RUNTIME_PROCESS_EVIDENCE_INVALID");
      }
      visited.add(parent.processId);
      current = parent;
    }
  }
  return rows.sort((left, right) => left.processId - right.processId);
}

export function createExternalHandoffEvent({ allowedUrls, occurredAt, url, userGesture }) {
  if (
    !Array.isArray(allowedUrls) ||
    allowedUrls.length !== 2 ||
    !allowedUrls.includes(PRIVACY_URL) ||
    !allowedUrls.includes(SUPPORT_URL) ||
    !allowedUrls.includes(url) ||
    !UTC.test(occurredAt) ||
    !Number.isFinite(Date.parse(occurredAt)) ||
    userGesture !== true
  ) {
    fail("MACOS_EXTERNAL_HANDOFF_INVALID");
  }
  return { occurredAt, url, userGesture: true };
}

export function validateMacOSRuntimeEgressEvidence(value) {
  if (
    !exactKeys(value, [
      "appSha256",
      "auditSamples",
      "buildNumber",
      "coreSha256",
      "deniedDnsAttempts",
      "deniedNonLoopbackAttempts",
      "handoffs",
      "helperSha256",
      "listeners",
      "marketingVersion",
      "observationSamples",
      "observedNonLoopbackConnections",
      "processPaths",
      "productSourceCommit",
      "productSourceTree",
      "schemaVersion",
    ]) ||
    value.schemaVersion !== 1 ||
    !SHA256.test(value.appSha256) ||
    !SHA256.test(value.coreSha256) ||
    !SHA256.test(value.helperSha256) ||
    !SHA40.test(value.productSourceCommit) ||
    !SHA40.test(value.productSourceTree) ||
    !VERSION.test(value.marketingVersion) ||
    !BUILD.test(value.buildNumber) ||
    !Number.isSafeInteger(value.auditSamples) ||
    value.auditSamples <= 0 ||
    !Number.isSafeInteger(value.observationSamples) ||
    value.observationSamples <= 0 ||
    value.deniedDnsAttempts !== 0 ||
    value.deniedNonLoopbackAttempts !== 0 ||
    value.observedNonLoopbackConnections !== 0 ||
    !sortedUniqueStrings(value.processPaths, { maximum: 64 }) ||
    !Array.isArray(value.handoffs) ||
    value.handoffs.length !== 2 ||
    !Array.isArray(value.listeners) ||
    value.listeners.length !== 1
  ) {
    fail();
  }
  const handoffUrls = value.handoffs.map((handoff) => {
    if (!exactKeys(handoff, ["occurredAt", "url", "userGesture"])) fail();
    return createExternalHandoffEvent({
      allowedUrls: [PRIVACY_URL, SUPPORT_URL],
      occurredAt: handoff.occurredAt,
      url: handoff.url,
      userGesture: handoff.userGesture,
    }).url;
  });
  if (
    handoffUrls[0] !== PRIVACY_URL ||
    handoffUrls[1] !== SUPPORT_URL ||
    Date.parse(value.handoffs[0].occurredAt) >= Date.parse(value.handoffs[1].occurredAt)
  ) {
    fail();
  }
  const listener = value.listeners[0];
  if (
    !exactKeys(listener, ["address", "owner", "port"]) ||
    listener.address !== "127.0.0.1" ||
    listener.owner !== "authenticated-core" ||
    !Number.isSafeInteger(listener.port) ||
    listener.port < 1024 ||
    listener.port > 65535
  ) {
    fail();
  }
  return value;
}
