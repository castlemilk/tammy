import assert from "node:assert/strict";
import test from "node:test";

import { checkPublicSite } from "./check-public-site.mjs";

const origin = "https://tammy.example";

const pages = {
  "/": `<!doctype html><html><body>
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
  </body></html>`,
  "/support": `<!doctype html><html><body>
    <nav><a href="/">Tammy</a><a href="/privacy">Privacy</a><a href="/support">Support</a></nav>
    <h1>Support</h1><a href="mailto:ben.ebsworth@gmail.com">ben.ebsworth@gmail.com</a>
    <p>Tammy Accounting version 0.1.0.</p>
  </body></html>`,
};

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
