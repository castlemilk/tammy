import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import {
  LOCAL_API_READ_RESOURCES,
  LOCAL_API_READ_TOOLS,
  type LocalApiReadTarget,
} from "./catalog.ts";

interface ConveyorManifest {
  readonly [module: string]: Readonly<Record<string, "command" | "query">>;
}

const manifest = JSON.parse(
  readFileSync(
    new URL("../../../apps/desktop/src/shared/conveyor-manifest.json", import.meta.url),
    "utf8",
  ),
) as ConveyorManifest;

const expectedTools = [
  "tammy.accounting.get_trial_balance",
  "tammy.accounting.list_accounts",
  "tammy.accounting.list_journals",
  "tammy.banking.get_summary",
  "tammy.documents.list_metadata",
  "tammy.identity.get_current_user",
  "tammy.organisation.get_current",
  "tammy.reporting.get_attention_summary",
  "tammy.reporting.get_capability",
  "tammy.reporting.get_current_bas_draft",
  "tammy.sbr.get_readiness",
  "tammy.system.get_diagnostics",
] as const;

const expectedResources = [
  "tammy://current/accounts",
  "tammy://current/journals",
  "tammy://current/organisation",
  "tammy://current/reporting/attention",
  "tammy://current/reporting/bas-draft",
  "tammy://current/sbr/readiness",
  "tammy://current/trial-balance",
  "tammy://current/user",
  "tammy://system/diagnostics",
] as const;

function targets(): readonly LocalApiReadTarget[] {
  return [
    ...LOCAL_API_READ_TOOLS.map(({ target }) => target),
    ...LOCAL_API_READ_RESOURCES.map(({ target }) => target),
  ];
}

test("publishes a stable read-only tool and resource catalog", () => {
  assert.deepEqual(LOCAL_API_READ_TOOLS.map(({ name }) => name).sort(), [...expectedTools]);
  assert.deepEqual(LOCAL_API_READ_RESOURCES.map(({ uri }) => uri).sort(), [...expectedResources]);
  assert.equal(Object.isFrozen(LOCAL_API_READ_TOOLS), true);
  assert.equal(Object.isFrozen(LOCAL_API_READ_TOOLS[0]), true);
  assert.equal(Object.isFrozen(LOCAL_API_READ_RESOURCES), true);
  assert.equal(Object.isFrozen(LOCAL_API_READ_RESOURCES[0]), true);
});

test("routes every public read to a currently connected Conveyor query", () => {
  for (const target of targets()) {
    assert.equal(
      manifest[target.module]?.[target.procedure],
      "query",
      `${target.module}.${target.procedure} is not a connected query`,
    );
  }
});

test("does not publish commands, credential workflows, document bodies, or unfinished services", () => {
  const publishedTargets = new Set(
    targets().map(({ module, procedure }) => `${module}.${procedure}`),
  );
  const publishedNames = JSON.stringify({
    resources: LOCAL_API_READ_RESOURCES,
    tools: LOCAL_API_READ_TOOLS,
  });

  for (const [module, procedures] of Object.entries(manifest)) {
    for (const [procedure, kind] of Object.entries(procedures)) {
      if (kind === "command") assert.equal(publishedTargets.has(`${module}.${procedure}`), false);
    }
  }
  for (const excluded of ["documents.getDocument", "sbr.getMachineCredentialStatus"]) {
    assert.equal(publishedTargets.has(excluded), false);
  }
  for (const excluded of [
    "call_rpc",
    "companyTax",
    "companyReturnSubmission",
    "financialClose",
    "importMachineCredential",
    "unlockWorkspace",
  ]) {
    assert.equal(publishedNames.includes(excluded), false);
  }
});
