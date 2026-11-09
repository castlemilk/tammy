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
async function read(route: string, query = "") {
  try {
    const response = await fetch(`http://127.0.0.1:${port}/api/mcp/${route}${query}`, {
      headers: { Authorization: `Bearer ${token}` },
      signal: AbortSignal.timeout(5000),
    });
    const body = await response.json();
    if (!response.ok)
      return {
        isError: true,
        content: [
          {
            type: "text" as const,
            text: "Tammy is unavailable or the local workspace is not authenticated.",
          },
        ],
      };
    return { content: [{ type: "text" as const, text: JSON.stringify(body) }] };
  } catch {
    return {
      isError: true,
      content: [{ type: "text" as const, text: "Could not reach the local Tammy gateway." }],
    };
  }
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
