import { createHash, randomBytes, timingSafeEqual } from "node:crypto";
import { readFile, realpath, stat } from "node:fs/promises";
import { createServer } from "node:http";
import pathModule from "node:path";
import { errorCode, httpError } from "./errors.mjs";
import { isTrustedRequest } from "./security.mjs";

const COOKIE = "tammy_session";
const MAX_BODY = 768 * 1024;
const IDLE_MS = 30 * 60 * 1000;
const routes = {
  "/api/overview": "overview",
  "/api/accounts": "accounts",
  "/api/journals": "journals",
  "/api/trial-balance": "trialBalance",
  "/api/documents": "documents",
  "/api/banking": "banking",
  "/api/bas": "bas",
  "/api/insights": "insights",
  "/api/capabilities": "capabilities",
};
const writes = {
  "/api/accounts": "createAccount",
  "/api/journals": "postJournal",
  "/api/banking/import": "importBank",
  "/api/banking/reconcile": "reconcile",
  "/api/bas": "createBas",
  "/api/documents/upload": "uploadDocument",
};

function uniqueHeader(req, name) {
  const values = [];
  for (let i = 0; i < req.rawHeaders.length; i += 2) {
    if (req.rawHeaders[i].toLowerCase() === name) values.push(req.rawHeaders[i + 1]);
  }
  return values.length === 1 ? values[0] : undefined;
}
function reply(res, status, body, extra = {}) {
  if (body?.error) body.error.code = errorCode(status, body.error.code);
  res.writeHead(status, {
    "Content-Type": "application/json; charset=utf-8",
    "Cache-Control": "no-store",
    "X-Content-Type-Options": "nosniff",
    ...extra,
  });
  res.end(JSON.stringify(body));
}
function cookieValue(req) {
  const header = uniqueHeader(req, "cookie");
  if (!header) return undefined;
  const values = header
    .split(";")
    .map((part) => part.trim())
    .filter((part) => part.startsWith(`${COOKIE}=`));
  if (values.length !== 1) return undefined;
  const value = values[0].slice(COOKIE.length + 1);
  return /^[A-Za-z0-9_-]{43}$/.test(value) ? value : undefined;
}
function issueCookie(token) {
  return `${COOKIE}=${token}; Path=/; HttpOnly; SameSite=Strict`;
}
function tokenMatches(supplied, expected) {
  if (!expected || typeof supplied !== "string") return false;
  const actual = createHash("sha256").update(supplied).digest();
  const wanted = createHash("sha256").update(expected).digest();
  return timingSafeEqual(actual, wanted);
}
async function jsonBody(req) {
  let size = 0;
  const chunks = [];
  for await (const chunk of req) {
    size += chunk.length;
    if (size > MAX_BODY) throw httpError("Request is too large.", 413);
    chunks.push(chunk);
  }
  try {
    const value = JSON.parse(Buffer.concat(chunks).toString("utf8"));
    if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error();
    return value;
  } catch {
    throw httpError("Enter valid request details.", 400, "invalid_request");
  }
}

