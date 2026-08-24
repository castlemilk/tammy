import { spawn } from "node:child_process";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const OUTPUT_LIMIT_BYTES = 65_536;
const TERMINATION_GRACE_MS = 5_000;
const PACKAGE_COMMAND = ["exec", "--", "pnpm", "desktop:start:scenario"];
const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

const defaultDependencies = {
  cancelSchedule: (timer) => clearTimeout(timer),
  makeTemporaryDirectory: (prefix) => mkdtemp(prefix),
  platform: process.platform,
  processRunner: (command, arguments_, options) => spawn(command, arguments_, options),
  schedule: (callback, milliseconds) => setTimeout(callback, milliseconds),
  signalSource: process,
  stderr: process.stderr,
  stdout: process.stdout,
  temporaryRoot: tmpdir(),
  terminateProcessGroup: (processGroupId, signal) => process.kill(processGroupId, signal),
};

function scenarioArguments(scenario, retainedRoot) {
  if (scenario === "accounting") return PACKAGE_COMMAND;
  if (
    (scenario === "accounting-fresh" || scenario === "sbr-simulator") &&
    retainedRoot !== undefined
  ) {
    return [...PACKAGE_COMMAND, "--", `--user-data-dir=${retainedRoot}`];
  }
  throw new Error("LOCAL_SCENARIO_INVALID");
}

function rejectUnimplementedScenario(scenario) {
  if (scenario === "sbr-evte") {
    throw new Error(`SBR_IMPLEMENTATION_INCOMPLETE:${scenario}`);
  }
}

