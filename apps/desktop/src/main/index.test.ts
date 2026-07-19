import { EventEmitter } from "node:events";
import { mkdir, mkdtemp, realpath, rm, symlink, writeFile } from "node:fs/promises";
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

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

function occurrences(calls: readonly string[], value: string): number {
  return calls.filter((call) => call === value).length;
}

afterEach(async () => {
  await Promise.all(
    temporaryDirectories
      .splice(0)
      .map((directory) => rm(directory, { force: true, recursive: true })),
  );
});

class FakeWindow extends EventEmitter {
  readonly close;
  readonly isDestroyed = vi.fn(() => false);
  readonly loadURL = vi.fn(async () => undefined);
  readonly show = vi.fn();

  constructor(calls: string[]) {
    super();
    this.close = vi.fn(() => calls.push("window:close"));
  }
}

function rig(overrides: Partial<DesktopDependencies> = {}) {
  const calls: string[] = [];
  const window = new FakeWindow(calls);
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
    expect(
      calls.filter((call) =>
        ["ipc:unregister", "window:close", "core:stop", "exit:0"].includes(call),
      ),
    ).toEqual(["ipc:unregister", "window:close", "core:stop", "exit:0"]);
    expect(window.close).toHaveBeenCalledOnce();
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

  it("does not progress when quit is requested while app readiness is pending", async () => {
    const readiness = deferred<void>();
    const { calls, dependencies, requestQuit } = rig({
      ready: vi.fn(() => readiness.promise),
    });
    const startup = startDesktopApplication(dependencies);
    void startup.catch(() => undefined);
    await vi.waitFor(() => expect(dependencies.ready).toHaveBeenCalledOnce());

    requestQuit();
    readiness.resolve();

    await expect(startup).rejects.toEqual(new DesktopApplicationError("APPLICATION_CLOSING"));
    expect(dependencies.installRuntimeSecurity).not.toHaveBeenCalled();
    expect(dependencies.core.start).not.toHaveBeenCalled();
    expect(dependencies.createWindow).not.toHaveBeenCalled();
    expect(occurrences(calls, "scheme:release")).toBe(1);
    expect(occurrences(calls, "quit:unlisten")).toBe(1);
    expect(occurrences(calls, "exit:0")).toBe(1);
  });

  it("releases late runtime security once and never starts core after quit", async () => {
    const installation = deferred<() => void>();
    const { calls, dependencies, requestQuit } = rig({
      installRuntimeSecurity: vi.fn(() => installation.promise),
    });
    const startup = startDesktopApplication(dependencies);
    void startup.catch(() => undefined);
    await vi.waitFor(() => expect(dependencies.installRuntimeSecurity).toHaveBeenCalledOnce());

    requestQuit();
    await Promise.resolve();
    expect(calls).not.toContain("exit:0");
    installation.resolve(() => calls.push("security:runtime:release"));

    await expect(startup).rejects.toEqual(new DesktopApplicationError("APPLICATION_CLOSING"));
    expect(dependencies.core.start).not.toHaveBeenCalled();
    expect(dependencies.createWindow).not.toHaveBeenCalled();
    expect(occurrences(calls, "security:runtime:release")).toBe(1);
    expect(occurrences(calls, "scheme:release")).toBe(1);
    expect(occurrences(calls, "quit:unlisten")).toBe(1);
    expect(occurrences(calls, "exit:0")).toBe(1);
  });

  it("confirms a late core start is stopped before quit can exit", async () => {
    const started = deferred<{
      caPem: string;
      capability: string;
      port: number;
      protocol: "tammy-core-ready-v1";
    }>();
    let running = false;
    const { calls, dependencies, requestQuit } = rig({
      core: {
        start: vi.fn(async () => {
          calls.push("core:start");
          const result = await started.promise;
          running = true;
          calls.push("core:started");
          return result;
        }),
        stop: vi.fn(async () => {
          calls.push("core:stop");
          running = false;
        }),
      },
    });
    const startup = startDesktopApplication(dependencies);
    void startup.catch(() => undefined);
    await vi.waitFor(() => expect(dependencies.core.start).toHaveBeenCalledOnce());

    requestQuit();
    await Promise.resolve();
    expect(calls).not.toContain("exit:0");
    started.resolve({
      caPem: "ca",
      capability: "capability",
      port: 1,
      protocol: "tammy-core-ready-v1",
    });

    await expect(startup).rejects.toEqual(new DesktopApplicationError("APPLICATION_CLOSING"));
    expect(running).toBe(false);
    expect(dependencies.core.stop).toHaveBeenCalledTimes(2);
    expect(dependencies.createClient).not.toHaveBeenCalled();
    expect(dependencies.createWindow).not.toHaveBeenCalled();
    expect(occurrences(calls, "security:runtime:release")).toBe(1);
    expect(occurrences(calls, "scheme:release")).toBe(1);
    expect(occurrences(calls, "quit:unlisten")).toBe(1);
    expect(occurrences(calls, "exit:0")).toBe(1);
    expect(calls.indexOf("core:started")).toBeLessThan(calls.lastIndexOf("core:stop"));
    expect(calls.lastIndexOf("core:stop")).toBeLessThan(calls.indexOf("exit:0"));
  });

  it("does not register IPC or create a window after quit during diagnostics", async () => {
    const diagnostics = deferred<{
      apiVersion: string;
      coreVersion: string;
      networkRequired: false;
      runtimeMode: "offline";
    }>();
    const { calls, dependencies, requestQuit } = rig({
      createClient: vi.fn(() => ({
        getDiagnostics: vi.fn(() => diagnostics.promise),
      })),
    });
    const startup = startDesktopApplication(dependencies);
    void startup.catch(() => undefined);
    await vi.waitFor(() => expect(dependencies.createClient).toHaveBeenCalledOnce());

    requestQuit();
    diagnostics.resolve({
      apiVersion: "tammy.v1",
      coreVersion: "0.1.0",
      networkRequired: false,
      runtimeMode: "offline",
    });

    await expect(startup).rejects.toEqual(new DesktopApplicationError("APPLICATION_CLOSING"));
    expect(dependencies.registerIpc).not.toHaveBeenCalled();
    expect(dependencies.createWindow).not.toHaveBeenCalled();
    expect(occurrences(calls, "security:runtime:release")).toBe(1);
    expect(occurrences(calls, "scheme:release")).toBe(1);
    expect(occurrences(calls, "quit:unlisten")).toBe(1);
    expect(occurrences(calls, "exit:0")).toBe(1);
  });

  it("cannot finish startup after quit while the application URL is loading", async () => {
    const loaded = deferred<undefined>();
    const { calls, dependencies, requestQuit, window } = rig();
    window.loadURL.mockImplementation(() => loaded.promise);
    const startup = startDesktopApplication(dependencies);
    void startup.catch(() => undefined);
    await vi.waitFor(() => expect(window.loadURL).toHaveBeenCalledOnce());

    requestQuit();
    loaded.resolve(undefined);

    await expect(startup).rejects.toEqual(new DesktopApplicationError("APPLICATION_CLOSING"));
    expect(window.show).not.toHaveBeenCalled();
    expect(window.close).toHaveBeenCalledOnce();
    expect(occurrences(calls, "ipc:unregister")).toBe(1);
    expect(occurrences(calls, "security:window:release")).toBe(1);
    expect(occurrences(calls, "security:runtime:release")).toBe(1);
    expect(occurrences(calls, "scheme:release")).toBe(1);
    expect(occurrences(calls, "quit:unlisten")).toBe(1);
    expect(occurrences(calls, "exit:0")).toBe(1);
  });

  it.each(["scheme", "quit-listener"] as const)(
    "maps a throwing %s bootstrap acquisition to the stable failure path",
    async (phase) => {
      const secret = "capability=bootstrap-secret";
      const releaseScheme = vi.fn();
      const { calls, dependencies } = rig({
        listenForQuit:
          phase === "quit-listener"
            ? vi.fn(() => {
                throw new Error(secret);
              })
            : vi.fn(() => vi.fn()),
        registerScheme:
          phase === "scheme"
            ? vi.fn(() => {
                throw new Error(secret);
              })
            : vi.fn(() => releaseScheme),
      });

      await expect(startDesktopApplication(dependencies)).rejects.toEqual(
        new DesktopApplicationError("LOCAL_ENGINE_UNAVAILABLE"),
      );
      expect(dependencies.ready).not.toHaveBeenCalled();
      expect(dependencies.core.start).not.toHaveBeenCalled();
      expect(dependencies.createWindow).not.toHaveBeenCalled();
      expect(calls).toContain("log:LOCAL_ENGINE_UNAVAILABLE");
      expect(calls).toContain("exit:1");
      expect(calls.join(" ")).not.toContain(secret);
      expect(releaseScheme).toHaveBeenCalledTimes(phase === "quit-listener" ? 1 : 0);
    },
  );
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

      const developmentBinary = path.join(development, "core", `${platform}-${arch}`, name);
      const packagedBinary = path.join(packaged, "core", `${platform}-${arch}`, name);
      await expect(
        resolveBundledCorePath({
          arch,
          developmentResourcesPath: development,
          isPackaged: false,
          platform,
          resourcesPath: packaged,
        }),
      ).resolves.toBe(await realpath(developmentBinary));
      await expect(
        resolveBundledCorePath({
          arch,
          developmentResourcesPath: path.join(root, "unused"),
          isPackaged: true,
          platform,
          resourcesPath: packaged,
        }),
      ).resolves.toBe(await realpath(packagedBinary));
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

  it.each([false, true])(
    "rejects a symlinked resources root for isPackaged=%s",
    async (isPackaged) => {
      const { development, packaged, root } = await resourcesFixture();
      const selectedResources = isPackaged ? packaged : development;
      const outsideResources = path.join(root, "outside-resources");
      const binary = path.join(outsideResources, "core/darwin-arm64/tammy-core");
      await mkdir(path.dirname(binary), { recursive: true });
      await writeFile(binary, "binary");
      await rm(selectedResources, { recursive: true });
      await symlink(outsideResources, selectedResources);

      await expect(
        resolveBundledCorePath({
          arch: "arm64",
          developmentResourcesPath: development,
          isPackaged,
          platform: "darwin",
          resourcesPath: packaged,
        }),
      ).rejects.toThrow("INVALID_CORE_BINARY");
    },
  );

  it.each([false, true])("rejects a symlinked core root for isPackaged=%s", async (isPackaged) => {
    const { development, packaged, root } = await resourcesFixture();
    const selectedResources = isPackaged ? packaged : development;
    const outsideCore = path.join(root, "outside-core");
    const binary = path.join(outsideCore, "darwin-arm64/tammy-core");
    await mkdir(path.dirname(binary), { recursive: true });
    await writeFile(binary, "binary");
    await symlink(outsideCore, path.join(selectedResources, "core"));

    await expect(
      resolveBundledCorePath({
        arch: "arm64",
        developmentResourcesPath: development,
        isPackaged,
        platform: "darwin",
        resourcesPath: packaged,
      }),
    ).rejects.toThrow("INVALID_CORE_BINARY");
  });

  it.each([false, true])(
    "rejects a symlinked target directory for isPackaged=%s",
    async (isPackaged) => {
      const { development, packaged, root } = await resourcesFixture();
      const selectedResources = isPackaged ? packaged : development;
      const outsideTarget = path.join(root, "outside-target");
      await mkdir(path.join(selectedResources, "core"), { recursive: true });
      await mkdir(outsideTarget);
      await writeFile(path.join(outsideTarget, "tammy-core"), "binary");
      await symlink(outsideTarget, path.join(selectedResources, "core/darwin-arm64"));

      await expect(
        resolveBundledCorePath({
          arch: "arm64",
          developmentResourcesPath: development,
          isPackaged,
          platform: "darwin",
          resourcesPath: packaged,
        }),
      ).rejects.toThrow("INVALID_CORE_BINARY");
    },
  );
});
