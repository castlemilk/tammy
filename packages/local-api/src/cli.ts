import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";

import { readLocalApiConnectionFile } from "./connection.ts";
import { createLocalApiMcpServer } from "./mcp.ts";

async function main(args: string[]): Promise<void> {
  if (args.length !== 2 || args[0] !== "--connection" || args[1] === undefined) {
    throw new Error("INVALID_CLI_ARGUMENTS");
  }
  const connection = await readLocalApiConnectionFile(args[1]);
  const server = createLocalApiMcpServer(connection);
  server.onerror = () => {
    process.stderr.write("LOCAL_MCP_PROTOCOL_ERROR\n");
  };
  await server.connect(new StdioServerTransport());
  const close = () => {
    void server.close().finally(() => process.exit(0));
  };
  process.once("SIGINT", close);
  process.once("SIGTERM", close);
}

if (import.meta.main) {
  void main(process.argv.slice(2)).catch((error: unknown) => {
    const code =
      error instanceof Error &&
      ["INVALID_CLI_ARGUMENTS", "INVALID_CONNECTION_FILE"].includes(error.message)
        ? error.message
        : "LOCAL_MCP_START_FAILED";
    process.stderr.write(`${code}\n`);
    process.exitCode = 1;
  });
}
