import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROUTES = ["/", "/privacy", "/support"];
const MAX_RESPONSE_BYTES = 1_000_000;
const MAX_PREVIEW_OUTPUT_BYTES = 65_536;
const PREVIEW_TIMEOUT_MS = 30_000;
const MAX_REDIRECT_HOPS = 5;
const REQUEST_TIMEOUT_MS = 15_000;
const DEPLOYMENT_KEYS = [
  "access",
  "deployedAt",
  "deploymentId",
  "origin",
  "policySha256",
  "projectId",
  "provider",
  "routes",
  "schemaVersion",
  "sourceCommit",
  "versionId",
];
const ROUTE_KEYS = ["check", "contentType", "path", "status"];
const SAFE_IDENTIFIER = /^[A-Za-z0-9][A-Za-z0-9._~-]{0,255}$/;
const TRUSTED_PUBLIC_ORIGIN = "https://tammy-accounting.castlemilk.chatgpt.site";

function validateOrigin(origin, mode) {
  let parsed;
  try {
    parsed = new URL(origin);
  } catch {
    throw new Error("Site origin must be an absolute URL");
  }
  if (
    parsed.username ||
    parsed.password ||
    parsed.search ||
    parsed.hash ||
    parsed.pathname !== "/"
  ) {
    throw new Error("Site origin must not include credentials, a path, query, or fragment");
  }
  if (mode === "deployed" && parsed.protocol !== "https:") {
    throw new Error("Deployed site verification requires HTTPS");
  }
  if (mode === "preview" && !["127.0.0.1", "localhost", "[::1]"].includes(parsed.hostname)) {
    throw new Error("Preview verification requires a loopback origin");
  }
  return parsed.origin;
}

async function readBoundedHtml(response, signal) {
  const declaredLength = Number(response.headers.get("content-length"));
  if (Number.isFinite(declaredLength) && declaredLength > MAX_RESPONSE_BYTES) {
    throw new Error("Site response exceeds the maximum response size in bytes");
  }
  if (!response.body) {
    return "";
  }

  const reader = response.body.getReader();
  const cancelOnAbort = () => {
    void reader.cancel(signal.reason).catch(() => {});
  };
  signal.addEventListener("abort", cancelOnAbort, { once: true });
  const chunks = [];
  let byteLength = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      byteLength += value.byteLength;
      if (byteLength > MAX_RESPONSE_BYTES) {
        await reader.cancel();
        throw new Error("Site response exceeds the maximum response size in bytes");
      }
      chunks.push(value);
    }
  } finally {
    signal.removeEventListener("abort", cancelOnAbort);
    reader.releaseLock();
  }
  const bytes = new Uint8Array(byteLength);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder().decode(bytes);
}

function requireText(html, pattern, label, route) {
  if (!pattern.test(html)) {
    throw new Error(`${route} is missing required ${label}`);
  }
}

function visibleText(html) {
  return html
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/<script\b[\s\S]*?<\/script>/gi, " ")
    .replace(/<style\b[\s\S]*?<\/style>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replaceAll("&nbsp;", " ")
    .replaceAll("&amp;", "&")
    .replace(/\s+/g, " ")
    .trim();
}

