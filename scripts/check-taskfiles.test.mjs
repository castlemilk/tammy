import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import YAML from "yaml";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const rootTaskfile = path.join(repositoryRoot, "Taskfile.yml");
const scenarioFiles = [
  "taskfiles/setup.yml",
  "taskfiles/dev.yml",
  "taskfiles/diagnostics.yml",
  "taskfiles/test.yml",
  "taskfiles/build.yml",
  "taskfiles/package.yml",
  "taskfiles/release.yml",
  "taskfiles/ci.yml",
];
const allowedExecutablePatterns = [
  /^mise install$/,
  /^mise exec -- corepack prepare pnpm@11\.15\.0 --activate$/,
  /^mise exec -- pnpm (?:--version|install --frozen-lockfile|check:toolchain|test|core:build|desktop:start)$/,
  /^mise exec -- task --(?:list|version)$/,
  /^mise exec -- node --version$/,
];
const diagnosticMutationPattern =
  /^mise exec -- (?:pnpm|npm|yarn)\b[\s\S]*\b(?:build|package|start|make)\b/i;
const coreDiagnosticLines = [
  "Expected owned source core: apps/desktop/resources/core/darwin-arm64/tammy-core or apps/desktop/resources/core/win32-x64/tammy-core.exe",
  "Expected build manifest: apps/desktop/resources/build/build-manifest.json",
  'macOS read-only process-path command: ps -ax -o pid=,command= | rg "[T]ammy|[t]ammy-core"',
  'Windows read-only process-path command: Get-CimInstance Win32_Process | Where-Object { $_.Name -match "Tammy|tammy-core" } | Select-Object ProcessId,Name,ExecutablePath',
  "Inspect the Electron and core process paths manually; confirm they resolve inside this checkout before taking any action.",
];
const dataDiagnosticLines = [
  "macOS development: ~/Library/Application Support/Tammy/local-core-development",
  "macOS packaged: ~/Library/Application Support/Tammy/local-core",
  "Windows development: %APPDATA%\\Tammy\\local-core-development",
  "Windows packaged: %APPDATA%\\Tammy\\local-core",
  "Development memory anchors are process-memory-only; this task only prints paths.",
];
const packageDiagnosticLine =
  "Locator verification for an existing local artefact: mise exec -- node apps/desktop/scripts/find-packaged-app.mjs --verify --source-manifest apps/desktop/resources/build/build-manifest.json";
const rootBoundaryScript =
  'console.log("Tammy boundaries: local-first encrypted accounting; development-only BAS workpapers; no ATO submission; local packages are not production/App Store evidence; upload remains manual.")';
const targetPreconditionScript = `const target = process.argv[1] ?? \`\${process.platform}/\${process.arch}\`; if (!new Set(["darwin/arm64", "win32/x64"]).has(target)) { console.error(\`UNSUPPORTED_SQLCIPHER_TARGET:\${target}\`); process.exit(1); }`;
const setupTaskVersionScript = `let output=""; process.stdin.on("data", (chunk) => output += chunk); process.stdin.on("end", () => { if (output.trim() !== "3.52.0") { console.error(\`UNSUPPORTED_TASK_VERSION:\${output.trim()}\`); process.exit(1); } })`;

async function readTaskfile(relativePath) {
  return YAML.parse(await readFile(path.join(repositoryRoot, relativePath), "utf8"));
}

function taskCommands(task) {
  return task.cmds ?? [];
}

function shellCommands(task) {
  return taskCommands(task).flatMap((command) => {
    if (typeof command === "string") return [command];
    return typeof command?.cmd === "string" ? [command.cmd] : [];
  });
}

function taskReferences(task) {
  return taskCommands(task)
    .filter((command) => command && typeof command === "object" && "task" in command)
    .map((command) => command.task);
}

