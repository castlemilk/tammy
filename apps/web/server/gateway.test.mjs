import assert from "node:assert/strict";
import { once } from "node:events";
import { request } from "node:http";
import { test } from "node:test";
import { createGateway } from "./gateway.mjs";

function call(port, path, { method = "GET", origin, cookie, body, raw, host, authorization } = {}) {
  return new Promise((resolve, reject) => {
    const req = request(
      {
        host: "127.0.0.1",
        port,
        path,
        method,
        headers: {
          Host: host ?? `127.0.0.1:${port}`,
          ...(origin ? { Origin: origin } : {}),
          ...(cookie ? { Cookie: cookie } : {}),
          ...(authorization ? { Authorization: authorization } : {}),
          ...(body || raw !== undefined ? { "Content-Type": "application/json" } : {}),
        },
      },
      (res) => {
        const chunks = [];
        res.on("data", (chunk) => chunks.push(chunk));
        res.on("end", () =>
          resolve({
            status: res.statusCode,
            headers: res.headers,
            body: JSON.parse(Buffer.concat(chunks).toString()),
          }),
        );
      },
    );
    req.on("error", reject);
    req.end(raw ?? (body ? JSON.stringify(body) : undefined));
  });
}

test("requires a browser session, validates origin and rotates its cookie on login", async () => {
  const facade = {
    state: () => ({ state: "locked" }),
    unlock: async () => ({
      authentication: { secret: "CORE_SESSION_SECRET" },
      organisation: { id: "org", name: "Example" },
    }),
    accounts: async () => ({ accounts: [{ code: "100" }] }),
    logout: async () => {},
  };
  const server = createGateway({ facade, origin: "http://127.0.0.1:5173" });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    assert.equal((await call(port, "/api/accounts")).status, 401);
    assert.equal((await call(port, "/api/session", { host: "evil.example" })).status, 403);
    const start = await call(port, "/api/session");
    assert.equal(start.body.state, "locked");
    const cookie = start.headers["set-cookie"][0].split(";")[0];
    assert.match(start.headers["set-cookie"][0], /HttpOnly/);
    assert.match(start.headers["set-cookie"][0], /SameSite=Strict/);
    assert.equal(
      (
        await call(port, "/api/workspace/unlock", {
          method: "POST",
          cookie,
          origin: "https://evil.example",
          body: {},
        })
      ).status,
      403,
    );
    const login = await call(port, "/api/workspace/unlock", {
      method: "POST",
      cookie,
      origin: "http://127.0.0.1:5173",
      body: {},
    });
    assert.equal(login.status, 200);
    assert.doesNotMatch(JSON.stringify(login.body), /CORE_SESSION_SECRET/);
    const authenticatedCookie = login.headers["set-cookie"][0].split(";")[0];
    assert.notEqual(authenticatedCookie, cookie);
    assert.equal((await call(port, "/api/accounts", { cookie })).status, 401);
    assert.deepEqual((await call(port, "/api/accounts", { cookie: authenticatedCookie })).body, {
      accounts: [{ code: "100" }],
    });
    await call(port, "/api/session/logout", {
      method: "POST",
      cookie: authenticatedCookie,
      origin: "http://127.0.0.1:5173",
      body: {},
    });
    assert.equal((await call(port, "/api/accounts", { cookie: authenticatedCookie })).status, 401);
  } finally {
    server.close();
  }
});

test("MCP reads require an explicit token and an active authenticated browser", async () => {
  const secret = "a-secret-that-is-at-least-32-characters-long";
  const facade = {
    state: () => ({ state: "locked" }),
    unlock: async () => ({
      authentication: { id: "core" },
      organisation: { id: "org", name: "Example" },
    }),
    accounts: async () => ({ accounts: [{ code: "100" }] }),
    logout: async () => {},
  };
  const server = createGateway({ facade, origin: "http://127.0.0.1:5173", mcpToken: secret });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    assert.equal(
      (await call(port, "/api/mcp/accounts", { authorization: `Bearer ${secret}` })).status,
      401,
    );
    const cookie = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    const login = await call(port, "/api/workspace/unlock", {
      method: "POST",
      cookie,
      origin: "http://127.0.0.1:5173",
      body: {},
    });
    const auth = login.headers["set-cookie"][0].split(";")[0];
    assert.equal(
      (await call(port, "/api/mcp/accounts", { authorization: "Bearer wrong" })).status,
      401,
    );
    assert.deepEqual(
      (await call(port, "/api/mcp/accounts", { authorization: `Bearer ${secret}` })).body,
      { accounts: [{ code: "100" }] },
    );
    await call(port, "/api/session/logout", {
      method: "POST",
      cookie: auth,
      origin: "http://127.0.0.1:5173",
      body: {},
    });
    assert.equal(
      (await call(port, "/api/mcp/accounts", { authorization: `Bearer ${secret}` })).status,
      401,
    );
  } finally {
    server.close();
  }
});