function validatePage(pathname, html) {
  for (const route of ROUTES) {
    const escapedRoute = route.replaceAll("/", "\\/");
    if (!new RegExp(`href=["']${escapedRoute}["']`, "i").test(html)) {
      throw new Error(`${pathname} is missing canonical navigation to ${route}`);
    }
  }

  const forbidden = [
    /\bTODO\b/i,
    /lorem ipsum/i,
    /TestFlight/i,
    /lodges? with the ATO/i,
    /submit(?:s|ted|ting)? BAS to the ATO/i,
  ];
  if (forbidden.some((pattern) => pattern.test(html))) {
    throw new Error(`${pathname} contains forbidden placeholder or release claim`);
  }

  const text = visibleText(html);

  if (pathname === "/") {
    requireText(text, /Tammy Accounting/i, "app name", pathname);
    requireText(text, /Gamma Systems Pty Ltd/i, "publisher", pathname);
    requireText(text, /macOS 14/i, "minimum macOS version", pathname);
    requireText(text, /arm64/i, "architecture", pathname);
    requireText(text, /preparation-only/i, "preparation-only boundary", pathname);
    requireText(text, /not lodged/i, "not-lodged boundary", pathname);
    const socialImage = `${TRUSTED_PUBLIC_ORIGIN}/og.png`;
    for (const [label, fragment] of [
      ["Open Graph image metadata", `property="og:image" content="${socialImage}"`],
      ["X image metadata", `name="twitter:image" content="${socialImage}"`],
      ["X card metadata", 'name="twitter:card" content="summary_large_image"'],
    ]) {
      if (!html.includes(fragment)) {
        throw new Error(`/ is missing required social metadata: ${label}`);
      }
    }
  } else if (pathname === "/privacy") {
    requireText(text, /Privacy policy/i, "privacy heading", pathname);
    requireText(text, /30 August 2026/i, "policy effective date", pathname);
    requireText(text, /does not transmit your accounting records/i, "data boundary", pathname);
    if (!html.includes(`href="${TRUSTED_PUBLIC_ORIGIN}/support"`)) {
      throw new Error("/privacy is missing required canonical support link");
    }
  } else {
    requireText(text, /Support/i, "support heading", pathname);
    requireText(html, /mailto:ben\.ebsworth@gmail\.com/i, "support email", pathname);
    requireText(text, /version 0\.1\.0/i, "app version", pathname);
  }
}

async function fetchSameOrigin(requestedUrl, canonicalOrigin, fetchImpl, signal) {
  let currentUrl = requestedUrl;
  for (let hop = 0; hop <= MAX_REDIRECT_HOPS; hop += 1) {
    const response = await fetchImpl(currentUrl, { redirect: "manual", signal });
    if (response.status >= 300 && response.status < 400 && response.status !== 304) {
      const location = response.headers.get("location");
      if (!location) throw new Error("Redirect response is missing its Location header");
      const nextUrl = new URL(location, currentUrl);
      if (nextUrl.origin !== canonicalOrigin) {
        throw new Error("Redirect leaves the expected origin");
      }
      if (hop === MAX_REDIRECT_HOPS) {
        throw new Error("Redirect chain exceeds the maximum hop count");
      }
      currentUrl = nextUrl.href;
      continue;
    }

    const finalUrl = new URL(response.url || currentUrl);
    if (finalUrl.origin !== canonicalOrigin) {
      throw new Error("Response leaves the expected origin");
    }
    return response;
  }
  throw new Error("Redirect chain exceeds the maximum hop count");
}

async function withDeadline(label, timeoutMs, operation) {
  const controller = new AbortController();
  let timer;
  const timeout = new Promise((_, reject) => {
    timer = setTimeout(() => {
      const error = new Error(`${label} timed out after ${timeoutMs}ms`);
      controller.abort(error);
      reject(error);
    }, timeoutMs);
  });
  try {
    return await Promise.race([operation(controller.signal), timeout]);
  } finally {
    clearTimeout(timer);
  }
}

export async function checkPublicSite({
  origin,
  mode = "deployed",
  fetchImpl = fetch,
  timeoutMs = REQUEST_TIMEOUT_MS,
}) {
  if (!Number.isInteger(timeoutMs) || timeoutMs <= 0) {
    throw new Error("Site request timeout must be a positive integer");
  }
  const canonicalOrigin = validateOrigin(origin, mode);
  const routes = [];

  for (const pathname of ROUTES) {
    const requestedUrl = new URL(pathname, `${canonicalOrigin}/`).href;
    const { html } = await withDeadline(pathname, timeoutMs, async (signal) => {
      const response = await fetchSameOrigin(requestedUrl, canonicalOrigin, fetchImpl, signal);
      if (response.status !== 200) {
        throw new Error(`${pathname} returned unexpected status ${response.status}`);
      }
      const contentType = response.headers.get("content-type") ?? "";
      if (!/^text\/html(?:;|$)/i.test(contentType)) {
        throw new Error(`${pathname} did not return HTML`);
      }
      return { html: await readBoundedHtml(response, signal) };
    });
    validatePage(pathname, html);
    routes.push({ path: pathname, status: 200, contentType: "text/html", check: "passed" });
  }

  return { schemaVersion: 1, origin: canonicalOrigin, routes };
}