export async function launchLocalScenario(scenario, overrides = {}) {
  const dependencies = { ...defaultDependencies, ...overrides };
  rejectUnimplementedScenario(scenario);
  if (
    scenario !== "accounting" &&
    scenario !== "accounting-fresh" &&
    scenario !== "sbr-simulator"
  ) {
    throw new Error("LOCAL_SCENARIO_INVALID");
  }

  let retainedRoot;
  if (scenario === "accounting-fresh" || scenario === "sbr-simulator") {
    try {
      retainedRoot = await dependencies.makeTemporaryDirectory(
        path.join(
          dependencies.temporaryRoot,
          scenario === "accounting-fresh" ? "tammy-accounting-fresh-" : "tammy-sbr-simulator-",
        ),
      );
    } catch {
      throw new Error("LOCAL_SCENARIO_TEMPORARY_ROOT_FAILED");
    }
  }
  if (retainedRoot !== undefined && !path.isAbsolute(retainedRoot)) {
    throw new Error("LOCAL_SCENARIO_TEMPORARY_ROOT_INVALID");
  }

  let child;
  try {
    child = dependencies.processRunner("mise", scenarioArguments(scenario, retainedRoot), {
      cwd: repositoryRoot,
      detached: dependencies.platform === "darwin",
      shell: false,
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch {
    if (retainedRoot !== undefined) {
      dependencies.stdout.write(`LOCAL_SCENARIO_RETAINED_ROOT:${retainedRoot}\n`);
    }
    throw new Error("LOCAL_SCENARIO_START_FAILED");
  }

  return new Promise((resolve, reject) => {
    let bytesWritten = 0;
    let final = false;
    let forwardedSignal;
    let outputFailure = false;
    let stdoutAtLineStart = true;
    let terminationEscalated = false;
    let terminationStarted = false;
    let terminationTimer;

    const forwardBounded = (destination, isStdout) => (chunk) => {
      if (outputFailure) return;
      const value = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
      const available = OUTPUT_LIMIT_BYTES - bytesWritten;
      const forwarded = value.subarray(0, Math.max(available, 0));
      if (forwarded.length > 0) {
        destination.write(forwarded);
        if (isStdout) stdoutAtLineStart = forwarded.at(-1) === 0x0a;
      }
      bytesWritten += Math.min(value.length, Math.max(available, 0));
      if (value.length > available) {
        outputFailure = true;
        beginTermination("SIGTERM");
      }
    };

    const onStdout = forwardBounded(dependencies.stdout, true);
    const onStderr = forwardBounded(dependencies.stderr, false);

    const writeRetainedRoot = () => {
      if (retainedRoot === undefined) return;
      if (!stdoutAtLineStart) dependencies.stdout.write("\n");
      dependencies.stdout.write(`LOCAL_SCENARIO_RETAINED_ROOT:${retainedRoot}\n`);
      stdoutAtLineStart = true;
    };

    const finalize = (error) => {
      if (final) return;
      final = true;
      dependencies.signalSource.off("SIGINT", onSigint);
      dependencies.signalSource.off("SIGTERM", onSigterm);
      child.stdout.off("data", onStdout);
      child.stderr.off("data", onStderr);
      if (terminationTimer !== undefined) dependencies.cancelSchedule(terminationTimer);
      writeRetainedRoot();
      if (error === undefined) resolve();
      else reject(error);
    };

    function terminate(signal) {
      if (final) return;
      try {
        if (
          dependencies.platform === "darwin" &&
          Number.isSafeInteger(child.pid) &&
          child.pid > 0
        ) {
          dependencies.terminateProcessGroup(-child.pid, signal);
        } else {
          child.kill(signal);
        }
      } catch (error) {
        if (error?.code === "ESRCH") return;
        try {
          child.kill(signal);
        } catch (fallbackError) {
          if (fallbackError?.code !== "ESRCH") {
            finalize(new Error("LOCAL_SCENARIO_TERMINATION_FAILED"));
          }
        }
      }
    }

    function beginTermination(signal) {
      if (terminationStarted || final) return;
      terminationStarted = true;
      terminate(signal);
      if (final) return;
      terminationTimer = dependencies.schedule(escalateTermination, TERMINATION_GRACE_MS);
      terminationTimer.unref?.();
    }

    function escalateTermination() {
      if (terminationEscalated || final) return;
      terminationEscalated = true;
      terminate("SIGKILL");
    }

    const forwardSignal = (signal) => {
      if (forwardedSignal !== undefined || final) return;
      forwardedSignal = signal;
      beginTermination(signal);
    };
    const onSigint = () => forwardSignal("SIGINT");
    const onSigterm = () => forwardSignal("SIGTERM");

    child.stdout.on("data", onStdout);
    child.stderr.on("data", onStderr);
    dependencies.signalSource.on("SIGINT", onSigint);
    dependencies.signalSource.on("SIGTERM", onSigterm);
    child.once("error", () => {
      if (terminationStarted) escalateTermination();
      if (outputFailure) finalize(new Error("LOCAL_SCENARIO_OUTPUT_LIMIT_EXCEEDED"));
      else if (forwardedSignal !== undefined) {
        finalize(new Error(`LOCAL_SCENARIO_CHILD_SIGNAL:${forwardedSignal}`));
      } else finalize(new Error("LOCAL_SCENARIO_START_FAILED"));
    });
    child.once("close", (code, signal) => {
      if (terminationStarted) escalateTermination();
      if (outputFailure) {
        finalize(new Error("LOCAL_SCENARIO_OUTPUT_LIMIT_EXCEEDED"));
        return;
      }
      if (forwardedSignal !== undefined) {
        finalize(new Error(`LOCAL_SCENARIO_CHILD_SIGNAL:${forwardedSignal}`));
        return;
      }
      if (signal !== null) {
        finalize(new Error(`LOCAL_SCENARIO_CHILD_SIGNAL:${signal}`));
        return;
      }
      if (code !== 0) {
        finalize(new Error(`LOCAL_SCENARIO_CHILD_EXIT:${code ?? "UNKNOWN"}`));
        return;
      }
      finalize();
    });
  });
}

function validateDesktopScenarioOwnerArguments(arguments_, temporaryRoot) {
  const forwardedArguments = arguments_[0] === "--" ? arguments_.slice(1) : arguments_;
  if (forwardedArguments.length === 0) return [];
  if (forwardedArguments.length !== 1 || !forwardedArguments[0].startsWith("--user-data-dir=")) {
    throw new Error("LOCAL_SCENARIO_OWNER_ARGUMENTS_INVALID");
  }
  const userDataPath = forwardedArguments[0].slice("--user-data-dir=".length);
  if (
    !path.isAbsolute(userDataPath) ||
    path.dirname(userDataPath) !== path.resolve(temporaryRoot) ||
    !/^tammy-(?:accounting-fresh|sbr-simulator)-[A-Za-z0-9_-]+$/u.test(path.basename(userDataPath))
  ) {
    throw new Error("LOCAL_SCENARIO_OWNER_ARGUMENTS_INVALID");
  }
  return forwardedArguments;
}

function runOwnedProcess(command, arguments_, dependencies, failureCode) {
  return new Promise((resolve, reject) => {
    let child;
    try {
      child = dependencies.processRunner(command, arguments_, {
        cwd: repositoryRoot,
        shell: false,
        stdio: "inherit",
      });
    } catch {
      reject(new Error(`${failureCode}:START`));
      return;
    }
    let settled = false;
    const settle = (error) => {
      if (settled) return;
      settled = true;
      if (error === undefined) resolve();
      else reject(error);
    };
    child.once("error", () => settle(new Error(`${failureCode}:START`)));
    child.once("close", (code, signal) => {
      if (signal !== null) settle(new Error(`${failureCode}:SIGNAL:${signal}`));
      else if (code !== 0) settle(new Error(`${failureCode}:EXIT:${code ?? "UNKNOWN"}`));
      else settle();
    });
  });
}

export async function runDesktopScenarioOwner(arguments_, overrides = {}) {
  const dependencies = { ...defaultDependencies, ...overrides };
  const validatedArguments = validateDesktopScenarioOwnerArguments(
    arguments_,
    dependencies.temporaryRoot,
  );
  await runOwnedProcess("pnpm", ["core:build"], dependencies, "LOCAL_SCENARIO_CORE_BUILD_FAILED");
  await runOwnedProcess(
    "pnpm",
    [
      "--dir",
      "apps/desktop",
      "start",
      ...(validatedArguments.length === 0 ? [] : ["--", ...validatedArguments]),
    ],
    dependencies,
    "LOCAL_SCENARIO_ELECTRON_START_FAILED",
  );
}

async function main() {
  const [scenario, ...extras] = process.argv.slice(2);
  if (scenario === "--electron-forge") {
    await runDesktopScenarioOwner(extras);
    return;
  }
  if (scenario === undefined || extras.length !== 0) {
    throw new Error("LOCAL_SCENARIO_ARGUMENTS_INVALID");
  }
  await launchLocalScenario(scenario);
}

if (process.argv[1] && fileURLToPath(import.meta.url) === path.resolve(process.argv[1])) {
  main().catch((error) => {
    const message = error instanceof Error ? error.message : "LOCAL_SCENARIO_UNKNOWN_FAILURE";
    process.stderr.write(`${message}\n`);
    process.exitCode = 1;
  });
}
