import { EventEmitter } from "node:events";
import { mkdir, mkdtemp, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  app: {},
  BrowserWindow: class {},
  ipcMain: {},
  net: {},
  protocol: {},
  session: {},
}));

import {
  DesktopApplicationError,
  type DesktopDependencies,
  startDesktopApplication,
} from "./index";
import { resolveBundledCorePath } from "./index-paths";

const temporaryDirectories: string[] = [];

afterEach(async () => {
  await Promise.all(
    temporaryDirectories
      .splice(0)
      .map((directory) => rm(directory, { force: true, recursive: true })),
  );
});

class FakeWindow extends EventEmitter {
  readonly close = vi.fn();
  readonly isDestroyed = vi.fn(() => false);
  readonly loadURL = vi.fn(async () => undefined);
  readonly show = vi.fn();
}

function rig(overrides: Partial<DesktopDependencies> = {}) {
  const calls: string[] = [];
  const window = new FakeWindow();
  let requestQuit: (() => void) | undefined;
  const readiness = Object.freeze({
    caPem: "private readiness detail",
    capability: "private capability",
    port: 54_321,
    protocol: "tammy-core-ready-v1" as const,
  });
  const dependencies: DesktopDependencies = {
    applicationUrl: "tammy://app/",
    core: {
      start: vi.fn(async () => {
        calls.push("core:start");
        return readiness;
      }),
      stop: vi.fn(async () => {
        calls.push("core:stop");
      }),
    },
    createClient: vi.fn(() => ({
      getDiagnostics: async () => {
        calls.push("diagnostics");
        return {
          apiVersion: "tammy.v1",
          coreVersion: "0.1.0",
          networkRequired: false as const,
          runtimeMode: "offline" as const,
        };
      },
    })),
    createWindow: vi.fn(() => {
      calls.push("window:create");
      return window;
    }),
    exit: vi.fn((code) => calls.push(`exit:${code}`)),
    installRuntimeSecurity: vi.fn(async () => {
      calls.push("security:runtime");
      return () => calls.push("security:runtime:release");
    }),
    installWindowSecurity: vi.fn(() => {
      calls.push("security:window");
      return () => calls.push("security:window:release");
    }),
    listenForQuit: vi.fn((listener) => {
      calls.push("quit:listen");
      requestQuit = listener;
      return () => calls.push("quit:unlisten");
    }),
    logger: { error: vi.fn((message) => calls.push(`log:${message}`)) },
    ready: vi.fn(async () => {
      calls.push("app:ready");
    }),
    registerIpc: vi.fn(() => {
      calls.push("ipc:register");
      return () => calls.push("ipc:unregister");
    }),
    registerScheme: vi.fn(() => {
      calls.push("scheme");
      return () => calls.push("scheme:release");
    }),
    ...overrides,
  };
  return {
    calls,
    dependencies,
    requestQuit: () => requestQuit?.(),
    window,
  };
}

describe("desktop application composition", () => {
  it("starts in the exact secure local-engine order and shows only when ready", async () => {
    const { calls, dependencies, window } = rig();
    const application = await startDesktopApplication(dependencies);

    expect(calls).toEqual([
      "scheme",
      "quit:listen",
      "app:ready",
      "security:runtime",
      "core:start",
      "diagnostics",
      "ipc:register",
      "window:create",
      "security:window",
    ]);
    expect(window.loadURL).toHaveBeenCalledExactlyOnceWith("tammy://app/");
    expect(window.show).not.toHaveBeenCalled();
    window.emit("ready-to-show");
    expect(window.show).toHaveBeenCalledOnce();

    await application.shutdown();
    expect(calls.indexOf("ipc:unregister")).toBeLessThan(calls.indexOf("core:stop"));
    expect(calls.indexOf("window:create")).toBeLessThan(calls.indexOf("core:stop"));
    expect(window.close).toHaveBeenCalledOnce();
    expect(calls.at(-1)).toBe("exit:0");
  });

  it.each(["start", "diagnostics"] as const)(
    "maps %s failure to one stable local-engine path without creating a window or leaking details",
    async (failure) => {
      const secret = "capability=do-not-log TLS=private";
      const failureOverride: Partial<DesktopDependencies> =
        failure === "start"
          ? {
              core: {
                start: vi.fn(async () => {
                  throw new Error(secret);
                }),
                stop: vi.fn(async () => undefined),
              },
            }
          : {
              createClient: vi.fn(() => ({
                getDiagnostics: async () => {
                  throw new Error(secret);
                },
              })),
            };
      const { calls, dependencies } = rig(failureOverride);

      await expect(startDesktopApplication(dependencies)).rejects.toMatchObject({
        code: "LOCAL_ENGINE_UNAVAILABLE",
        message: "The local engine is unavailable.",
      });
      expect(calls).not.toContain("window:create");
      expect(calls.join(" ")).not.toContain(secret);
      expect(calls).toContain("log:LOCAL_ENGINE_UNAVAILABLE");
      expect(calls).toContain("exit:1");
    },
  );

  it("deduplicates concurrent quit paths and waits for confirmed core stop before exit", async () => {
    let resolveStop: (() => void) | undefined;
    const stop = new Promise<void>((resolve) => {
      resolveStop = resolve;
    });
    const { calls, dependencies, requestQuit } = rig({
      core: {
        start: vi.fn(async () => ({
          caPem: "ca",
          capability: "capability",
          port: 1,
          protocol: "tammy-core-ready-v1" as const,
        })),
        stop: vi.fn(async () => stop),
      },
    });
    const application = await startDesktopApplication(dependencies);

    requestQuit();
    const second = application.shutdown();
    await Promise.resolve();
    expect(calls).not.toContain("exit:0");
    expect(dependencies.core.stop).toHaveBeenCalledOnce();
    resolveStop?.();
    await second;
    expect(calls.filter((call) => call === "ipc:unregister")).toHaveLength(1);
    expect(calls.filter((call) => call === "exit:0")).toHaveLength(1);
  });

  it("surfaces shutdown failure safely and never reports successful exit", async () => {
    const { calls, dependencies } = rig({
      core: {
        start: vi.fn(async () => ({
          caPem: "ca",
          capability: "capability",
          port: 1,
          protocol: "tammy-core-ready-v1" as const,
        })),
        stop: vi.fn(async () => {
          throw new Error("pid=123 private");
        }),
      },
    });
    const application = await startDesktopApplication(dependencies);

    await expect(application.shutdown()).rejects.toEqual(
      new DesktopApplicationError("LOCAL_ENGINE_SHUTDOWN_FAILED"),
    );
    expect(calls).not.toContain("exit:0");
    expect(calls).toContain("log:LOCAL_ENGINE_SHUTDOWN_FAILED");
    expect(calls).toContain("exit:1");
    expect(calls.join(" ")).not.toContain("pid=123");
  });
});