function assertExactKeys(value, expected, label) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`Public site deployment ${label} must be an object`);
  }
  const actual = Object.keys(value).sort();
  if (actual.length !== expected.length || actual.some((key, index) => key !== expected[index])) {
    throw new Error(`Public site deployment ${label} has unexpected or missing fields`);
  }
}

function assertIdentifier(value, label) {
  if (typeof value !== "string" || !SAFE_IDENTIFIER.test(value)) {
    throw new Error(`Public site deployment ${label} is invalid`);
  }
}

export function validatePublicSiteDeployment(record, { expectedProjectId } = {}) {
  assertExactKeys(record, DEPLOYMENT_KEYS, "record");
  if (record.schemaVersion !== 1 || record.provider !== "OpenAI Sites") {
    throw new Error("Public site deployment provider/schema is invalid");
  }
  if (record.access !== "public") {
    throw new Error("Public site deployment access must be public");
  }
  for (const field of ["projectId", "versionId", "deploymentId"]) {
    assertIdentifier(record[field], field);
  }
  if (expectedProjectId !== undefined && record.projectId !== expectedProjectId) {
    throw new Error("Public site deployment project ID does not match");
  }
  let origin;
  try {
    origin = new URL(record.origin);
  } catch {
    throw new Error("Public site deployment origin is invalid");
  }
  if (
    origin.protocol !== "https:" ||
    !origin.hostname ||
    origin.username ||
    origin.password ||
    origin.pathname !== "/" ||
    origin.search ||
    origin.hash ||
    origin.origin !== record.origin
  ) {
    throw new Error("Public site deployment origin must be an immutable HTTPS origin");
  }
  if (
    typeof record.deployedAt !== "string" ||
    !/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/.test(record.deployedAt) ||
    new Date(record.deployedAt).toISOString() !== record.deployedAt
  ) {
    throw new Error("Public site deployment time must be UTC RFC3339");
  }
  if (!/^[0-9a-f]{40}$/.test(record.sourceCommit)) {
    throw new Error("Public site deployment source commit is invalid");
  }
  if (!/^[0-9a-f]{64}$/.test(record.policySha256)) {
    throw new Error("Public site deployment policy hash is invalid");
  }
  if (!Array.isArray(record.routes) || record.routes.length !== ROUTES.length) {
    throw new Error("Public site deployment routes are invalid");
  }
  for (const [index, route] of record.routes.entries()) {
    assertExactKeys(route, ROUTE_KEYS, "route");
    if (
      route.path !== ROUTES[index] ||
      route.status !== 200 ||
      route.contentType !== "text/html" ||
      route.check !== "passed"
    ) {
      throw new Error("Public site deployment route evidence is invalid");
    }
  }
  return record;
}

function deploymentEvidencePath(recordsRoot, deploymentId) {
  return path.join(path.resolve(recordsRoot), "deployments", `${deploymentId}.json`);
}

export async function writePublicSiteDeployment({
  record,
  recordsRoot = "docs/release/public-site",
}) {
  validatePublicSiteDeployment(record);
  const evidencePath = deploymentEvidencePath(recordsRoot, record.deploymentId);
  await mkdir(path.dirname(evidencePath), { recursive: true });
  await writeFile(evidencePath, `${JSON.stringify(record, null, 2)}\n`, { flag: "wx" });
  return evidencePath;
}

