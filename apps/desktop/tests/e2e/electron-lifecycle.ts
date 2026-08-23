import path from "node:path";

export interface StagedArtifact {
  readonly kind: "screenshot" | "trace" | "video";
  readonly path: string;
}

export const STAGED_ARTIFACT_FILENAMES = Object.freeze({
  screenshot: "failure.png",
  trace: "electron-trace.zip",
  video: "failure.webm",
} satisfies Record<StagedArtifact["kind"], string>);

export function assertOwnedStagedArtifact(stagingRoot: string, artifact: StagedArtifact): void {
  const expected = path.join(stagingRoot, STAGED_ARTIFACT_FILENAMES[artifact.kind]);
  if (
    !path.isAbsolute(stagingRoot) ||
    path.normalize(stagingRoot) !== stagingRoot ||
    artifact.path !== expected
  ) {
    throw new Error("UNOWNED_STAGED_ARTIFACT");
  }
}

export interface ElectronLifecycleOperations<State, Harness> {
  assertNoOrphan(state: State): Promise<void>;
  attachArtifact(state: State, artifact: StagedArtifact): Promise<void>;
  closeAndReap(state: State): Promise<void>;
  deleteStagedArtifacts(state: State, artifacts: readonly StagedArtifact[]): Promise<void>;
  didTestFail(): boolean;
  removeRawArtifacts(state: State): Promise<void>;
  setup(state: State): Promise<Harness>;
  stageScreenshot(state: State): Promise<StagedArtifact | undefined>;
  stageVideo(state: State): Promise<StagedArtifact | undefined>;
  stopAndStageTrace(state: State): Promise<StagedArtifact | undefined>;
  use(harness: Harness): Promise<void>;
  finalize?(state: State, clean: boolean): Promise<void>;
}

export async function runElectronLifecycle<State, Harness>(
  state: State,
  operations: ElectronLifecycleOperations<State, Harness>,
): Promise<void> {
  const failures: unknown[] = [];
  const stagedArtifacts: StagedArtifact[] = [];
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
    await collectArtifact(failures, stagedArtifacts, () => operations.stageScreenshot(state));
    if (hasStartedTrace(state)) {
      await collectArtifact(failures, stagedArtifacts, () => operations.stopAndStageTrace(state));
    }
    await collectFailure(failures, () => operations.closeAndReap(state));
    await collectArtifact(failures, stagedArtifacts, () => operations.stageVideo(state));
    await collectFailure(failures, () => operations.removeRawArtifacts(state));
    await collectFailure(failures, () => operations.assertNoOrphan(state));
    if (failed || failures.length > 0) {
      await attachStagedArtifacts(failures, stagedArtifacts, state, operations);
    } else {
      try {
        await operations.deleteStagedArtifacts(state, stagedArtifacts);
      } catch (error) {
        failures.push(error);
        await attachStagedArtifacts(failures, stagedArtifacts, state, operations);
      }
    }
    if (operations.finalize) {
      await collectFailure(
        failures,
        () => operations.finalize?.(state, !failed && failures.length === 0) ?? Promise.resolve(),
      );
    }
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

async function collectArtifact(
  failures: unknown[],
  artifacts: StagedArtifact[],
  operation: () => Promise<StagedArtifact | undefined>,
): Promise<void> {
  try {
    const artifact = await operation();
    if (artifact) artifacts.push(artifact);
  } catch (error) {
    failures.push(error);
  }
}

async function attachStagedArtifacts<State, Harness>(
  failures: unknown[],
  artifacts: readonly StagedArtifact[],
  state: State,
  operations: ElectronLifecycleOperations<State, Harness>,
): Promise<void> {
  for (const artifact of artifacts) {
    await collectFailure(failures, () => operations.attachArtifact(state, artifact));
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
