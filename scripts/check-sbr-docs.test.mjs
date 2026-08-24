import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import YAML from "yaml";

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

async function read(relativePath) {
  return readFile(path.join(repositoryRoot, relativePath), "utf8");
}

async function collectTaskNames(relativePath = "Taskfile.yml", namespace = "", names = new Set()) {
  const taskfile = YAML.parse(await read(relativePath));
  for (const taskName of Object.keys(taskfile.tasks ?? {})) {
    names.add([namespace, taskName].filter(Boolean).join(":"));
  }
  for (const [includeName, include] of Object.entries(taskfile.includes ?? {})) {
    const configuration = typeof include === "string" ? { taskfile: include } : include;
    const includePath = path.posix.normalize(
      path.posix.join(path.posix.dirname(relativePath), configuration.taskfile),
    );
    const includeNamespace = configuration.flatten
      ? namespace
      : [namespace, includeName].filter(Boolean).join(":");
    await collectTaskNames(includePath, includeNamespace, names);
  }
  return names;
}

function shellBlocks(markdown) {
  return [...markdown.matchAll(/```(?:sh|shell|bash)\n([\s\S]*?)```/g)].map((match) => match[1]);
}

test("SBR operator docs use exact existing Task scenarios", async () => {
  const [readme, foundation, guide, taskNames] = await Promise.all([
    read("README.md"),
    read("docs/development/foundation.md"),
    read("docs/development/sbr-local-readiness.md"),
    collectTaskNames(),
  ]);
  const documents = [readme, foundation, guide];
  const required = [
    "setup",
    "dev:accounting:fresh",
    "dev:sbr:simulator",
    "sbr:doctor",
    "sbr:registration:check",
    "test:sbr",
    "package:e2e",
    "dev:sbr:evte",
    "evidence:sbr",
  ];

  for (const taskName of required) {
    assert.equal(taskNames.has(taskName), true, `documented Task scenario must exist: ${taskName}`);
    assert.match(guide, new RegExp(`mise exec -- task ${taskName.replaceAll(":", "\\:")}\\b`));
  }
  assert.match(readme, /docs\/development\/sbr-local-readiness\.md/);
  assert.match(foundation, /sbr-local-readiness\.md/);

  for (const document of documents) {
    for (const match of document.matchAll(/mise exec -- task ([a-z][a-z0-9:-]*)/g)) {
      assert.equal(
        taskNames.has(match[1]),
        true,
        `documentation references unknown task: ${match[1]}`,
      );
    }
    assert.doesNotMatch(document, /\brtk\b/, "operator documentation must not use agent commands");
  }
});

test("every documented runnable SBR scenario has a real owner", async () => {
  const taskfile = YAML.parse(await read("taskfiles/sbr.yml"));
  const expectedCommands = new Map([
    ["launch-simulator", ["mise exec -- node scripts/launch-local-scenario.mjs sbr-simulator"]],
    ["run-doctor", ["mise exec -- node scripts/check-sbr-registration.mjs --doctor-preflight", "mise exec -- node scripts/launch-local-scenario.mjs sbr-doctor"]],
    ["run-registration-check", ["mise exec -- node scripts/check-sbr-registration.mjs"]],
    ["run-evidence", ["mise exec -- node scripts/write-sbr-evidence.mjs"]],
  ]);

  for (const [taskName, commands] of expectedCommands) {
    assert.deepEqual(
      taskfile.tasks?.[taskName]?.cmds,
      commands,
      `${taskName} must use its real owner`,
    );
  }
  const focusedTests = taskfile.tasks?.["run-test"]?.cmds ?? [];
  assert.ok(focusedTests.length >= 3, "test:sbr must run the helper, core, and desktop owners");
  assert.equal(
    focusedTests.some((command) => String(command).includes("sbr-incomplete.mjs")),
    false,
  );
  assert.deepEqual(taskfile.tasks?.["launch-evte"]?.cmds, [
    "mise exec -- node scripts/check-sbr-registration.mjs",
    "mise exec -- node scripts/launch-local-scenario.mjs sbr-evte",
  ]);
});

test("SBR guide states the credential lifecycle and security boundary", async () => {
  const guide = await read("docs/development/sbr-local-readiness.md");
  for (const phrase of [
    "import, inspect, unlock-for-use, replace, and remove",
    "native file chooser",
    "password and fresh TOTP",
    "only in Tammy",
    "Continue commits the import",
    "machine-credential state and displayed fingerprint",
    "no pre-import preview or separate accept step",
    "issuer, serial, creation and expiry dates, component version, and fingerprint",
    "core validates the binding",
    "never leaves this Mac",
    "not included in workspace backups",
    "Task accepts no live credential",
    "signed readiness passes",
  ]) {
    assert.match(guide, new RegExp(phrase, "i"), `SBR guide must retain: ${phrase}`);
  }
  for (const location of [
    "repository",
    "terminal",
    "environment variables",
    "command arguments",
    "logs",
    "evidence",
    "cloud storage",
  ]) {
    assert.match(
      guide,
      new RegExp(`never[^.]+${location}`, "i"),
      `SBR guide prohibits ${location}`,
    );
  }
  assert.doesNotMatch(guide, /verify[^.]+(?:ABN|expiry)[^.]+before (?:accept|import)/i);
});

test("SBR guide owns the complete external registration handoff", async () => {
  const guide = await read("docs/development/sbr-local-readiness.md");
  assert.match(guide, /exact fixed installed signed-input locations/i);
  assert.match(guide, /exits non-zero[^.]+EVTE_SIGNED_INPUTS_REQUIRED/i);
  assert.match(guide, /validates? installed signed inputs/i);
  for (const item of [
    "DSP registration",
    "product registration",
    "OSF assessment",
    "ATO-approved credential component licence",
    "component target",
    "EVTE access",
    "signed endpoint profile",
    "service enrolment",
    "conformance",
    "independent review",
    "expiry and revalidation",
    "evidence export",
  ]) {
    assert.match(guide, new RegExp(item, "i"), `registration checklist must include ${item}`);
  }
  assert.match(
    guide,
    /dev:sbr:evte[^.]+remains blocked[^.]+externally issued, signed inputs[^.]+installed/i,
  );
  assert.match(
    guide,
    /actual component integration[^.]+next separately approved plan[^.]+after issuance/i,
  );
});

test("current-state docs prohibit production SBR and BAS actions", async () => {
  const [readme, foundation, guide, technicalState] = await Promise.all([
    read("README.md"),
    read("docs/development/foundation.md"),
    read("docs/development/sbr-local-readiness.md"),
    read("docs/development/tech-state.md"),
  ]);
  for (const [name, document] of [
    ["README", readme],
    ["foundation", foundation],
    ["SBR guide", guide],
    ["technical state", technicalState],
  ]) {
    assert.match(document, /no production SBR path/i, `${name} must state the production boundary`);
    assert.match(
      document,
      /no BAS (?:submit or lodge|submission or lodgement) action/i,
      `${name} must state the BAS boundary`,
    );
  }

  for (const block of shellBlocks(guide)) {
    assert.doesNotMatch(block, /\b(?:credential|password|secret|token|product[_ -]?id)\s*=/i);
    assert.doesNotMatch(block, /--(?:credential|password|secret|token|product-id)\b/i);
    assert.doesNotMatch(block, /\.(?:p12|pfx|pem)\b/i);
    assert.doesNotMatch(block, /\b(?:submit|lodge|production)\b/i);
  }
  assert.doesNotMatch(guide, /-----BEGIN (?:PRIVATE KEY|CERTIFICATE)-----/);
});
