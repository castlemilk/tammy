import { afterEach, describe, expect, it, vi } from "vitest";

import manifest from "../shared/conveyor-manifest.json";
import { createRestrictedConveyorBridge, exposeRestrictedConveyorBridge } from "./conveyor-bridge";

describe("restricted Conveyor preload bridge", () => {
  it("exposes only the structural Conveyor bridge members", () => {
    const exposeInMainWorld = vi.fn();
    const bridge = createRestrictedConveyorBridge({ invoke: vi.fn(), sendSync: vi.fn(() => manifest) });

    exposeRestrictedConveyorBridge({ exposeInMainWorld }, bridge);

    expect(exposeInMainWorld).toHaveBeenCalledExactlyOnceWith("conveyor", bridge);
    expect(Object.keys(bridge)).toEqual(["invoke", "manifest", "subscribe"]);
    expect(Object.isFrozen(bridge)).toBe(true);
  });

  it("allows only exact own channel, method, and zero-or-one argument contracts before IPC", async () => {
    const invoke = vi.fn(async () => ({ ok: true }));
    const bridge = createRestrictedConveyorBridge({ invoke, sendSync: vi.fn(() => manifest) });

    await expect(bridge.invoke("conveyor:system", "getSystemDiagnostics")).resolves.toEqual({ ok: true });
    await expect(bridge.invoke("conveyor:sbr", "selectMachineCredentialFile")).resolves.toEqual({ ok: true });
    await expect(bridge.invoke("conveyor:workspace", "createWorkspace", { name: "Tammy" })).resolves.toEqual({ ok: true });
    expect(invoke).toHaveBeenNthCalledWith(1, "conveyor:system", "getSystemDiagnostics");
    expect(invoke).toHaveBeenNthCalledWith(2, "conveyor:sbr", "selectMachineCredentialFile");
    expect(invoke).toHaveBeenNthCalledWith(3, "conveyor:workspace", "createWorkspace", { name: "Tammy" });

    for (const [channel, method, args] of [
      ["constructor", "getSystemDiagnostics", []],
      ["conveyor:toString", "getSystemDiagnostics", []],
      ["conveyor:system", "constructor", []],
      ["conveyor:system", "getSystemDiagnostics", [{}]],
      ["conveyor:sbr", "selectMachineCredentialFile", [{}]],
      ["conveyor:workspace", "createWorkspace", []],
      ["conveyor:workspace", "createWorkspace", [{}, {}]],
    ] as const) {
      await expect(bridge.invoke(channel, method, ...args)).rejects.toThrow("INVALID_CONVEYOR_REQUEST");
    }
    expect(invoke).toHaveBeenCalledTimes(3);
  });

  it("uses only the guarded synchronous manifest IPC and returns an independent deep-frozen copy", () => {
    const source = structuredClone(manifest);
    const secondSource = structuredClone(manifest);
    const sendSync = vi.fn()
      .mockReturnValueOnce(source)
      .mockReturnValueOnce(secondSource)
      .mockReturnValueOnce(source);
    const bridge = createRestrictedConveyorBridge({ invoke: vi.fn(), sendSync });

    const first = bridge.manifest();
    source.system.getSystemDiagnostics = "command";
    const second = bridge.manifest();

    expect(sendSync).toHaveBeenCalledTimes(2);
    expect(sendSync).toHaveBeenNthCalledWith(1, "conveyor:manifest");
    expect(Object.isFrozen(first)).toBe(true);
    expect(Object.isFrozen(first.system)).toBe(true);
    expect(first).toEqual(manifest);
    expect(first.system?.getSystemDiagnostics).toBe("query");
    expect(first).not.toBe(second);
    expect(first.system).not.toBe(second.system);
    expect(() => bridge.manifest()).toThrow("INVALID_CONVEYOR_MANIFEST");
    expect(() => { (first.system as Record<string, unknown>).getSystemDiagnostics = "command"; }).toThrow();
  });

  it("rejects malformed, extra, nested, mutated, and prototype-bearing manifests", () => {
    const invalids: unknown[] = [
      { ...manifest, system: { getSystemDiagnostics: "command" } },
      { ...manifest, system: { ...manifest.system, extra: "query" } },
      { ...manifest, extra: {} },
      Object.assign(Object.create({ system: manifest.system }), manifest),
      { ...manifest, system: Object.assign(Object.create({ inherited: "query" }), manifest.system) },
    ];

    for (const response of invalids) {
      const bridge = createRestrictedConveyorBridge({ invoke: vi.fn(), sendSync: vi.fn(() => response) });
      expect(() => bridge.manifest()).toThrow("INVALID_CONVEYOR_MANIFEST");
    }
  });

  it("does not attach a listener because subscriptions are disabled", () => {
    const on = vi.fn();
    const bridge = createRestrictedConveyorBridge({ invoke: vi.fn(), on, sendSync: vi.fn(() => manifest) });

    expect(() => bridge.subscribe("conveyor:system", vi.fn())).toThrow("IPC_SUBSCRIPTIONS_DISABLED");
    expect(on).not.toHaveBeenCalled();
  });

  it("maps rejected transport errors to a stable code without source text", async () => {
    const bridge = createRestrictedConveyorBridge({
      invoke: vi.fn(async () => Promise.reject(new Error("secret /private/path from core"))),
      sendSync: vi.fn(() => manifest),
    });

    await expect(bridge.invoke("conveyor:system", "getSystemDiagnostics")).rejects.toThrow("IPC_TRANSPORT_FAILED");
    await expect(bridge.invoke("conveyor:system", "getSystemDiagnostics")).rejects.not.toThrow(/secret|path|core/i);
  });
});
