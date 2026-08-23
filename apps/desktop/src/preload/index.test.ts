import { afterEach, describe, expect, it, vi } from "vitest";

import { ATTENTION_SUMMARY_CHANNEL } from "../main/rpc-router";
import { REPORTING_CAPABILITY_CHANNEL } from "../shared/desktop-api";
import preloadMethods from "../shared/preload-methods.json";

afterEach(() => {
  vi.doUnmock("electron");
  vi.resetModules();
});

describe("preload desktop bridge", () => {
  const addedMethods = [
    "getCurrentUser",
    "enrolTotp",
    "confirmTotp",
    "assertTotp",
    "getOrganisation",
    "recordEntityVerification",
    "getSbrReadiness",
    "getMachineCredentialStatus",
    "removeMachineCredential",
    "runSbrReadinessFixture",
    "selectMachineCredentialFile",
    "importMachineCredential",
    "replaceMachineCredential",
    "unlockMachineCredential",
  ] as const;

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

  it("appends the exact frozen SBR preload surface in protocol order", async () => {
    const exposeInMainWorld = vi.fn();
    vi.doMock("electron", () => ({
      contextBridge: { exposeInMainWorld },
      ipcRenderer: { invoke: vi.fn() },
    }));
    const { createTammyDesktopAPI } = await import("./index");

    const api = createTammyDesktopAPI(vi.fn());

    expect(Object.keys(api).slice(-addedMethods.length)).toEqual(addedMethods);
    expect(Object.isFrozen(api)).toBe(true);
  });

  it("maps the exact added generic methods and defensively copies binary frames", async () => {
    const exposeInMainWorld = vi.fn();
    vi.doMock("electron", () => ({
      contextBridge: { exposeInMainWorld },
      ipcRenderer: { invoke: vi.fn() },
    }));
    const { createTammyDesktopAPI } = await import("./index");
    const response = Uint8Array.of(4, 5, 6);
    const invoke = vi.fn(async (_channel: string, ..._args: unknown[]) => response);
    const api = createTammyDesktopAPI(invoke) as unknown as Record<
      string,
      (request: Uint8Array) => Promise<Uint8Array>
    >;
    const mappings = [
      ["getCurrentUser", "tammy:identity-current-user"],
      ["enrolTotp", "tammy:identity-enrol-totp"],
      ["confirmTotp", "tammy:identity-confirm-totp"],
      ["assertTotp", "tammy:identity-assert-totp"],
      ["getOrganisation", "tammy:organisation-get"],
      ["recordEntityVerification", "tammy:organisation-record-verification"],
      ["getSbrReadiness", "tammy:sbr-readiness"],
      ["getMachineCredentialStatus", "tammy:sbr-credential-status"],
      ["removeMachineCredential", "tammy:sbr-credential-remove"],
      ["runSbrReadinessFixture", "tammy:sbr-run-fixture"],
    ] as const;

    for (const [method, channel] of mappings) {
      invoke.mockClear();
      const request = Uint8Array.of(1, 2, 3);
      const result = await api[method]?.(request);
      expect(result).toEqual(response);
      expect(result).not.toBe(response);
      expect(invoke).toHaveBeenCalledExactlyOnceWith(channel, expect.any(Uint8Array));
      expect(invoke.mock.calls[0]?.[1]).not.toBe(request);
    }
  });

  it("maps mediated calls as one exact argument and defensively copies command bytes", async () => {
    const exposeInMainWorld = vi.fn();
    vi.doMock("electron", () => ({
      contextBridge: { exposeInMainWorld },
      ipcRenderer: { invoke: vi.fn() },
    }));
    const { createTammyDesktopAPI } = await import("./index");
    const response = Uint8Array.of(4, 5, 6);
    const invoke = vi.fn(async (channel: string, ..._args: unknown[]) =>
      channel === "tammy:sbr-credential-select"
        ? { selected: true, handle: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb" }
        : response,
    );
    const api = createTammyDesktopAPI(invoke) as unknown as {
      selectMachineCredentialFile(): Promise<unknown>;
      importMachineCredential(input: unknown): Promise<Uint8Array>;
      replaceMachineCredential(input: unknown): Promise<Uint8Array>;
      unlockMachineCredential(input: unknown): Promise<Uint8Array>;
    };

    await expect(api.selectMachineCredentialFile()).resolves.toEqual({
      selected: true,
      handle: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb",
    });
    expect(invoke).toHaveBeenLastCalledWith("tammy:sbr-credential-select");

    const cases = [
      [
        "importMachineCredential",
        "tammy:sbr-credential-import",
        {
          command: Uint8Array.of(1, 2, 3),
          handle: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb",
          password: "transient-password",
        },
      ],
      [
        "replaceMachineCredential",
        "tammy:sbr-credential-replace",
        {
          command: Uint8Array.of(1, 2, 3),
          handle: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb",
          password: "transient-password",
        },
      ],
      [
        "unlockMachineCredential",
        "tammy:sbr-credential-unlock",
        { command: Uint8Array.of(1, 2, 3), password: "transient-password" },
      ],
    ] as const;

    for (const [method, channel, input] of cases) {
      invoke.mockClear();
      const result = await api[method](input);
      expect(result).toEqual(response);
      expect(result).not.toBe(response);
      expect(invoke).toHaveBeenCalledTimes(1);
      expect(invoke.mock.calls[0]?.[0]).toBe(channel);
      expect(invoke.mock.calls[0]).toHaveLength(2);
      const forwarded = invoke.mock.calls[0]?.[1] as { command: Uint8Array };
      expect(forwarded).not.toBe(input);
      expect(forwarded.command).toEqual(input.command);
      expect(forwarded.command).not.toBe(input.command);
    }
  });

  it("rejects invalid mediated shapes and caps before invoking Electron", async () => {
    const exposeInMainWorld = vi.fn();
    vi.doMock("electron", () => ({
      contextBridge: { exposeInMainWorld },
      ipcRenderer: { invoke: vi.fn() },
    }));
    const { createTammyDesktopAPI } = await import("./index");
    const invoke = vi.fn();
    const api = createTammyDesktopAPI(invoke) as unknown as Record<
      string,
      (input: unknown) => Promise<unknown>
    >;

    await expect(
      api.importMachineCredential?.({
        command: new Uint8Array(8_193),
        handle: "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb",
        password: "ok",
      }),
    ).rejects.toThrow("INVALID_RPC_REQUEST");
    await expect(
      api.importMachineCredential?.({
        command: Uint8Array.of(1),
        handle: "018F2F2A-7C1D-7A62-8D11-216B8D6EA4CB",
        password: "ok",
      }),
    ).rejects.toThrow("INVALID_RPC_REQUEST");
    await expect(
      api.unlockMachineCredential?.({
        command: Uint8Array.of(1),
        password: "x".repeat(1_025),
      }),
    ).rejects.toThrow("INVALID_RPC_REQUEST");
    expect(invoke).not.toHaveBeenCalled();
  });

  it("enforces exact argument counts and 32 KiB response caps at preload", async () => {
    const exposeInMainWorld = vi.fn();
    vi.doMock("electron", () => ({
      contextBridge: { exposeInMainWorld },
      ipcRenderer: { invoke: vi.fn() },
    }));
    const { createTammyDesktopAPI } = await import("./index");
    const invoke = vi.fn(async () => new Uint8Array(32_769));
    const api = createTammyDesktopAPI(invoke) as unknown as Record<
      string,
      (...args: unknown[]) => Promise<unknown>
    >;
    const command = Uint8Array.of(1);

    await expect(api.getCurrentUser?.(command)).rejects.toThrow("CORE_REQUEST_FAILED");
    invoke.mockClear();
    await expect(api.getCurrentUser?.(command, "extra")).rejects.toMatchObject({
      code: "INVALID_RPC_REQUEST",
      message: "INVALID_RPC_REQUEST",
    });
    await expect(api.selectMachineCredentialFile?.("extra")).rejects.toThrow("INVALID_RPC_REQUEST");
    await expect(
      api.unlockMachineCredential?.({ command, password: "password" }, "extra"),
    ).rejects.toThrow("INVALID_RPC_REQUEST");
    expect(invoke).not.toHaveBeenCalled();
  });

  it("normalizes invoke failures without exposing core or credential details", async () => {
    const exposeInMainWorld = vi.fn();
    vi.doMock("electron", () => ({
      contextBridge: { exposeInMainWorld },
      ipcRenderer: { invoke: vi.fn() },
    }));
    const { createTammyDesktopAPI } = await import("./index");
    const invoke = vi
      .fn()
      .mockRejectedValueOnce(
        Object.assign(new Error("INVALID_RPC_REQUEST"), { code: "INVALID_RPC_REQUEST" }),
      )
      .mockRejectedValueOnce(new Error("/private/credential.p12 product-secret core-secret"));
    const api = createTammyDesktopAPI(invoke);

    await expect(api.getCurrentUser(Uint8Array.of(1))).rejects.toMatchObject({
      code: "INVALID_RPC_REQUEST",
      message: "INVALID_RPC_REQUEST",
    });
    const error = await api.getCurrentUser(Uint8Array.of(1)).catch((caught: unknown) => caught);
    expect(error).toMatchObject({ code: "CORE_REQUEST_FAILED", message: "CORE_REQUEST_FAILED" });
    expect(String(error)).not.toContain("credential.p12");
    expect(String(error)).not.toContain("product-secret");
    expect(String(error)).not.toContain("core-secret");
  });
});
