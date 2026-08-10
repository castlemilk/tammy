import { describe, expect, it, vi } from "vitest";

import { DESKTOP_PRELOAD_METHODS, SYSTEM_DIAGNOSTICS_CHANNEL } from "../shared/desktop-api";
import { DIAGNOSTICS_PRELOAD_METHOD, registerDiagnosticsIpc } from "./ipc";
import { ATTENTION_SUMMARY_CHANNEL, type DesktopRpcRouter } from "./rpc-router";

interface FakeFrame {
  readonly url: string;
  isDestroyed: () => boolean;
}

function createHarness(applicationUrl = "tammy://app/") {
  const mainFrame: FakeFrame = {
    url: applicationUrl,
    isDestroyed: () => false,
  };
  const webContents = {
    mainFrame,
    isDestroyed: () => false,
  };
  const mainWindow = {
    webContents,
    isDestroyed: () => false,
  };
  const handlers = new Map<string, (event: unknown, ...args: unknown[]) => unknown>();
  const ipcMain = {
    handle: vi.fn((channel: string, handler: (event: unknown, ...args: unknown[]) => unknown) => {
      if (handlers.has(channel)) {
        throw new Error("duplicate handler");
      }
      handlers.set(channel, handler);
    }),
    removeHandler: vi.fn((channel: string) => {
      handlers.delete(channel);
    }),
  };
  const diagnostics = Object.freeze({
    apiVersion: "tammy.v1",
    coreVersion: "test-core",
    runtimeMode: "offline" as const,
    networkRequired: false as const,
  });
  const getSystemDiagnostics = vi.fn(async () => diagnostics);

  return {
    applicationUrl,
    diagnostics,
    getSystemDiagnostics,
    handlers,
    ipcMain,
    mainFrame,
    mainWindow,
    webContents,
  };
}

function invoke(
  harness: ReturnType<typeof createHarness>,
  overrides: {
    readonly sender?: unknown;
    readonly senderFrame?: unknown;
  } = {},
): unknown {
  const handler = harness.handlers.get(SYSTEM_DIAGNOSTICS_CHANNEL);
  if (!handler) {
    throw new Error("test handler missing");
  }
  return handler({
    sender: Object.hasOwn(overrides, "sender") ? overrides.sender : harness.webContents,
    senderFrame: Object.hasOwn(overrides, "senderFrame")
      ? overrides.senderFrame
      : harness.mainFrame,
  });
}

describe("registerDiagnosticsIpc", () => {
  it("shares the production preload method manifest with the desktop API", () => {
    expect(DESKTOP_PRELOAD_METHODS).toEqual([
      "getSystemDiagnostics",
      "createWorkspace",
      "confirmRecovery",
      "unlockWorkspace",
      "signIn",
      "createOrganisation",
      "getAttentionSummary",
    ]);
    expect(DIAGNOSTICS_PRELOAD_METHOD).toBe(DESKTOP_PRELOAD_METHODS[0]);
  });

  it("registers the one private channel and returns the injected diagnostics projection", async () => {
    const harness = createHarness();

    const unregister = registerDiagnosticsIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
    });

    await expect(invoke(harness)).resolves.toBe(harness.diagnostics);
    expect(harness.getSystemDiagnostics).toHaveBeenCalledTimes(1);
    expect(harness.ipcMain.handle).toHaveBeenCalledWith(
      SYSTEM_DIAGNOSTICS_CHANNEL,
      expect.any(Function),
    );

    unregister();
    expect(harness.handlers.has(SYSTEM_DIAGNOSTICS_CHANNEL)).toBe(false);
  });

  it.each([
    ["missing sender frame", { senderFrame: null }],
    [
      "lookalike main frame",
      {
        senderFrame: {
          url: "tammy://app/",
          isDestroyed: () => false,
        },
      },
    ],
    [
      "subframe",
      {
        senderFrame: {
          url: "tammy://app/",
          isDestroyed: () => false,
        },
      },
    ],
    ["different web contents", { sender: { isDestroyed: () => false } }],
  ])("rejects a %s without calling the core", async (_name, overrides) => {
    const harness = createHarness();
    registerDiagnosticsIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
    });

    const error = await Promise.resolve(invoke(harness, overrides)).catch(
      (caught: unknown) => caught,
    );

    expect(error).toMatchObject({
      code: "IPC_SENDER_REJECTED",
      message: "IPC_SENDER_REJECTED",
    });
    expect(String(error)).not.toContain("tammy://");
    expect(harness.getSystemDiagnostics).not.toHaveBeenCalled();
  });

  it.each([
    "tammy://app.evil/",
    "tammy://app@evil/",
    "tammy://APP/",
    "tammy://app:443/",
    "tammy://app/path",
    "tammy://app/?query",
    "tammy://app/#fragment",
    "file:///index.html",
    "data:text/html,hello",
    "javascript:alert(1)",
    "http://localhost:5173/",
    "https://app/",
  ])("rejects the main frame at non-allowlisted URL %s", async (url) => {
    const harness = createHarness();
    Object.defineProperty(harness.mainFrame, "url", { value: url });
    registerDiagnosticsIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
    });

    await expect(Promise.resolve(invoke(harness))).rejects.toMatchObject({
      code: "IPC_SENDER_REJECTED",
    });
    expect(harness.getSystemDiagnostics).not.toHaveBeenCalled();
  });

  it("accepts only the exact configured development application URL", async () => {
    const harness = createHarness("http://localhost:5173/");
    registerDiagnosticsIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
    });

    await expect(invoke(harness)).resolves.toBe(harness.diagnostics);

    Object.defineProperty(harness.mainFrame, "url", {
      configurable: true,
      value: "http://localhost:5173/dashboard",
    });
    await expect(Promise.resolve(invoke(harness))).rejects.toMatchObject({
      code: "IPC_SENDER_REJECTED",
    });
  });

  it.each(["frame", "contents", "window"] as const)(
    "rejects a destroyed %s without reading unsafe state",
    async (destroyed) => {
      const harness = createHarness();
      if (destroyed === "frame") {
        harness.mainFrame.isDestroyed = () => true;
      } else if (destroyed === "contents") {
        harness.webContents.isDestroyed = () => true;
      } else {
        harness.mainWindow.isDestroyed = () => true;
      }
      registerDiagnosticsIpc({
        applicationUrl: harness.applicationUrl,
        getSystemDiagnostics: harness.getSystemDiagnostics,
        ipcMain: harness.ipcMain,
        mainWindow: harness.mainWindow,
      });

      await expect(Promise.resolve(invoke(harness))).rejects.toMatchObject({
        code: "IPC_SENDER_REJECTED",
      });
      expect(harness.getSystemDiagnostics).not.toHaveBeenCalled();
    },
  );

  it("replaces an existing registrar safely and stale cleanup cannot remove the replacement", async () => {
    const harness = createHarness();
    const firstDiagnostics = vi.fn(async () => harness.diagnostics);
    const firstCleanup = registerDiagnosticsIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: firstDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
    });
    const secondCleanup = registerDiagnosticsIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
    });

    firstCleanup();
    await expect(invoke(harness)).resolves.toBe(harness.diagnostics);
    expect(firstDiagnostics).not.toHaveBeenCalled();
    expect(harness.getSystemDiagnostics).toHaveBeenCalledTimes(1);

    secondCleanup();
    expect(harness.handlers.has(SYSTEM_DIAGNOSTICS_CHANNEL)).toBe(false);
  });

  it("refuses a remote URL as the application allowlist", () => {
    const harness = createHarness("https://example.com/");

    expect(() =>
      registerDiagnosticsIpc({
        applicationUrl: harness.applicationUrl,
        getSystemDiagnostics: harness.getSystemDiagnostics,
        ipcMain: harness.ipcMain,
        mainWindow: harness.mainWindow,
      }),
    ).toThrow("INVALID_APPLICATION_URL");
    expect(harness.ipcMain.handle).not.toHaveBeenCalled();
  });
});

