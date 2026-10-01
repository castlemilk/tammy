import type { StandardSchemaV1 } from "electron-conveyor/define";
import { defineModule, query } from "electron-conveyor/define";
import { expect, test, vi } from "vitest";
import { z } from "zod";

vi.mock("electron", () => ({
  app: { isPackaged: true },
  BrowserWindow: { fromWebContents: () => null },
  ipcMain: {
    handlers: new Map<string, Listener>(),
    listeners: new Map<string, Set<Listener>>(),
    handle(channel: string, listener: Listener) {
      this.handlers.set(channel, listener);
    },
    removeAllListeners(channel: string) {
      this.listeners.delete(channel);
    },
    removeHandler(channel: string) {
      this.handlers.delete(channel);
    },
    on(channel: string, listener: Listener) {
      const channelListeners = this.listeners.get(channel) ?? new Set<Listener>();
      channelListeners.add(listener);
      this.listeners.set(channel, channelListeners);
    },
  },
}));

const { createRouter } = await import("electron-conveyor/main");
const { ipcMain: defaultIpcMain } = await import("electron");

type Listener = (event: FakeEvent, ...args: unknown[]) => unknown;

interface FakeEvent {
  asyncAuthorizer?: boolean;
  rejected?: boolean;
  returnValue?: unknown;
  sender: {
    id: number;
  };
  thenableAuthorizer?: boolean;
}

function createFakeIpcMain({ rejectDuplicateHandlers = false } = {}) {
  const handlers = new Map<string, Listener>();
  const listeners = new Map<string, Set<Listener>>();
  const removedHandlers: string[] = [];

  return {
    handlers,
    listeners,
    removedHandlers,
    handle(channel: string, listener: Listener) {
      if (rejectDuplicateHandlers && handlers.has(channel)) {
        throw new Error(`Handler already registered: ${channel}`);
      }
      handlers.set(channel, listener);
    },
    removeHandler(channel: string) {
      removedHandlers.push(channel);
      handlers.delete(channel);
    },
    on(channel: string, listener: Listener) {
      const channelListeners = listeners.get(channel) ?? new Set<Listener>();
      channelListeners.add(listener);
      listeners.set(channel, channelListeners);
      return this;
    },
    removeListener(channel: string, listener: Listener) {
      listeners.get(channel)?.delete(listener);
      return this;
    },
    invoke(channel: string, event: FakeEvent, ...args: unknown[]) {
      const handler = handlers.get(channel);
      if (!handler) throw new Error(`Missing handler: ${channel}`);
      return handler(event, ...args);
    },
    sendSync(channel: string, event: FakeEvent) {
      for (const listener of listeners.get(channel) ?? []) listener(event);
      return event.returnValue;
    },
  };
}

test("router enforces the injected-boundary security and ownership contract", async () => {
  const ipcMain = createFakeIpcMain();
  const calls: string[] = [];
  const contextCalls: string[] = [];
  const sentinel = () => undefined;
  const acceptedEvent: FakeEvent = { sender: { id: 1 } };
  const rejectedEvent: FakeEvent = { rejected: true, sender: { id: 2 } };

  ipcMain.on("conveyor:manifest", sentinel);

  const modules = {
    secure: defineModule({
      lookup: query(
        z.string().superRefine((input, context) => {
          calls.push("validate");
          if (input === "invalid") context.addIssue({ code: "custom", message: "invalid input" });
        }) as unknown as StandardSchemaV1,
        ({ input }) => {
          calls.push("handle");
          return input;
        },
      ),
      status: query(() => "ready"),
    }),
  };

  const router = createRouter(modules, {
    ipcMain: ipcMain as unknown as Pick<
      Electron.IpcMain,
      "handle" | "removeHandler" | "on" | "removeListener"
    >,
    streams: false,
    createContext() {
      contextCalls.push("context");
      return {};
    },
    authorize(event) {
      calls.push("authorize");
      if ((event as FakeEvent).rejected) throw new Error("untrusted sender");
    },
  });

  await expect(ipcMain.invoke("conveyor:secure", acceptedEvent, "lookup", "ok")).resolves.toEqual({
    ok: true,
    data: "ok",
  });
  expect(calls).toEqual(["authorize", "validate", "handle"]);
  expect(contextCalls).toEqual(["context"]);

  calls.length = 0;
  contextCalls.length = 0;
  await expect(ipcMain.invoke("conveyor:secure", acceptedEvent, "missing")).resolves.toMatchObject({
    ok: false,
    error: { code: "UNKNOWN_PROCEDURE" },
  });
  expect(calls).toEqual(["authorize"]);
  expect(contextCalls).toEqual(["context"]);

  calls.length = 0;
  contextCalls.length = 0;
  await expect(
    ipcMain.invoke("conveyor:secure", acceptedEvent, "lookup", "invalid"),
  ).resolves.toMatchObject({
    ok: false,
    error: { code: "INVALID_INPUT" },
  });
  expect(calls).toEqual(["authorize", "validate"]);
  expect(contextCalls).toEqual(["context"]);

  calls.length = 0;
  contextCalls.length = 0;
  await expect(ipcMain.invoke("conveyor:secure", rejectedEvent, "lookup", "ok")).resolves.toEqual({
    ok: false,
    error: { code: "IPC_SENDER_REJECTED", message: "IPC_SENDER_REJECTED" },
  });
  expect(calls).toEqual(["authorize"]);
  expect(contextCalls).toEqual([]);

  calls.length = 0;
  contextCalls.length = 0;
  await expect(ipcMain.invoke("conveyor:secure", acceptedEvent)).resolves.toMatchObject({
    ok: false,
    error: { code: "INVALID_INPUT" },
  });
  expect(calls).toEqual(["authorize"]);
  expect(contextCalls).toEqual([]);

  await expect(ipcMain.invoke("conveyor:secure", acceptedEvent, "status")).resolves.toEqual({
    ok: true,
    data: "ready",
  });
  await expect(
    ipcMain.invoke("conveyor:secure", acceptedEvent, "lookup", "ok", "unexpected"),
  ).resolves.toMatchObject({ ok: false, error: { code: "INVALID_INPUT" } });

  expect(ipcMain.handlers.has("conveyor:stream:start")).toBe(false);
  expect(ipcMain.handlers.has("conveyor:stream:cancel")).toBe(false);

  expect(ipcMain.sendSync("conveyor:manifest", acceptedEvent)).toEqual({
    secure: { lookup: "query", status: "query" },
  });
  expect(ipcMain.sendSync("conveyor:manifest", rejectedEvent)).toEqual({});

  expect(() =>
    createRouter(modules, {
      ipcMain: ipcMain as unknown as Pick<
        Electron.IpcMain,
        "handle" | "removeHandler" | "on" | "removeListener"
      >,
      streams: false,
      authorize() {},
    }),
  ).toThrow(/already owned/i);

  router.dispose();
  router.dispose();
  expect(ipcMain.handlers.has("conveyor:secure")).toBe(false);
  expect(ipcMain.listeners.get("conveyor:manifest")).toEqual(new Set([sentinel]));
});

