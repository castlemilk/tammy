import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { z } from "zod";

const token = process.env.TAMMY_MCP_TOKEN;
const port = Number(process.env.TAMMY_WEB_PORT ?? "4174");
if (!token || token.length < 32 || !Number.isInteger(port) || port < 1 || port > 65535) {
  process.stderr.write(
    "Set a 32-character or longer TAMMY_MCP_TOKEN shared with the local Tammy web server.\n",
  );
  process.exit(1);
}
const server = new McpServer({ name: "tammy-local", version: "0.1.0" });
const annotations = { readOnlyHint: true, destructiveHint: false, openWorldHint: false };
function failure(text: string) {
  return { isError: true, content: [{ type: "text" as const, text }] };
}
/** The gateway's client-safe `{ error: { code, message } }` body, when it sent one. */
function gatewayError(body: unknown) {
  if (typeof body !== "object" || body === null || !("error" in body)) return undefined;
  const error = body.error;
  if (typeof error !== "object" || error === null) return undefined;
  const code = "code" in error ? error.code : undefined;
  const message = "message" in error ? error.message : undefined;
  return typeof code === "string" && typeof message === "string" ? { code, message } : undefined;
}
async function read(route: string, query = "") {
  let response: Response;
  let body: unknown;
  try {
    response = await fetch(`http://127.0.0.1:${port}/api/mcp/${route}${query}`, {
      headers: { Authorization: `Bearer ${token}` },
      signal: AbortSignal.timeout(5000),
    });
    body = await response.json();
  } catch {
    return failure("Could not reach the local Tammy gateway.");
  }
  if (response.ok) return { content: [{ type: "text" as const, text: JSON.stringify(body) }] };
  // Other 4xx responses say what to change, such as an invalid date. A 401 (no unlocked browser
  // session or a wrong token) and 5xx failures keep the generic text.
  const error = gatewayError(body);
  if (response.status !== 401 && response.status < 500 && error)
    return failure(`${error.code}: ${error.message}`);
  return failure("Tammy is unavailable or the local workspace is not authenticated.");
}
const tools = [
  ["get_overview", "overview", "Read current attention counts from the active local workspace."],
  [
    "list_accounts",
    "accounts",
    "List up to 200 chart accounts; the page object indicates possible truncation.",
  ],
  [
    "list_journals",
    "journals",
    "List up to 200 retained journals; the page object indicates possible truncation.",
  ],
  ["list_documents", "documents", "List up to 200 retained document metadata and review states."],
  [
    "list_bank_lines",
    "banking",
    "List up to 200 imported bank lines and their reconciliation states.",
  ],
  ["get_gst_workpaper", "bas", "Read the current local GST workpaper. It is a draft, not lodged."],
  [
    "get_insights",
    "insights",
    "Read rule-based attention items derived from the current core snapshot.",
  ],
] as const;
for (const [name, route, description] of tools)
  server.registerTool(name, { description, inputSchema: {}, annotations }, async () => read(route));
server.registerTool(
  "get_trial_balance",
  {
    description:
      "Read the core trial balance as of an inclusive date, using exact AUD minor units.",
    inputSchema: {
      asOfDate: z
        .string()
        .regex(/^\d{4}-\d{2}-\d{2}$/)
        .optional()
        .describe("Optional inclusive date YYYY-MM-DD; defaults to today in Melbourne."),
    },
    annotations,
  },
  async ({ asOfDate }) =>
    read("trial-balance", asOfDate ? `?asOfDate=${encodeURIComponent(asOfDate)}` : ""),
);
await server.connect(new StdioServerTransport());