describe("registerDesktopIpc", () => {
  it("registers the named Overview channel and forwards only its request bytes", async () => {
    const harness = createHarness();
    const frame = Uint8Array.of(1, 2, 3);
    const response = Uint8Array.of(4, 5, 6);
    const router: DesktopRpcRouter = {
      invoke: vi.fn(async () => response),
    };
    const { registerDesktopIpc } = await import("./ipc");
    registerDesktopIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
      router,
    });
    const handler = harness.handlers.get(ATTENTION_SUMMARY_CHANNEL);
    expect(handler).toBeDefined();

    await expect(
      handler?.(
        { sender: harness.webContents, senderFrame: harness.mainFrame },
        frame,
      ),
    ).resolves.toEqual(response);
    expect(router.invoke).toHaveBeenCalledExactlyOnceWith(ATTENTION_SUMMARY_CHANNEL, frame);
  });
});

describe("preload desktop bridge", () => {
  it("exposes only the named frozen use-case functions and never ipcRenderer", async () => {
    vi.resetModules();
    const exposeInMainWorld = vi.fn();
    const invoke = vi.fn(async () =>
      Object.freeze({
        apiVersion: "tammy.v1",
        coreVersion: "test-core",
        runtimeMode: "offline",
        networkRequired: false,
      }),
    );
    vi.doMock("electron", () => ({
      contextBridge: { exposeInMainWorld },
      ipcRenderer: { invoke },
    }));

    await import("../preload/index");

    expect(exposeInMainWorld).toHaveBeenCalledTimes(1);
    expect(exposeInMainWorld).toHaveBeenCalledWith("tammy", expect.any(Object));
    const api = exposeInMainWorld.mock.calls[0]?.[1] as
      | {
          readonly getAttentionSummary: (request: Uint8Array) => Promise<Uint8Array>;
          readonly getSystemDiagnostics: () => Promise<unknown>;
        }
      | undefined;
    expect(api).toBeDefined();
    expect(Object.keys(api ?? {})).toEqual([
      "getSystemDiagnostics",
      "createWorkspace",
      "confirmRecovery",
      "unlockWorkspace",
      "signIn",
      "createOrganisation",
      "getAttentionSummary",
    ]);
    expect(Object.isFrozen(api)).toBe(true);
    await expect(api?.getSystemDiagnostics()).resolves.toMatchObject({
      runtimeMode: "offline",
      networkRequired: false,
    });
    expect(invoke).toHaveBeenCalledWith(SYSTEM_DIAGNOSTICS_CHANNEL);
    expect(api).not.toHaveProperty("ipcRenderer");

    vi.doUnmock("electron");
  });
});
