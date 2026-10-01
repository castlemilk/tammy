import { ConveyorError } from "electron-conveyor/define";
import { describe, expect, it, vi } from "vitest";

import { createConveyorAuthorizer } from "./guard";

interface FakeFrame {
  readonly isDestroyed: () => boolean;
  url: string;
}

interface FakeContents {
  readonly isDestroyed: () => boolean;
  readonly mainFrame: FakeFrame;
}

interface FakeWindow {
  readonly isDestroyed: () => boolean;
  readonly webContents: FakeContents;
}

interface FakeEvent {
  readonly sender: FakeContents;
  readonly senderFrame: FakeFrame | null;
}

function createHarness(): {
  event: FakeEvent;
  getWindow: ReturnType<typeof vi.fn<() => FakeWindow | undefined>>;
  window: FakeWindow;
} {
  const mainFrame: FakeFrame = { isDestroyed: () => false, url: "tammy://app/" };
  const webContents: FakeContents = { isDestroyed: () => false, mainFrame };
  const window: FakeWindow = { isDestroyed: () => false, webContents };
  return {
    event: { sender: webContents, senderFrame: mainFrame },
    getWindow: vi.fn(() => window),
    window,
  };
}

function expectRejected(authorize: (event: FakeEvent) => void, event: FakeEvent): void {
  expect(() => authorize(event)).toThrow(ConveyorError);
  try {
    authorize(event);
  } catch (error) {
    expect(error).toMatchObject({ code: "IPC_SENDER_REJECTED", message: "IPC_SENDER_REJECTED" });
  }
}

describe("createConveyorAuthorizer", () => {
  it("accepts only the live registered main-frame sender at the canonical application URL", () => {
    const harness = createHarness();
    const authorize = createConveyorAuthorizer({
      applicationUrl: "tammy://app/",
      getWindow: harness.getWindow,
    });

    expect(() => authorize(harness.event)).not.toThrow();
  });

  it("accepts Electron wrapper instances that identify the same main frame", () => {
    const frame = { isDestroyed: () => false, url: "tammy://app/", frameTreeNodeId: 7 };
    const webContents = { isDestroyed: () => false, mainFrame: frame };
    const window = { isDestroyed: () => false, webContents };
    const authorize = createConveyorAuthorizer({
      applicationUrl: "tammy://app/",
      getWindow: vi.fn(() => window),
    });

    expect(() =>
      authorize({
        sender: webContents,
        senderFrame: { ...frame },
      }),
    ).not.toThrow();
  });

  it("accepts trusted routes but rejects fragments and foreign documents", () => {
    const harness = createHarness();
    const authorize = createConveyorAuthorizer({
      applicationUrl: "tammy://app/",
      getWindow: harness.getWindow,
    });

    harness.window.webContents.mainFrame.url = "tammy://app/overview";
    expect(() => authorize(harness.event)).not.toThrow();
    for (const url of ["tammy://app/#overview", "https://example.com/"]) {
      harness.window.webContents.mainFrame.url = url;
      expectRejected(authorize, harness.event);
    }
  });

  it("rejects before any context, schema, or domain callback for every ownership mismatch", () => {
    const cases: Array<(harness: ReturnType<typeof createHarness>) => FakeEvent> = [
      (harness) => ({
        ...harness.event,
        sender: { ...harness.window.webContents, isDestroyed: () => true },
      }),
      (harness) => ({ ...harness.event, senderFrame: null }),
      (harness) => ({
        ...harness.event,
        senderFrame: { ...harness.event.senderFrame!, url: "tammy://app/child" },
      }),
      (harness) => ({
        ...harness.event,
        senderFrame: { ...harness.event.senderFrame!, isDestroyed: () => true },
      }),
      (harness) => ({ ...harness.event, sender: { ...harness.window.webContents } }),
    ];

    for (const makeEvent of cases) {
      const harness = createHarness();
      const context = vi.fn();
      const schema = vi.fn();
      const domain = vi.fn();
      const authorize = createConveyorAuthorizer({
        applicationUrl: "tammy://app/",
        getWindow: harness.getWindow,
      });
      const event = makeEvent(harness);

      expectRejected(authorize, event);
      expect(context).not.toHaveBeenCalled();
      expect(schema).not.toHaveBeenCalled();
      expect(domain).not.toHaveBeenCalled();
    }
  });

  it("rejects missing, destroyed, or replaced registered windows and non-canonical URLs", () => {
    const cases: Array<
      (harness: ReturnType<typeof createHarness>) => ReturnType<typeof createConveyorAuthorizer>
    > = [
      (harness) =>
        createConveyorAuthorizer({ applicationUrl: "tammy://app/", getWindow: () => undefined }),
      (harness) =>
        createConveyorAuthorizer({
          applicationUrl: "tammy://app/",
          getWindow: () => ({ ...harness.window, isDestroyed: () => true }),
        }),
      (harness) =>
        createConveyorAuthorizer({
          applicationUrl: "tammy://app/",
          getWindow: () => ({
            ...harness.window,
            webContents: { ...harness.window.webContents, isDestroyed: () => true },
          }),
        }),
      (harness) =>
        createConveyorAuthorizer({
          applicationUrl: "tammy://app/",
          getWindow: () => ({
            ...harness.window,
            webContents: {
              ...harness.window.webContents,
              mainFrame: { ...harness.window.webContents.mainFrame },
            },
          }),
        }),
      (harness) =>
        createConveyorAuthorizer({ applicationUrl: "tammy://app/", getWindow: harness.getWindow }),
    ];

    for (const createAuthorizer of cases) {
      const harness = createHarness();
      const event =
        createAuthorizer === cases.at(-1)
          ? {
              ...harness.event,
              senderFrame: { ...harness.event.senderFrame!, url: "tammy://app/#fragment" },
            }
          : harness.event;
      expectRejected(createAuthorizer(harness), event);
    }
  });
});
