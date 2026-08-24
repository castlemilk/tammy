import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { chmod, mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises";
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
  "taskfiles/sbr.yml",
  "taskfiles/sbr-helper.yml",
];
const allowedExecutablePatterns = [
  /^mise install$/,
  /^mise exec -- corepack prepare pnpm@11\.15\.0 --activate$/,
  /^mise exec -- pnpm (?:--version|install --frozen-lockfile|check:toolchain|check:macos-store|test|core:build|build:manifest|desktop:start|desktop:test|desktop:typecheck|contracts|contracts:production|lint|sqlcipher:test|sqlcipher:build|desktop:package|desktop:e2e)$/,
  /^TAMMY_MACOS_SIGNING_MODE=(?:development|distribution) mise exec -- pnpm desktop:make:mas$/,
  /^mise exec -- pnpm --filter @tammy\/connect-client typecheck$/,
  /^mise exec -- pnpm --dir apps\/desktop package$/,
  /^mise exec -- go test -race -tags tammy_sqlcipher \.\/services\/core\/internal\/storage\/sqlcipher\/\.\.\. -count=1$/,
  /^mise exec -- node scripts\/check-clean-tree\.mjs$/,
  /^mise exec -- node scripts\/launch-local-scenario\.mjs (?:accounting-fresh|sbr-simulator|sbr-doctor|sbr-evte)$/,
  /^mise exec -- node scripts\/check-sbr-registration\.mjs(?: --doctor-preflight)?$/,
  /^mise exec -- node scripts\/write-sbr-evidence\.mjs$/,
  /^mise exec -- go test -race \.\/services\/sbr-helper\/\.\.\. -count=1$/,
  /^mise exec -- go test -race \.\/services\/core\/internal\/sbrprofile \.\/services\/core\/internal\/sbrhelper \.\/services\/core\/internal\/sbr -count=1$/,
  /^mise exec -- go test -race -tags tammy_sqlcipher \.\/services\/core\/internal\/storage\/migrations \.\/services\/core\/internal\/sbr \.\/services\/core\/internal\/backup \.\/services\/core\/internal\/restore \.\/services\/core\/internal\/app \.\/services\/core\/cmd\/tammy-core -count=1$/,
  /^mise exec -- node --test scripts\/sbr-profile-schema\.test\.mjs scripts\/sbr-component-schema\.test\.mjs scripts\/sbr-registration-schema\.test\.mjs scripts\/check-sbr-registration\.test\.mjs scripts\/write-sbr-evidence\.test\.mjs scripts\/test-sbr-security-bookmark\.test\.mjs$/,
  /^mise exec -- node --test scripts\/check-slice-one-coverage-policy\.test\.mjs scripts\/check-e2e-coverage\.test\.mjs$/,
  /^mise exec -- pnpm --dir apps\/desktop exec vitest run --maxWorkers=1 .+$/,
  /^mise exec -- node scripts\/build-sbr-helper\.mjs$/,
  /^git diff --check$/,
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
const sbrTargetPreconditionScript = `const target = \`\${process.platform}/\${process.arch}\`; if (target !== "darwin/arm64") { console.error(\`UNSUPPORTED_SBR_TARGET:\${target}\`); process.exit(1); }`;
const setupTaskVersionScript = `let output=""; process.stdin.on("data", (chunk) => output += chunk); process.stdin.on("end", () => { if (output.trim() !== "3.52.0") { console.error(\`UNSUPPORTED_TASK_VERSION:\${output.trim()}\`); process.exit(1); } })`;
const ciTaskVersionCommand = `task --version | node -e '${setupTaskVersionScript}'`;
const macosReleaseTargetPreconditionScript = `const target = process.argv[1] ?? \`\${process.platform}/\${process.arch}\`; if (target !== "darwin/arm64") { console.error(\`UNSUPPORTED_MACOS_RELEASE_TARGET:\${target}\`); process.exit(1); }`;
const macosReleaseTargetVariableScript = `process.stdout.write(process.platform + "/" + process.arch)`;
const macosReleaseVerificationTargetPreconditionScript = `const target = process.argv[1] ?? \`\${process.platform}/\${process.arch}\`; if (target !== "darwin/arm64") { console.error(\`UNSUPPORTED_MACOS_RELEASE_TARGET:\${target}\`); process.exit(1); }`;
const macosReleaseRequiredEnvironment = [
  "TAMMY_MACOS_BUILD_NUMBER",
  "TAMMY_MACOS_EXPORT_COMPLIANCE",
  "TAMMY_MACOS_PROVISIONING_PROFILE",
  "TAMMY_MACOS_PRIVACY_POLICY_URL",
  "TAMMY_MACOS_SIGNING_IDENTITY",
  "TAMMY_MACOS_SUPPORT_URL",
  "TAMMY_MACOS_TEAM_ID",
];
function missingMacosReleaseEnvironmentScript(variable) {
  return `if (!process.env.${variable}?.trim()) { console.error("MISSING_MACOS_RELEASE_ENV:${variable}"); process.exit(1); }`;
}
const macosReleaseDevelopmentInstallerScript = `if (process.env.TAMMY_MACOS_INSTALLER_IDENTITY?.trim()) { console.error("DEVELOPMENT_INSTALLER_IDENTITY_FORBIDDEN"); process.exit(1); }`;
const fullVerificationRunnerScript = `const target = \`\${process.platform}/\${process.arch}\`; if (process.platform === "linux") process.exit(0); if (target === "darwin/arm64" || target === "win32/x64") { const { status } = await import("node:child_process").then(({ spawnSync }) => spawnSync("mise", ["exec", "--", "task", "test:sqlcipher"], { stdio: "inherit", shell: false })); process.exit(status ?? 1); } console.error(\`UNSUPPORTED_SQLCIPHER_TARGET:\${target}\`); process.exit(1);`;
const windowsSqlcipherHandoff = `$ErrorActionPreference = "Stop"; mise exec -- pnpm sqlcipher:test; if ($LASTEXITCODE -ne 0) { throw "SQLCIPHER_NODE_TEST_FAILED" }; mise exec -- pnpm sqlcipher:build; if ($LASTEXITCODE -ne 0) { throw "SQLCIPHER_BUILD_FAILED" }; $probe = (Resolve-Path ".tmp/sqlcipher/ordinary/win32-x64/ordinary-sqlite3.exe").Path; $env:TAMMY_ORDINARY_SQLITE3 = $probe; mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/storage/sqlcipher/... -count=1; if ($LASTEXITCODE -ne 0) { throw "SQLCIPHER_GO_INTEGRATION_FAILED" }`;
const sqlcipherTargetRunnerScript = `const target = \`\${process.platform}/\${process.arch}\`; const { status } = await import("node:child_process").then(({ spawnSync }) => { const run = (args) => spawnSync("mise", ["exec", "--", ...args], { stdio: "inherit", shell: false }); if (target === "darwin/arm64") { for (const args of [["pnpm", "sqlcipher:test"], ["pnpm", "sqlcipher:build"], ["go", "test", "-race", "-tags", "tammy_sqlcipher", "./services/core/internal/storage/sqlcipher/...", "-count=1"]]) { const result = run(args); if (result.status !== 0) process.exit(result.status ?? 1); } return { status: 0 }; } if (target === "win32/x64") return run(["pwsh", "-NoProfile", "-EncodedCommand", Buffer.from(\`${windowsSqlcipherHandoff}\`, "utf16le").toString("base64")]); console.error(\`UNSUPPORTED_SQLCIPHER_TARGET:\${target}\`); return { status: 1 }; }); process.exit(status ?? 1);`;

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

function resolveTaskReference(namespace, reference) {
  return reference.startsWith(":")
    ? reference.slice(1)
    : [namespace, reference].filter(Boolean).join(":");
}

async function collectTaskGraph(relativePath, namespace = "", graph = new Map()) {
  const taskfile = await readTaskfile(relativePath);
  for (const [taskName, task] of Object.entries(taskfile.tasks ?? {})) {
    const fullName = namespace ? `${namespace}:${taskName}` : taskName;
    assert.equal(graph.has(fullName), false, `duplicate task ${fullName}`);
    graph.set(fullName, { namespace, task, taskfile: relativePath });
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

function run(command, args, environment, { clearTaskEnvironment = false } = {}) {
  return new Promise((resolve, reject) => {
    const baseEnvironment = clearTaskEnvironment
      ? Object.fromEntries(
          Object.entries(process.env).filter(([name]) => !name.startsWith("TASK_")),
        )
      : process.env;
    const child = spawn(command, args, {
      env: environment ? { ...baseEnvironment, ...environment } : baseEnvironment,
      stdio: ["ignore", "pipe", "pipe"],
    });
    let stderr = "";
    let stdout = "";
    child.stdout.on("data", (chunk) => (stdout += chunk));
    child.stderr.on("data", (chunk) => (stderr += chunk));
    child.on("error", reject);
    child.on("close", (code) => resolve({ code, stderr, stdout }));
  });
}

function runTask(taskName, environment, taskArguments, options) {
  return run(
    "mise",
    ["exec", "--", "task", taskName, ...(taskArguments ?? [])],
    environment,
    options,
  );
}

function nodePrintScript(lines) {
  return `console.log(${JSON.stringify(lines.join("\n"))})`;
}

function nodePrintCommand(lines) {
  return `mise exec -- node -e '${nodePrintScript(lines)}'`;
}

test("documentation presents Task scenarios as the local command front door", async () => {
  const [readme, foundation, releaseRunbook] = await Promise.all([
    readFile(path.join(repositoryRoot, "README.md"), "utf8"),
    readFile(path.join(repositoryRoot, "docs/development/foundation.md"), "utf8"),
    readFile(path.join(repositoryRoot, "docs/release/macos-app-store.md"), "utf8"),
  ]);
  const bootstrap = /```sh\nmise install\nmise exec -- task setup\nmise exec -- task dev\n```/;
  assert.match(readme, bootstrap, "README must show the complete Task bootstrap flow");
  assert.match(foundation, bootstrap, "foundation must show the complete Task bootstrap flow");
  for (const [document, name, taskNames] of [
    [
      readme,
      "README",
      [
        "test",
        "verify",
        "package:e2e",
        "release:check",
        "release:development",
        "release:candidate",
        "deploy:mas",
      ],
    ],
    [
      foundation,
      "foundation",
      ["test", "verify", "build", "package", "package:e2e", "diagnose:data"],
    ],
    [
      releaseRunbook,
      "release runbook",
      ["release:check", "release:development", "release:candidate", "deploy:mas"],
    ],
  ]) {
    for (const taskName of taskNames) {
      assert.match(
        document,
        new RegExp(`mise exec -- task ${taskName.replaceAll(":", "\\:")}`),
        `${name} must show the ${taskName} scenario`,
      );
    }
  }
  for (const document of [readme, foundation, releaseRunbook]) {
    assert.doesNotMatch(
      document,
      /\brtk\b/,
      "documentation commands are for operators, not agents",
    );
  }

  for (const command of [
    "mise exec -- corepack prepare pnpm@11.15.0 --activate",
    "mise exec -- pnpm install --frozen-lockfile",
    "mise exec -- pnpm check:toolchain",
  ]) {
    assert.match(
      foundation,
      new RegExp(command.replaceAll(/[.*+?^${}()|[\]\\]/g, "\\$&")),
      `foundation retains the underlying ${command} reference`,
    );
  }

  for (const phrase of [
    "local-first encrypted accounting",
    "supervised Go core",
    "local-core-development",
    "local-core",
    "development-memory-anchors",
    "Do not manually delete",
    "recoverable",
    "no ATO submission",
    "protobuf source, not generated",
    "focused failing test",
    "loader/linker",
    "orphan",
  ]) {
    assert.match(foundation, new RegExp(phrase, "i"), `foundation retains ${phrase}`);
  }
  for (const variable of [...macosReleaseRequiredEnvironment, "TAMMY_MACOS_INSTALLER_IDENTITY"]) {
    assert.match(releaseRunbook, new RegExp(variable), `release runbook retains ${variable}`);
  }
  for (const phrase of [
    "## One-time Apple setup",
    "legal entity",
    "Mac App ID",
    "Apple Development and Apple Distribution",
    "separate Mac App Store development and distribution provisioning profiles",
    "App Store Connect record",
    "OPERATOR_REQUIRED",
    "export-compliance determination",
    "CFBundleVersion",
    "forces development signing",
    "force(?:s)? distribution signing",
    "apps/desktop/out/Tammy-mas-arm64/Tammy.app",
    "apps/desktop/out/make/pkg/arm64/Tammy-<version>-build.<number>.pkg",
    "## Inspect the signed build",
    "codesign --verify",
    "App Sandbox",
    "Electron helpers inherit the sandbox",
    "ElectronTeamID",
    "PrivacyInfo.xcprivacy",
    "Every nested framework, helper, library, and executable",
    "Xcode's privacy report",
    "pkgutil --check-signature",
    "spctl --assess --type install",
    "observational local Gatekeeper evidence",
    "## Metadata and App Review",
    "Open `/privacy` before sign-in",
    "one to ten factual screenshots",
    "BAS draft — not lodged",
    "no remote account or demo credentials",
    "no advertising, analytics, tracking",
    "manual.*Transporter",
    "never upload",
    "## Release record and rollback",
    "Record the commit, marketing version, build number, signing/profile names, package SHA-256",
    "App Store rollback means",
    "Never reuse an uploaded build number",
  ]) {
    assert.match(releaseRunbook, new RegExp(phrase, "i"), `release runbook retains ${phrase}`);
  }
});

const allowedNodeScripts = new Set([
  rootBoundaryScript,
  targetPreconditionScript,
  sbrTargetPreconditionScript,
  setupTaskVersionScript,
  macosReleaseTargetPreconditionScript,
  macosReleaseTargetVariableScript,
  macosReleaseVerificationTargetPreconditionScript,
  ...macosReleaseRequiredEnvironment.map(missingMacosReleaseEnvironmentScript),
  missingMacosReleaseEnvironmentScript("TAMMY_MACOS_INSTALLER_IDENTITY"),
  macosReleaseDevelopmentInstallerScript,
  sqlcipherTargetRunnerScript,
  fullVerificationRunnerScript,
  nodePrintScript(coreDiagnosticLines),
  nodePrintScript([packageDiagnosticLine]),
  nodePrintScript(dataDiagnosticLines),
]);

async function runTargetPrecondition(precondition, target, environment) {
  const match = precondition.sh.match(/^mise exec -- node -e '([\s\S]*)'$/);
  assert.ok(match, "the native precondition must be an extractable pinned Node command");
  return run("mise", ["exec", "--", "node", "-e", match[1], target], environment);
}

async function runSbrTargetPrecondition(precondition, platform, architecture) {
  const match = precondition.sh.match(/^mise exec -- node -e '([\s\S]*)'$/);
  assert.ok(match, "the SBR precondition must be an extractable pinned Node command");
  const fixture = `Object.defineProperty(process, "platform", { value: ${JSON.stringify(platform)} }); Object.defineProperty(process, "arch", { value: ${JSON.stringify(architecture)} }); ${match[1]}`;
  return run("mise", ["exec", "--", "node", "-e", fixture]);
}

const sensitiveSbrSurfacePattern =
  /\b(?:credential[ _-]?(?:path|password)|private[ _-]?key[ _-]?path|product[ _-]?id(?!-form\.test\.tsx)|endpoint[ _-]?url|secret[ _-]?token)\b/i;

function assertNoSensitiveSbrSurface(task) {
  for (const [surface, value] of Object.entries({
    commands: task.cmds,
    preconditions: task.preconditions,
    summary: task.summary,
    variables: task.vars,
  })) {
    assert.doesNotMatch(
      JSON.stringify(value ?? null),
      sensitiveSbrSurfacePattern,
      `SBR task ${surface} must not accept or print sensitive inputs`,
    );
  }
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
  assert.ok((await collectTaskGraph("Taskfile.yml")).has("sbr-helper:build"));

  const setup = await readTaskfile("taskfiles/setup.yml");
  const dev = await readTaskfile("taskfiles/dev.yml");
  const diagnostics = await readTaskfile("taskfiles/diagnostics.yml");
  const sbr = await readTaskfile("taskfiles/sbr.yml");
  for (const taskfile of [setup, dev, diagnostics, sbr]) assert.equal(taskfile.version, "3");
  const taskGraph = await collectTaskGraph("Taskfile.yml");

  const rootTasks = root.tasks ?? {};
  for (const taskName of [
    "default",
    "setup",
    "dev",
    "test",
    "verify",
    "verify:quick",
    "verify:full",
    "verify:release",
    "build",
    "package",
    "deploy:mas",
    "sbr:doctor",
    "sbr:registration:check",
    "test:sbr",
    "evidence:sbr",
  ]) {
    assert.match(rootTasks[taskName]?.desc ?? "", /.+/, `${taskName} requires a description`);
  }
  assert.match(rootTasks.default.summary ?? "", /local-first encrypted accounting/i);
  assert.match(rootTasks.default.summary ?? "", /development-only BAS workpapers/i);
  assert.match(rootTasks.default.summary ?? "", /no ATO submission/i);
  assert.match(rootTasks.default.summary ?? "", /not production\/App Store evidence/i);
  assert.match(rootTasks.default.summary ?? "", /upload.*manual/i);
  assert.deepEqual(taskReferences(rootTasks.setup), ["setup:tools", "setup:deps", "setup:check"]);
  assert.deepEqual(taskReferences(rootTasks.dev), ["dev:launch"]);
  const expectedSbrAliases = new Map([
    ["dev:accounting", ["dev:launch", "taskfiles/dev.yml"]],
    ["dev:accounting:fresh", ["sbr:launch-accounting-fresh", "taskfiles/dev.yml"]],
    ["dev:sbr:simulator", ["sbr:launch-simulator", "taskfiles/dev.yml"]],
    ["dev:sbr:evte", ["sbr:launch-evte", "taskfiles/dev.yml"]],
    ["sbr:doctor", ["sbr:run-doctor", "Taskfile.yml"]],
    ["sbr:registration:check", ["sbr:run-registration-check", "Taskfile.yml"]],
    ["test:sbr", ["sbr:run-test", "Taskfile.yml"]],
    ["evidence:sbr", ["sbr:run-evidence", "Taskfile.yml"]],
  ]);
  for (const [alias, [reference, taskfile]] of expectedSbrAliases) {
    const entry = taskGraph.get(alias);
    assert.ok(entry, `${alias} exists`);
    assert.equal(entry.taskfile, taskfile, `${alias} belongs to its public namespace owner`);
    assert.deepEqual(
      taskReferences(entry.task).map((taskReference) =>
        resolveTaskReference(entry.namespace, taskReference),
      ),
      [reference],
      `${alias} exact delegation`,
    );
    assert.equal(entry.task.deps, undefined, `${alias} uses sequential task calls`);
  }
  assert.equal(
    taskGraph.get("dev:accounting").task.preconditions?.[0]?.sh,
    `mise exec -- node -e '${targetPreconditionScript}'`,
    "ordinary accounting retains the supported SQLCipher host guard",
  );
  assert.deepEqual(shellCommands(rootTasks.test), ["mise exec -- pnpm test"]);
  assert.equal(taskCommands(rootTasks.test).length, 1, "test delegates to pnpm test exactly once");
  for (const taskName of ["setup", "dev", "test"]) {
    assert.equal(
      rootTasks[taskName].deps,
      undefined,
      `${taskName} must use sequential cmds, not deps`,
    );
  }
  const rootDevPrecondition = rootTasks.dev.preconditions?.[0];
  assert.equal(rootDevPrecondition?.sh, `mise exec -- node -e '${targetPreconditionScript}'`);
  for (const { target, accepted } of [
    { target: "darwin/arm64", accepted: true },
    { target: "win32/x64", accepted: true },
    { target: "linux/x64", accepted: false },
  ]) {
    const result = await runTargetPrecondition(rootDevPrecondition, target);
    assert.equal(result.code === 0, accepted, `dev target ${target}`);
  }
  assert.deepEqual(taskReferences(rootTasks.verify), ["verify:full"]);
  assert.deepEqual(taskReferences(rootTasks["verify:quick"]), ["test:verify:quick"]);
  assert.deepEqual(taskReferences(rootTasks["verify:full"]), ["test:verify:full"]);
  assert.deepEqual(taskReferences(rootTasks["verify:release"]), ["test:verify:release"]);
  assert.deepEqual(taskReferences(rootTasks.build), ["build:desktop"]);
  assert.deepEqual(taskReferences(rootTasks.package), ["package:verify"]);
  const deployMas = rootTasks["deploy:mas"];
  assert.match(deployMas.summary ?? "", /locally validates.*package/i);
  assert.match(deployMas.summary ?? "", /manual.*submission/i);
  assert.equal(deployMas.platforms, undefined, "deploy:mas must fail instead of silently skipping");
  assert.equal(
    deployMas.preconditions?.[0]?.sh,
    `mise exec -- node -e '${macosReleaseTargetPreconditionScript}'`,
  );
  assert.equal(
    deployMas.preconditions?.[0]?.msg,
    "UNSUPPORTED_MACOS_RELEASE_TARGET:{{.NODE_TARGET}}",
  );
  assert.equal(
    deployMas.vars?.NODE_TARGET?.sh,
    `mise exec -- node -e '${macosReleaseTargetVariableScript}'`,
  );
  for (const { target, accepted } of [
    { target: "darwin/arm64", accepted: true },
    { target: "linux/x64", accepted: false },
  ]) {
    const result = await runTargetPrecondition(deployMas.preconditions[0], target);
    assert.equal(result.code === 0, accepted, `deploy:mas target ${target}`);
    if (!accepted) {
      assert.match(
        `${result.stdout}${result.stderr}`,
        /UNSUPPORTED_MACOS_RELEASE_TARGET:linux\/x64/,
      );
    }
  }
  assert.deepEqual(taskCommands(deployMas), [{ task: "release:candidate" }]);
  assert.deepEqual(taskReferences(deployMas), ["release:candidate"]);
  for (const taskName of ["build", "package"]) {
    const precondition = rootTasks[taskName].preconditions?.[0];
    assert.equal(precondition?.sh, `mise exec -- node -e '${targetPreconditionScript}'`);
    for (const { target, accepted } of [
      { target: "darwin/arm64", accepted: true },
      { target: "win32/x64", accepted: true },
      { target: "linux/x64", accepted: false },
    ]) {
      const result = await runTargetPrecondition(precondition, target);
      assert.equal(result.code === 0, accepted, `${taskName} target ${target}`);
    }
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

  const sbrLeaves = new Map([
    [
      "launch-accounting-fresh",
      ["mise exec -- node scripts/launch-local-scenario.mjs accounting-fresh"],
    ],
    ["launch-simulator", ["mise exec -- node scripts/launch-local-scenario.mjs sbr-simulator"]],
    ["launch-evte", ["mise exec -- node scripts/check-sbr-registration.mjs", "mise exec -- node scripts/launch-local-scenario.mjs sbr-evte"]],
    ["run-doctor", ["mise exec -- node scripts/check-sbr-registration.mjs --doctor-preflight", "mise exec -- node scripts/launch-local-scenario.mjs sbr-doctor"]],
    ["run-registration-check", ["mise exec -- node scripts/check-sbr-registration.mjs"]],
    [
      "run-test",
      [
        "mise exec -- go test -race ./services/sbr-helper/... -count=1",
        "mise exec -- go test -race ./services/core/internal/sbrprofile ./services/core/internal/sbrhelper ./services/core/internal/sbr -count=1",
        "mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/storage/migrations ./services/core/internal/sbr ./services/core/internal/backup ./services/core/internal/restore ./services/core/internal/app ./services/core/cmd/tammy-core -count=1",
        "mise exec -- node --test scripts/sbr-profile-schema.test.mjs scripts/sbr-component-schema.test.mjs scripts/sbr-registration-schema.test.mjs scripts/check-sbr-registration.test.mjs scripts/write-sbr-evidence.test.mjs scripts/test-sbr-security-bookmark.test.mjs",
        "mise exec -- pnpm --dir apps/desktop exec vitest run --maxWorkers=1 src/preload/index.test.ts src/main/core-client.test.ts src/main/rpc-router.test.ts src/main/ipc.test.ts src/main/sbr-file-intake.test.ts src/main/index-production.test.ts src/main/index.test.ts src/main/playwright-config.test.ts src/main/launch-scenario.test.ts src/renderer/app.test.tsx src/renderer/app-shell/simulator-banner.test.tsx src/renderer/features/sbr/sbr-readiness-screen.test.tsx src/renderer/features/sbr/machine-credential-form.test.tsx src/renderer/features/sbr/product-id-form.test.tsx src/renderer/features/sbr/organisation-verification-form.test.tsx src/renderer/features/sbr/sbr-simulator-panel.test.tsx",
        "mise exec -- node --test scripts/check-slice-one-coverage-policy.test.mjs scripts/check-e2e-coverage.test.mjs",
        "mise exec -- pnpm --dir apps/desktop exec vitest run --maxWorkers=1 src/main/e2e-process-check.test.ts src/main/sbr-result.test.ts",
        "mise exec -- pnpm contracts",
      ],
    ],
    ["run-evidence", ["mise exec -- node scripts/write-sbr-evidence.mjs"]],
  ]);
  assert.deepEqual(Object.keys(sbr.tasks ?? {}), [...sbrLeaves.keys()]);
  assert.deepEqual(
    [...sbrLeaves.keys()].filter((taskName) => sbr.tasks?.[taskName]?.internal !== true),
    [],
    "every SBR implementation leaf must be internal",
  );
  const publicTaskList = await run("mise", ["exec", "--", "task", "--list"]);
  assert.equal(publicTaskList.code, 0, "the public Task list renders");
  for (const taskName of sbrLeaves.keys()) {
    assert.doesNotMatch(
      publicTaskList.stdout,
      new RegExp(`\\* sbr:${taskName.replaceAll(":", "\\:")}:`),
      `sbr:${taskName} must be absent from the public Task list`,
    );
  }
  for (const taskName of expectedSbrAliases.keys()) {
    assert.match(
      publicTaskList.stdout,
      new RegExp(`\\* ${taskName.replaceAll(":", "\\:")}:`),
      `${taskName} remains public`,
    );
  }
  const guardedPublicSbrTasks = [
    ...[...expectedSbrAliases.keys()].filter(
      (taskName) => taskName !== "dev:accounting" && taskName !== "dev:accounting:fresh",
    ),
  ];
  assert.equal(
    taskGraph.get("dev:accounting:fresh").task.preconditions?.[0]?.sh,
    `mise exec -- node -e '${targetPreconditionScript}'`,
    "fresh accounting uses the ordinary SQLCipher host guard, not the macOS SBR guard",
  );
  for (const taskName of guardedPublicSbrTasks) {
    const task = taskGraph.get(taskName).task;
    assert.equal(
      task.preconditions?.[0]?.sh,
      `mise exec -- node -e '${sbrTargetPreconditionScript}'`,
      `${taskName} uses the exact real-host SBR guard`,
    );
    assert.equal(
      task.preconditions?.[0]?.msg,
      "UNSUPPORTED_SBR_TARGET:{{OS}}/{{ARCH}}",
      `${taskName} exposes the exact real-host SBR guard error`,
    );
    assertNoSensitiveSbrSurface(task);
  }
  for (const [taskName, commands] of sbrLeaves) {
    const task = sbr.tasks?.[taskName];
    assert.match(task?.desc ?? "", /.+/, `sbr:${taskName} requires a description`);
    assert.match(task?.summary ?? "", /.+/, `sbr:${taskName} requires a summary`);
    assert.equal(task.deps, undefined, `sbr:${taskName} uses sequential commands`);
    if (taskName === "launch-accounting-fresh") {
      assert.equal(task.preconditions, undefined, "ordinary fresh accounting has no SBR guard");
    } else {
      assert.equal(
        task.preconditions?.[0]?.sh,
        `mise exec -- node -e '${sbrTargetPreconditionScript}'`,
        `sbr:${taskName} uses the exact real-host SBR guard`,
      );
      assert.equal(
        task.preconditions?.[0]?.msg,
        "UNSUPPORTED_SBR_TARGET:{{OS}}/{{ARCH}}",
        `sbr:${taskName} exposes the exact real-host SBR guard error`,
      );
    }
    assert.deepEqual(shellCommands(task), commands);
    assertNoSensitiveSbrSurface(task);
  }
  const publicSbrSummaries = guardedPublicSbrTasks
    .map((taskName) => taskGraph.get(taskName).task.summary ?? "")
    .join("\n");
  for (const pattern of [
    /simulator.*synthetic.*network-disabled/i,
    /EVTE.*non-production.*signed external inputs/i,
    /no live credential.*accepted by Task/i,
    /BAS.*preparation-only.*no submit.*lodge/i,
    /production.*unavailable/i,
  ]) {
    assert.match(publicSbrSummaries, pattern, `public SBR summaries retain ${pattern}`);
  }
  for (const [surface, value] of [
    ["commands", { cmds: ["print credential_path"] }],
    ["variables", { vars: { credential_password: "unsafe" } }],
    ["preconditions", { preconditions: [{ sh: "read private-key-path" }] }],
    ["summary", { summary: "Accept Product ID and endpoint URL plus secret token." }],
  ]) {
    assert.throws(
      () => assertNoSensitiveSbrSurface(value),
      assert.AssertionError,
      `negative ${surface} fixture must be rejected`,
    );
  }
  for (const [platform, architecture, accepted] of [
    ["darwin", "arm64", true],
    ["linux", "x64", false],
  ]) {
    const result = await runSbrTargetPrecondition(
      sbr.tasks["launch-simulator"].preconditions[0],
      platform,
      architecture,
    );
    assert.equal(result.code === 0, accepted, `SBR target ${platform}/${architecture}`);
    if (!accepted) {
      assert.equal(result.stdout, "");
      assert.equal(result.stderr, `UNSUPPORTED_SBR_TARGET:${platform}/${architecture}\n`);
    }
  }
  const actualSbrTarget = `${process.platform}/${process.arch}`;
  const sbrOutputDirectory = await mkdtemp(path.join("/private/tmp", "tammy-sbr-task-output-"));
  try {
    const registration = await runTask("sbr:registration:check", {
      TAMMY_SBR_EVIDENCE_DIR: sbrOutputDirectory,
    });
    if (actualSbrTarget === "darwin/arm64") {
      assert.notEqual(registration.code, 0);
      const report = JSON.parse(registration.stdout);
      assert.equal(report.status, "BLOCKED");
      assert.equal(report.code, "EVTE_SIGNED_INPUTS_REQUIRED");
      assert.equal(
        registration.stderr,
        'task: [sbr:run-registration-check] mise exec -- node scripts/check-sbr-registration.mjs\ntask: Failed to run task "sbr:registration:check": task: Failed to run task "sbr:run-registration-check": exit status 1\n',
      );
    } else {
      assert.notEqual(registration.code, 0);
      assert.match(
        `${registration.stdout}${registration.stderr}`,
        new RegExp(`UNSUPPORTED_SBR_TARGET:${actualSbrTarget}`),
      );
    }

    const evte = await runTask("dev:sbr:evte", {
      TAMMY_SBR_EVIDENCE_DIR: sbrOutputDirectory,
    });
    assert.notEqual(evte.code, 0);
    assert.match(
      `${evte.stdout}${evte.stderr}`,
      actualSbrTarget === "darwin/arm64"
        ? /EVTE_SIGNED_INPUTS_REQUIRED/
        : new RegExp(`UNSUPPORTED_SBR_TARGET:${actualSbrTarget}`),
    );
    assert.deepEqual(await readdir(sbrOutputDirectory), []);
  } finally {
    await rm(sbrOutputDirectory, { force: true, recursive: true });
  }

  const rootPackage = JSON.parse(await readFile(path.join(repositoryRoot, "package.json"), "utf8"));
  assert.equal(
    rootPackage.scripts?.["desktop:start:scenario"],
    "node scripts/launch-local-scenario.mjs --electron-forge",
    "the launcher owns one narrow Electron Forge package scenario",
  );

  const testTasks = (await readTaskfile("taskfiles/test.yml")).tasks;
  for (const taskName of [
    "core",
    "desktop",
    "contracts",
    "sqlcipher",
    "verify:quick",
    "verify:full",
    "verify:release",
  ]) {
    assert.match(testTasks[taskName]?.desc ?? "", /.+/, `test:${taskName} requires a description`);
    assert.equal(testTasks[taskName].deps, undefined, `test:${taskName} must use sequential cmds`);
    assert.equal(
      testTasks[taskName].platforms,
      undefined,
      `test:${taskName} must not silently skip`,
    );
  }
  assert.deepEqual(shellCommands(testTasks.core), ["mise exec -- pnpm test"]);
  assert.deepEqual(shellCommands(testTasks.desktop), ["mise exec -- pnpm desktop:test"]);
  assert.deepEqual(shellCommands(testTasks.contracts), ["mise exec -- pnpm contracts"]);
  const sqlcipherPrecondition = testTasks.sqlcipher.preconditions?.[0];
  assert.equal(sqlcipherPrecondition?.sh, `mise exec -- node -e '${targetPreconditionScript}'`);
  assert.deepEqual(shellCommands(testTasks.sqlcipher), [
    `mise exec -- node -e '${sqlcipherTargetRunnerScript}'`,
  ]);
  assert.match(
    sqlcipherTargetRunnerScript,
    /\["pnpm", "sqlcipher:test"\], \["pnpm", "sqlcipher:build"\], \["go", "test", "-race", "-tags", "tammy_sqlcipher", "\.\/services\/core\/internal\/storage\/sqlcipher\/\.\.\.", "-count=1"\]/,
    "macOS SQLCipher verification must build its clean-checkout native resource before tagged Go tests",
  );
  assert.match(
    windowsSqlcipherHandoff,
    /mise exec -- pnpm sqlcipher:test; if \(\$LASTEXITCODE -ne 0\) \{ throw "SQLCIPHER_NODE_TEST_FAILED" \}; mise exec -- pnpm sqlcipher:build; if \(\$LASTEXITCODE -ne 0\) \{ throw "SQLCIPHER_BUILD_FAILED" \}; [\s\S]*; mise exec -- go test -race -tags tammy_sqlcipher \.\/services\/core\/internal\/storage\/sqlcipher\/\.\.\. -count=1; if \(\$LASTEXITCODE -ne 0\) \{ throw "SQLCIPHER_GO_INTEGRATION_FAILED" \}/,
    "the Windows handoff must fail immediately after every native command",
  );
  for (const { target, accepted } of [
    { target: "darwin/arm64", accepted: true },
    { target: "win32/x64", accepted: true },
    { target: "linux/x64", accepted: false },
  ]) {
    const result = await runTargetPrecondition(sqlcipherPrecondition, target);
    assert.equal(result.code === 0, accepted, `test:sqlcipher target ${target}`);
  }
  assert.deepEqual(taskReferences(testTasks["verify:quick"]), []);
  assert.deepEqual(shellCommands(testTasks["verify:quick"]), [
    "mise exec -- pnpm check:toolchain",
    "mise exec -- pnpm test",
    "mise exec -- pnpm --filter @tammy/connect-client typecheck",
    "mise exec -- pnpm desktop:typecheck",
    "mise exec -- pnpm contracts",
    "mise exec -- pnpm lint",
    "git diff --check",
  ]);
  assert.deepEqual(taskReferences(testTasks["verify:full"]), ["verify:quick"]);
  assert.deepEqual(shellCommands(testTasks["verify:full"]), [
    `mise exec -- node -e '${fullVerificationRunnerScript}'`,
  ]);
  const releaseVerificationPrecondition = testTasks["verify:release"].preconditions?.[0];
  assert.equal(
    releaseVerificationPrecondition?.sh,
    `mise exec -- node -e '${macosReleaseVerificationTargetPreconditionScript}'`,
  );
  for (const { target, accepted } of [
    { target: "darwin/arm64", accepted: true },
    { target: "linux/x64", accepted: false },
  ]) {
    const result = await runTargetPrecondition(releaseVerificationPrecondition, target);
    assert.equal(result.code === 0, accepted, `test:verify:release target ${target}`);
  }
  assert.deepEqual(taskReferences(testTasks["verify:release"]), [
    "verify:full",
    ":package:e2e",
    ":release:check",
  ]);
  assert.deepEqual(taskCommands(testTasks["verify:release"]), [
    "mise exec -- node scripts/check-clean-tree.mjs",
    "mise exec -- pnpm contracts:production",
    { task: "verify:full" },
    { task: ":package:e2e" },
    { task: ":release:check" },
  ]);

  const buildTasks = (await readTaskfile("taskfiles/build.yml")).tasks;
  for (const taskName of ["sqlcipher", "core", "desktop"]) {
    const task = buildTasks[taskName];
    assert.equal(task.platforms, undefined, `build:${taskName} must not silently skip`);
    assert.equal(task.preconditions?.[0]?.sh, `mise exec -- node -e '${targetPreconditionScript}'`);
    for (const { target, accepted } of [
      { target: "darwin/arm64", accepted: true },
      { target: "win32/x64", accepted: true },
      { target: "linux/x64", accepted: false },
    ]) {
      const result = await runTargetPrecondition(task.preconditions[0], target);
      assert.equal(result.code === 0, accepted, `build:${taskName} target ${target}`);
    }
  }
  assert.deepEqual(shellCommands(buildTasks.sqlcipher), ["mise exec -- pnpm sqlcipher:build"]);
  assert.deepEqual(shellCommands(buildTasks.core), ["mise exec -- pnpm core:build"]);
  assert.equal(buildTasks.manifest.preconditions, undefined);
  assert.deepEqual(shellCommands(buildTasks.manifest), ["mise exec -- pnpm build:manifest"]);
  assert.deepEqual(shellCommands(buildTasks.desktop), ["mise exec -- pnpm desktop:package"]);
  assert.deepEqual(taskReferences(buildTasks.desktop), []);
  assert.doesNotMatch(
    JSON.stringify(buildTasks.desktop.cmds),
    /sbr-helper:build|build:manifest|apps\/desktop package/,
  );
  assert.match(buildTasks.desktop.summary ?? "", /verified.*single build owner/i);

  const packageTasks = (await readTaskfile("taskfiles/package.yml")).tasks;
  assert.equal(packageTasks.launch, undefined, "package:launch must not exist");
  for (const taskName of ["verify", "e2e"]) {
    const task = packageTasks[taskName];
    assert.equal(task.platforms, undefined, `package:${taskName} must not silently skip`);
    assert.equal(task.preconditions?.[0]?.sh, `mise exec -- node -e '${targetPreconditionScript}'`);
    for (const { target, accepted } of [
      { target: "darwin/arm64", accepted: true },
      { target: "win32/x64", accepted: true },
      { target: "linux/x64", accepted: false },
    ]) {
      const result = await runTargetPrecondition(task.preconditions[0], target);
      assert.equal(result.code === 0, accepted, `package:${taskName} target ${target}`);
    }
    assert.match(task.summary ?? "", /not.*evidence/i);
  }
  assert.match(
    packageTasks.verify.summary ?? "",
    /authenticates[\s\S]*artifact[\s\S]*source[\s\S]*core[\s\S]*signature[\s\S]*resource/i,
  );
  assert.match(
    packageTasks.verify.summary ?? "",
    /excludes[\s\S]*runtime launch[\s\S]*isolated Electron[\s\S]*userData[\s\S]*orphan proof/i,
  );
  assert.match(
    packageTasks.e2e.summary ?? "",
    /authenticates[\s\S]*source[\s\S]*core[\s\S]*package/i,
  );
  assert.match(
    packageTasks.e2e.summary ?? "",
    /isolated Electron userData[\s\S]*runtime[\s\S]*orphan evidence/i,
  );
  assert.deepEqual(shellCommands(packageTasks.verify), ["mise exec -- pnpm desktop:package"]);
  assert.deepEqual(shellCommands(packageTasks.e2e), ["mise exec -- pnpm desktop:e2e"]);

  const releaseTasks = (await readTaskfile("taskfiles/release.yml")).tasks;
  assert.deepEqual(Object.keys(releaseTasks), ["check", "development", "candidate"]);
  assert.equal(releaseTasks.check.preconditions, undefined);
  assert.deepEqual(shellCommands(releaseTasks.check), ["mise exec -- pnpm check:macos-store"]);
  for (const [taskName, signingMode, requiredVariables] of [
    ["development", "development", macosReleaseRequiredEnvironment],
    [
      "candidate",
      "distribution",
      [...macosReleaseRequiredEnvironment, "TAMMY_MACOS_INSTALLER_IDENTITY"],
    ],
  ]) {
    const task = releaseTasks[taskName];
    assert.match(task.desc ?? "", /.+/, `release:${taskName} requires a description`);
    assert.equal(
      task.platforms,
      undefined,
      `release:${taskName} must fail instead of silently skipping`,
    );
    assert.equal(
      task.env?.TAMMY_MACOS_SIGNING_MODE,
      undefined,
      "signing mode must be command-scoped",
    );
    assert.equal(task.preconditions?.[0]?.msg, "UNSUPPORTED_MACOS_RELEASE_TARGET:{{.NODE_TARGET}}");
    for (const [index, precondition] of (task.preconditions ?? []).slice(1).entries()) {
      assert.equal(
        precondition.msg,
        taskName === "development" && index === macosReleaseRequiredEnvironment.length
          ? "DEVELOPMENT_INSTALLER_IDENTITY_FORBIDDEN"
          : `MISSING_MACOS_RELEASE_ENV:${requiredVariables[index]}`,
        `release:${taskName} must expose exact input errors`,
      );
    }
    assert.equal(
      task.vars?.NODE_TARGET?.sh,
      `mise exec -- node -e '${macosReleaseTargetVariableScript}'`,
    );
    assert.equal(
      task.preconditions?.[0]?.sh,
      `mise exec -- node -e '${macosReleaseTargetPreconditionScript}'`,
    );
    assert.deepEqual(
      task.preconditions
        ?.slice(1, requiredVariables.length + 1)
        .map((precondition) => precondition.sh),
      requiredVariables.map(
        (variable) => `mise exec -- node -e '${missingMacosReleaseEnvironmentScript(variable)}'`,
      ),
    );
    for (const { target, accepted } of [
      { target: "darwin/arm64", accepted: true },
      { target: "linux/x64", accepted: false },
    ]) {
      const result = await runTargetPrecondition(task.preconditions[0], target);
      assert.equal(result.code === 0, accepted, `release:${taskName} target ${target}`);
      if (!accepted) {
        assert.match(
          `${result.stdout}${result.stderr}`,
          /UNSUPPORTED_MACOS_RELEASE_TARGET:linux\/x64/,
        );
      }
    }
    const missing = await runTargetPrecondition(task.preconditions[1], "darwin/arm64", {});
    assert.equal(missing.code, 1, `release:${taskName} rejects missing signing inputs`);
    assert.match(
      `${missing.stdout}${missing.stderr}`,
      /MISSING_MACOS_RELEASE_ENV:TAMMY_MACOS_BUILD_NUMBER/,
    );
    assert.deepEqual(shellCommands(task), [
      "mise exec -- node scripts/check-clean-tree.mjs",
      `TAMMY_MACOS_SIGNING_MODE=${signingMode} mise exec -- pnpm desktop:make:mas`,
    ]);
  }
  assert.equal(
    releaseTasks.development.preconditions?.[8]?.sh,
    `mise exec -- node -e '${macosReleaseDevelopmentInstallerScript}'`,
  );
  const developmentInstaller = await runTargetPrecondition(
    releaseTasks.development.preconditions[8],
    "darwin/arm64",
    {
      TAMMY_MACOS_INSTALLER_IDENTITY:
        "3rd Party Mac Developer Installer: Tammy Pty Ltd (ABCDE12345)",
    },
  );
  assert.equal(developmentInstaller.code, 1, "release:development rejects an installer identity");
  assert.match(
    `${developmentInstaller.stdout}${developmentInstaller.stderr}`,
    /DEVELOPMENT_INSTALLER_IDENTITY_FORBIDDEN/,
  );
  const missingReleaseEnvironment = Object.fromEntries(
    [...macosReleaseRequiredEnvironment, "TAMMY_MACOS_INSTALLER_IDENTITY"].map((name) => [
      name,
      "",
    ]),
  );
  const actualTarget = `${process.platform}/${process.arch}`;
  const taskFailure =
    actualTarget === "darwin/arm64"
      ? /MISSING_MACOS_RELEASE_ENV:TAMMY_MACOS_BUILD_NUMBER/
      : new RegExp(`UNSUPPORTED_MACOS_RELEASE_TARGET:${actualTarget}`);
  for (const [taskName, expected] of [
    ["release:development", /MISSING_MACOS_RELEASE_ENV:TAMMY_MACOS_BUILD_NUMBER/],
    ["release:candidate", /MISSING_MACOS_RELEASE_ENV:TAMMY_MACOS_BUILD_NUMBER/],
    ["deploy:mas", /MISSING_MACOS_RELEASE_ENV:TAMMY_MACOS_BUILD_NUMBER/],
  ]) {
    const result = await runTask(taskName, missingReleaseEnvironment);
    assert.notEqual(result.code, 0, `${taskName} must fail before its signing owner`);
    assert.match(
      `${result.stdout}${result.stderr}`,
      actualTarget === "darwin/arm64" ? expected : taskFailure,
      `${taskName} exposes its host-appropriate exact error`,
    );
    assert.doesNotMatch(
      `${result.stdout}${result.stderr}`,
      /desktop:make:mas/,
      `${taskName} must not start its signing owner`,
    );
  }
  const spoofedTarget = actualTarget === "darwin/arm64" ? "linux/x64" : "darwin/arm64";
  for (const [label, environment, arguments_] of [
    ["environment", { ...missingReleaseEnvironment, NODE_TARGET: spoofedTarget }, undefined],
    ["CLI", missingReleaseEnvironment, [`NODE_TARGET=${spoofedTarget}`]],
  ]) {
    const callerTargetOverride = await runTask("release:development", environment, arguments_);
    assert.notEqual(callerTargetOverride.code, 0, "release:development fails before signing");
    assert.match(
      `${callerTargetOverride.stdout}${callerTargetOverride.stderr}`,
      taskFailure,
      `${label} NODE_TARGET cannot change the production target guard`,
    );
  }
  const actualDevelopmentInstaller = await runTask("release:development", {
    TAMMY_MACOS_BUILD_NUMBER: "1",
    TAMMY_MACOS_EXPORT_COMPLIANCE: "exempt",
    TAMMY_MACOS_PROVISIONING_PROFILE: "/private/tmp/tammy.provisionprofile",
    TAMMY_MACOS_PRIVACY_POLICY_URL: "https://example.com/tammy/privacy",
    TAMMY_MACOS_SIGNING_IDENTITY: "Apple Development: Tammy Pty Ltd (ABCDE12345)",
    TAMMY_MACOS_SUPPORT_URL: "https://example.com/tammy/support",
    TAMMY_MACOS_TEAM_ID: "ABCDE12345",
    TAMMY_MACOS_INSTALLER_IDENTITY: "3rd Party Mac Developer Installer: Tammy Pty Ltd (ABCDE12345)",
  });
  assert.notEqual(
    actualDevelopmentInstaller.code,
    0,
    "installer rejection must stop before signing",
  );
  assert.match(
    `${actualDevelopmentInstaller.stdout}${actualDevelopmentInstaller.stderr}`,
    actualTarget === "darwin/arm64" ? /DEVELOPMENT_INSTALLER_IDENTITY_FORBIDDEN/ : taskFailure,
    "release:development exposes its host-appropriate installer or target error",
  );
  assert.match(
    releaseTasks.development.summary ?? "",
    /apps\/desktop\/out\/Tammy-mas-arm64\/Tammy\.app/,
  );
  assert.match(releaseTasks.development.summary ?? "", /no installer.*upload/i);
  assert.match(releaseTasks.candidate.summary ?? "", /JSON.*pkg.*SHA-256.*Gatekeeper/is);
  const releaseCommands = Object.values(releaseTasks).flatMap(shellCommands).join("\n");
  assert.doesNotMatch(releaseCommands, /upload|Transporter|xcrun|API/i);

  if (process.platform === "darwin") {
    const signingFixtureDirectory = await mkdtemp(
      path.join("/private/tmp", "tammy-taskfile-signing-mode-"),
    );
    try {
      const fixtureMise = path.join(signingFixtureDirectory, "mise");
      const fixturePnpm = path.join(signingFixtureDirectory, "pnpm");
      const fixtureOutput = path.join(signingFixtureDirectory, "mode.txt");
      const fixtureTaskfile = path.join(signingFixtureDirectory, "Taskfile.yml");
      await writeFile(
        fixtureMise,
        '#!/bin/sh\nif [ "$1" = "exec" ] && [ "$2" = "--" ]; then\n  shift 2\n  exec "$@"\nfi\nexit 2\n',
      );
      await writeFile(
        fixturePnpm,
        '#!/bin/sh\nprintf "%s" "$TAMMY_MACOS_SIGNING_MODE" > "$TAMMY_TASK_TEST_OUTPUT"\n',
      );
      await writeFile(
        fixtureTaskfile,
        "version: '3'\ntasks:\n  force-mode:\n    cmds:\n      - TAMMY_MACOS_SIGNING_MODE=development mise exec -- pnpm desktop:make:mas\n",
      );
      await Promise.all([chmod(fixtureMise, 0o755), chmod(fixturePnpm, 0o755)]);
      const fixture = await run(
        "mise",
        ["exec", "--", "task", "--taskfile", fixtureTaskfile, "force-mode"],
        {
          PATH: `${signingFixtureDirectory}:${process.env.PATH}`,
          TAMMY_MACOS_SIGNING_MODE: "distribution",
          TAMMY_TASK_TEST_OUTPUT: fixtureOutput,
        },
        { clearTaskEnvironment: true },
      );
      assert.equal(fixture.code, 0, "the isolated signing-mode fixture succeeds");
      assert.equal(
        await readFile(fixtureOutput, "utf8"),
        "development",
        "the inline development assignment overrides an inherited distribution mode",
      );
    } finally {
      await rm(signingFixtureDirectory, { force: true, recursive: true });
    }
  }

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

  for (const [taskName, { namespace, task }] of taskGraph) {
    for (const reference of taskReferences(task)) {
      assert.equal(
        taskGraph.has(resolveTaskReference(namespace, reference)),
        true,
        `${taskName} references missing task ${resolveTaskReference(namespace, reference)}`,
      );
    }
  }

  const ciTasks = (await readTaskfile("taskfiles/ci.yml")).tasks;
  const ciBaseCommands = [
    ciTaskVersionCommand,
    "pnpm check:toolchain",
    "node scripts/check-clean-tree.mjs",
  ];
  assert.deepEqual(Object.keys(ciTasks), [
    "contracts",
    "linux",
    "macos",
    "windows-smoke",
    "windows11",
  ]);
  assert.deepEqual(shellCommands(ciTasks.contracts), [
    ...ciBaseCommands,
    "pnpm ci:lint",
    "pnpm test:proto-breaking",
    "pnpm contracts",
    "pnpm proto:descriptors:verify",
  ]);
  assert.deepEqual(shellCommands(ciTasks.linux), [
    ...ciBaseCommands,
    "go test -race ./services/core/...",
    "command -v xvfb-run",
    "xvfb-run -a pnpm desktop:test",
    "pnpm desktop:typecheck",
    "pnpm lint",
  ]);
  assert.deepEqual(shellCommands(ciTasks.macos), [
    ...ciBaseCommands,
    "TAMMY_SQLCIPHER_REPRODUCIBILITY=1 pnpm sqlcipher:test",
    "go test -race -tags tammy_sqlcipher ./services/core/internal/storage/sqlcipher/... -count=1",
    "pnpm desktop:e2e",
  ]);
  assert.deepEqual(shellCommands(ciTasks["windows-smoke"]), [
    ...ciBaseCommands,
    'pwsh -NoProfile -NonInteractive -Command \'& { if ($env:EVIDENCE_CLASSIFICATION -ne "WINDOWS_SERVER_SMOKE_ONLY") { throw "WINDOWS_SERVER_EVIDENCE_CLASSIFICATION_INVALID" }; if ($env:PROCESSOR_ARCHITECTURE -ne "AMD64") { throw "WINDOWS_SERVER_SMOKE_ONLY_REQUIRES_AMD64" } }\'',
    "pnpm test:proto-breaking",
    "pnpm proto:descriptors:verify",
    "pnpm desktop:test",
    "pnpm desktop:typecheck",
  ]);
  const windows11Commands = shellCommands(ciTasks.windows11);
  assert.deepEqual(windows11Commands.slice(0, 3), ciBaseCommands);
  assert.equal(
    windows11Commands.length,
    4,
    "Windows 11 native handoff must stay in one task command",
  );
  assert.match(windows11Commands[3], /^pwsh -NoProfile -NonInteractive -Command '& \{/);
  for (const expression of [
    "pnpm sqlcipher:test",
    "pnpm sqlcipher:build",
    'Resolve-Path ".tmp/sqlcipher/ordinary/win32-x64/ordinary-sqlite3.exe"',
    "$env:TAMMY_ORDINARY_SQLITE3 = $probe",
    "mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/storage/sqlcipher/... ./services/core/internal/sbr ./services/core/internal/app ./services/core/cmd/tammy-core -count=1",
    "SBR_UNAVAILABLE_ON_TARGET",
    "pnpm desktop:e2e",
    'classification = "WINDOWS11_23H2_X64_RELEASE_GATE"',
    'status = "PASSED"',
    "run_id = $env:GITHUB_RUN_ID",
    "run_attempt = $env:GITHUB_RUN_ATTEMPT",
  ]) {
    assert.match(
      windows11Commands[3],
      new RegExp(expression.replaceAll(/[.*+?^${}()|[\]\\]/g, "\\$&")),
    );
  }
  assert.equal(ciTasks.windows11.status, undefined, "CI tasks must never be cached");
  const ciCommandsWithoutPinnedWindowsGo = Object.values(ciTasks)
    .flatMap(shellCommands)
    .join("\n")
    .replace(
      "mise exec -- go test -race -tags tammy_sqlcipher ./services/core/internal/storage/sqlcipher/... ./services/core/internal/sbr ./services/core/internal/app ./services/core/cmd/tammy-core -count=1",
      "go test",
    );
  assert.doesNotMatch(
    ciCommandsWithoutPinnedWindowsGo,
    /\bmise(?:\s|$)/,
    "CI tasks use Actions-provisioned tools directly",
  );

  const allCommands = [];
  for (const { namespace, task } of taskGraph.values()) {
    for (const command of shellCommands(task)) {
      if (namespace !== "ci") {
        allCommands.push(command);
        if (
          command !== "mise install" &&
          command !== "git diff --check" &&
          !command.startsWith("TAMMY_MACOS_SIGNING_MODE=")
        ) {
          assert.match(command, /^mise exec -- /, "local commands must use pinned mise execution");
        }
      }
    }
    if (namespace !== "ci") {
      for (const precondition of task.preconditions ?? []) {
        if (typeof precondition?.sh === "string") allCommands.push(precondition.sh);
      }
    }
    assert.equal(task.status, undefined, "Task caching is not allowed");
    assert.equal(task.sources, undefined, "Task caching is not allowed");
    assert.equal(task.generates, undefined, "Task caching is not allowed");
    assert.equal(task.deps, undefined, "Task dependencies are not allowed in this slice");
    assert.equal(task.platforms, undefined, "Task platforms must not silently skip scenarios");
  }
  for (const command of allCommands) assertAllowedShellCommand(command);
  assert.throws(
    () => assertAllowedShellCommand('mise exec -- node -e \'require("node:fs").rmSync(".tmp")\''),
    assert.AssertionError,
    "Node filesystem mutation must be rejected",
  );
  assert.throws(
    () => assertAllowedShellCommand("mise exec -- pnpm desktop:make:mas"),
    assert.AssertionError,
    "a signed macOS owner must always force its signing mode inline",
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
