import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  checkPublicSite,
  createRollbackEvent,
  validatePublicSiteDeployment,
  writeCurrentPublicSitePointer,
  writePublicSiteDeployment,
} from "./check-public-site.mjs";

const origin = "https://tammy-accounting.castlemilk.chatgpt.site";

const pages = {
  "/": `<!doctype html><html><head>
    <meta property="og:image" content="${origin}/og.png">
    <meta name="twitter:image" content="${origin}/og.png">
    <meta name="twitter:card" content="summary_large_image">
  </head><body>
    <nav><a href="/">Tammy</a><a href="/privacy">Privacy</a><a href="/support">Support</a></nav>
    <h1>Local accounting for Australia</h1>
    <p>Tammy Accounting by Gamma Systems Pty Ltd.</p>
    <p>Requires macOS 14 or later on arm64.</p>
    <p>Reporting is preparation-only and submissions are not lodged.</p>
  </body></html>`,
  "/privacy": `<!doctype html><html><body>
    <nav><a href="/">Tammy</a><a href="/privacy">Privacy</a><a href="/support">Support</a></nav>
    <h1>Privacy policy</h1><p>Effective 30 August 2026.</p>
    <p>Gamma Systems Pty Ltd does not transmit your accounting records.</p>
    <a href="${origin}/support">Deletion support</a>
  </body></html>`,
  "/support": `<!doctype html><html><body>
    <nav><a href="/">Tammy</a><a href="/privacy">Privacy</a><a href="/support">Support</a></nav>
    <h1>Support</h1><a href="mailto:ben.ebsworth@gmail.com">ben.ebsworth@gmail.com</a>
    <p>Tammy Accounting version 0.1.0.</p>
  </body></html>`,
};

function deployment(overrides = {}) {
  return {
    schemaVersion: 1,
    provider: "OpenAI Sites",
    access: "public",
    projectId: "project-1",
    versionId: "project-1~version-1",
    deploymentId: "deployment-1",
    origin,
    deployedAt: "2026-08-30T08:00:00.000Z",
    sourceCommit: "a".repeat(40),
    policySha256: "b".repeat(64),
    routes: [
      { path: "/", status: 200, contentType: "text/html", check: "passed" },
      { path: "/privacy", status: 200, contentType: "text/html", check: "passed" },
      { path: "/support", status: 200, contentType: "text/html", check: "passed" },
    ],
    ...overrides,
  };
}

function mockFetch(overrides = {}) {
  const calls = [];
  const fetchImpl = async (url) => {
    calls.push(url);
    const parsed = new URL(url);
    const override = overrides[parsed.pathname] ?? {};
    const response = new Response(override.body ?? pages[parsed.pathname], {
      status: override.status ?? 200,
      headers: {
        "content-type": override.contentType ?? "text/html; charset=utf-8",
      },
    });
    Object.defineProperty(response, "url", {
      value: override.url ?? url,
    });
    return response;
  };
  return { calls, fetchImpl };
}

test("checks exactly the canonical preview routes", async () => {
  const { calls, fetchImpl } = mockFetch();
  const result = await checkPublicSite({
    origin: "http://127.0.0.1:4173",
    mode: "preview",
    fetchImpl,
  });

  assert.deepEqual(
    calls.map((url) => new URL(url).pathname),
    ["/", "/privacy", "/support"],
  );
  assert.deepEqual(
    result.routes.map(({ path, status, contentType, check }) => ({
      path,
      status,
      contentType,
      check,
    })),
    [
      { path: "/", status: 200, contentType: "text/html", check: "passed" },
      { path: "/privacy", status: 200, contentType: "text/html", check: "passed" },
      { path: "/support", status: 200, contentType: "text/html", check: "passed" },
    ],
  );
});

test("requires HTTPS outside built preview mode", async () => {
  const { fetchImpl } = mockFetch();
  await assert.rejects(
    checkPublicSite({ origin: "http://tammy.example", mode: "deployed", fetchImpl }),
    /HTTPS/i,
  );
});

test("accepts framework hydration comments inside required visible text", async () => {
  const { fetchImpl } = mockFetch({
    "/": {
      body: pages["/"]
        .replace("macOS 14", "macOS <!-- -->14")
        .replace("arm64", "arm<!-- -->64")
        .replace("not lodged", "not <!-- -->lodged"),
    },
  });
  await checkPublicSite({ origin, mode: "deployed", fetchImpl });
});

