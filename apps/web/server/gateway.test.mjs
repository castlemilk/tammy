import assert from "node:assert/strict";
import { once } from "node:events";
import { request } from "node:http";
import { test } from "node:test";
import { createGateway } from "./gateway.mjs";

function call(port, path, { method = "GET", origin, cookie, body, host, authorization } = {}) {
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
          ...(body ? { "Content-Type": "application/json" } : {}),
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
    req.end(body ? JSON.stringify(body) : undefined);
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
    assert.equal(
      (
        await call(port, "/api/workspace/create", {
          method: "POST",
          cookie: a,
          origin: "http://127.0.0.1:5173",
          body: {},
        })
      ).status,
      409,
    );
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

test("idle expiry locks the core before a new browser can unlock it", async () => {
  let now = 0,
    locked = true;
  const facade = {
    state: () => ({ state: "locked" }),
    unlock: async () => {
      if (!locked) throw Object.assign(new Error("already open"), { status: 409 });
      locked = false;
      return { authentication: { id: "core" }, organisation: { id: "org", name: "Example" } };
    },
    logout: async () => {
      locked = true;
    },
  };
  const server = createGateway({
    facade,
    origin: "http://127.0.0.1:5173",
    now: () => now,
    idleMs: 100,
  });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    let cookie = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    await call(port, "/api/workspace/unlock", {
      method: "POST",
      cookie,
      origin: "http://127.0.0.1:5173",
      body: {},
    });
    now = 101;
    const next = await call(port, "/api/session");
    cookie = next.headers["set-cookie"][0].split(";")[0];
    assert.equal(
      (
        await call(port, "/api/workspace/unlock", {
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

test("core session expiration clears the browser session and permits unlock again", async () => {
  let locked = true;
  const facade = {
    state: () => ({ state: "locked" }),
    unlock: async () => {
      if (!locked) throw Object.assign(new Error("already open"), { status: 409 });
      locked = false;
      return { authentication: { id: "core" }, organisation: { id: "org", name: "Example" } };
    },
    logout: async () => {
      locked = true;
    },
    accounts: async () => {
      throw Object.assign(new Error("expired"), { status: 401, code: "unauthenticated" });
    },
  };
  const server = createGateway({ facade, origin: "http://127.0.0.1:5173" });
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  try {
    let cookie = (await call(port, "/api/session")).headers["set-cookie"][0].split(";")[0];
    const first = await call(port, "/api/workspace/unlock", {
      method: "POST",
      cookie,
      origin: "http://127.0.0.1:5173",
      body: {},
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
