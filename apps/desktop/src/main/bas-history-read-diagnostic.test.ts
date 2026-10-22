import { performance } from "node:perf_hooks";
import { runInNewContext } from "node:vm";
import { describe, expect, it, vi } from "vitest";
import {
  installBasHistoryReadDiagnosticShimInMain,
  readBasHistoryReadDiagnosticInMain,
  restoreBasHistoryReadDiagnosticShimInMain,
} from "../../tests/e2e/support/bas-history-read-diagnostic";

const CHANNEL = "conveyor:basPreparation";
const STATE_KEY = "__basHistoryReadDiagnosticTestState";

type Handler = (event: unknown, ...args: unknown[]) => unknown;

function serializeIntoIsolatedContext<T extends (...args: never[]) => unknown>(fn: T): T {
  return runInNewContext(`(${fn.toString()})`, { performance }) as T;
}

function installed(original: Handler) {
  const handlers = new Map<string, Handler>([[CHANNEL, original]]);
  const ipcMain = { _invokeHandlers: handlers };
  const electron = { ipcMain };
  const install = serializeIntoIsolatedContext(installBasHistoryReadDiagnosticShimInMain);
  const isolatedRead = serializeIntoIsolatedContext(readBasHistoryReadDiagnosticInMain);
  const restore = serializeIntoIsolatedContext(restoreBasHistoryReadDiagnosticShimInMain);
  const read: typeof readBasHistoryReadDiagnosticInMain = (targetElectron, stateKey) =>
    JSON.parse(JSON.stringify(isolatedRead(targetElectron, stateKey)));
  install(electron, STATE_KEY);
  const handler = handlers.get(CHANNEL);
  if (!handler) throw new Error("BAS_HISTORY_DIAGNOSTIC_TEST_HANDLER_MISSING");
  return { electron, handler, handlers, ipcMain, original, read, restore };
}

describe("BAS history read diagnostic shim", () => {
  it("returns the successful response and records only fixed read metadata", async () => {
    const response = {
      ok: true,
      data: { privateResponse: "sensitive-response-body" },
    };
    const original = vi.fn(async (_event: unknown, ..._args: unknown[]) => response);
    const { electron, handler, original: installedOriginal, read, restore } = installed(original);
    const event = { sender: { id: "private-sender-id" } };
    const request = {
      authentication: { sessionId: "private-session-id" },
      page: { cursor: "private-page-cursor" },
    };

    await expect(handler(event, "listBasPeriods", request)).resolves.toBe(response);
    expect(installedOriginal).toHaveBeenCalledWith(event, "listBasPeriods", request);

    const events = read(electron, STATE_KEY);
    expect(events).toHaveLength(1);
    expect(events[0]).toMatchObject({ method: "listBasPeriods", code: "OK" });
    expect(events[0]?.elapsedMs).toEqual(expect.any(Number));
    expect(events[0]?.elapsedMs).toBeGreaterThanOrEqual(0);
    expect(events[0]?.elapsedMs).toBeLessThanOrEqual(120_000);
    expect(JSON.stringify(events)).not.toContain("private-");
    restore(electron, STATE_KEY);
  });

  it("preserves sanitized failure envelopes and original thrown errors without retaining details", async () => {
    const failureEnvelope = {
      ok: false,
      error: {
        code: "BAS_REQUEST_TIMEOUT",
        message: "private-core-message",
        details: [{ value: "private-protobuf-detail" }],
      },
    };
    const thrownFailure = Object.assign(new Error("private-thrown-message"), {
      code: "BAS_CONFLICT",
      details: [{ value: "private-thrown-detail" }],
    });
    const original = vi.fn(async (_event: unknown, method: unknown) => {
      if (method === "listBasPreparations") return failureEnvelope;
      throw thrownFailure;
    });
    const { electron, handler, original: installedOriginal, read, restore } = installed(original);
    const event = { sender: { id: "private-sender-id" } };
    const request = { authentication: { sessionId: "private-session-id" } };

    await expect(handler(event, "listBasPreparations", request)).resolves.toBe(failureEnvelope);
    await expect(handler(event, "getBasCalendar", request)).rejects.toBe(thrownFailure);
    expect(installedOriginal).toHaveBeenNthCalledWith(1, event, "listBasPreparations", request);
    expect(installedOriginal).toHaveBeenNthCalledWith(2, event, "getBasCalendar", request);

    const events = read(electron, STATE_KEY);
    expect(events).toHaveLength(2);
    expect(events.map(({ method, code }) => ({ method, code }))).toEqual([
      { method: "listBasPreparations", code: "BAS_REQUEST_TIMEOUT" },
      { method: "getBasCalendar", code: "BAS_CONFLICT" },
    ]);
    const serialized = JSON.stringify(events);
    for (const privateValue of [
      "private-core-message",
      "private-protobuf-detail",
      "private-thrown-message",
      "private-thrown-detail",
      "private-session-id",
      "private-sender-id",
    ])
      expect(serialized).not.toContain(privateValue);
    restore(electron, STATE_KEY);
  });

  it("reduces unrecognized methods and failure codes without changing the original call", async () => {
    const response = { ok: false, error: { code: "PRIVATE_CONNECT_DETAIL", message: "secret" } };
    const original = vi.fn(async (_event: unknown, ..._args: unknown[]) => response);
    const { electron, handler, original: installedOriginal, read, restore } = installed(original);
    const event = {};
    const request = { private: "request" };

    await expect(handler(event, "confirmBasPeriod", request)).resolves.toBe(response);
    await expect(handler(event, "listFrozenBas", request)).resolves.toBe(response);
    expect(installedOriginal).toHaveBeenNthCalledWith(1, event, "confirmBasPeriod", request);
    expect(installedOriginal).toHaveBeenNthCalledWith(2, event, "listFrozenBas", request);
    expect(read(electron, STATE_KEY)).toEqual([
      expect.objectContaining({ method: "listFrozenBas", code: "UNCLASSIFIED" }),
    ]);
    restore(electron, STATE_KEY);
  });
});
