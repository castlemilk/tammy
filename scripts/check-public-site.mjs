import { spawn } from "node:child_process";
import { mkdir, rename, writeFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";

const ROUTES = ["/", "/privacy", "/support"];
const MAX_RESPONSE_BYTES = 1_000_000;
const MAX_PREVIEW_OUTPUT_BYTES = 65_536;
const PREVIEW_TIMEOUT_MS = 30_000;
const MAX_REDIRECT_HOPS = 5;

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

async function readBoundedHtml(response) {
  const declaredLength = Number(response.headers.get("content-length"));
  if (Number.isFinite(declaredLength) && declaredLength > MAX_RESPONSE_BYTES) {
    throw new Error("Site response exceeds the maximum response size in bytes");
  }
  if (!response.body) {
    return "";
  }

  const reader = response.body.getReader();
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
  } else if (pathname === "/privacy") {
    requireText(text, /Privacy policy/i, "privacy heading", pathname);
    requireText(text, /30 August 2026/i, "policy effective date", pathname);
    requireText(text, /does not transmit your accounting records/i, "data boundary", pathname);
  } else {
    requireText(text, /Support/i, "support heading", pathname);
    requireText(html, /mailto:ben\.ebsworth@gmail\.com/i, "support email", pathname);
    requireText(text, /version 0\.1\.0/i, "app version", pathname);
  }
}

async function fetchSameOrigin(requestedUrl, canonicalOrigin, fetchImpl) {
  let currentUrl = requestedUrl;
  for (let hop = 0; hop <= MAX_REDIRECT_HOPS; hop += 1) {
    const response = await fetchImpl(currentUrl, { redirect: "manual" });
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

export async function checkPublicSite({ origin, mode = "deployed", fetchImpl = fetch }) {
  const canonicalOrigin = validateOrigin(origin, mode);
  const routes = [];

  for (const pathname of ROUTES) {
    const requestedUrl = new URL(pathname, `${canonicalOrigin}/`).href;
    const response = await fetchSameOrigin(requestedUrl, canonicalOrigin, fetchImpl);
    if (response.status !== 200) {
      throw new Error(`${pathname} returned unexpected status ${response.status}`);
    }
    const contentType = response.headers.get("content-type") ?? "";
    if (!/^text\/html(?:;|$)/i.test(contentType)) {
      throw new Error(`${pathname} did not return HTML`);
    }
    const html = await readBoundedHtml(response);
    validatePage(pathname, html);
    routes.push({ path: pathname, status: 200, contentType: "text/html", check: "passed" });
  }

  return { schemaVersion: 1, origin: canonicalOrigin, routes };
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