test("pending setup is bound to its initiating browser session", async () => {
  const facade = {
    state: () => ({ state: "setup" }),
    create: async () => ({ setupId: "pending", recoveryCode: "once" }),
    confirm: async () => ({
      authentication: { sessionId: "core" },
      organisation: { id: "org", name: "Example" },
    }),
  };
  const server = createGateway({ facade, origin: "http://127.0.0.1:5173" });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    const a = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    const b = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    await call(port, "/api/workspace/create", {
      method: "POST",
      cookie: a,
      origin: "http://127.0.0.1:5173",
      body: {},
    });
    assert.equal(
      (
        await call(port, "/api/workspace/confirm", {
          method: "POST",
          cookie: b,
          origin: "http://127.0.0.1:5173",
          body: {},
        })
      ).status,
      403,
    );
    assert.equal(
      (
        await call(port, "/api/workspace/confirm", {
          method: "POST",
          cookie: a,
          origin: "http://127.0.0.1:5173",
          body: {},
        })
      ).status,
      200,
    );
  } finally {
    server.close();
  }
});

test("anonymous session churn cannot prevent a legitimate browser from starting", async () => {
  const server = createGateway({
    facade: { state: () => ({ state: "setup" }) },
    origin: "http://127.0.0.1:5173",
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    for (let i = 0; i < 65; i++) assert.equal((await call(port, "/api/session")).status, 200);
    assert.equal((await call(port, "/api/session")).body.state, "setup");
  } finally {
    server.close();
  }
});

test("returns safe machine-readable errors from the accounting facade", async () => {
  const facade = {
    state: () => ({ state: "locked" }),
    unlock: async () => ({
      authentication: { id: "core" },
      organisation: { id: "org", name: "Example" },
    }),
    accounts: async () => {
      throw Object.assign(new Error("The resource was not found."), {
        status: 404,
        code: "not_found",
      });
    },
  };
  const server = createGateway({ facade, origin: "http://127.0.0.1:5173" });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    const cookie = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    const login = await call(port, "/api/workspace/unlock", {
      method: "POST",
      cookie,
      origin: "http://127.0.0.1:5173",
      body: {},
    });
    const auth = login.headers["set-cookie"][0].split(";")[0];
    const result = await call(port, "/api/accounts", { cookie: auth });
    assert.equal(result.status, 404);
    assert.deepEqual(result.body, {
      error: { code: "not_found", message: "The resource was not found." },
    });
  } finally {
    server.close();
  }
});