export async function writeCurrentPublicSitePointer({
  record,
  evidencePath,
  recordsRoot = "docs/release/public-site",
}) {
  validatePublicSiteDeployment(record);
  const expectedEvidencePath = deploymentEvidencePath(recordsRoot, record.deploymentId);
  if (path.resolve(evidencePath) !== expectedEvidencePath) {
    throw new Error("Public site deployment evidence path does not match its deployment ID");
  }
  const persisted = JSON.parse(await readFile(expectedEvidencePath, "utf8"));
  validatePublicSiteDeployment(persisted, { expectedProjectId: record.projectId });
  if (JSON.stringify(persisted) !== JSON.stringify(record)) {
    throw new Error("Public site deployment evidence does not match the current record");
  }

  const root = path.resolve(recordsRoot);
  const pointerPath = path.join(root, "current.json");
  const temporaryPath = path.join(root, `.current.json.tmp-${randomUUID()}`);
  const pointer = {
    schemaVersion: 1,
    deploymentEvidence: `deployments/${record.deploymentId}.json`,
  };
  await mkdir(root, { recursive: true });
  try {
    await writeFile(temporaryPath, `${JSON.stringify(pointer, null, 2)}\n`, { flag: "wx" });
    await rename(temporaryPath, pointerPath);
  } finally {
    await rm(temporaryPath, { force: true }).catch(() => {});
  }
  return pointerPath;
}

export async function createRollbackEvent({
  fromDeployment,
  rollbackDeployment,
  priorEvidencePath,
  recordsRoot = "docs/release/public-site",
}) {
  validatePublicSiteDeployment(fromDeployment);
  validatePublicSiteDeployment(rollbackDeployment, {
    expectedProjectId: fromDeployment.projectId,
  });
  const root = path.resolve(recordsRoot);
  const relativePriorPath = path.relative(root, path.resolve(priorEvidencePath));
  if (!/^deployments\/[A-Za-z0-9][A-Za-z0-9._-]{0,127}\.json$/.test(relativePriorPath)) {
    throw new Error("Prior deployment evidence path is invalid");
  }
  let priorDeployment;
  try {
    priorDeployment = JSON.parse(await readFile(path.resolve(priorEvidencePath), "utf8"));
  } catch (error) {
    if (error?.code === "ENOENT") {
      throw new Error("Prior deployment evidence does not exist");
    }
    throw error;
  }
  validatePublicSiteDeployment(priorDeployment, {
    expectedProjectId: fromDeployment.projectId,
  });
  if (
    priorDeployment.versionId === fromDeployment.versionId ||
    rollbackDeployment.versionId !== priorDeployment.versionId ||
    rollbackDeployment.deploymentId === fromDeployment.deploymentId
  ) {
    throw new Error("Rollback requires a distinct prior passing deployment version");
  }
  if (path.resolve(priorEvidencePath) !== deploymentEvidencePath(root, priorDeployment.deploymentId)) {
    throw new Error("Prior deployment evidence filename does not match its deployment ID");
  }

  const event = {
    schemaVersion: 1,
    kind: "rollback",
    deploymentId: rollbackDeployment.deploymentId,
    versionId: rollbackDeployment.versionId,
    deployedAt: rollbackDeployment.deployedAt,
    fromVersionId: fromDeployment.versionId,
    toVersionId: priorDeployment.versionId,
    priorDeploymentEvidence: relativePriorPath,
    routes: rollbackDeployment.routes,
  };
  const timestamp = rollbackDeployment.deployedAt.replaceAll(":", "-");
  const eventPath = path.join(
    root,
    "events",
    `${timestamp}-rollback-to-${rollbackDeployment.versionId}.json`,
  );
  await mkdir(path.dirname(eventPath), { recursive: true });
  await writeFile(eventPath, `${JSON.stringify(event, null, 2)}\n`, { flag: "wx" });
  return { event, eventPath };
}

function waitForExit(child) {
  return new Promise((resolve) => child.once("exit", resolve));
}

