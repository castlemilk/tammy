export interface BasHistoryDiagnosticElectronMain {
  readonly ipcMain: unknown;
}

export type BasHistoryReadMethod =
  | "listBasObligationsProfiles"
  | "listBasPeriods"
  | "listBasPreparations"
  | "listFrozenBas"
  | "listBasExternalDisputeResolutions"
  | "getBasCalendar";

export type BasHistoryReadDiagnostic = {
  readonly method: BasHistoryReadMethod;
  readonly code: string;
  readonly elapsedMs: number;
};

type InvokeHandler = (event: unknown, ...args: unknown[]) => unknown;

export function installBasHistoryReadDiagnosticShimInMain(
  electron: BasHistoryDiagnosticElectronMain,
  stateKey: string,
): void {
  type MutableDiagnostic = {
    method: BasHistoryReadMethod;
    code: string;
    elapsedMs: number;
  };
  type State = {
    events: MutableDiagnostic[];
    original: InvokeHandler;
    wrapped: InvokeHandler;
  };

  const ipcMain = electron.ipcMain as {
    _invokeHandlers?: Map<string, InvokeHandler>;
  } & Record<string, unknown>;
  const channel = "conveyor:basPreparation";
  const handlers = ipcMain._invokeHandlers;
  const original = handlers?.get(channel);
  if (!handlers || !original) throw new Error("BAS_HISTORY_DIAGNOSTIC_SHIM_UNAVAILABLE");
  if (Object.hasOwn(ipcMain, stateKey)) throw new Error("BAS_HISTORY_DIAGNOSTIC_STATE_EXISTS");

  const methods = [
    "listBasObligationsProfiles",
    "listBasPeriods",
    "listBasPreparations",
    "listFrozenBas",
    "listBasExternalDisputeResolutions",
    "getBasCalendar",
  ];
  const safeCodes = new Set([
    "CORE_REQUEST_FAILED",
    "BAS_SOURCE_CAPACITY",
    "BAS_CONFLICT",
    "BAS_IDEMPOTENCY_CONFLICT",
    "BAS_PERMISSION_DENIED",
    "BAS_AUTHENTICATION_REQUIRED",
    "BAS_REQUEST_TIMEOUT",
    "INVALID_INPUT",
    "INVALID_RPC_REQUEST",
    "IPC_SENDER_REJECTED",
  ]);
  const events: MutableDiagnostic[] = [];
  const ownValue = (value: unknown, key: string): unknown => {
    try {
      if ((typeof value !== "object" && typeof value !== "function") || value === null)
        return undefined;
      const descriptor = Object.getOwnPropertyDescriptor(value, key);
      return descriptor && "value" in descriptor ? descriptor.value : undefined;
    } catch {
      return undefined;
    }
  };
  const safeCode = (value: unknown): string =>
    typeof value === "string" && safeCodes.has(value) ? value : "UNCLASSIFIED";
  const responseCode = (response: unknown): string => {
    const ok = ownValue(response, "ok");
    if (ok === true) return "OK";
    if (ok !== false) return "UNCLASSIFIED";
    return safeCode(ownValue(ownValue(response, "error"), "code"));
  };
  const record = (method: BasHistoryReadMethod, code: string, durationMs: number) => {
    const prior = events.find((entry) => entry.method === method);
    if (prior) {
      if (prior.code === "OK" && code !== "OK") {
        prior.code = code;
        prior.elapsedMs = durationMs;
      } else if (prior.code === "OK") {
        prior.elapsedMs = Math.min(120_000, prior.elapsedMs + durationMs);
      }
      return;
    }
    if (events.length >= methods.length) return;
    events.push({ method, code, elapsedMs: durationMs });
  };
  const wrapped: InvokeHandler = async (event, ...args) => {
    const methodValue = args[0];
    if (typeof methodValue !== "string" || !methods.includes(methodValue))
      return Reflect.apply(original, ipcMain, [event, ...args]);

    const method = methodValue as BasHistoryReadMethod;
    const startedAt = performance.now();
    const elapsed = () => Math.max(0, Math.min(120_000, Math.round(performance.now() - startedAt)));
    try {
      const result = await Reflect.apply(original, ipcMain, [event, ...args]);
      record(method, responseCode(result), elapsed());
      return result;
    } catch (error) {
      record(method, safeCode(ownValue(error, "code")), elapsed());
      throw error;
    }
  };
  const state: State = { events, original, wrapped };
  Object.defineProperty(ipcMain, stateKey, { configurable: true, value: state });
  handlers.set(channel, wrapped);
}

export function readBasHistoryReadDiagnosticInMain(
  electron: BasHistoryDiagnosticElectronMain,
  stateKey: string,
): readonly BasHistoryReadDiagnostic[] {
  const ipcMain = electron.ipcMain as Record<string, unknown>;
  const state = ipcMain[stateKey] as
    | { readonly events: readonly BasHistoryReadDiagnostic[] }
    | undefined;
  if (!state) throw new Error("BAS_HISTORY_DIAGNOSTIC_STATE_MISSING");
  return state.events.map(({ method, code, elapsedMs: durationMs }) => ({
    method,
    code,
    elapsedMs: durationMs,
  }));
}

export function restoreBasHistoryReadDiagnosticShimInMain(
  electron: BasHistoryDiagnosticElectronMain,
  stateKey: string,
): void {
  const ipcMain = electron.ipcMain as {
    _invokeHandlers?: Map<string, InvokeHandler>;
  } & Record<string, unknown>;
  const state = ipcMain[stateKey] as
    | { readonly original: InvokeHandler; readonly wrapped: InvokeHandler }
    | undefined;
  if (!state) return;
  const handlers = ipcMain._invokeHandlers;
  const channel = "conveyor:basPreparation";
  if (handlers?.get(channel) === state.wrapped) handlers.set(channel, state.original);
  delete ipcMain[stateKey];
}
