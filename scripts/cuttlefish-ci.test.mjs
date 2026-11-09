import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";
import YAML from "yaml";

test("Cuttlefish CI checks out the event revision before running the canonical Linux scenarios", async () => {
  const workflow = YAML.parse(
    await readFile(new URL("../workflows/tammy-ci.yaml", import.meta.url), "utf8"),
  );
  assert.equal(workflow.apiVersion, "cuttlefish.dev/v1alpha1");
  assert.equal(workflow.kind, "Workflow");
  assert.equal(workflow.metadata.name, "tammy-ci");
  const checkout = workflow.spec.nodes.find((node) => node.id === "checkout");
  assert.deepEqual(checkout.taskRef, { name: "pipeline/checkout", version: "0.6.1" });
  assert.equal(checkout.inputs.repo.value, "https://github.com/castlemilk/tammy");
  assert.equal(checkout.inputs.ref.value, "master");
  const prepare = workflow.spec.nodes.find((node) => node.id === "prepare");
  assert.match(prepare.script, /node scripts\/prepare-cuttlefish-checkout\.mjs/);
  const linux = workflow.spec.nodes.find((node) => node.id === "foundation-linux");
  assert.equal(linux.run.shell, "bash");
  assert.match(linux.script, /scripts\/cuttlefish-linux-ci\.sh/);
  assert.ok(
    workflow.spec.edges.some((edge) => edge.from.node === "checkout" && edge.to.node === "prepare"),
  );
  assert.ok(
    workflow.spec.edges.some(
      (edge) => edge.from.node === "prepare" && edge.to.node === "foundation-linux",
    ),
  );
  const site = workflow.spec.nodes.find((node) => node.id === "site-verify");
  assert.equal(site.run.shell, "bash");
  assert.match(site.script, /scripts\/cuttlefish-site-ci\.sh/);
  assert.ok(
    workflow.spec.edges.some(
      (edge) => edge.from.node === "prepare" && edge.to.node === "site-verify",
    ),
  );
  assert.ok(workflow.spec.triggers.some((trigger) => trigger.kind === "manual"));
  assert.ok(
    workflow.spec.triggers.some(
      (trigger) =>
        trigger.kind === "github" &&
        trigger.repositories.includes("castlemilk/tammy") &&
        trigger.events.includes("push") &&
        trigger.events.includes("pull_request"),
    ),
  );
});

test("automatic foundation CI no longer schedules unsupported Windows jobs", async () => {
  const foundation = YAML.parse(
    await readFile(new URL("../.github/workflows/foundation-ci.yml", import.meta.url), "utf8"),
  );
  assert.equal(foundation.jobs["windows-server-x64-package-smoke"], undefined);
  const windows11 = YAML.parse(
    await readFile(
      new URL("../.github/workflows/foundation-windows11-e2e.yml", import.meta.url),
      "utf8",
    ),
  );
  assert.deepEqual(Object.keys(windows11.on), ["workflow_dispatch"]);
});

test("the unprivileged Linux job overrides Cuttlefish's root-owned pnpm store", async () => {
  const script = await readFile(new URL("./cuttlefish-linux-ci.sh", import.meta.url), "utf8");
  assert.match(
    script,
    /runuser -u node -- env[\s\S]*pnpm_config_store_dir="\$user_pnpm_store"[\s\S]*npm_config_store_dir="\$user_pnpm_store"[\s\S]*pnpm install --frozen-lockfile/,
  );
  assert.match(script, /user_pnpm_store="\/home\/node\//);
});
