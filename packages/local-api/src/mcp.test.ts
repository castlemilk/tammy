import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test, { type TestContext } from "node:test";
import { fileURLToPath } from "node:url";

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StdioClientTransport } from "@modelcontextprotocol/sdk/client/stdio.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";

import { type LocalApiConnection, startLocalApiBroker } from "./broker.ts";
import { LOCAL_API_READ_RESOURCES, LOCAL_API_READ_TOOLS } from "./catalog.ts";
import type { ValidatedLocalApiRequest } from "./request.ts";

async function connect(t: TestContext, connection: LocalApiConnection) {
  const module = await import("./mcp.ts").catch(() => null);
  assert.ok(module !== null, "the MCP relay is implemented");
  const server = module.createLocalApiMcpServer(connection);
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "tammy-tests", version: "1.0.0" });
  await server.connect(serverTransport);
  await client.connect(clientTransport);
  t.after(async () => {
    await client.close();
    await server.close();
  });
  return client;
}

function targetOf(request: ValidatedLocalApiRequest) {
  return request.operation === "tool.call" ? request.tool.target : request.resource.target;
}

function textData(result: Awaited<ReturnType<Client["callTool"]>>) {
  assert.ok(Array.isArray(result.content));
  const content = result.content[0];
  assert.ok(content !== undefined && content.type === "text" && typeof content.text === "string");
  return JSON.parse(content.text) as unknown;
}

test("SDK initialization and discovery expose exactly the safe read-only catalog", async (t) => {
  const broker = await startLocalApiBroker({ execute: async () => ({}) });
  t.after(() => broker.close());
  const client = await connect(t, broker.connection);
  assert.equal(client.getServerVersion()?.name, "tammy-local-read");
  assert.deepEqual(client.getServerCapabilities(), { resources: {}, tools: {} });
  const { tools } = await client.listTools();
  assert.deepEqual(
    tools.map((tool) => tool.name),
    LOCAL_API_READ_TOOLS.map((tool) => tool.name),
  );
  for (const tool of tools) {
    assert.equal(tool.annotations?.readOnlyHint, true);
    assert.equal(tool.annotations?.destructiveHint, false);
    assert.equal(tool.annotations?.openWorldHint, false);
    assert.deepEqual(tool.inputSchema, {
      type: "object",
      properties: {},
      additionalProperties: false,
    });
  }
  const { resources } = await client.listResources();
  assert.deepEqual(
    resources.map((resource) => resource.uri),
    LOCAL_API_READ_RESOURCES.map((resource) => resource.uri),
  );
});

test("SDK tool calls and resource reads traverse the actual authenticated HTTP broker", async (t) => {
  const received: ValidatedLocalApiRequest[] = [];
  const broker = await startLocalApiBroker({
    execute: async (request) => {
      received.push(request);
      return targetOf(request);
    },
  });
  t.after(() => broker.close());
  const client = await connect(t, broker.connection);
  for (const tool of LOCAL_API_READ_TOOLS) {
    const result = await client.callTool({ name: tool.name, arguments: {} });
    assert.notEqual(result.isError, true);
    assert.deepEqual(textData(result), tool.target);
  }
  for (const resource of LOCAL_API_READ_RESOURCES) {
    const result = await client.readResource({ uri: resource.uri });
    assert.deepEqual(result.contents, [
      { uri: resource.uri, mimeType: "application/json", text: JSON.stringify(resource.target) },
    ]);
  }
  assert.equal(received.length, 21);
});

test("MCP rejects nonempty arguments and unknown/write reads without executing them", async (t) => {
  let executions = 0;
  const broker = await startLocalApiBroker({
    execute: async () => {
      executions++;
      return {};
    },
  });
  t.after(() => broker.close());
  const client = await connect(t, broker.connection);
  for (const [name, args, code] of [
    ["tammy.accounting.list_accounts", { organisationId: "secret-other" }, "INVALID_ARGUMENTS"],
    ["tammy.accounting.create_journal", {}, "UNKNOWN_TOOL"],
    ["secret-unknown-tool", {}, "UNKNOWN_TOOL"],
  ] as const) {
    const result = await client.callTool({ name, arguments: args });
    assert.equal(result.isError, true);
    assert.deepEqual(textData(result), { ok: false, error: { code } });
  }
  await assert.rejects(client.readResource({ uri: "tammy://secrets" }), /UNKNOWN_RESOURCE/);
  assert.equal(executions, 0);
});

test("MCP handles pairing denial, wrong capabilities, broker revocation and sanitized errors", async (t) => {
  let message = "credential-secret database password";
  const broker = await startLocalApiBroker({
    execute: async () => {
      throw new Error(message);
    },
  });
  t.after(() => broker.close());
  const client = await connect(t, broker.connection);
  for (const code of ["LOCAL_READ_FAILED", "LOCAL_PAIRING_REVOKED", "LOCAL_SESSION_REQUIRED"]) {
    const result = await client.callTool({ name: LOCAL_API_READ_TOOLS[0].name, arguments: {} });
    assert.deepEqual(textData(result), { ok: false, error: { code } });
    message = code === "LOCAL_READ_FAILED" ? "LOCAL_PAIRING_REVOKED" : "LOCAL_SESSION_REQUIRED";
  }
  const wrong = await connect(t, { ...broker.connection, capability: "A".repeat(43) });
  await assert.rejects(wrong.listTools(), /UNAUTHORIZED/);
  await broker.close();
  await assert.rejects(client.listResources(), /LOCAL_BROKER_UNAVAILABLE/);
  const result = await client.callTool({ name: LOCAL_API_READ_TOOLS[0].name, arguments: {} });
  assert.deepEqual(textData(result), { ok: false, error: { code: "LOCAL_BROKER_UNAVAILABLE" } });
});