test("serves the built application on the same loopback origin", async () => {
  const { mkdtemp, writeFile, rm } = await import("node:fs/promises");
  const { tmpdir } = await import("node:os");
  const { join } = await import("node:path");
  const root = await mkdtemp(join(tmpdir(), "tammy-static-"));
  await writeFile(join(root, "index.html"), "<!doctype html><title>Tammy</title>");
  const server = createGateway({ facade: {}, origin: "http://127.0.0.1:5173", staticRoot: root });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    const response = await fetch(`http://127.0.0.1:${port}/overview`);
    assert.equal(response.status, 200);
    assert.match(await response.text(), /<title>Tammy<\/title>/);
  } finally {
    server.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("a slow pending create cannot lose its owner to anonymous session churn", async () => {
  let release, started;
  const began = new Promise((resolve) => {
    started = resolve;
  });
  const wait = new Promise((resolve) => {
    release = resolve;
  });
  const facade = {
    state: () => ({ state: "setup" }),
    create: async () => {
      started();
      await wait;
      return { setupId: "pending", recoveryCode: "once" };
    },
    confirm: async () => ({
      authentication: { id: "core" },
      organisation: { id: "org", name: "Example" },
    }),
  };
  const server = createGateway({ facade, origin: "http://127.0.0.1:5173" });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    const cookie = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    const create = call(port, "/api/workspace/create", {
      method: "POST",
      cookie,
      origin: "http://127.0.0.1:5173",
      body: {},
    });
    await began;
    for (let i = 0; i < 70; i++) await call(port, "/api/session");
    release();
    assert.equal((await create).status, 200);
    assert.equal(
      (
        await call(port, "/api/workspace/confirm", {
          method: "POST",
          cookie,
          origin: "http://127.0.0.1:5173",
          body: {},
        })
      ).status,
      200,
    );
  } finally {
    server.close();
  }
});

test("a duplicate create cannot release the first pending setup reservation", async () => {
  let release, started;
  const began = new Promise((resolve) => {
    started = resolve;
  });
  const wait = new Promise((resolve) => {
    release = resolve;
  });
  let count = 0;
  const facade = {
    state: () => ({ state: "setup" }),
    create: async () => {
      count++;
      if (count === 1) {
        started();
        await wait;
        return { setupId: "pending", recoveryCode: "once" };
      }
      throw Object.assign(new Error("busy"), { status: 409 });
    },
    confirm: async () => ({
      authentication: { id: "core" },
      organisation: { id: "org", name: "Example" },
    }),
  };
  const server = createGateway({ facade, origin: "http://127.0.0.1:5173" });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    const a = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    const b = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    const first = call(port, "/api/workspace/create", {
      method: "POST",
      cookie: a,
      origin: "http://127.0.0.1:5173",
      body: {},
    });
    await began;
    const duplicate = await call(port, "/api/workspace/create", {
      method: "POST",
      cookie: a,
      origin: "http://127.0.0.1:5173",
      body: {},
    });
    assert.equal(duplicate.status, 409);
    assert.equal(duplicate.body.error.code, "conflict");
    release();
    await first;
    assert.equal(
      (
        await call(port, "/api/workspace/confirm", {
          method: "POST",
          cookie: b,
          origin: "http://127.0.0.1:5173",
          body: {},
        })
      ).status,
      403,
    );
  } finally {
    server.close();
  }
});

/**
 * A facade that behaves like the real one over the real core. Unlock checks the credentials before
 * it disturbs a signed-in browser: wrong ones fail with 401 and change nothing, and right ones end
 * the earlier core session. Logout locks only while its session still owns the open workspace.
 */
function coreLikeFacade(calls = [], { passphrase = "right" } = {}) {
  let owner;
  let signIns = 0;
  return {
    state: () => ({ state: "locked" }),
    unlock: async (body) => {
      calls.push("unlock");
      if (body.workspacePassphrase !== passphrase)
        throw Object.assign(new Error("The workspace passphrase is incorrect."), {
          status: 401,
          code: "unauthenticated",
        });
      signIns += 1;
      owner = `core-${signIns}`;
      return {
        authentication: { id: owner },
        organisation: { id: "org", name: "Example" },
      };
    },
    logout: async (session) => {
      calls.push("logout");
      if (session.authentication.id === owner) owner = undefined;
    },
    accounts: async (session) => {
      if (session.authentication.id !== owner)
        throw Object.assign(new Error("Your session is no longer valid."), {
          status: 401,
          code: "unauthenticated",
        });
      return { accounts: [{ code: session.authentication.id }] };
    },
  };
}

async function browser(port, origin) {
  let cookie = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
  return {
    cookie: () => cookie,
    get: (path) => call(port, path, { cookie }),
    unlock: async (workspacePassphrase = "right") => {
      const result = await call(port, "/api/workspace/unlock", {
        method: "POST",
        cookie,
        origin,
        body: { workspacePassphrase },
      });
      if (result.headers["set-cookie"]) cookie = result.headers["set-cookie"][0].split(";")[0];
      return result;
    },
  };
}

