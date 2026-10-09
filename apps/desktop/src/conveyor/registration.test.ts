import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("electron", () => ({
  app: { isPackaged: true },
  BrowserWindow: { fromWebContents: () => undefined },
}));

import { registerTammyConveyor } from "./registration";

afterEach(() => {
  vi.doUnmock("electron-conveyor/main");
  vi.resetModules();
});

type Handler = (event: unknown, ...args: unknown[]) => Promise<unknown>;
type Listener = (event: { returnValue?: unknown }) => void;

function createIpcMain() {
  const handlers = new Map<string, Handler>();
  const listeners = new Map<string, Listener>();
  return {
    handlers,
    listeners,
    handle: vi.fn((channel: string, handler: Handler) => {
      handlers.set(channel, handler);
    }),
    removeHandler: vi.fn((channel: string) => {
      handlers.delete(channel);
    }),
    on: vi.fn((channel: string, listener: Listener) => {
      listeners.set(channel, listener);
    }),
    removeListener: vi.fn((channel: string, listener: Listener) => {
      if (listeners.get(channel) === listener) listeners.delete(channel);
    }),
  };
}

function createWindow() {
  const frame = { isDestroyed: () => false, url: "tammy://app/" };
  const contents = { isDestroyed: () => false, mainFrame: frame };
  return {
    window: { isDestroyed: () => false, webContents: contents },
    event: { sender: contents, senderFrame: frame },
  };
}

function options(
  ipcMain: ReturnType<typeof createIpcMain>,
  getWindow = () => createWindow().window,
) {
  return {
    applicationUrl: "tammy://app/",
    client: {
      getDiagnostics: vi.fn(async () => ({
        apiVersion: "tammy.v1",
        coreVersion: "test",
        runtimeMode: "offline",
        networkRequired: false,
      })),
    } as never,
    fileIntake: {
      clear: vi.fn(),
      selectMachineCredentialFile: vi.fn(),
      consumeMachineCredentialFile: vi.fn(),
    } as never,
    getWindow: getWindow as never,
    ipcMain: ipcMain as never,
  };
}

describe("owned Conveyor registration", () => {
  it("passes a shared frozen context and a live window authorizer to the router", async () => {
    const createRouter = vi.fn((..._args: unknown[]) => ({ dispose: vi.fn() }));
    vi.resetModules();
    vi.doMock("electron-conveyor/main", () => ({ createRouter }));
    const { registerTammyConveyor: register } = await import("./registration");
    const ipcMain = createIpcMain();
    const first = createWindow();
    const second = createWindow();
    let currentWindow = first.window;
    const client = { getDiagnostics: vi.fn() };
    const fileIntake = {
      clear: vi.fn(),
      selectMachineCredentialFile: vi.fn(),
      consumeMachineCredentialFile: vi.fn(),
    };

    register({
      applicationUrl: "tammy://app/",
      client: client as never,
      fileIntake: fileIntake as never,
      getWindow: () => currentWindow as never,
      ipcMain: ipcMain as never,
    });

    expect(createRouter).toHaveBeenCalledOnce();
    const configuration = createRouter.mock.calls[0]?.[1] as unknown as {
      authorize(event: unknown): void;
      createContext(): { client: unknown; fileIntake: unknown };
      ipcMain: unknown;
      streams: boolean;
    };
    const firstContext = configuration.createContext();
    const secondContext = configuration.createContext();
    expect(firstContext).toBe(secondContext);
    expect(Object.isFrozen(firstContext)).toBe(true);
    expect(firstContext.client).toBe(client);
    expect(firstContext.fileIntake).toBe(fileIntake);
    expect(configuration.ipcMain).toBe(ipcMain);
    expect(configuration.streams).toBe(false);

    expect(() => configuration.authorize(first.event)).not.toThrow();
    currentWindow = second.window;
    expect(() => configuration.authorize(first.event)).toThrow("IPC_SENDER_REJECTED");
    expect(() => configuration.authorize(second.event)).not.toThrow();
  });

  it("registers exactly nine procedure handlers and one guarded manifest with streams disabled", () => {
    const ipcMain = createIpcMain();
    const harness = createWindow();
    const dispose = registerTammyConveyor(options(ipcMain, () => harness.window));

    expect([...ipcMain.handlers.keys()]).toEqual([
      "conveyor:system",
      "conveyor:workspace",
      "conveyor:organisation",
      "conveyor:accounting",
      "conveyor:banking",
      "conveyor:documents",
      "conveyor:purchases",
      "conveyor:reporting",
      "conveyor:sbr",
    ]);
    expect([...ipcMain.listeners.keys()]).toEqual(["conveyor:manifest"]);
    expect(ipcMain.handlers).not.toHaveProperty("conveyor:stream:start");
    dispose();
  });

  it("authorizes before building context and shares one frozen context across live calls", async () => {
    const ipcMain = createIpcMain();
    const harness = createWindow();
    const client = {
      getDiagnostics: vi.fn(async () => ({
        apiVersion: "tammy.v1",
        coreVersion: "test",
        runtimeMode: "offline",
        networkRequired: false,
      })),
    };
    const dispose = registerTammyConveyor({
      ...options(ipcMain, () => harness.window),
      client: client as never,
    });
    const handler = ipcMain.handlers.get("conveyor:system")!;

    const rejected = await handler({ sender: {}, senderFrame: null }, "getSystemDiagnostics");
    expect(rejected).toMatchObject({ ok: false, error: { code: "IPC_SENDER_REJECTED" } });
    expect(client.getDiagnostics).not.toHaveBeenCalled();

    await expect(handler(harness.event, "getSystemDiagnostics")).resolves.toMatchObject({
      ok: true,
    });
    await expect(handler(harness.event, "getSystemDiagnostics")).resolves.toMatchObject({
      ok: true,
    });
    expect(client.getDiagnostics).toHaveBeenCalledTimes(2);
    dispose();
  });

  it("guards the synchronous manifest before returning it", () => {
    const ipcMain = createIpcMain();
    const harness = createWindow();
    const dispose = registerTammyConveyor(options(ipcMain, () => harness.window));
    const listener = ipcMain.listeners.get("conveyor:manifest")!;
    const denied: { returnValue?: unknown } = {};
    const accepted: { returnValue?: unknown } = {};

    listener(denied as never);
    listener(Object.assign(accepted, harness.event) as never);

    expect(denied.returnValue).toEqual({});
    expect(accepted.returnValue).toMatchObject({ system: { getSystemDiagnostics: "query" } });
    dispose();
  });

  it("rolls back partial registration, rejects a second owner, and disposes idempotently", () => {
    const partial = createIpcMain();
    partial.handle
      .mockImplementationOnce((channel: string, handler: Handler) => {
        partial.handlers.set(channel, handler);
      })
      .mockImplementationOnce(() => {
        throw new Error("partial registration failure");
      });
    expect(() => registerTammyConveyor(options(partial))).toThrow("partial registration failure");
    expect(partial.handlers).toHaveLength(0);
    expect(partial.listeners).toHaveLength(0);

    const ipcMain = createIpcMain();
    const first = registerTammyConveyor(options(ipcMain));
    expect(() => registerTammyConveyor(options(ipcMain))).toThrow("already owned");
    first();
    first();
    expect(ipcMain.handlers).toHaveLength(0);
    expect(ipcMain.listeners).toHaveLength(0);
    const second = registerTammyConveyor(options(ipcMain));
    second();
  });
});