test("rejects failures, non-HTML responses, and off-origin redirects", async () => {
  for (const overrides of [
    { "/privacy": { status: 503 } },
    { "/support": { contentType: "application/json" } },
    { "/": { url: "https://attacker.example/" } },
  ]) {
    const { fetchImpl } = mockFetch(overrides);
    await assert.rejects(
      checkPublicSite({ origin, mode: "deployed", fetchImpl }),
      /status|HTML|origin/i,
    );
  }
});

test("rejects an off-origin intermediate redirect even if it could return", async () => {
  const calls = [];
  const fetchImpl = async (url, options) => {
    calls.push({ url, options });
    if (calls.length === 1) {
      return new Response(null, {
        status: 302,
        headers: { location: "https://attacker.example/bounce" },
      });
    }
    return new Response(pages["/"], {
      status: 200,
      headers: { "content-type": "text/html" },
    });
  };

  await assert.rejects(
    checkPublicSite({ origin, mode: "deployed", fetchImpl }),
    /redirect.*origin/i,
  );
  assert.equal(calls.length, 1);
});

test("follows a bounded same-origin redirect manually", async () => {
  const calls = [];
  const fetchImpl = async (url, options) => {
    calls.push({ url, options });
    const parsed = new URL(url);
    if (calls.length === 1) {
      return new Response(null, {
        status: 302,
        headers: { location: "/home" },
      });
    }
    const pathname = parsed.pathname === "/home" ? "/" : parsed.pathname;
    const response = new Response(pages[pathname], {
      status: 200,
      headers: { "content-type": "text/html" },
    });
    Object.defineProperty(response, "url", { value: url });
    return response;
  };

  await checkPublicSite({ origin, mode: "deployed", fetchImpl });
  assert.equal(calls[0].options.redirect, "manual");
  assert.equal(new URL(calls[1].url).pathname, "/home");
});

test("requires canonical identity, navigation, privacy, support, platform, and boundary copy", async () => {
  for (const [path, needle] of [
    ["/", "Tammy Accounting"],
    ["/", "Gamma Systems Pty Ltd"],
    ["/", "macOS 14"],
    ["/", "arm64"],
    ["/", "preparation-only"],
    ["/", "not lodged"],
    ["/privacy", "30 August 2026"],
    ["/privacy", `href="${origin}/support"`],
    ["/support", "mailto:ben.ebsworth@gmail.com"],
  ]) {
    const { fetchImpl } = mockFetch({
      [path]: { body: pages[path].replace(needle, "removed") },
    });
    await assert.rejects(
      checkPublicSite({ origin, mode: "deployed", fetchImpl }),
      /missing|required/i,
    );
  }

  for (const route of Object.keys(pages)) {
    const { fetchImpl } = mockFetch({
      [route]: { body: pages[route].replace(`href="/support"`, "") },
    });
    await assert.rejects(checkPublicSite({ origin, mode: "deployed", fetchImpl }), /navigation/i);
  }
});

test("requires absolute trusted Open Graph and X image metadata", async () => {
  for (const needle of [
    `<meta property="og:image" content="${origin}/og.png">`,
    `<meta name="twitter:image" content="${origin}/og.png">`,
    `<meta name="twitter:card" content="summary_large_image">`,
  ]) {
    const { fetchImpl } = mockFetch({
      "/": { body: pages["/"].replace(needle, "") },
    });
    await assert.rejects(
      checkPublicSite({ origin, mode: "deployed", fetchImpl }),
      /social|metadata/i,
    );
  }
});

test("rejects placeholders, TestFlight copy, and positive production lodgement claims", async () => {
  for (const forbidden of [
    "TODO",
    "Lorem ipsum",
    "Join TestFlight",
    "Tammy lodges with the ATO",
    "Submit BAS to the ATO",
  ]) {
    const { fetchImpl } = mockFetch({
      "/": { body: `${pages["/"]}<p>${forbidden}</p>` },
    });
    await assert.rejects(checkPublicSite({ origin, mode: "deployed", fetchImpl }), /forbidden/i);
  }
});

test("bounds response bodies", async () => {
  const { fetchImpl } = mockFetch({
    "/support": { body: "x".repeat(1_000_001) },
  });
  await assert.rejects(
    checkPublicSite({ origin, mode: "deployed", fetchImpl }),
    /large|size|bytes/i,
  );
});

test("times out a fetch that never returns headers", async () => {
  await assert.rejects(
    checkPublicSite({
      origin,
      mode: "deployed",
      fetchImpl: async () => new Promise(() => {}),
      timeoutMs: 20,
    }),
    /timed out/i,
  );
});