test("relay bounds untrusted broker responses and never follows redirects with the capability", async (t) => {
  let mode = "redirect";
  let requests = 0;
  const server = createServer((_request, response) => {
    requests++;
    if (mode === "redirect") {
      response.writeHead(302, { Location: "/stolen-capability" });
      response.end();
    } else if (mode === "large") {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end("x".repeat(1024 * 1024 + 1));
    } else if (mode === "secret") {
      response.writeHead(403, { "Content-Type": "application/json" });
      response.end(JSON.stringify({ ok: false, error: { code: "secret password" } }));
    } else {
      response.writeHead(200, { "Content-Type": "application/json" });
      response.end("{private invalid-json");
    }
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(
    () =>
      new Promise<void>((resolve) => {
        server.close(() => resolve());
        server.closeAllConnections();
      }),
  );
  const address = server.address();
  assert.ok(address !== null && typeof address !== "string");
  const client = await connect(t, {
    version: "v1",
    origin: `http://127.0.0.1:${address.port}`,
    capability: "A".repeat(43),
  });
  for (const next of ["redirect", "large", "secret", "invalid-json"]) {
    mode = next;
    const result = await client.callTool({ name: LOCAL_API_READ_TOOLS[0].name, arguments: {} });
    assert.equal(result.isError, true);
    assert.deepEqual(textData(result), {
      ok: false,
      error: { code: "LOCAL_BROKER_INVALID_RESPONSE" },
    });
  }
  assert.equal(requests, 4);
});

test("spawned stdio CLI supports SDK initialize/list/call/read end to end and no stdout logging", async (t) => {
  const broker = await startLocalApiBroker({
    execute: async (request) => ({ target: targetOf(request), source: "stdio-e2e" }),
  });
  t.after(() => broker.close());
  const directory = await mkdtemp(join(tmpdir(), "tammy-mcp-"));
  t.after(() => rm(directory, { recursive: true, force: true }));
  const connectionFile = join(directory, "connection.json");
  await writeFile(connectionFile, JSON.stringify(broker.connection), { mode: 0o600 });
  const transport = new StdioClientTransport({
    args: [fileURLToPath(new URL("./cli.ts", import.meta.url)), "--connection", connectionFile],
    command: process.execPath,
    stderr: "pipe",
  });
  let stderr = "";
  transport.stderr?.on("data", (data: Buffer) => {
    stderr += data.toString();
  });
  const protocolErrors: Error[] = [];
  const client = new Client({ name: "tammy-stdio-e2e", version: "1.0.0" });
  client.onerror = (error) => protocolErrors.push(error);
  await client.connect(transport);
  t.after(() => client.close());
  assert.ok(transport.pid !== null);
  assert.equal((await client.listTools()).tools.length, 12);
  assert.equal((await client.listResources()).resources.length, 9);
  assert.deepEqual(
    textData(await client.callTool({ name: "tammy.accounting.list_accounts", arguments: {} })),
    {
      target: { module: "accounting", procedure: "listAccounts" },
      source: "stdio-e2e",
    },
  );
  assert.deepEqual((await client.readResource({ uri: "tammy://current/organisation" })).contents, [
    {
      uri: "tammy://current/organisation",
      mimeType: "application/json",
      text: JSON.stringify({
        target: { module: "organisation", procedure: "getOrganisation" },
        source: "stdio-e2e",
      }),
    },
  ]);
  await broker.close();
  assert.deepEqual(
    textData(await client.callTool({ name: "tammy.accounting.list_accounts", arguments: {} })),
    {
      ok: false,
      error: { code: "LOCAL_BROKER_UNAVAILABLE" },
    },
  );
  await client.close();
  assert.deepEqual(protocolErrors, []);
  assert.equal(stderr, "");
});

test("CLI rejects invalid options and unsafe files without echoing secrets to either stream", async () => {
  const cli = fileURLToPath(new URL("./cli.ts", import.meta.url));
  for (const args of [
    [],
    ["--connection", "relative-secret.json"],
    ["--token", "private-secret-token"],
  ]) {
    const result = await new Promise<{ code: number | null; stderr: string; stdout: string }>(
      (resolve, reject) => {
        const child = spawn(process.execPath, [cli, ...args], {
          stdio: ["ignore", "pipe", "pipe"],
        });
        let stderr = "";
        let stdout = "";
        child.stderr.on("data", (data: Buffer) => {
          stderr += data.toString();
        });
        child.stdout.on("data", (data: Buffer) => {
          stdout += data.toString();
        });
        child.on("error", reject);
        child.on("exit", (code) => resolve({ code, stderr, stdout }));
      },
    );
    assert.equal(result.code, 1);
    assert.equal(result.stdout, "");
    assert.match(result.stderr, /^(INVALID_CLI_ARGUMENTS|INVALID_CONNECTION_FILE)\n$/);
  }
});