/** Local-only gateway. The facade receives authenticated context; it is never returned to the browser. */
export function createGateway({
  facade,
  origin,
  mcpToken,
  staticRoot,
  now: clock = Date.now,
  idleMs = IDLE_MS,
}) {
  const sessions = new Map();
  let active;
  let pendingOwner;
  let cleanup = Promise.resolve();
  function closeSession(token, session) {
    sessions.delete(token);
    if (active === token) active = undefined;
    cleanup = cleanup.then(() => facade.logout(session)).catch(() => {});
    return cleanup;
  }
  const server = createServer(async (req, res) => {
    let currentToken;
    let currentSession;
    try {
      await cleanup;
      const address = server.address();
      const host = address && typeof address === "object" ? `127.0.0.1:${address.port}` : "";
      const method = req.method ?? "";
      if (
        !isTrustedRequest(
          {
            method,
            host: uniqueHeader(req, "host"),
            origin: uniqueHeader(req, "origin"),
            contentType: uniqueHeader(req, "content-type"),
            fetchSite: uniqueHeader(req, "sec-fetch-site"),
          },
          { host, origin },
        )
      ) {
        return reply(res, 403, { error: { message: "Request rejected." } });
      }
      const url = new URL(req.url ?? "/", `http://${host}`);
      const path = url.pathname;
      if (!path.startsWith("/api/") && method === "GET" && staticRoot) {
        const requested = decodeURIComponent(path);
        if (requested.includes("\\") || requested.split("/").includes(".."))
          return reply(res, 404, { error: { message: "Not found." } });
        const file = pathModule.extname(requested) ? requested.slice(1) : "index.html";
        const root = await realpath(staticRoot).catch(() => undefined);
        const target = root && (await realpath(pathModule.join(root, file)).catch(() => undefined));
        if (!root || !target?.startsWith(`${root}${pathModule.sep}`))
          return reply(res, 404, { error: { message: "Not found." } });
        const info = await stat(target);
        if (!info.isFile() || info.size > 16 * 1024 * 1024)
          return reply(res, 404, { error: { message: "Not found." } });
        const types = {
          ".html": "text/html; charset=utf-8",
          ".js": "text/javascript; charset=utf-8",
          ".css": "text/css; charset=utf-8",
          ".svg": "image/svg+xml",
          ".png": "image/png",
          ".woff2": "font/woff2",
        };
        const type = types[pathModule.extname(target)];
        if (!type) return reply(res, 404, { error: { message: "Not found." } });
        const content = await readFile(target);
        res.writeHead(200, {
          "Content-Type": type,
          "X-Content-Type-Options": "nosniff",
          "Content-Security-Policy":
            "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'",
          "X-Frame-Options": "DENY",
          "Cache-Control": "no-cache",
        });
        return res.end(content);
      }
      const now = clock();
      for (const [token, session] of sessions)
        if (now - session.lastUsed > idleMs) {
          if (active === token && session.authentication) await closeSession(token, session);
          else sessions.delete(token);
          if (pendingOwner === token) pendingOwner = undefined;
        }

      if (path.startsWith("/api/mcp/")) {
        const authorization = uniqueHeader(req, "authorization");
        const supplied = authorization?.startsWith("Bearer ") ? authorization.slice(7) : "";
        const names = {
          overview: "overview",
          accounts: "accounts",
          journals: "journals",
          "trial-balance": "trialBalance",
          documents: "documents",
          banking: "banking",
          bas: "bas",
          insights: "insights",
        };
        const operation = names[path.slice("/api/mcp/".length)];
        const session = active && sessions.get(active);
        if (
          method !== "GET" ||
          !operation ||
          !tokenMatches(supplied, mcpToken) ||
          !session?.authentication
        )
          return reply(res, 401, { error: { message: "MCP authorization required." } });
        currentToken = active;
        currentSession = session;
        session.lastUsed = now;
        return reply(res, 200, await facade[operation](session, url.searchParams));
      }

      let token = cookieValue(req);
      let session = token && sessions.get(token);
      currentToken = token;
      currentSession = session;
      if (session) session.lastUsed = now;
      if (method === "GET" && path === "/api/session") {
        let setCookie;
        if (!session) {
          if (sessions.size >= 64) {
            const evict = [...sessions].find(
              ([candidate, value]) => !value.authentication && candidate !== pendingOwner,
            );
            if (!evict) return reply(res, 429, { error: { message: "Too many sessions." } });
            sessions.delete(evict[0]);
          }
          token = randomBytes(32).toString("base64url");
          session = { lastUsed: now };
          sessions.set(token, session);
          setCookie = issueCookie(token);
        }
        const state = session.authentication
          ? { state: "authenticated", organisation: session.organisation }
          : await facade.state();
        return reply(res, 200, state, setCookie ? { "Set-Cookie": setCookie } : {});
      }
      if (!session)
        return reply(res, 401, { error: { message: "Start a browser session first." } });
      if (
        method === "POST" &&
        (path === "/api/workspace/create" ||
          path === "/api/workspace/confirm" ||
          path === "/api/workspace/unlock")
      ) {
        const body = await jsonBody(req);
        const operation = path.endsWith("/create")
          ? "create"
          : path.endsWith("/confirm")
            ? "confirm"
            : "unlock";
        if (operation === "confirm" && pendingOwner && pendingOwner !== token)
          return reply(res, 403, {
            error: { message: "Finish setup in the browser that started it." },
          });
        if (operation === "create" && pendingOwner)
          return reply(res, 409, { error: { message: "Workspace setup is already in progress." } });
        if (operation === "create") pendingOwner = token;
        let result;
        try {
          result = await facade[operation](body);
        } catch (error) {
          if (operation === "create" && pendingOwner === token) pendingOwner = undefined;
          throw error;
        }
        if (!result.authentication) return reply(res, 200, result);
        pendingOwner = undefined;
        sessions.clear();
        token = randomBytes(32).toString("base64url");
        session = {
          lastUsed: now,
          authentication: result.authentication,
          organisation: result.organisation,
        };
        sessions.set(token, session);
        active = token;
        return reply(
          res,
          200,
          { state: "authenticated", organisation: session.organisation },
          { "Set-Cookie": issueCookie(token) },
        );
      }
      if (!session.authentication)
        return reply(res, 401, { error: { message: "Unlock your workspace first." } });
      if (method === "POST" && path === "/api/session/logout") {
        await jsonBody(req);
        await closeSession(token, session);
        return reply(
          res,
          200,
          { state: "locked" },
          { "Set-Cookie": `${COOKIE}=; Path=/; HttpOnly; SameSite=Strict; Max-Age=0` },
        );
      }
      if (method === "GET" && routes[path])
        return reply(res, 200, await facade[routes[path]](session, url.searchParams));
      if (method === "POST") {
        const body = await jsonBody(req);
        if (writes[path]) return reply(res, 200, await facade[writes[path]](session, body));
        const document = path.match(/^\/api\/documents\/([0-9a-f-]+)\/review$/);
        if (document)
          return reply(res, 200, await facade.reviewDocument(session, document[1], body));
        const bank = path.match(/^\/api\/banking\/([0-9a-f-]+)\/match$/);
        if (bank) return reply(res, 200, await facade.matchBank(session, bank[1], body));
      }
      return reply(res, 404, { error: { message: "Endpoint not found." } });
    } catch (error) {
      const status =
        Number.isInteger(error?.status) && error.status >= 400 && error.status < 600
          ? error.status
          : 500;
      if (
        status === 401 &&
        currentSession?.authentication &&
        sessions.get(currentToken) === currentSession
      )
        await closeSession(currentToken, currentSession);
      return reply(res, status, {
        error: {
          code: errorCode(status, error?.code),
          message:
            status < 500 ? error.message : "The accounting service could not complete the request.",
        },
      });
    }
  });
  server.requestTimeout = 10_000;
  server.headersTimeout = 5_000;
  server.keepAliveTimeout = 5_000;
  return server;
}
