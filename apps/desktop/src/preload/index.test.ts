import { afterEach, describe, expect, it, vi } from "vitest";

import { ATTENTION_SUMMARY_CHANNEL } from "../main/rpc-router";
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

  it("exposes Overview as one named binary method without a generic RPC primitive", async () => {
    const exposeInMainWorld = vi.fn();
    vi.doMock("electron", () => ({
      contextBridge: { exposeInMainWorld },
      ipcRenderer: { invoke: vi.fn() },
    }));
    const { createTammyDesktopAPI } = await import("./index");
    const response = Uint8Array.of(4, 5, 6);
    const invoke = vi.fn(async () => response);
    const api = createTammyDesktopAPI(invoke);
    const request = Uint8Array.of(1, 2, 3);

    await expect(api.getAttentionSummary(request)).resolves.toEqual(response);
    expect(invoke).toHaveBeenCalledExactlyOnceWith(ATTENTION_SUMMARY_CHANNEL, request);
    expect(api).not.toHaveProperty("invoke");
    expect(api).not.toHaveProperty("request");
  });
});
