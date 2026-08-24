import { describe, expect, it, vi } from "vitest";
import {
  DESKTOP_PRELOAD_METHODS,
  REPORTING_CAPABILITY_CHANNEL,
  SYSTEM_DIAGNOSTICS_CHANNEL,
} from "../shared/desktop-api";
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

function desktopRouter(invokeRouter: DesktopRpcRouter["invoke"]): DesktopRpcRouter {
  return {
    invoke: invokeRouter,
    selectMachineCredentialFile: vi.fn(async () => ({ selected: false as const })),
    importMachineCredential: vi.fn(),
    replaceMachineCredential: vi.fn(),
    unlockMachineCredential: vi.fn(),
    importSbrProductId: vi.fn(),
  };
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
      "createAccount",
      "listAccounts",
      "postManualJournal",
      "listJournals",
      "getJournal",
      "getTrialBalance",
      "importBankStatement",
      "listBankStatementLines",
      "matchBankStatementLine",
      "completeBankReconciliation",
      "getBankingSummary",
      "ingestDocument",
      "listDocuments",
      "getDocument",
      "saveDocumentReview",
      "createBasDraft",
      "getCurrentBasDraft",
      "getReportingCapability",
      "getAttentionSummary",
      "getCurrentUser",
      "enrolTotp",
      "confirmTotp",
      "assertTotp",
      "getOrganisation",
      "recordEntityVerification",
      "getSbrReadiness",
      "getMachineCredentialStatus",
      "removeMachineCredential",
      "removeSbrProductId",
      "runSbrReadinessFixture",
      "selectMachineCredentialFile",
      "importMachineCredential",
      "replaceMachineCredential",
      "unlockMachineCredential",
      "importSbrProductId",
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

  it("accepts the exact configured development application and its same-origin routes", async () => {
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
    await expect(invoke(harness)).resolves.toBe(harness.diagnostics);

    Object.defineProperty(harness.mainFrame, "url", {
      configurable: true,
      value: "http://localhost:5174/dashboard",
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
    const router = desktopRouter(vi.fn(async () => response));
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
      handler?.({ sender: harness.webContents, senderFrame: harness.mainFrame }, frame),
    ).resolves.toEqual(response);
    expect(router.invoke).toHaveBeenCalledExactlyOnceWith(ATTENTION_SUMMARY_CHANNEL, frame);
  });

  it("registers the reporting capability channel and forwards one copied frame", async () => {
    const harness = createHarness();
    const frame = Uint8Array.of(1, 2, 3);
    const response = Uint8Array.of(4, 5, 6);
    const router = desktopRouter(vi.fn(async () => response));
    const { registerDesktopIpc } = await import("./ipc");
    registerDesktopIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
      router,
    });

    const handler = harness.handlers.get(REPORTING_CAPABILITY_CHANNEL);
    await expect(
      handler?.({ sender: harness.webContents, senderFrame: harness.mainFrame }, frame),
    ).resolves.toEqual(response);
    expect(router.invoke).toHaveBeenCalledExactlyOnceWith(REPORTING_CAPABILITY_CHANNEL, frame);
    expect(vi.mocked(router.invoke).mock.calls[0]?.[1]).not.toBe(frame);
  });

  it("registers the exact generic and mediated channels while keeping mediation non-generic", async () => {
    const harness = createHarness();
    const router = {
      invoke: vi.fn(async () => Uint8Array.of(1)),
      selectMachineCredentialFile: vi.fn(async () => ({ selected: false as const })),
      importMachineCredential: vi.fn(async () => Uint8Array.of(2)),
      replaceMachineCredential: vi.fn(async () => Uint8Array.of(3)),
      unlockMachineCredential: vi.fn(async () => Uint8Array.of(4)),
      importSbrProductId: vi.fn(async () => Uint8Array.of(5)),
    } as unknown as DesktopRpcRouter;
    const { registerDesktopIpc } = await import("./ipc");

    registerDesktopIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
      router,
    });

    expect([...harness.handlers.keys()].slice(-16)).toEqual([
      "tammy:identity-current-user",
      "tammy:identity-enrol-totp",
      "tammy:identity-confirm-totp",
      "tammy:identity-assert-totp",
      "tammy:organisation-get",
      "tammy:organisation-record-verification",
      "tammy:sbr-readiness",
      "tammy:sbr-credential-status",
      "tammy:sbr-credential-remove",
      "tammy:sbr-product-id-remove",
      "tammy:sbr-run-fixture",
      "tammy:sbr-credential-select",
      "tammy:sbr-credential-import",
      "tammy:sbr-credential-replace",
      "tammy:sbr-credential-unlock",
      "tammy:sbr-product-id-import",
    ]);

    const event = { sender: harness.webContents, senderFrame: harness.mainFrame };
    const handle = "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb";
    const command = Uint8Array.of(8, 9);
    const mediated = [
      [
        "tammy:sbr-credential-import",
        "importMachineCredential",
        { command, handle, password: "password" },
      ],
      [
        "tammy:sbr-credential-replace",
        "replaceMachineCredential",
        { command, handle, password: "password" },
      ],
      ["tammy:sbr-credential-unlock", "unlockMachineCredential", { command, password: "password" }],
      ["tammy:sbr-product-id-import", "importSbrProductId", { command, productId: "product" }],
    ] as const;
    for (const [channel, method, argument] of mediated) {
      const handler = harness.handlers.get(channel);
      await handler?.(event, argument);
      expect(router.invoke).not.toHaveBeenCalledWith(channel, expect.anything());
      const call = vi.mocked(router[method]).mock.calls.at(-1);
      expect(call).toHaveLength(1);
      const forwarded = call?.[0] as { command: Uint8Array } | undefined;
      expect(forwarded).toBeDefined();
      expect(forwarded).not.toBe(argument);
      expect(forwarded).toMatchObject(argument);
      expect(forwarded?.command).not.toBe(command);
    }
  });

  it("rejects extra arguments and untrusted mediated senders before trusted main", async () => {
    const harness = createHarness();
    const router = {
      invoke: vi.fn(),
      selectMachineCredentialFile: vi.fn(),
      importMachineCredential: vi.fn(),
      replaceMachineCredential: vi.fn(),
      unlockMachineCredential: vi.fn(),
      importSbrProductId: vi.fn(),
    } as unknown as DesktopRpcRouter;
    const { registerDesktopIpc } = await import("./ipc");
    registerDesktopIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
      router,
    });
    const handler = harness.handlers.get("tammy:sbr-credential-import");
    const argument = {
      command: Uint8Array.of(1),
      handle: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb",
      password: "password",
    };

    await expect(
      handler?.({ sender: harness.webContents, senderFrame: harness.mainFrame }, argument, "extra"),
    ).rejects.toMatchObject({ code: "INVALID_RPC_REQUEST" });
    await expect(
      handler?.({ sender: {}, senderFrame: harness.mainFrame }, argument),
    ).rejects.toMatchObject({ code: "IPC_SENDER_REJECTED" });
    await expect(
      handler?.(
        { sender: harness.webContents, senderFrame: harness.mainFrame },
        { ...argument, selectedLocalPath: "/renderer/secret.p12" },
      ),
    ).rejects.toMatchObject({ code: "INVALID_RPC_REQUEST" });
    expect(router.importMachineCredential).not.toHaveBeenCalled();
  });

  it("enforces generic channel byte caps before copying or routing", async () => {
    const harness = createHarness();
    const router = desktopRouter(vi.fn(async () => Uint8Array.of(1)));
    const { registerDesktopIpc } = await import("./ipc");
    registerDesktopIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
      router,
    });
    const handler = harness.handlers.get("tammy:identity-current-user");
    const event = { sender: harness.webContents, senderFrame: harness.mainFrame };
    const exact = new Uint8Array(8_192);

    await expect(handler?.(event, exact)).resolves.toEqual(Uint8Array.of(1));
    expect(router.invoke).toHaveBeenCalledExactlyOnceWith(
      "tammy:identity-current-user",
      expect.any(Uint8Array),
    );
    expect(vi.mocked(router.invoke).mock.calls[0]?.[1]).not.toBe(exact);

    vi.mocked(router.invoke).mockClear();
    const oversized = new Uint8Array(8_193);
    Object.defineProperty(oversized, Symbol.iterator, {
      value: vi.fn(() => {
        throw new Error("COPY_SHOULD_NOT_BE_ATTEMPTED");
      }),
    });
    await expect(handler?.(event, oversized)).rejects.toMatchObject({
      code: "INVALID_RPC_REQUEST",
      message: "INVALID_RPC_REQUEST",
    });
    expect(router.invoke).not.toHaveBeenCalled();

    const verificationHandler = harness.handlers.get("tammy:organisation-record-verification");
    const verificationCap = Math.floor(1.1 * 1024 * 1024);
    await expect(verificationHandler?.(event, new Uint8Array(verificationCap))).resolves.toEqual(
      Uint8Array.of(1),
    );
    expect(router.invoke).toHaveBeenCalledExactlyOnceWith(
      "tammy:organisation-record-verification",
      expect.any(Uint8Array),
    );
    vi.mocked(router.invoke).mockClear();
    await expect(
      verificationHandler?.(event, new Uint8Array(verificationCap + 1)),
    ).rejects.toMatchObject({ code: "INVALID_RPC_REQUEST", message: "INVALID_RPC_REQUEST" });
    expect(router.invoke).not.toHaveBeenCalled();
  });

  it("rejects shared, forged, and subclassed command views before routing", async () => {
    const harness = createHarness();
    const router = desktopRouter(vi.fn());
    const { registerDesktopIpc } = await import("./ipc");
    registerDesktopIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
      router,
    });
    const handler = harness.handlers.get("tammy:identity-current-user");
    const event = { sender: harness.webContents, senderFrame: harness.mainFrame };
    class DerivedBytes extends Uint8Array {}
    const inputs = [
      new Uint8Array(new SharedArrayBuffer(1)),
      Object.create(Uint8Array.prototype),
      new DerivedBytes(1),
    ];

    for (const input of inputs) {
      await expect(handler?.(event, input)).rejects.toMatchObject({
        code: "INVALID_RPC_REQUEST",
        message: "INVALID_RPC_REQUEST",
      });
    }
    expect(router.invoke).not.toHaveBeenCalled();
  });

  it("caps mediated command bytes and transient UTF-8 before routing", async () => {
    const harness = createHarness();
    const router = {
      ...desktopRouter(vi.fn()),
      importMachineCredential: vi.fn(async () => Uint8Array.of(2)),
      importSbrProductId: vi.fn(async () => Uint8Array.of(3)),
    };
    const { registerDesktopIpc } = await import("./ipc");
    registerDesktopIpc({
      applicationUrl: harness.applicationUrl,
      getSystemDiagnostics: harness.getSystemDiagnostics,
      ipcMain: harness.ipcMain,
      mainWindow: harness.mainWindow,
      router,
    });
    const event = { sender: harness.webContents, senderFrame: harness.mainFrame };
    const credentialHandler = harness.handlers.get("tammy:sbr-credential-import");
    const productHandler = harness.handlers.get("tammy:sbr-product-id-import");
    const handle = "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb";

    await expect(
      credentialHandler?.(event, {
        command: new Uint8Array(8_192),
        handle,
        password: "a".repeat(1_024),
      }),
    ).resolves.toEqual(Uint8Array.of(2));
    expect(router.importMachineCredential).toHaveBeenCalledTimes(1);

    vi.mocked(router.importMachineCredential).mockClear();
    const oversizedCommand = new Uint8Array(8_193);
    Object.defineProperty(oversizedCommand, Symbol.iterator, {
      value: vi.fn(() => {
        throw new Error("COPY_SHOULD_NOT_BE_ATTEMPTED");
      }),
    });
    for (const input of [
      { command: oversizedCommand, handle, password: "ok" },
      { command: Uint8Array.of(1), handle, password: "a".repeat(2_000_000) },
      { command: Uint8Array.of(1), handle, password: "😀".repeat(257) },
      { command: Uint8Array.of(1), handle, password: "\ud800" },
    ]) {
      await expect(credentialHandler?.(event, input)).rejects.toMatchObject({
        code: "INVALID_RPC_REQUEST",
        message: "INVALID_RPC_REQUEST",
      });
    }
    expect(router.importMachineCredential).not.toHaveBeenCalled();

    await expect(
      productHandler?.(event, {
        command: Uint8Array.of(1),
        productId: "😀".repeat(256),
      }),
    ).resolves.toEqual(Uint8Array.of(3));
    vi.mocked(router.importSbrProductId).mockClear();
    await expect(
      productHandler?.(event, {
        command: Uint8Array.of(1),
        productId: "x".repeat(2_000_000),
      }),
    ).rejects.toMatchObject({ code: "INVALID_RPC_REQUEST", message: "INVALID_RPC_REQUEST" });
    expect(router.importSbrProductId).not.toHaveBeenCalled();
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

    expect(exposeInMainWorld).toHaveBeenCalledTimes(2);
    expect(exposeInMainWorld).toHaveBeenCalledWith("tammy", expect.any(Object));
    expect(exposeInMainWorld).toHaveBeenCalledWith("tammyLaunchScenario", "accounting");
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
      "createAccount",
      "listAccounts",
      "postManualJournal",
      "listJournals",
      "getJournal",
      "getTrialBalance",
      "importBankStatement",
      "listBankStatementLines",
      "matchBankStatementLine",
      "completeBankReconciliation",
      "getBankingSummary",
      "ingestDocument",
      "listDocuments",
      "getDocument",
      "saveDocumentReview",
      "createBasDraft",
      "getCurrentBasDraft",
      "getReportingCapability",
      "getAttentionSummary",
      "getCurrentUser",
      "enrolTotp",
      "confirmTotp",
      "assertTotp",
      "getOrganisation",
      "recordEntityVerification",
      "getSbrReadiness",
      "getMachineCredentialStatus",
      "removeMachineCredential",
      "removeSbrProductId",
      "runSbrReadinessFixture",
      "selectMachineCredentialFile",
      "importMachineCredential",
      "replaceMachineCredential",
      "unlockMachineCredential",
      "importSbrProductId",
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