test("idle expiry locks the core before a new browser can unlock it", async () => {
  let now = 0;
  const calls = [];
  const origin = "http://127.0.0.1:5173";
  const server = createGateway({
    facade: coreLikeFacade(calls),
    origin,
    now: () => now,
    idleMs: 100,
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    assert.equal((await (await browser(port, origin)).unlock()).status, 200);
    now = 101;
    // Any request runs the idle check, which signs the idle session out and locks the workspace.
    const next = await browser(port, origin);
    assert.deepEqual(calls, ["unlock", "logout"]);
    assert.equal((await next.unlock()).status, 200);
    assert.deepEqual(calls, ["unlock", "logout", "unlock"]);
  } finally {
    server.close();
  }
});

test("a newer browser takes over only after the facade accepts its credentials", async () => {
  const calls = [];
  const origin = "http://127.0.0.1:5173";
  const secret = "a-secret-that-is-at-least-32-characters-long";
  const server = createGateway({ facade: coreLikeFacade(calls), origin, mcpToken: secret });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  const mcp = () => call(port, "/api/mcp/accounts", { authorization: `Bearer ${secret}` });
  try {
    const a = await browser(port, origin);
    assert.equal((await a.unlock()).status, 200);
    assert.deepEqual((await mcp()).body, { accounts: [{ code: "core-1" }] });
    const b = await browser(port, origin);
    // A rejected takeover leaves the signed-in browser and MCP as they were.
    const wrong = await b.unlock("wrong");
    assert.equal(wrong.status, 401);
    assert.equal(wrong.body.error.message, "The workspace passphrase is incorrect.");
    assert.deepEqual((await a.get("/api/accounts")).body, { accounts: [{ code: "core-1" }] });
    assert.deepEqual((await mcp()).body, { accounts: [{ code: "core-1" }] });
    assert.equal((await b.unlock()).status, 200);
    // The gateway never logs the earlier browser out itself: the facade's sign-in has already
    // ended that core session, and a lock now would end the new one.
    assert.deepEqual(calls, ["unlock", "unlock", "unlock"]);
    const replaced = await a.get("/api/accounts");
    assert.equal(replaced.status, 401);
    assert.equal(replaced.body.error.message, "Start a browser session first.");
    assert.deepEqual((await b.get("/api/accounts")).body, { accounts: [{ code: "core-2" }] });
    // MCP follows the active browser session.
    assert.deepEqual((await mcp()).body, { accounts: [{ code: "core-2" }] });
  } finally {
    server.close();
  }
});

test("a logout that finishes after a takeover leaves the newer session signed in", async () => {
  const calls = [];
  const origin = "http://127.0.0.1:5173";
  let watching = false;
  let lookedUp = () => {};
  const server = createGateway({
    facade: coreLikeFacade(calls),
    origin,
    // The gateway reads the clock just before it looks up the request's session.
    now: () => {
      if (watching) lookedUp();
      return Date.now();
    },
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    const a = await browser(port, origin);
    assert.equal((await a.unlock()).status, 200);
    // A's logout looks up A's session, then waits for its body.
    const reached = new Promise((resolve) => {
      lookedUp = resolve;
    });
    watching = true;
    let sendBody = () => {};
    const logout = new Promise((resolve, reject) => {
      const req = request(
        {
          host: "127.0.0.1",
          port,
          path: "/api/session/logout",
          method: "POST",
          headers: {
            Host: `127.0.0.1:${port}`,
            Origin: origin,
            Cookie: a.cookie(),
            "Content-Type": "application/json",
            "Content-Length": "2",
          },
        },
        (res) => {
          const chunks = [];
          res.on("data", (chunk) => chunks.push(chunk));
          res.on("end", () =>
            resolve({ status: res.statusCode, body: JSON.parse(Buffer.concat(chunks).toString()) }),
          );
        },
      );
      req.on("error", reject);
      req.flushHeaders();
      sendBody = () => req.end("{}");
    });
    await reached;
    watching = false;
    const b = await browser(port, origin);
    assert.equal((await b.unlock()).status, 200);
    sendBody();
    assert.deepEqual(await logout, { status: 200, body: { state: "locked" } });
    // The replaced session is not logged out again, so nothing locks B's workspace.
    assert.deepEqual(calls, ["unlock", "unlock"]);
    assert.deepEqual((await b.get("/api/accounts")).body, { accounts: [{ code: "core-2" }] });
  } finally {
    server.close();
  }
});

test("core session expiration clears the browser session and permits unlock again", async () => {
  const facade = {
    ...coreLikeFacade(),
    accounts: async () => {
      throw Object.assign(new Error("expired"), { status: 401, code: "unauthenticated" });
    },
  };
  const origin = "http://127.0.0.1:5173";
  const server = createGateway({ facade, origin });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    let cookie = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    const first = await call(port, "/api/workspace/unlock", {
      method: "POST",
      cookie,
      origin,
      body: { workspacePassphrase: "right" },
    });
    cookie = first.headers["set-cookie"][0].split(";")[0];
    assert.equal((await call(port, "/api/accounts", { cookie })).status, 401);
    const next = await call(port, "/api/session", { cookie });
    assert.equal(next.body.state, "locked");
    cookie = next.headers["set-cookie"][0].split(";")[0];
    assert.equal(
      (
        await call(port, "/api/workspace/unlock", {
          method: "POST",
          cookie,
          origin,
          body: { workspacePassphrase: "right" },
        })
      ).status,
      200,
    );
  } finally {
    server.close();
  }
});

test("a confirmation that discards the pending setup releases its browser", async () => {
  let pending = false;
  const facade = {
    state: () => (pending ? { state: "recovery", setupId: "pending" } : { state: "setup" }),
    create: async () => {
      pending = true;
      return { setupId: "pending", recoveryCode: "once" };
    },
    confirm: async (body) => {
      if (body.recoveryCode === "wrong")
        throw Object.assign(new Error("The recovery code does not match."), { status: 400 });
      pending = false;
      throw Object.assign(new Error("This setup expired before it was confirmed."), {
        status: 409,
      });
    },
  };
  const origin = "http://127.0.0.1:5173";
  const server = createGateway({ facade, origin });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  const post = (path, cookie, body = {}) =>
    call(port, path, { method: "POST", cookie, origin, body });
  try {
    const a = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    const b = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    assert.equal((await post("/api/workspace/create", a)).status, 200);
    assert.equal((await post("/api/workspace/create", b)).status, 409);
    // A wrong code keeps the setup, and its owner.
    assert.equal((await post("/api/workspace/confirm", a, { recoveryCode: "wrong" })).status, 400);
    assert.equal((await post("/api/workspace/confirm", b)).status, 403);
    const expired = await post("/api/workspace/confirm", a);
    assert.equal(expired.status, 409);
    assert.equal(expired.body.error.code, "conflict");
    assert.deepEqual((await call(port, "/api/session", { cookie: b })).body, { state: "setup" });
    assert.equal((await post("/api/workspace/create", b)).status, 200);
  } finally {
    server.close();
  }
});

test("inherited object keys never select a route or an MCP read", async () => {
  const secret = "a-secret-that-is-at-least-32-characters-long";
  const origin = "http://127.0.0.1:5173";
  const server = createGateway({ facade: coreLikeFacade(), origin, mcpToken: secret });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    const a = await browser(port, origin);
    assert.equal((await a.unlock()).status, 200);
    for (const name of ["constructor", "__proto__", "toString", "hasOwnProperty"]) {
      const mcp = await call(port, `/api/mcp/${name}`, { authorization: `Bearer ${secret}` });
      assert.equal(mcp.status, 401, name);
      assert.equal(mcp.body.error.code, "unauthenticated");
      assert.equal((await a.get(`/api/${name}`)).status, 404, name);
    }
  } finally {
    server.close();
  }
});