async function stopPreview(child) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  child.kill("SIGTERM");
  const timeout = new Promise((resolve) => setTimeout(resolve, 3_000, "timeout"));
  if ((await Promise.race([waitForExit(child), timeout])) === "timeout") {
    child.kill("SIGKILL");
    await waitForExit(child);
  }
}

async function startBuiltPreview(siteDirectory) {
  const resolvedDirectory = path.resolve(siteDirectory);
  const child = spawn("pnpm", ["--dir", resolvedDirectory, "preview"], {
    cwd: resolvedDirectory,
    env: { ...process.env, CI: "true", NO_COLOR: "1" },
    shell: false,
    stdio: ["ignore", "pipe", "pipe"],
  });

  return new Promise((resolve, reject) => {
    let output = "";
    const timer = setTimeout(() => {
      void stopPreview(child).finally(() => reject(new Error("Preview startup timed out")));
    }, PREVIEW_TIMEOUT_MS);
    const inspect = (chunk) => {
      output += chunk.toString();
      if (Buffer.byteLength(output) > MAX_PREVIEW_OUTPUT_BYTES) {
        clearTimeout(timer);
        void stopPreview(child).finally(() =>
          reject(new Error("Preview output exceeded its bound")),
        );
        return;
      }
      const matches = [...output.matchAll(/https?:\/\/(?:127\.0\.0\.1|localhost|\[::1\]):\d+/g)];
      const origins = [...new Set(matches.map(([match]) => new URL(match).origin))];
      if (origins.length === 1) {
        clearTimeout(timer);
        resolve({ child, origin: origins[0] });
      } else if (origins.length > 1) {
        clearTimeout(timer);
        void stopPreview(child).finally(() =>
          reject(new Error("Preview emitted multiple loopback URLs")),
        );
      }
    };
    child.stdout.on("data", inspect);
    child.stderr.on("data", inspect);
    child.once("error", (error) => {
      clearTimeout(timer);
      reject(error);
    });
    child.once("exit", (code) => {
      clearTimeout(timer);
      reject(new Error(`Preview exited before readiness with code ${code}`));
    });
  });
}

async function writeEvidence(result) {
  const evidencePath = path.resolve(".tmp/public-site-check.json");
  await mkdir(path.dirname(evidencePath), { recursive: true });
  const temporaryPath = `${evidencePath}.${process.pid}.tmp`;
  await writeFile(temporaryPath, `${JSON.stringify(result, null, 2)}\n`, { flag: "wx" });
  await rename(temporaryPath, evidencePath);
  return evidencePath;
}

async function main(argv) {
  const builtPreviewIndex = argv.indexOf("--built-preview");
  const originIndex = argv.indexOf("--origin");
  const wantsEvidence = argv.includes("--write-evidence");
  const readOnly = argv.includes("--read-only");
  if (builtPreviewIndex >= 0 === originIndex >= 0 || (wantsEvidence && readOnly)) {
    throw new Error("Choose exactly one of --built-preview <directory> or --origin <https-url>");
  }

  let preview;
  try {
    preview =
      builtPreviewIndex >= 0 ? await startBuiltPreview(argv[builtPreviewIndex + 1]) : undefined;
    const result = await checkPublicSite({
      origin: preview?.origin ?? argv[originIndex + 1],
      mode: preview ? "preview" : "deployed",
    });
    const evidencePath = wantsEvidence ? await writeEvidence(result) : undefined;
    process.stdout.write(`${JSON.stringify({ ...result, evidencePath })}\n`);
  } finally {
    if (preview) await stopPreview(preview.child);
  }
}

const invokedPath = process.argv[1] ? path.resolve(process.argv[1]) : "";
if (invokedPath === fileURLToPath(import.meta.url)) {
  main(process.argv.slice(2)).catch((error) => {
    process.stderr.write(`${error instanceof Error ? error.message : String(error)}\n`);
    process.exitCode = 1;
  });
}