describe("bundled core resolver", () => {
  async function resourcesFixture() {
    const root = await mkdtemp(path.join(tmpdir(), "tammy-core-paths-"));
    temporaryDirectories.push(root);
    const development = path.join(root, "apps/desktop/resources");
    const packaged = path.join(root, "package/resources");
    await Promise.all([
      mkdir(development, { recursive: true }),
      mkdir(packaged, { recursive: true }),
    ]);
    return { development, packaged, root };
  }

  it.each([
    ["darwin", "arm64", "tammy-core"],
    ["win32", "x64", "tammy-core.exe"],
  ])(
    "resolves a regular %s/%s binary in development and packaging",
    async (platform, arch, name) => {
      const { development, packaged, root } = await resourcesFixture();
      for (const resources of [development, packaged]) {
        const binary = path.join(resources, "core", `${platform}-${arch}`, name);
        await mkdir(path.dirname(binary), { recursive: true });
        await writeFile(binary, "binary");
      }

      await expect(
        resolveBundledCorePath({
          arch,
          developmentResourcesPath: development,
          isPackaged: false,
          platform,
          resourcesPath: packaged,
        }),
      ).resolves.toBe(path.join(development, "core", `${platform}-${arch}`, name));
      await expect(
        resolveBundledCorePath({
          arch,
          developmentResourcesPath: path.join(root, "unused"),
          isPackaged: true,
          platform,
          resourcesPath: packaged,
        }),
      ).resolves.toBe(path.join(packaged, "core", `${platform}-${arch}`, name));
    },
  );

  it.each([
    ["linux", "x64"],
    ["darwin", "x64"],
    ["win32", "arm64"],
  ])("rejects unsupported %s/%s", async (platform, arch) => {
    const { development, packaged } = await resourcesFixture();
    await expect(
      resolveBundledCorePath({
        arch,
        developmentResourcesPath: development,
        isPackaged: false,
        platform,
        resourcesPath: packaged,
      }),
    ).rejects.toThrow("UNSUPPORTED_CORE_TARGET");
  });

  it("rejects missing files, symlinks, and traversal-like target data", async () => {
    const { development, packaged, root } = await resourcesFixture();
    const outside = path.join(root, "outside");
    await writeFile(outside, "outside");
    const binary = path.join(development, "core/darwin-arm64/tammy-core");
    await mkdir(path.dirname(binary), { recursive: true });
    await symlink(outside, binary);

    await expect(
      resolveBundledCorePath({
        arch: "arm64",
        developmentResourcesPath: development,
        isPackaged: false,
        platform: "darwin",
        resourcesPath: packaged,
      }),
    ).rejects.toThrow("INVALID_CORE_BINARY");
    await expect(
      resolveBundledCorePath({
        arch: "../arm64",
        developmentResourcesPath: development,
        isPackaged: false,
        platform: "darwin",
        resourcesPath: packaged,
      }),
    ).rejects.toThrow("UNSUPPORTED_CORE_TARGET");
  });
});
