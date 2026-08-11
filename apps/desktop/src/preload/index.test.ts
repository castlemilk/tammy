import { afterEach, describe, expect, it, vi } from "vitest";

import { ATTENTION_SUMMARY_CHANNEL } from "../main/rpc-router";
import { REPORTING_CAPABILITY_CHANNEL } from "../shared/desktop-api";
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
    const invoke = vi.fn(async (_channel: string, ..._args: unknown[]) => response);
    const api = createTammyDesktopAPI(invoke);
    const request = Uint8Array.of(1, 2, 3);

    await expect(api.getAttentionSummary(request)).resolves.toEqual(response);
    expect(invoke).toHaveBeenCalledExactlyOnceWith(ATTENTION_SUMMARY_CHANNEL, request);
    expect(api).not.toHaveProperty("invoke");
    expect(api).not.toHaveProperty("request");
  });

  it("exposes reporting capability as the exact named binary method", async () => {
    const exposeInMainWorld = vi.fn();
    vi.doMock("electron", () => ({
      contextBridge: { exposeInMainWorld },
      ipcRenderer: { invoke: vi.fn() },
    }));
    const { createTammyDesktopAPI } = await import("./index");
    const response = Uint8Array.of(4, 5, 6);
    const invoke = vi.fn(async (_channel: string, ..._args: unknown[]) => response);
    const api = createTammyDesktopAPI(invoke);
    const request = Uint8Array.of(1, 2, 3);

    const result = await api.getReportingCapability(request);

    expect(result).toEqual(response);
    expect(result).not.toBe(response);
    expect(invoke).toHaveBeenCalledExactlyOnceWith(REPORTING_CAPABILITY_CHANNEL, request);
    expect(invoke.mock.calls[0]?.[1]).not.toBe(request);
  });
});
