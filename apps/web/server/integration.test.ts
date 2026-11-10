import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtemp, readFile, rm, stat } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { fileURLToPath } from "node:url";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { startApp } from "./main.ts";

type TestResponse = {
  state: string;
  setupId: string;
  recoveryCode: string | undefined;
  expiresAt: string | undefined;
  organisation: { name: string };
  account: { id: string };
  journal: { id: string; lines: Array<{ id: string }> };
  journals: Array<{ id: string }>;
  totalDebitsMinor: number;
  totalCreditsMinor: number;
  document: { id: string; status: string; version: string };
  basStatus: string;
  workpaper: {
    status: string;
    gstCredits1bMinor: number;
    candidateGstMinor: number | null;
    salesG1Minor: null;
    gstOnSales1aMinor: null;
    netGstPayableMinor: null;
    sourcesChanged: boolean;
    reviewWarnings: string[];
    limitations: string[];
    sources: unknown[];
    candidates: Array<{
      id: string;
      date: string | null;
      supplierName: string;
      candidateGstMinor: number;
    }>;
  };
  lines: Array<{ id: string; status: string; version: string }>;
  reporting: { taxRuleBundle: { id: string; revision: string; contentHash: string } | null };
  error: { code: string; message: string };
};
type App = Awaited<ReturnType<typeof startApp>>;

const webRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const origin = "http://127.0.0.1:5173";
const mcpToken = "local-test-token-at-least-32-characters-long";
const credentials = {
  workspacePassphrase: "workspace-passphrase-long-enough",
  username: "admin@tammy.local",
  administratorPassword: "administrator-password-long-enough",
};
const setupDetails = { displayName: "Tammy Admin", ...credentials };
const business = {
  businessLegalName: "Tammy Business Pty Ltd",
  businessDisplayName: "Tammy Business",
  abn: "51824753556",
};
const credentialsRejected =
  "The workspace passphrase, username or administrator password is incorrect. After 5 failed attempts, wait 15 minutes before trying again.";

function uuid() {
  const bytes = randomBytes(16);
  let now = Date.now();
  for (let i = 5; i >= 0; i--) {
    bytes[i] = now & 255;
    now = Math.floor(now / 256);
  }
  bytes[6] = ((bytes[6] ?? 0) & 15) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 63) | 0x80;
  const h = bytes.toString("hex");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}

function portOf(app: App) {
  const address = app.server.address();
  assert(address && typeof address === "object");
  return address.port;
}

/** One browser: its own session cookie against whichever app instance is current. */
function browser(current: () => App) {
  let cookie = "";
  async function raw(route: string, body?: unknown) {
    const response = await fetch(`http://127.0.0.1:${portOf(current())}${route}`, {
      method: body ? "POST" : "GET",
      headers: {
        ...(cookie ? { cookie } : {}),
        ...(body ? { "content-type": "application/json", origin } : {}),
      },
      ...(body ? { body: JSON.stringify(body) } : {}),
    });
    const setCookie = response.headers.get("set-cookie");
    if (setCookie) cookie = setCookie.split(";")[0] ?? "";
    return { status: response.status, body: (await response.json()) as TestResponse };
  }
  return {
    raw,
    call: async (route: string, body?: unknown) => {
      const { status, body: value } = await raw(route, body);
      assert.equal(status, 200, JSON.stringify(value));
      return value;
    },
    forget: () => {
      cookie = "";
    },
  };
}

function mcpRead(app: App, route: string) {
  return fetch(`http://127.0.0.1:${portOf(app)}/api/mcp/${route}`, {
    headers: { authorization: `Bearer ${mcpToken}` },
  });
}

/** A code that cannot be the one the core issued. */
function wrongCode(recoveryCode: string | undefined) {
  return recoveryCode?.startsWith("AAAA") ? "BBBB-BBBB-BBBB" : "AAAA-AAAA-AAAA";
}