test("error codes follow the documented HTTP status contract", async () => {
  const failures = [
    Object.assign(new Error("Refresh and try again."), { status: 409 }),
    Object.assign(new Error("Already open."), { status: 409, code: "invalid_argument" }),
    Object.assign(new Error("Check the amount."), { status: 400, code: "invalid_argument" }),
  ];
  const facade = {
    state: () => ({ state: "locked" }),
    unlock: async () => ({
      authentication: { id: "core" },
      organisation: { id: "org", name: "Example" },
    }),
    postJournal: async () => {
      throw failures.shift();
    },
  };
  const origin = "http://127.0.0.1:5173";
  const server = createGateway({ facade, origin });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    const cookie = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    const login = await call(port, "/api/workspace/unlock", {
      method: "POST",
      cookie,
      origin,
      body: {},
    });
    const auth = login.headers["set-cookie"][0].split(";")[0];
    const post = (options) =>
      call(port, "/api/journals", { method: "POST", cookie: auth, origin, ...options });

    // The body cap is 768 KiB; a body just over it is fully sent before the gateway answers.
    const oversized = await post({ raw: `{"memo":"${"x".repeat(768 * 1024)}"}` });
    assert.equal(oversized.status, 413);
    assert.deepEqual(oversized.body, {
      error: { code: "request_too_large", message: "Request is too large." },
    });
    const malformed = await post({ raw: "{" });
    assert.equal(malformed.status, 400);
    assert.equal(malformed.body.error.code, "invalid_request");
    for (const message of ["Refresh and try again.", "Already open."]) {
      const conflict = await post({ body: {} });
      assert.equal(conflict.status, 409);
      assert.deepEqual(conflict.body, { error: { code: "conflict", message } });
    }
    const invalid = await post({ body: {} });
    assert.equal(invalid.status, 400);
    assert.equal(invalid.body.error.code, "invalid_argument");
    assert.equal((await call(port, "/api/accounts", { cookie: auth })).body.error.code, "internal");
  } finally {
    server.close();
  }
});
