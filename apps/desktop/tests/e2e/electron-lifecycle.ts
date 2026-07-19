export interface ElectronLifecycleOperations<State, Harness> {
  assertNoOrphan(state: State): Promise<void>;
  attachTrace(state: State): Promise<void>;
  closeAndReap(state: State): Promise<void>;
  didTestFail(): boolean;
  handleVideo(state: State, retained: boolean): Promise<void>;
  removeRawArtifacts(state: State): Promise<void>;
  screenshot(state: State): Promise<void>;
  setup(state: State): Promise<Harness>;
  stopTrace(state: State, retained: boolean): Promise<void>;
  use(harness: Harness): Promise<void>;
}

export async function runElectronLifecycle<State, Harness>(
  state: State,
  operations: ElectronLifecycleOperations<State, Harness>,
): Promise<void> {
  const failures: unknown[] = [];
  let failed = false;
  try {
    const harness = await operations.setup(state);
    await operations.use(harness);
  } catch (error) {
    failures.push(error);
    failed = true;
  } finally {
    try {
      failed ||= operations.didTestFail();
    } catch (error) {
      failures.push(error);
      failed = true;
    }
    if (failed) {
      await collectFailure(failures, () => operations.screenshot(state));
    }
    if (hasStartedTrace(state)) {
      await collectFailure(failures, () => operations.stopTrace(state, failed));
      if (failed) {
        await collectFailure(failures, () => operations.attachTrace(state));
      }
    }
    await collectFailure(failures, () => operations.closeAndReap(state));
    await collectFailure(failures, () => operations.handleVideo(state, failed));
    await collectFailure(failures, () => operations.removeRawArtifacts(state));
    await collectFailure(failures, () => operations.assertNoOrphan(state));
  }
  throwFailures(failures, "ELECTRON_LIFECYCLE_FAILED");
}

export async function closeAndReapElectron(options: {
  readonly forceKillMain: () => void;
  readonly gracefulClose: () => Promise<void>;
  readonly mainClosed: Promise<void>;
  readonly timeoutMs: number;
}): Promise<void> {
  const failures: unknown[] = [];
  try {
    await bounded(
      Promise.all([options.gracefulClose(), options.mainClosed]).then(() => undefined),
      options.timeoutMs,
      "ELECTRON_CLOSE_TIMEOUT",
    );
    return;
  } catch (error) {
    failures.push(error);
  }
  try {
    options.forceKillMain();
  } catch (error) {
    failures.push(error);
  }
  try {
    await bounded(options.mainClosed, options.timeoutMs, "ELECTRON_REAP_TIMEOUT");
  } catch (error) {
    failures.push(error);
  }
  throwFailures(failures, "ELECTRON_CLOSE_FAILED");
}

export async function pollForNoCoreProcesses(options: {
  readonly intervalMs: number;
  readonly now?: () => number;
  readonly query: () => Promise<readonly unknown[]>;
  readonly sleep?: (milliseconds: number) => Promise<void>;
  readonly timeoutMs: number;
}): Promise<void> {
  if (
    !Number.isSafeInteger(options.intervalMs) ||
    options.intervalMs <= 0 ||
    !Number.isSafeInteger(options.timeoutMs) ||
    options.timeoutMs <= 0
  ) {
    throw new Error("INVALID_ORPHAN_POLL_TIMEOUT");
  }
  const now = options.now ?? Date.now;
  const sleep =
    options.sleep ??
    ((milliseconds: number) =>
      new Promise<void>((resolve) => {
        setTimeout(resolve, milliseconds);
      }));
  const deadline = now() + options.timeoutMs;
  while (true) {
    if ((await options.query()).length === 0) return;
    const remaining = deadline - now();
    if (remaining <= 0) throw new Error("CORE_PROCESS_ORPHAN");
    await sleep(Math.min(options.intervalMs, remaining));
  }
}

async function collectFailure(failures: unknown[], operation: () => Promise<void>): Promise<void> {
  try {
    await operation();
  } catch (error) {
    failures.push(error);
  }
}

function hasStartedTrace<State>(state: State): boolean {
  return (
    typeof state === "object" &&
    state !== null &&
    "traceStarted" in state &&
    state.traceStarted === true
  );
}

function throwFailures(failures: readonly unknown[], message: string): void {
  if (failures.length === 0) return;
  if (failures.length === 1) throw failures[0];
  throw new AggregateError(failures, message, { cause: failures[0] });
}

async function bounded<T>(promise: Promise<T>, timeoutMs: number, code: string): Promise<T> {
  if (!Number.isSafeInteger(timeoutMs) || timeoutMs <= 0) throw new Error(code);
  let timer: NodeJS.Timeout | undefined;
  try {
    return await Promise.race([
      promise,
      new Promise<never>((_, reject) => {
        timer = setTimeout(() => reject(new Error(code)), timeoutMs);
      }),
    ]);
  } finally {
    if (timer) clearTimeout(timer);
  }
}