test("real core: recovery, balanced journal, replay and restart preserve the ledger", {
  timeout: 180_000,
}, async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-web-test-"));
  const start = () => startApp({ dataRoot: root, port: 0, origin, mcpToken });
  let app = await start();
  const page = browser(() => app);
  const { call } = page;
  try {
    assert.equal((await call("/api/session")).state, "setup");
    const setup = await call("/api/workspace/create", setupDetails);
    assert(setup.setupId && setup.recoveryCode);
    assert.equal((await call("/api/session")).state, "recovery");
    await call("/api/workspace/confirm", {
      setupId: setup.setupId,
      recoveryCode: setup.recoveryCode,
      username: credentials.username,
      administratorPassword: credentials.administratorPassword,
      ...business,
    });
    assert.equal((await call("/api/session")).organisation.name, "Tammy Business");
    // The organisation records the tax rules bundled with this core, not the known legacy hash.
    const bundledHash = (
      await readFile(
        path.resolve(webRoot, "../../services/core/internal/artefacts/bundles/au_gst_v1.sha256"),
        "utf8",
      )
    ).trim();
    assert.equal(
      (await call("/api/capabilities")).reporting.taxRuleBundle?.contentHash,
      bundledHash,
    );
    const secondWorkspace = await page.raw("/api/workspace/create", {});
    assert.equal(secondWorkspace.status, 409);
    assert.deepEqual(secondWorkspace.body, {
      error: { code: "conflict", message: "A workspace already exists." },
    });
    const expense = (
      await call("/api/accounts", {
        idempotencyKey: uuid(),
        code: "6100",
        name: "Office expenses",
        type: "expense",
      })
    ).account;
    const equity = (
      await call("/api/accounts", {
        idempotencyKey: uuid(),
        code: "3100",
        name: "Owner contributions",
        type: "equity",
      })
    ).account;
    const inherited = await page.raw("/api/accounts", {
      idempotencyKey: uuid(),
      code: "6200",
      name: "Inherited key",
      type: "constructor",
    });
    assert.equal(inherited.status, 400);
    assert.deepEqual(inherited.body, {
      error: { code: "invalid_argument", message: "Choose an account type." },
    });
    const journal = {
      idempotencyKey: uuid(),
      date: "2026-08-10",
      description: "Office supplies",
      lines: [
        { accountId: expense.id, description: "Supplies", debitMinor: 31900, creditMinor: 0 },
        { accountId: equity.id, description: "Owner", debitMinor: 0, creditMinor: 31900 },
      ],
    };
    const posted = await call("/api/journals", journal);
    const replayed = await call("/api/journals", journal);
    assert.equal(posted.journal.id, replayed.journal.id);
    assert.match(
      posted.journal.lines[0]?.id ?? "",
      /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    assert.equal(posted.journal.lines[0]?.id, replayed.journal.lines[0]?.id);
    const oversizedMemo = await page.raw("/api/journals", {
      ...journal,
      description: "x".repeat(513),
    });
    assert.equal(oversizedMemo.status, 400);
    assert.equal(oversizedMemo.body.error.code, "invalid_argument");
    const oversizedBody = await page.raw("/api/journals", {
      ...journal,
      description: "x".repeat(768 * 1024),
    });
    assert.equal(oversizedBody.status, 413);
    assert.equal(oversizedBody.body.error.code, "request_too_large");
    const trial = await call("/api/trial-balance?asOfDate=2026-08-10");
    assert.equal(trial.totalDebitsMinor, 31900);
    assert.equal(trial.totalCreditsMinor, 31900);
    // CI has no mise: the adapter runs on this Node binary with tsx, in apps/web.
    const transport = new StdioClientTransport({
      command: process.execPath,
      args: ["--import", "tsx", "server/mcp.ts"],
      cwd: webRoot,
      env: {
        PATH: process.env.PATH ?? "",
        HOME: process.env.HOME ?? "",
        TMPDIR: os.tmpdir(),
        TAMMY_WEB_PORT: String(portOf(app)),
        TAMMY_MCP_TOKEN: mcpToken,
      },
    });
    const mcp = new Client({ name: "tammy-test", version: "1.0.0" });
    await mcp.connect(transport);
    try {
      const tools = await mcp.listTools();
      assert(tools.tools.some((tool) => tool.name === "list_journals"));
      const result = await mcp.callTool({ name: "list_journals", arguments: {} });
      assert.match(JSON.stringify(result), new RegExp(posted.journal.id));
      // A rejected request is reported with the gateway's code and message.
      assert.deepEqual(
        await mcp.callTool({ name: "get_trial_balance", arguments: { asOfDate: "2024-02-30" } }),
        {
          isError: true,
          content: [
            { type: "text", text: "invalid_argument: Use a valid date in YYYY-MM-DD format." },
          ],
        },
      );
    } finally {
      await mcp.close();
    }
    async function reviewedDocument(supplierName: string, date: string | undefined, gst: number) {
      const uploaded = (
        await call("/api/documents/upload", {
          idempotencyKey: uuid(),
          sourceDisplayName: `${supplierName}.pdf`,
          mimeType: "application/pdf",
          originalBase64: Buffer.from(`%PDF-1.4\n${supplierName}\n%%EOF`).toString("base64"),
          extractedText: supplierName,
          candidate: {},
        })
      ).document;
      return (
        await call(`/api/documents/${uploaded.id}/review`, {
          idempotencyKey: uuid(),
          version: uploaded.version,
          supplierName,
          invoiceNumber: supplierName,
          ...(date ? { date } : {}),
          subtotalMinor: gst * 10,
          gstMinor: gst,
          totalMinor: gst * 11,
        })
      ).document;
    }
    const document = (
      await call("/api/documents/upload", {
        idempotencyKey: uuid(),
        sourceDisplayName: "receipt.pdf",
        mimeType: "application/pdf",
        originalBase64: Buffer.from("%PDF-1.4\nReceipt\n%%EOF").toString("base64"),
        extractedText: "Receipt",
        candidate: {
          supplierName: "Officeworks",
          invoiceNumber: "INV-1",
          date: "2024-05-12",
          subtotalMinor: 29000,
          gstMinor: 2900,
          totalMinor: 31900,
        },
      })
    ).document;
    assert.equal(document.status, "needs_review");
    const maximumFile = Buffer.alloc(512 * 1024);
    maximumFile.write("%PDF-");
    assert.equal(
      (
        await call("/api/documents/upload", {
          idempotencyKey: uuid(),
          sourceDisplayName: "maximum.pdf",
          mimeType: "application/pdf",
          originalBase64: maximumFile.toString("base64"),
          candidate: {},
        })
      ).document.status,
      "needs_review",
    );
    const oversizedFile = Buffer.alloc(512 * 1024 + 1);
    oversizedFile.write("%PDF-");
    const rejected = await page.raw("/api/documents/upload", {
      idempotencyKey: uuid(),
      sourceDisplayName: "large.pdf",
      mimeType: "application/pdf",
      originalBase64: oversizedFile.toString("base64"),
      candidate: {},
    });
    assert.equal(rejected.status, 400);
    const reviewed = (
      await call(`/api/documents/${document.id}/review`, {
        idempotencyKey: uuid(),
        version: document.version,
        supplierName: "Officeworks",
        invoiceNumber: "INV-1",
        date: "2024-05-12",
        subtotalMinor: 29000,
        gstMinor: 2900,
        totalMinor: 31900,
      })
    ).document;
    assert.equal(reviewed.status, "reviewed");
    const staleReview = await page.raw(`/api/documents/${document.id}/review`, {
      idempotencyKey: uuid(),
      version: document.version,
      supplierName: "Officeworks",
      invoiceNumber: "INV-1",
      date: "2024-05-12",
      subtotalMinor: 29000,
      gstMinor: 2900,
      totalMinor: 31900,
    });
    assert.equal(staleReview.status, 409);
    assert.equal(staleReview.body.error.code, "conflict");
    // Candidates are every reviewed document not posted as a purchase, whatever its date.
    const nextYear = await reviewedDocument("NextYear", "2025-09-01", 100000);
    const undated = await reviewedDocument("Undated", undefined, 50);
    const workpaper = (
      await call("/api/bas", {
        idempotencyKey: uuid(),
        periodStart: "2024-04-01",
        periodEnd: "2024-06-30",
      })
    ).workpaper;
    assert.equal(workpaper.status, "draft_not_lodged");
    // The core counts only posted purchases in 1B; a reviewed document is a provisional candidate.
    assert.equal(workpaper.gstCredits1bMinor, 0);
    assert.equal(workpaper.candidateGstMinor, 102950);
    assert.deepEqual(workpaper.sources, []);
    assert.deepEqual(
      workpaper.candidates
        .map(({ id, date, supplierName, candidateGstMinor }) => ({
          id,
          date,
          supplierName,
          candidateGstMinor,
        }))
        .sort((left, right) => left.candidateGstMinor - right.candidateGstMinor),
      [
        { id: undated.id, date: null, supplierName: "Undated", candidateGstMinor: 50 },
        {
          id: document.id,
          date: "2024-05-12",
          supplierName: "Officeworks",
          candidateGstMinor: 2900,
        },
        {
          id: nextYear.id,
          date: "2025-09-01",
          supplierName: "NextYear",
          candidateGstMinor: 100000,
        },
      ],
    );
    assert.match(
      workpaper.limitations.join(" "),
      /not yet posted as purchases, whatever their date/,
    );
    assert.equal(workpaper.salesG1Minor, null);
    assert.equal(workpaper.gstOnSales1aMinor, null);
    assert.equal(workpaper.netGstPayableMinor, null);
    assert.equal(workpaper.sourcesChanged, false);
    assert.match(workpaper.reviewWarnings.join(" "), /legacy candidates/);
    // The overview reports the core's BAS preparation status, which the web cannot advance.
    assert.equal((await call("/api/overview")).basStatus, "action_needed");
    // A document reviewed after the draft makes the retained draft stale.
    await reviewedDocument("AfterDraft", "2024-06-01", 700);
    const stale = (await call("/api/bas")).workpaper;
    assert.equal(stale.sourcesChanged, true);
    assert.equal(stale.candidateGstMinor, 102950);
    await call("/api/banking/import", {
      idempotencyKey: uuid(),
      openingBalanceMinor: 100000,
      lines: [{ date: "2024-05-12", description: "Officeworks", amountMinor: -31900 }],
    });
    const bank = (await call("/api/banking")).lines[0];
    assert(bank);
    assert.equal(bank.status, "unmatched");
    await call(`/api/banking/${bank.id}/match`, {
      idempotencyKey: uuid(),
      version: bank.version,
      reference: "Reviewed receipt",
    });
    await call("/api/banking/reconcile", { idempotencyKey: uuid() });
    assert.equal((await call("/api/banking")).lines[0]?.status, "reconciled");
    await app.close();
    page.forget();
    app = await start();
    assert.equal((await call("/api/session")).state, "locked");
    const wrongSignIn = await page.raw("/api/workspace/unlock", {
      ...credentials,
      administratorPassword: "incorrect-password-long-enough",
    });
    assert.equal(wrongSignIn.status, 401);
    assert.deepEqual(wrongSignIn.body.error, {
      code: "unauthenticated",
      message: credentialsRejected,
    });
    await call("/api/workspace/unlock", credentials);
    assert.equal((await call("/api/journals")).journals[0]?.id, posted.journal.id);
    assert.equal((await call("/api/trial-balance?asOfDate=2026-08-10")).totalDebitsMinor, 31900);
  } finally {
    await app.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("pending setup survives a server restart without displaying the recovery code again", {
  timeout: 120_000,
}, async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-web-recovery-"));
  const start = () => startApp({ dataRoot: root, port: 0, origin });
  let app = await start();
  const page = browser(() => app);
  const { call } = page;
  try {
    await call("/api/session");
    const created = await call("/api/workspace/create", setupDetails);
    // The core gives a pending setup 15 minutes to be confirmed.
    const deadline = Date.parse(created.expiresAt ?? "");
    assert(deadline > Date.now() + 14 * 60_000 && deadline <= Date.now() + 15 * 60_000);
    await app.close();
    page.forget();
    app = await start();
    const state = await call("/api/session");
    assert.equal(state.state, "recovery");
    assert.equal(state.setupId, created.setupId);
    assert.equal(state.expiresAt, created.expiresAt);
    assert.equal(state.recoveryCode, undefined);
    const details = {
      setupId: created.setupId,
      recoveryCode: created.recoveryCode,
      ...credentials,
      ...business,
      abn: "00000000000",
    };
    const failed = await page.raw("/api/workspace/confirm", details);
    assert.equal(failed.status, 400);
    await call("/api/workspace/confirm", { ...details, abn: "51824753556" });
    assert.equal((await call("/api/session")).state, "authenticated");
  } finally {
    await app.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("real core: five incorrect recovery codes discard the setup so setup can start again", {
  timeout: 120_000,
}, async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-web-attempts-"));
  const app = await startApp({ dataRoot: root, port: 0, origin });
  const page = browser(() => app);
  try {
    await page.call("/api/session");
    const created = await page.call("/api/workspace/create", setupDetails);
    const attempt = () =>
      page.raw("/api/workspace/confirm", {
        setupId: created.setupId,
        recoveryCode: wrongCode(created.recoveryCode),
        ...credentials,
        ...business,
      });
    for (const remaining of ["4 attempts remain", "3 attempts remain", "2 attempts remain"]) {
      const wrong = await attempt();
      assert.equal(wrong.status, 400);
      assert.deepEqual(wrong.body.error, {
        code: "invalid_argument",
        message: `The recovery code does not match. ${remaining} before this setup is discarded.`,
      });
    }
    assert.equal((await attempt()).status, 400);
    const fifth = await attempt();
    assert.equal(fifth.status, 409);
    assert.deepEqual(fifth.body.error, {
      code: "conflict",
      message: "This setup was discarded after 5 incorrect recovery codes. Start setup again.",
    });
    assert.deepEqual(await page.call("/api/session"), { state: "setup" });
    await assert.rejects(stat(path.join(root, "web-workspace.json")), { code: "ENOENT" });
    const again = await page.call("/api/workspace/create", setupDetails);
    assert.notEqual(again.setupId, created.setupId);
    await page.call("/api/workspace/confirm", {
      setupId: again.setupId,
      recoveryCode: again.recoveryCode,
      ...credentials,
      ...business,
    });
    assert.equal((await page.call("/api/session")).state, "authenticated");
  } finally {
    await app.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("real core: incorrect codes counted before a restart still discard the setup cleanly", {
  timeout: 120_000,
}, async () => {
  // The core's attempt journal restarts empty with the web server, so after this restart the core
  // has seen only two failures when the web discards the setup. The web makes sure the core
  // discards it too; otherwise the new setup would find the old workspace files.
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-web-restart-attempts-"));
  const start = () => startApp({ dataRoot: root, port: 0, origin });
  let app = await start();
  const page = browser(() => app);
  try {
    await page.call("/api/session");
    const created = await page.call("/api/workspace/create", setupDetails);
    const attempt = () =>
      page.raw("/api/workspace/confirm", {
        setupId: created.setupId,
        recoveryCode: wrongCode(created.recoveryCode),
        ...credentials,
        ...business,
      });
    for (let count = 0; count < 3; count++) assert.equal((await attempt()).status, 400);
    await app.close();
    page.forget();
    app = await start();
    assert.equal((await page.call("/api/session")).state, "recovery");
    const fourth = await attempt();
    assert.equal(fourth.status, 400);
    assert.match(fourth.body.error.message, /1 attempt remains/);
    assert.equal((await attempt()).status, 409);
    assert.deepEqual(await page.call("/api/session"), { state: "setup" });
    assert((await page.call("/api/workspace/create", setupDetails)).recoveryCode);
  } finally {
    await app.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("real core: a newer browser takes over only with the right credentials", {
  timeout: 180_000,
}, async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-web-takeover-"));
  const app = await startApp({ dataRoot: root, port: 0, origin, mcpToken });
  const a = browser(() => app);
  const b = browser(() => app);
  try {
    await a.call("/api/session");
    const created = await a.call("/api/workspace/create", setupDetails);
    await a.call("/api/workspace/confirm", {
      setupId: created.setupId,
      recoveryCode: created.recoveryCode,
      ...credentials,
      ...business,
    });
    assert.equal((await mcpRead(app, "accounts")).status, 200);
    // Browser B: another browser, a private window, or the same browser after losing its cookie.
    assert.deepEqual(await b.call("/api/session"), { state: "locked" });
    const takeover = await b.call("/api/workspace/unlock", credentials);
    assert.equal(takeover.state, "authenticated");
    assert.equal((await a.raw("/api/accounts")).status, 401);
    assert.equal((await b.raw("/api/accounts")).status, 200);
    assert.equal((await mcpRead(app, "accounts")).status, 200);
    // Attempts without the right credentials leave B and MCP signed in.
    assert.deepEqual(await a.call("/api/session"), { state: "locked" });
    assert.equal((await a.raw("/api/workspace/unlock", {})).status, 400);
    const wrongPassword = {
      ...credentials,
      administratorPassword: "incorrect-password-long-enough",
    };
    for (let attempt = 0; attempt < 3; attempt++) {
      const wrong = await a.raw("/api/workspace/unlock", wrongPassword);
      assert.equal(wrong.status, 401);
      assert.deepEqual(wrong.body.error, { code: "unauthenticated", message: credentialsRejected });
      assert.equal((await b.raw("/api/accounts")).status, 200);
    }
    // The fourth takeover is refused before it reaches the core, which would lock the
    // administrator account, and B's session with it, at five failures.
    const limited = await a.raw("/api/workspace/unlock", credentials);
    assert.equal(limited.status, 429);
    assert.deepEqual(limited.body.error, {
      code: "resource_exhausted",
      message: "Too many failed attempts. Wait 15 minutes before trying again.",
    });
    assert.equal((await b.raw("/api/accounts")).status, 200);
    assert.equal((await mcpRead(app, "accounts")).status, 200);
    // The limit applies only to takeovers: once B locks, A unlocks as usual.
    await b.call("/api/session/logout", {});
    assert.equal((await mcpRead(app, "accounts")).status, 401);
    await a.call("/api/workspace/unlock", credentials);
    assert.equal((await a.raw("/api/accounts")).status, 200);
    assert.equal((await mcpRead(app, "accounts")).status, 200);
  } finally {
    await app.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("real core: setup leaves no workspace file behind a rejected password and reports one it finds", {
  timeout: 120_000,
}, async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-web-orphan-"));
  const workspaceFile = path.join(root, "core", "workspace", "tammy-workspace.db");
  const start = () => startApp({ dataRoot: root, port: 0, origin });
  let app = await start();
  const page = browser(() => app);
  try {
    await page.call("/api/session");
    // The core checks the administrator password only after reserving the setup's files.
    const rejected = await page.raw("/api/workspace/create", {
      ...setupDetails,
      administratorPassword: "x".repeat(129),
    });
    assert.equal(rejected.status, 400);
    await assert.rejects(stat(workspaceFile), { code: "ENOENT" });
    await page.call("/api/workspace/create", setupDetails);
    await stat(workspaceFile);
    // The server loses its setup record, as when the core finishes a create after it timed out.
    await app.close();
    await rm(path.join(root, "web-workspace.json"));
    page.forget();
    app = await start();
    assert.deepEqual(await page.call("/api/session"), { state: "setup" });
    const refused = await page.raw("/api/workspace/create", setupDetails);
    assert.equal(refused.status, 409);
    assert.deepEqual(refused.body.error, {
      code: "conflict",
      message:
        "This data folder already holds a workspace that this web server has no record of. Stop the server, move the folder aside or set TAMMY_WEB_DATA_ROOT to a new folder, then start again.",
    });
  } finally {
    await app.close();
    await rm(root, { recursive: true, force: true });
  }
});

test("real core: the gateway stops serving when the core exits", { timeout: 60_000 }, async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-web-core-exit-"));
  let exited: (code: string) => void = () => undefined;
  const stopped = new Promise<string>((resolve) => {
    exited = resolve;
  });
  const app = await startApp({
    dataRoot: root,
    port: 0,
    origin,
    onCoreExit: (code) => exited(code),
  });
  try {
    assert.equal((await browser(() => app).call("/api/session")).state, "setup");
    const children = execFileSync("ps", ["-A", "-o", "pid=,ppid=,comm="], { encoding: "utf8" })
      .split("\n")
      .map((row) => row.trim().split(/\s+/))
      .filter(
        ([, parent, command]) => Number(parent) === process.pid && command?.endsWith("tammy-core"),
      );
    assert.equal(children.length, 1);
    process.kill(Number(children[0]?.[0]), "SIGKILL");
    assert.equal(await stopped, "CORE_EXITED");
    assert.equal(app.server.listening, false);
  } finally {
    await app.close();
    await rm(root, { recursive: true, force: true });
  }
});
