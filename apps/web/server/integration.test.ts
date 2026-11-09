import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { startApp } from "./main.ts";

type TestResponse = {
  state: string;
  setupId: string;
  recoveryCode: string | undefined;
  organisation: { name: string };
  account: { id: string };
  journal: { id: string; lines: Array<{ id: string }> };
  journals: Array<{ id: string }>;
  totalDebitsMinor: number;
  totalCreditsMinor: number;
  document: { id: string; status: string; version: string };
  workpaper: {
    status: string;
    gstCredits1bMinor: number;
    salesG1Minor: null;
    gstOnSales1aMinor: null;
    netGstPayableMinor: null;
  };
  lines: Array<{ id: string; status: string; version: string }>;
};

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

test("real core: recovery, balanced journal, replay and restart preserve the ledger", {
  timeout: 120_000,
}, async () => {
  const root = await mkdtemp(path.join(os.tmpdir(), "tammy-web-test-"));
  const origin = "http://127.0.0.1:5173";
  const mcpToken = "local-test-token-at-least-32-characters-long";
  const start = () => startApp({ dataRoot: root, port: 0, origin, mcpToken });
  let app = await start();
  let cookie = "";
  async function call(route: string, body?: unknown) {
    const address = app.server.address();
    assert(address && typeof address === "object");
    const response = await fetch(`http://127.0.0.1:${address.port}${route}`, {
      method: body ? "POST" : "GET",
      headers: {
        ...(cookie ? { cookie } : {}),
        ...(body ? { "content-type": "application/json", origin } : {}),
      },
      ...(body ? { body: JSON.stringify(body) } : {}),
    });
    const setCookie = response.headers.get("set-cookie");
    if (setCookie) cookie = setCookie.split(";")[0] ?? "";
    const value = await response.json();
    assert.equal(response.status, 200, JSON.stringify(value));
    return value as TestResponse;
  }
  try {
    assert.equal((await call("/api/session")).state, "setup");
    const setup = await call("/api/workspace/create", {
      displayName: "Tammy Admin",
      username: "admin@tammy.local",
      workspacePassphrase: "workspace-passphrase-long-enough",
      administratorPassword: "administrator-password-long-enough",
    });
    assert(setup.setupId && setup.recoveryCode);
    assert.equal((await call("/api/session")).state, "recovery");
    await call("/api/workspace/confirm", {
      setupId: setup.setupId,
      recoveryCode: setup.recoveryCode,
      username: "admin@tammy.local",
      administratorPassword: "administrator-password-long-enough",
      businessLegalName: "Tammy Business Pty Ltd",
      businessDisplayName: "Tammy Business",
      abn: "51824753556",
    });
    assert.equal((await call("/api/session")).organisation.name, "Tammy Business");
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
    const currentAddress = app.server.address();
    assert(currentAddress && typeof currentAddress === "object");
    const oversizedMemo = await fetch(`http://127.0.0.1:${currentAddress.port}/api/journals`, {
      method: "POST",
      headers: { cookie, "content-type": "application/json", origin },
      body: JSON.stringify({ ...journal, description: "x".repeat(513) }),
    });
    assert.equal(oversizedMemo.status, 400);
    assert.equal(
      ((await oversizedMemo.json()) as { error: { code: string } }).error.code,
      "invalid_argument",
    );
    const trial = await call("/api/trial-balance?asOfDate=2026-08-10");
    assert.equal(trial.totalDebitsMinor, 31900);
    assert.equal(trial.totalCreditsMinor, 31900);
    const address = app.server.address();
    assert(address && typeof address === "object");
    const transport = new StdioClientTransport({
      command: "mise",
      args: ["exec", "--", "pnpm", "--silent", "--dir", path.resolve("."), "mcp"],
      env: {
        PATH: process.env.PATH ?? "",
        HOME: process.env.HOME ?? "",
        TAMMY_WEB_PORT: String(address.port),
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
    } finally {
      await mcp.close();
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
    const rejected = await fetch(`http://127.0.0.1:${currentAddress.port}/api/documents/upload`, {
      method: "POST",
      headers: { cookie, "content-type": "application/json", origin },
      body: JSON.stringify({
        idempotencyKey: uuid(),
        sourceDisplayName: "large.pdf",
        mimeType: "application/pdf",
        originalBase64: oversizedFile.toString("base64"),
        candidate: {},
      }),
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
    const workpaper = (
      await call("/api/bas", {
        idempotencyKey: uuid(),
        periodStart: "2024-04-01",
        periodEnd: "2024-06-30",
      })
    ).workpaper;
    assert.equal(workpaper.status, "draft_not_lodged");
    assert.equal(workpaper.gstCredits1bMinor, 2900);
    assert.equal(workpaper.salesG1Minor, null);
    assert.equal(workpaper.gstOnSales1aMinor, null);
    assert.equal(workpaper.netGstPayableMinor, null);
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
    cookie = "";
    app = await start();
    assert.equal((await call("/api/session")).state, "locked");
    const reopenedAddress = app.server.address();
    assert(reopenedAddress && typeof reopenedAddress === "object");
    const wrongSignIn = await fetch(
      `http://127.0.0.1:${reopenedAddress.port}/api/workspace/unlock`,
      {
        method: "POST",
        headers: { cookie, "content-type": "application/json", origin },
        body: JSON.stringify({
          workspacePassphrase: "workspace-passphrase-long-enough",
          username: "admin@tammy.local",
          administratorPassword: "incorrect-password-long-enough",
        }),
      },
    );
    assert.equal(wrongSignIn.status, 401);
    await call("/api/workspace/unlock", {
      workspacePassphrase: "workspace-passphrase-long-enough",
      username: "admin@tammy.local",
      administratorPassword: "administrator-password-long-enough",
    });
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
  const origin = "http://127.0.0.1:5173";
  const start = () => startApp({ dataRoot: root, port: 0, origin });
  let app = await start();
  let cookie = "";
  async function call(route: string, body?: unknown) {
    const address = app.server.address();
    assert(address && typeof address === "object");
    const response = await fetch(`http://127.0.0.1:${address.port}${route}`, {
      method: body ? "POST" : "GET",
      headers: {
        ...(cookie ? { cookie } : {}),
        ...(body ? { "content-type": "application/json", origin } : {}),
      },
      ...(body ? { body: JSON.stringify(body) } : {}),
    });
    const set = response.headers.get("set-cookie");
    if (set) cookie = set.split(";")[0] ?? "";
    const value = await response.json();
    assert.equal(response.status, 200, JSON.stringify(value));
    return value as TestResponse;
  }
  try {
    await call("/api/session");
    const created = await call("/api/workspace/create", {
      displayName: "Tammy Admin",
      username: "admin@tammy.local",
      workspacePassphrase: "workspace-passphrase-long-enough",
      administratorPassword: "administrator-password-long-enough",
    });
    await app.close();
    cookie = "";
    app = await start();
    const state = await call("/api/session");
    assert.equal(state.state, "recovery");
    assert.equal(state.setupId, created.setupId);
    assert.equal(state.recoveryCode, undefined);
    const details = {
      setupId: created.setupId,
      recoveryCode: created.recoveryCode,
      workspacePassphrase: "workspace-passphrase-long-enough",
      username: "admin@tammy.local",
      administratorPassword: "administrator-password-long-enough",
      businessLegalName: "Tammy Business Pty Ltd",
      businessDisplayName: "Tammy Business",
      abn: "00000000000",
    };
    const address = app.server.address();
    assert(address && typeof address === "object");
    const failed = await fetch(`http://127.0.0.1:${address.port}/api/workspace/confirm`, {
      method: "POST",
      headers: { cookie, "content-type": "application/json", origin },
      body: JSON.stringify(details),
    });
    assert.equal(failed.status, 400);
    await call("/api/workspace/confirm", { ...details, abn: "51824753556" });
    assert.equal((await call("/api/session")).state, "authenticated");
  } finally {
    await app.close();
    await rm(root, { recursive: true, force: true });
  }
});