async function collectTaskGraph(relativePath, namespace = "", graph = new Map()) {
  const taskfile = await readTaskfile(relativePath);
  for (const [taskName, task] of Object.entries(taskfile.tasks ?? {})) {
    const fullName = namespace ? `${namespace}:${taskName}` : taskName;
    assert.equal(graph.has(fullName), false, `duplicate task ${fullName}`);
    graph.set(fullName, { task, taskfile: relativePath });
  }
  for (const [includeName, include] of Object.entries(taskfile.includes ?? {})) {
    const configuration = typeof include === "string" ? { taskfile: include } : include;
    const includePath = path.posix.normalize(
      path.posix.join(path.posix.dirname(relativePath), configuration.taskfile),
    );
    const includeNamespace = configuration.flatten
      ? namespace
      : [namespace, includeName].filter(Boolean).join(":");
    await collectTaskGraph(includePath, includeNamespace, graph);
  }
  return graph;
}

function run(command, args) {
  return new Promise((resolve, reject) => {
    const child = spawn(command, args, { stdio: ["ignore", "pipe", "pipe"] });
    let stderr = "";
    let stdout = "";
    child.stdout.on("data", (chunk) => (stdout += chunk));
    child.stderr.on("data", (chunk) => (stderr += chunk));
    child.on("error", reject);
    child.on("close", (code) => resolve({ code, stderr, stdout }));
  });
}

function nodePrintScript(lines) {
  return `console.log(${JSON.stringify(lines.join("\n"))})`;
}

function nodePrintCommand(lines) {
  return `mise exec -- node -e '${nodePrintScript(lines)}'`;
}

const allowedNodeScripts = new Set([
  rootBoundaryScript,
  targetPreconditionScript,
  setupTaskVersionScript,
  nodePrintScript(coreDiagnosticLines),
  nodePrintScript([packageDiagnosticLine]),
  nodePrintScript(dataDiagnosticLines),
]);

async function runTargetPrecondition(precondition, target) {
  const match = precondition.sh.match(/^mise exec -- node -e '([\s\S]*)'$/);
  assert.ok(match, "the native precondition must be an extractable pinned Node command");
  return run("mise", ["exec", "--", "node", "-e", match[1], target]);
}

function assertAllowedShellAction(action) {
  const nodeCommand = action.match(/^mise exec -- node -e '([\s\S]*)'$/);
  if (nodeCommand) {
    assert.equal(
      allowedNodeScripts.has(nodeCommand[1]),
      true,
      `unsupported Node Task action: ${nodeCommand[1]}`,
    );
    return;
  }
  assert.equal(
    allowedExecutablePatterns.some((pattern) => pattern.test(action)),
    true,
    `unsupported Task action: ${action}`,
  );
}

function splitPipeline(command) {
  const actions = [];
  let quoted = false;
  let start = 0;
  for (let index = 0; index < command.length; index += 1) {
    if (command[index] === "'") quoted = !quoted;
    if (!quoted && command.slice(index, index + 3) === " | ") {
      actions.push(command.slice(start, index));
      start = index + 3;
      index += 2;
    }
  }
  actions.push(command.slice(start));
  return actions;
}

function assertAllowedShellCommand(command) {
  for (const action of splitPipeline(command)) assertAllowedShellAction(action);
}

function assertReadOnlyDiagnostic(command) {
  assert.doesNotMatch(
    command,
    diagnosticMutationPattern,
    "diagnostics must not execute build/package/start/make commands",
  );
  assertAllowedShellCommand(command);
}

