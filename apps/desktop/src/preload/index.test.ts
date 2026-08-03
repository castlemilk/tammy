import { afterEach, describe, expect, it, vi } from "vitest";

import preloadMethods from "../shared/preload-methods.json";

afterEach(() => {
  vi.doUnmock("electron");
  vi.resetModules();
});

describe("preload desktop bridge", () => {
  it("constructs exactly the production preload method manifest", async () => {
    const exposeInMainWorld = vi.fn();
    vi.doMock("electron", () => ({
      contextBridge: { exposeInMainWorld },
      ipcRenderer: { invoke: vi.fn() },
    }));
    const { createTammyDesktopAPI } = await import("./index");
    const invoke = vi.fn();

    const api = createTammyDesktopAPI(invoke);

    expect(Object.keys(api)).toEqual(preloadMethods);
    expect(Object.isFrozen(api)).toBe(true);
  });
});
