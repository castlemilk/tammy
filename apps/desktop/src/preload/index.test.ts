import { afterEach, describe, expect, it, vi } from "vitest";

afterEach(() => {
  vi.doUnmock("electron");
  vi.resetModules();
});

describe("preload Conveyor entry", () => {
  it("exposes only the restricted Conveyor bridge and launch scenario", async () => {
    const exposeInMainWorld = vi.fn();
    const ipcRenderer = { invoke: vi.fn(), sendSync: vi.fn(() => ({})) };
    vi.doMock("electron", () => ({ contextBridge: { exposeInMainWorld }, ipcRenderer }));

    await import("./index");

    expect(exposeInMainWorld.mock.calls.map(([key]) => key)).toEqual([
      "conveyor",
      "tammyLaunchScenario",
    ]);
    expect(exposeInMainWorld).not.toHaveBeenCalledWith("tammy", expect.anything());
    const bridge = exposeInMainWorld.mock.calls[0]?.[1] as Record<string, unknown>;
    expect(Object.keys(bridge)).toEqual(["invoke", "manifest", "subscribe"]);
  });
});