test("router fails closed for promise and thenable authorizers", async () => {
  const ipcMain = createFakeIpcMain();
  const calls: string[] = [];
  const contextCalls: string[] = [];
  const modules = {
    secure: defineModule({
      lookup: query(
        z.string().superRefine(() => {
          calls.push("validate");
        }) as unknown as StandardSchemaV1,
        () => {
          calls.push("handle");
          return "secret";
        },
      ),
    }),
  };
  const router = createRouter(modules, {
    ipcMain: ipcMain as unknown as Pick<
      Electron.IpcMain,
      "handle" | "removeHandler" | "on" | "removeListener"
    >,
    streams: false,
    createContext() {
      contextCalls.push("context");
      return {};
    },
    authorize(event) {
      calls.push("authorize");
      if ((event as FakeEvent).asyncAuthorizer) return Promise.resolve() as never;
      if ((event as FakeEvent).thenableAuthorizer) {
        return {
          // biome-ignore lint/suspicious/noThenProperty: This fixture verifies rejection of a non-Promise thenable authorizer.
          then(resolve: (value: undefined) => void) {
            resolve(undefined);
          },
        } as never;
      }
    },
  });

  for (const event of [
    { asyncAuthorizer: true, sender: { id: 3 } },
    { thenableAuthorizer: true, sender: { id: 4 } },
  ]) {
    calls.length = 0;
    contextCalls.length = 0;
    await expect(ipcMain.invoke("conveyor:secure", event, "lookup", "ok")).resolves.toEqual({
      ok: false,
      error: { code: "IPC_SENDER_REJECTED", message: "IPC_SENDER_REJECTED" },
    });
    expect(calls).toEqual(["authorize"]);
    expect(contextCalls).toEqual([]);
    expect(ipcMain.sendSync("conveyor:manifest", event)).toEqual({});
  }

  router.dispose();
});

test("injected boundaries preserve unrelated stream lifecycle handlers", () => {
  const ipcMain = createFakeIpcMain({ rejectDuplicateHandlers: true });
  const streamStartSentinel = () => undefined;
  const modules = { secure: defineModule({ lookup: query(() => "ok") }) };
  ipcMain.handlers.set("conveyor:stream:start", streamStartSentinel);

  expect(() =>
    createRouter(modules, {
      ipcMain: ipcMain as unknown as Pick<
        Electron.IpcMain,
        "handle" | "removeHandler" | "on" | "removeListener"
      >,
    }),
  ).toThrow(/Handler already registered/);
  expect(ipcMain.handlers.get("conveyor:stream:start")).toBe(streamStartSentinel);
  expect(ipcMain.removedHandlers).not.toContain("conveyor:stream:start");
  expect(ipcMain.handlers.has("conveyor:secure")).toBe(false);

  ipcMain.handlers.delete("conveyor:stream:start");
  const router = createRouter(modules, {
    ipcMain: ipcMain as unknown as Pick<
      Electron.IpcMain,
      "handle" | "removeHandler" | "on" | "removeListener"
    >,
  });
  expect(ipcMain.handlers.has("conveyor:secure")).toBe(true);
  router.dispose();
  expect(ipcMain.handlers.has("conveyor:secure")).toBe(false);
  expect(ipcMain.handlers.has("conveyor:stream:start")).toBe(false);
  expect(ipcMain.handlers.has("conveyor:stream:cancel")).toBe(false);
});

test("router keeps streams enabled when options are absent", () => {
  const ipcMain = defaultIpcMain as unknown as ReturnType<typeof createFakeIpcMain>;
  ipcMain.handlers.clear();
  ipcMain.listeners.clear();

  createRouter({ secure: defineModule({ lookup: query(() => "ok") }) });

  expect(ipcMain.handlers.has("conveyor:stream:start")).toBe(true);
  expect(ipcMain.handlers.has("conveyor:stream:cancel")).toBe(true);
});