test("local Task front door preserves the safe development contract", async () => {
  assert.equal(existsSync(rootTaskfile), true, "Taskfile.yml must exist");
  for (const scenarioFile of scenarioFiles) {
    assert.equal(
      existsSync(path.join(repositoryRoot, scenarioFile)),
      true,
      `${scenarioFile} must exist`,
    );
  }

  const root = await readTaskfile("Taskfile.yml");
  assert.equal(root.version, "3");
  const includedFiles = Object.values(root.includes ?? {}).map((include) =>
    typeof include === "string" ? include : include.taskfile,
  );
  assert.deepEqual([...includedFiles].sort(), [...scenarioFiles].sort());
  assert.equal(new Set(includedFiles).size, scenarioFiles.length, "each scenario is included once");

  const setup = await readTaskfile("taskfiles/setup.yml");
  const dev = await readTaskfile("taskfiles/dev.yml");
  const diagnostics = await readTaskfile("taskfiles/diagnostics.yml");
  for (const taskfile of [setup, dev, diagnostics]) assert.equal(taskfile.version, "3");
  for (const scenarioFile of [
    "taskfiles/test.yml",
    "taskfiles/build.yml",
    "taskfiles/package.yml",
    "taskfiles/release.yml",
    "taskfiles/ci.yml",
  ]) {
    assert.deepEqual(
      await readTaskfile(scenarioFile),
      { version: "3" },
      `${scenarioFile} is a Task 3 stub`,
    );
  }
  const taskGraph = await collectTaskGraph("Taskfile.yml");

  const rootTasks = root.tasks ?? {};
  for (const taskName of ["default", "setup", "dev", "test"]) {
    assert.match(rootTasks[taskName]?.desc ?? "", /.+/, `${taskName} requires a description`);
  }
  assert.match(rootTasks.default.summary ?? "", /local-first encrypted accounting/i);
  assert.match(rootTasks.default.summary ?? "", /development-only BAS workpapers/i);
  assert.match(rootTasks.default.summary ?? "", /no ATO submission/i);
  assert.match(rootTasks.default.summary ?? "", /not production\/App Store evidence/i);
  assert.match(rootTasks.default.summary ?? "", /upload.*manual/i);
  assert.deepEqual(taskReferences(rootTasks.setup), ["setup:tools", "setup:deps", "setup:check"]);
  assert.deepEqual(taskReferences(rootTasks.dev), ["dev:launch"]);
  assert.deepEqual(shellCommands(rootTasks.test), ["mise exec -- pnpm test"]);
  assert.equal(taskCommands(rootTasks.test).length, 1, "test delegates to pnpm test exactly once");
  for (const taskName of ["setup", "dev", "test"]) {
    assert.equal(
      rootTasks[taskName].deps,
      undefined,
      `${taskName} must use sequential cmds, not deps`,
    );
  }
  for (const unsupportedAlias of ["verify", "build", "package", "deploy:mas"]) {
    assert.equal(
      rootTasks[unsupportedAlias],
      undefined,
      `${unsupportedAlias} is not implemented yet`,
    );
  }

  for (const [taskName, task] of Object.entries(setup.tasks ?? {})) {
    assert.match(task.desc ?? "", /.+/, `setup:${taskName} requires a description`);
    assert.equal(task.deps, undefined, `setup:${taskName} must use sequential cmds, not deps`);
  }
  assert.deepEqual(shellCommands(setup.tasks.tools), ["mise install"]);
  assert.match(setup.tasks.tools.summary ?? "", /post-bootstrap.*repair\/update/i);
  assert.deepEqual(shellCommands(setup.tasks.deps), [
    "mise exec -- corepack prepare pnpm@11.15.0 --activate",
    "mise exec -- pnpm install --frozen-lockfile",
  ]);
  assert.equal(shellCommands(setup.tasks.check)[0], "mise exec -- pnpm check:toolchain");
  assert.match(shellCommands(setup.tasks.check).join("\n"), /mise exec -- task --version/);
  assert.match(shellCommands(setup.tasks.check).join("\n"), /3\.52\.0/);

  for (const taskName of ["core", "launch"]) {
    const task = dev.tasks?.[taskName];
    assert.match(task?.desc ?? "", /.+/, `dev:${taskName} requires a description`);
    assert.match(task?.summary ?? "", /local-core-development/i);
    assert.match(task?.summary ?? "", /development-memory-anchor/i);
    assert.match(task?.summary ?? "", /not production.*evidence/i);
    assert.equal(task.deps, undefined, `dev:${taskName} must not use deps`);
    assert.equal(
      task.platforms,
      undefined,
      `dev:${taskName} must fail instead of silently skipping`,
    );
    const precondition = task.preconditions?.[0];
    assert.match(precondition?.sh ?? "", /^mise exec -- node -e /);
    assert.match(precondition?.sh ?? "", /darwin\/arm64/);
    assert.match(precondition?.sh ?? "", /win32\/x64/);
    assert.match(precondition?.sh ?? "", /UNSUPPORTED_SQLCIPHER_TARGET/);
    for (const { target, accepted } of [
      { target: "darwin/arm64", accepted: true },
      { target: "win32/x64", accepted: true },
      { target: "linux/x64", accepted: false },
    ]) {
      const result = await runTargetPrecondition(precondition, target);
      assert.equal(result.code === 0, accepted, `dev:${taskName} target ${target}`);
      if (!accepted) {
        assert.match(`${result.stdout}${result.stderr}`, /UNSUPPORTED_SQLCIPHER_TARGET:linux\/x64/);
      }
    }
  }
  assert.deepEqual(shellCommands(dev.tasks.core), ["mise exec -- pnpm core:build"]);
  assert.deepEqual(shellCommands(dev.tasks.launch), ["mise exec -- pnpm desktop:start"]);

  for (const taskName of ["toolchain", "core", "package", "data"]) {
    const task = diagnostics.tasks?.[taskName];
    assert.match(task?.desc ?? "", /.+/, `diagnose:${taskName} requires a description`);
    assert.match(task?.summary ?? "", /read-only|does not/i);
    assert.equal(task.deps, undefined, `diagnose:${taskName} must not use deps`);
    assert.equal(
      task.preconditions,
      undefined,
      `diagnose:${taskName} must not mutate through preconditions`,
    );
  }
  assert.deepEqual(shellCommands(diagnostics.tasks.toolchain), [
    "mise exec -- task --version",
    "mise exec -- node --version",
    "mise exec -- pnpm --version",
    "mise exec -- pnpm check:toolchain",
  ]);
  assert.deepEqual(shellCommands(diagnostics.tasks.core), [nodePrintCommand(coreDiagnosticLines)]);
  assert.deepEqual(shellCommands(diagnostics.tasks.package), [
    nodePrintCommand([packageDiagnosticLine]),
  ]);
  assert.equal(
    diagnostics.tasks.package.summary,
    "Read-only diagnostic; it prints the exact package-locator verification command and never rebuilds a package.",
  );
  assert.deepEqual(shellCommands(diagnostics.tasks.data), [nodePrintCommand(dataDiagnosticLines)]);
  for (const command of Object.values(diagnostics.tasks ?? {}).flatMap(shellCommands)) {
    assertReadOnlyDiagnostic(command);
  }
  for (const mutation of [
    "mise exec -- pnpm core:build",
    "mise exec -- pnpm desktop:package",
    "mise exec -- pnpm desktop:start",
    "mise exec -- pnpm --dir apps/desktop make",
  ]) {
    assert.throws(() => assertReadOnlyDiagnostic(mutation), assert.AssertionError);
  }

  for (const [taskName, { task }] of taskGraph) {
    for (const reference of taskReferences(task)) {
      assert.equal(
        taskGraph.has(reference),
        true,
        `${taskName} references missing task ${reference}`,
      );
    }
  }

  const allCommands = [];
  for (const { task } of taskGraph.values()) {
    for (const command of shellCommands(task)) {
      allCommands.push(command);
      if (command !== "mise install") {
        assert.match(command, /^mise exec -- /, "local commands must use pinned mise execution");
      }
    }
    for (const precondition of task.preconditions ?? []) {
      if (typeof precondition?.sh === "string") allCommands.push(precondition.sh);
    }
    assert.equal(task.status, undefined, "Task caching is not allowed");
    assert.equal(task.sources, undefined, "Task caching is not allowed");
    assert.equal(task.generates, undefined, "Task caching is not allowed");
    assert.equal(task.deps, undefined, "Task dependencies are not allowed in this slice");
  }
  for (const command of allCommands) assertAllowedShellCommand(command);
  assert.throws(
    () => assertAllowedShellCommand('mise exec -- node -e \'require("node:fs").rmSync(".tmp")\''),
    assert.AssertionError,
    "Node filesystem mutation must be rejected",
  );
  for (const mutation of [
    'mise exec -- node -e \'console.log(process.getBuiltinModule("fs").rmdirSync(".tmp"))\'',
    'mise exec -- node -e \'console.log(process.getBuiltinModule("fs").renameSync("a", "b"))\'',
    'mise exec -- node -e \'console.log(process.getBuiltinModule("fs").truncateSync(".tmp"))\'',
  ]) {
    assert.throws(() => assertAllowedShellCommand(mutation), assert.AssertionError);
  }
  assert.throws(
    () => assertAllowedShellCommand("powershell -Command Remove-Item -Recurse -Force .tmp"),
    assert.AssertionError,
    "PowerShell deletion must be rejected",
  );
});