test("times out a response body that never completes", async () => {
  const fetchImpl = async (url) => {
    const response = new Response(new ReadableStream({ start() {} }), {
      status: 200,
      headers: { "content-type": "text/html" },
    });
    Object.defineProperty(response, "url", { value: url });
    return response;
  };
  await assert.rejects(
    checkPublicSite({ origin, mode: "deployed", fetchImpl, timeoutMs: 20 }),
    /timed out/i,
  );
});

test("validates the strict public Sites deployment record", () => {
  assert.deepEqual(
    validatePublicSiteDeployment(deployment(), { expectedProjectId: "project-1" }),
    deployment(),
  );

  for (const invalid of [
    deployment({ provider: "Other" }),
    deployment({ access: "private" }),
    deployment({ origin: "http://tammy.example" }),
    deployment({ origin: "https://tammy.example/?mutable=1" }),
    deployment({ origin: "https://tammy.example/#mutable" }),
    deployment({ routes: deployment().routes.slice(1) }),
    deployment({ routes: [...deployment().routes, deployment().routes[0]] }),
    deployment({ routes: deployment().routes.map((route, index) => index ? route : { ...route, check: "failed" }) }),
    { ...deployment(), token: "secret" },
    { ...deployment(), sourceWriteUrl: "https://example.com/write?token=secret" },
  ]) {
    assert.throws(
      () => validatePublicSiteDeployment(invalid, { expectedProjectId: "project-1" }),
      /deployment/i,
    );
  }
  assert.throws(
    () => validatePublicSiteDeployment(deployment(), { expectedProjectId: "different-project" }),
    /project/i,
  );
});

test("first deployment is immutable and creates no rollback event", async (context) => {
  const recordsRoot = await mkdtemp(
    path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-public-site-records-"),
  );
  context.after(() => rm(recordsRoot, { recursive: true, force: true }));
  const record = deployment();
  const evidencePath = await writePublicSiteDeployment({ record, recordsRoot });
  await writeCurrentPublicSitePointer({ record, evidencePath, recordsRoot });

  assert.deepEqual(JSON.parse(await readFile(evidencePath, "utf8")), record);
  assert.deepEqual(
    JSON.parse(await readFile(path.join(recordsRoot, "current.json"), "utf8")),
    { schemaVersion: 1, deploymentEvidence: "deployments/deployment-1.json" },
  );
  await assert.rejects(writePublicSiteDeployment({ record, recordsRoot }), /exist/i);
  await assert.rejects(readdir(path.join(recordsRoot, "events")), { code: "ENOENT" });
});

test("rollback event requires a distinct existing prior passing deployment", async (context) => {
  const recordsRoot = await mkdtemp(
    path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-public-site-rollback-"),
  );
  context.after(() => rm(recordsRoot, { recursive: true, force: true }));
  const prior = deployment({
    versionId: "version-prior",
    deploymentId: "deployment-prior",
    sourceCommit: "c".repeat(40),
  });
  const priorPath = await writePublicSiteDeployment({ record: prior, recordsRoot });
  const current = deployment({ versionId: "version-current", deploymentId: "deployment-current" });
  const rollbackDeployment = deployment({
    versionId: prior.versionId,
    deploymentId: "deployment-rollback",
    deployedAt: "2026-08-30T09:00:00.000Z",
    sourceCommit: prior.sourceCommit,
  });

  const { event, eventPath } = await createRollbackEvent({
    fromDeployment: current,
    rollbackDeployment,
    priorEvidencePath: priorPath,
    recordsRoot,
  });
  assert.deepEqual(JSON.parse(await readFile(eventPath, "utf8")), event);
  assert.equal(event.kind, "rollback");
  assert.equal(event.deploymentId, "deployment-rollback");
  assert.equal(event.versionId, "version-prior");
  assert.equal(event.fromVersionId, "version-current");
  assert.equal(event.toVersionId, "version-prior");
  assert.equal(event.priorDeploymentEvidence, "deployments/deployment-prior.json");
  assert.deepEqual(event.routes, rollbackDeployment.routes);
  assert.deepEqual(JSON.parse(await readFile(priorPath, "utf8")), prior);

  await assert.rejects(
    createRollbackEvent({
      fromDeployment: current,
      rollbackDeployment: current,
      priorEvidencePath: path.join(recordsRoot, "deployments/missing.json"),
      recordsRoot,
    }),
    /prior|distinct|exist/i,
  );
});
