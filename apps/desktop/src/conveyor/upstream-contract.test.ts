import { defineModule, query } from "electron-conveyor/define";
import type { StandardSchemaV1 } from "electron-conveyor/define";
import { z } from "zod";
import { expect, test, vi } from "vitest";

vi.mock("electron", () => ({
  app: { isPackaged: true },
  BrowserWindow: { fromWebContents: () => null },
  ipcMain: {
    handle: vi.fn(),
    removeAllListeners: vi.fn(),
    removeHandler: vi.fn(),
    on: vi.fn(),
  },
}));

const { createRouter } = await import("electron-conveyor/main");

type Listener = (event: FakeEvent, ...args: unknown[]) => unknown;

interface FakeEvent {
  rejected?: boolean;
  returnValue?: unknown;
  sender: {
    id: number;
  };
}

function createFakeIpcMain() {
  const handlers = new Map<string, Listener>();
  const listeners = new Map<string, Set<Listener>>();

  return {
    handlers,
    listeners,
    handle(channel: string, listener: Listener) {
      handlers.set(channel, listener);
    },
    removeHandler(channel: string) {
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
    }),
  };

  const router = createRouter(modules, {
    ipcMain: ipcMain as unknown as Pick<
      Electron.IpcMain,
      "handle" | "removeHandler" | "on" | "removeListener"
    >,
    streams: false,
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

  calls.length = 0;
  await expect(ipcMain.invoke("conveyor:secure", acceptedEvent, "missing")).resolves.toMatchObject({
    ok: false,
    error: { code: "UNKNOWN_PROCEDURE" },
  });
  expect(calls).toEqual(["authorize"]);

  calls.length = 0;
  await expect(ipcMain.invoke("conveyor:secure", acceptedEvent, "lookup", "invalid")).resolves.toMatchObject({
    ok: false,
    error: { code: "INVALID_INPUT" },
  });
  expect(calls).toEqual(["authorize", "validate"]);

  calls.length = 0;
  await expect(ipcMain.invoke("conveyor:secure", rejectedEvent, "lookup", "ok")).resolves.toEqual({
    ok: false,
    error: { code: "IPC_SENDER_REJECTED", message: expect.any(String) },
  });
  expect(calls).toEqual(["authorize"]);

  await expect(ipcMain.invoke("conveyor:secure", acceptedEvent)).resolves.toMatchObject({
    ok: false,
    error: { code: "INVALID_INPUT" },
  });
  await expect(
    ipcMain.invoke("conveyor:secure", acceptedEvent, "lookup", "ok", "unexpected"),
  ).resolves.toMatchObject({ ok: false, error: { code: "INVALID_INPUT" } });

  expect(ipcMain.handlers).not.toHaveProperty("conveyor:stream:start");
  expect(ipcMain.handlers).not.toHaveProperty("conveyor:stream:cancel");

  expect(ipcMain.sendSync("conveyor:manifest", acceptedEvent)).toEqual({
    secure: { lookup: "query" },
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
  expect(ipcMain.handlers).not.toHaveProperty("conveyor:secure");
  expect(ipcMain.listeners.get("conveyor:manifest")).toEqual(new Set([sentinel]));
});
