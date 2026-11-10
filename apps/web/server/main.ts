import { mkdir, realpath } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-node";
import { IdentityService } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { OrganisationService } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { WorkspaceService } from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { capabilityInterceptor, createCoreClient } from "../../desktop/src/main/core-client";
import { authenticateCoreExecutable } from "../../desktop/src/main/core-executable";
import { CoreProcess } from "../../desktop/src/main/core-process";
import { createFacade } from "./facade.mjs";
import { createGateway } from "./gateway.mjs";

const directory = path.dirname(fileURLToPath(import.meta.url));
// CoreProcess reports an unexpected exit only through its diagnostic, so the gateway polls it.
const CORE_WATCH_INTERVAL_MS = 250;
const defaultBinary = path.resolve(
  directory,
  `../../desktop/resources/core/${process.platform}-${process.arch}/${process.platform === "win32" ? "tammy-core.exe" : "tammy-core"}`,
);

/**
 * Authenticate the core executable before it is supervised. The supervisor re-hashes it immediately
 * before spawning and checks the live process image, so resolve symlinked parent directories here;
 * the executable itself must still be a regular file rather than a symlink.
 */
async function coreExecutable(requested: string) {
  const parent = path.dirname(requested);
  const physicalParent = await realpath(parent).catch(() => parent);
  return authenticateCoreExecutable(path.join(physicalParent, path.basename(requested)));
}

export async function startApp(
  options: {
    dataRoot?: string;
    corePath?: string;
    port?: number;
    origin?: string;
    mcpToken?: string;
    /**
     * Runs after the gateway has stopped because the supervised core exited or failed. The default
     * exits the process with status 1.
     */
    onCoreExit?: (errorCode: string) => void;
  } = {},
) {
  const requestedDataRoot = path.resolve(
    options.dataRoot ??
      process.env.TAMMY_WEB_DATA_ROOT ??
      path.join(os.homedir(), ".tammy-web", "development"),
  );
  const port = options.port ?? Number(process.env.TAMMY_WEB_PORT ?? "4174");
  const origin = options.origin ?? process.env.TAMMY_WEB_ORIGIN ?? `http://127.0.0.1:${port}`;
  const token = options.mcpToken ?? process.env.TAMMY_MCP_TOKEN;
  if (
    !Number.isInteger(port) ||
    port < 0 ||
    port > 65535 ||
    !/^http:\/\/127\.0\.0\.1:[0-9]{1,5}$/.test(origin) ||
    (token !== undefined && token.length < 32)
  )
    throw new Error("Invalid local web configuration.");
  await mkdir(requestedDataRoot, { recursive: true, mode: 0o700 });
  const dataRoot = await realpath(requestedDataRoot);
  const supervisor = new CoreProcess({
    binary: await coreExecutable(
      path.resolve(options.corePath ?? process.env.TAMMY_CORE_BINARY ?? defaultBinary),
    ),
    args: ["--data-root", dataRoot, "--development-memory-anchors"],
  });
  const readiness = await supervisor.start();
  try {
    const transport = createConnectTransport({
      baseUrl: `https://127.0.0.1:${readiness.port}`,
      httpVersion: "1.1",
      defaultTimeoutMs: 5000,
      nodeOptions: {
        ca: readiness.caPem,
        rejectUnauthorized: true,
        minVersion: "TLSv1.3",
        maxVersion: "TLSv1.3",
      },
      interceptors: [capabilityInterceptor(readiness.capability)],
    });
    const facade = await createFacade({
      client: createCoreClient(readiness),
      workspace: createClient(WorkspaceService, transport),
      identity: createClient(IdentityService, transport),
      organisation: createClient(OrganisationService, transport),
      dataRoot,
      mcpEnabled: Boolean(token),
    });
    const server = createGateway({
      facade,
      origin,
      mcpToken: token,
      staticRoot: path.resolve(directory, "../dist"),
    });
    await new Promise<void>((resolve, reject) => {
      server.once("error", reject);
      server.listen(port, "127.0.0.1", () => {
        server.off("error", reject);
        resolve();
      });
    });
    let closed = false;
    const close = async () => {
      if (closed) return;
      closed = true;
      clearInterval(watch);
      await new Promise<void>((resolve, reject) =>
        server.close((error?: Error) => (error ? reject(error) : resolve())),
      );
      await supervisor.stop();
    };
    // Without the core every request would fail, so stop serving instead of answering with 500s.
    const watch = setInterval(() => {
      const diagnostic = supervisor.getDiagnostic();
      if (diagnostic.state !== "FAILED") return;
      clearInterval(watch);
      const code = diagnostic.errorCode ?? "CORE_FAILED";
      process.stderr.write(
        `Tammy web stopped: the accounting core stopped unexpectedly (${code}). Start the web server again.\n`,
      );
      const stopping = close();
      server.closeAllConnections();
      void stopping
        .catch(() => undefined)
        .then(() => (options.onCoreExit ?? (() => process.exit(1)))(code));
    }, CORE_WATCH_INTERVAL_MS);
    watch.unref();
    return { server, close };
  } catch (error) {
    await supervisor.stop();
    throw error;
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const app = await startApp();
  const address = app.server.address();
  if (address && typeof address === "object")
    process.stderr.write(`Tammy web is listening at http://127.0.0.1:${address.port}\n`);
  for (const signal of ["SIGINT", "SIGTERM"] as const)
    process.once(signal, () => {
      void app.close().then(
        () => process.exit(0),
        () => process.exit(1),
      );
    });
}
