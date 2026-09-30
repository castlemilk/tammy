import { spawn } from "node:child_process";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../../../..");

export async function callPairedApi(connectionPath: string, name: string) {
  const { capability, origin } = JSON.parse(await readFile(connectionPath, "utf8"));
  const response = await fetch(`${origin}/v1/read`, {
    body: JSON.stringify({ version: "v1", operation: "tool.call", name }),
    headers: { authorization: `Bearer ${capability}`, "content-type": "application/json" },
    method: "POST",
    signal: AbortSignal.timeout(7_000),
  });
  return response.json();
}

export async function runPairedMcpSmoke(connectionPath: string) {
  const child = spawn(
    process.execPath,
    [path.join(root, "packages/local-api/src/cli.ts"), "--connection", connectionPath],
    { stdio: ["pipe", "pipe", "pipe"] },
  );
  const lines = createInterface({ input: child.stdout });
  const pending = new Map<
    number,
    { resolve(value: Record<string, unknown>): void; reject(error: Error): void }
  >();
  let id = 0;
  let exited = false;
  child.stderr.resume();
  const rejectAll = () => {
    for (const entry of pending.values()) entry.reject(new Error("PAIRED_MCP_EXITED"));
    pending.clear();
  };
  child.once("error", rejectAll);
  child.once("close", () => {
    exited = true;
    rejectAll();
  });
  lines.on("line", (line) => {
    try {
      const message = JSON.parse(line);
      if (typeof message.id === "number") {
        const entry = pending.get(message.id);
        pending.delete(message.id);
        if (message.error) entry?.reject(new Error("PAIRED_MCP_PROTOCOL_ERROR"));
        else entry?.resolve(message.result);
      }
    } catch {
      rejectAll();
    }
  });
  const request = (method: string, params: unknown = {}) => {
    const requestId = ++id;
    return new Promise<Record<string, unknown>>((resolve, reject) => {
      const timer = setTimeout(() => {
        pending.delete(requestId);
        reject(new Error("PAIRED_MCP_TIMEOUT"));
      }, 7_000);
      pending.set(requestId, {
        resolve: (value) => {
          clearTimeout(timer);
          resolve(value);
        },
        reject: (error) => {
          clearTimeout(timer);
          reject(error);
        },
      });
      child.stdin.write(`${JSON.stringify({ jsonrpc: "2.0", id: requestId, method, params })}\n`);
    });
  };
  try {
    const initialized = await request("initialize", {
      protocolVersion: "2025-11-25",
      capabilities: {},
      clientInfo: { name: "Tammy packaged acceptance", version: "1.0.0" },
    });
    child.stdin.write(
      `${JSON.stringify({ jsonrpc: "2.0", method: "notifications/initialized" })}\n`,
    );
    const tools = await request("tools/list");
    const result = await request("tools/call", {
      name: "tammy.accounting.list_accounts",
      arguments: {},
    });
    const resource = await request("resources/read", { uri: "tammy://current/organisation" });
    return { initialized, tools, result, resource };
  } finally {
    lines.close();
    const closed = exited
      ? Promise.resolve()
      : new Promise<void>((resolve) => child.once("close", () => resolve()));
    child.stdin.end();
    const timer = setTimeout(() => child.kill("SIGKILL"), 3_000);
    await closed;
    clearTimeout(timer);
  }
}
