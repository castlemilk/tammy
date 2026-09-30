import { randomUUID } from "node:crypto";
import { chmodSync, lstatSync, mkdirSync, renameSync, unlinkSync, writeFileSync } from "node:fs";
import path from "node:path";
import { startLocalApiBroker } from "@tammy/local-api/broker";
import type { ValidatedLocalApiRequest } from "@tammy/local-api/request";

type Reader = (request: ValidatedLocalApiRequest) => Promise<unknown>;

export interface DesktopLocalIntegration {
  readonly connectionPath: string;
  enable(): Promise<void>;
  revoke(): Promise<void>;
}

/** The pairing file contains only an ephemeral local API capability, never Core credentials. */
export function createDesktopLocalIntegration(
  userData: string,
  createReader: () => Promise<Reader>,
): DesktopLocalIntegration {
  const directory = path.join(userData, "local-integrations");
  const connectionPath = path.join(directory, "connection.json");
  let generation = 0;
  let broker: Awaited<ReturnType<typeof startLocalApiBroker>> | undefined;
  let closing = Promise.resolve();
  let published = false;
  const revoke = (): Promise<void> => {
    generation += 1;
    const previous = broker;
    broker = undefined;
    let cleanupFailed = false;
    if (published) {
      try {
        unlinkSync(connectionPath);
        published = false;
      } catch (error) {
        if ((error as NodeJS.ErrnoException).code === "ENOENT") published = false;
        else cleanupFailed = true;
      }
    }
    closing = Promise.all([closing.catch(() => undefined), previous?.close()]).then(() => undefined);
    return closing.then(() => {
      if (cleanupFailed) throw new Error("LOCAL_CONNECTION_CLEANUP_FAILED");
    });
  };
  return {
    connectionPath,
    revoke,
    enable: async () => {
      const stopping = revoke();
      const attempt = generation;
      await stopping;
      const reader = await createReader();
      if (attempt !== generation) throw new Error("LOCAL_PAIRING_REVOKED");
      mkdirSync(directory, { mode: 0o700, recursive: true });
      const directoryState = lstatSync(directory);
      if (!directoryState.isDirectory() || directoryState.isSymbolicLink()) {
        throw new Error("LOCAL_CONNECTION_DIRECTORY_INVALID");
      }
      chmodSync(directory, 0o700);
      const started = await startLocalApiBroker({
        execute: async (request) => {
          if (attempt !== generation) throw new Error("LOCAL_PAIRING_REVOKED");
          const result = await reader(request);
          if (attempt !== generation) throw new Error("LOCAL_PAIRING_REVOKED");
          return result;
        },
      });
      const temporaryPath = path.join(directory, `${randomUUID()}.json`);
      try {
        if (attempt !== generation) throw new Error("LOCAL_PAIRING_REVOKED");
        writeFileSync(temporaryPath, JSON.stringify(started.connection), {
          mode: 0o600,
          flag: "wx",
        });
        renameSync(temporaryPath, connectionPath);
        published = true;
        broker = started;
      } catch (error) {
        try {
          unlinkSync(temporaryPath);
        } catch {
          /* No temporary file may have been written. */
        }
        await started.close();
        throw error;
      }
    },
  };
}
