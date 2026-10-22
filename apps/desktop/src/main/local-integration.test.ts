// @vitest-environment node
import { mkdir, mkdtemp, readFile, rename, rm, stat, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { createDesktopLocalIntegration } from "./local-integration";

const roots: string[] = [];
afterEach(async () => {
  await Promise.all(roots.map((root) => rm(root, { recursive: true, force: true })));
  roots.length = 0;
});
async function directory() {
  const root = await mkdtemp(path.join(tmpdir(), "tammy-local-integration-test-"));
  roots.push(root);
  return root;
}

describe("desktop API pairing", () => {
  it("closes access and permits cleanup retry after a connection unlink failure", async () => {
    const integration = createDesktopLocalIntegration(
      await directory(),
      async () => async () => ({}),
    );
    await integration.enable();
    const connection = JSON.parse(await readFile(integration.connectionPath, "utf8"));
    const retained = `${integration.connectionPath}.retained`;
    await rename(integration.connectionPath, retained);
    await mkdir(integration.connectionPath);
    await expect(integration.revoke()).rejects.toThrow("LOCAL_CONNECTION_CLEANUP_FAILED");
    await expect(fetch(`${connection.origin}/v1/capabilities`)).rejects.toThrow();
    await rm(integration.connectionPath, { recursive: true });
    await rename(retained, integration.connectionPath);
    await integration.revoke();
    await expect(readFile(integration.connectionPath)).rejects.toMatchObject({ code: "ENOENT" });
    await integration.enable();
    await integration.revoke();
  });
  it("publishes a private connection and revokes it with the listener", async () => {
    const integration = createDesktopLocalIntegration(await directory(), async () => async () => ({
      name: "Example Pty Ltd",
    }));
    try {
      await integration.enable();
      const connection = JSON.parse(await readFile(integration.connectionPath, "utf8"));
      const connectionStat = await stat(integration.connectionPath);
      expect(connectionStat.isFile()).toBe(true);
      if (process.platform !== "win32") {
        expect(connectionStat.mode & 0o777).toBe(0o600);
      }
      expect(Object.keys(connection).sort()).toEqual(["capability", "origin", "version"]);
      expect(connection.capability).toMatch(/^[A-Za-z0-9_-]{43}$/u);
      expect(connection.origin).toMatch(/^http:\/\/127\.0\.0\.1:[0-9]+$/u);
      expect(connection.version).toBe("v1");
      const response = await fetch(`${connection.origin}/v1/read`, {
        method: "POST",
        headers: {
          authorization: `Bearer ${connection.capability}`,
          "content-type": "application/json",
        },
        body: JSON.stringify({
          version: "v1",
          operation: "tool.call",
          name: "tammy.organisation.get_current",
        }),
      });
      expect(await response.json()).toEqual({ ok: true, data: { name: "Example Pty Ltd" } });
      await integration.revoke();
      await expect(readFile(integration.connectionPath)).rejects.toMatchObject({ code: "ENOENT" });
      await expect(fetch(`${connection.origin}/v1/capabilities`)).rejects.toThrow();
    } finally {
      await integration.revoke();
    }
  });

  it("creates no listener or connection when native login is missing", async () => {
    const integration = createDesktopLocalIntegration(await directory(), async () => {
      throw new Error("LOCAL_SESSION_REQUIRED");
    });
    await expect(integration.enable()).rejects.toThrow("LOCAL_SESSION_REQUIRED");
    await expect(readFile(integration.connectionPath)).rejects.toMatchObject({ code: "ENOENT" });
    await integration.revoke();
  });

  it("cancels pairing while native session validation is pending", async () => {
    let finish!: (value: (request: unknown) => Promise<unknown>) => void;
    const integration = createDesktopLocalIntegration(
      await directory(),
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const enabling = integration.enable();
    await new Promise((resolve) => setTimeout(resolve, 10));
    await integration.revoke();
    finish(async () => ({}));
    await expect(enabling).rejects.toThrow("LOCAL_PAIRING_REVOKED");
    await expect(readFile(integration.connectionPath)).rejects.toMatchObject({ code: "ENOENT" });
  });

  it("refuses to publish a connection through a symlinked integration directory", async () => {
    const root = await directory();
    const elsewhere = await directory();
    await symlink(elsewhere, path.join(root, "local-integrations"));
    const integration = createDesktopLocalIntegration(root, async () => async () => ({}));
    await expect(integration.enable()).rejects.toThrow("LOCAL_CONNECTION_DIRECTORY_INVALID");
    await integration.revoke();
  });
});
