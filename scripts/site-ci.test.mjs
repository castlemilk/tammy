import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import test from "node:test";

const workflow = readFileSync(
  new URL("../.github/workflows/tammy-site.yml", import.meta.url),
  "utf8",
);

// Inspect the workflow itself without dependencies: CI runs these contracts before
// installing packages. Indentation boundaries deliberately fail closed if its
// structure changes; this is not a general YAML parser.
function job(name) {
  const match = new RegExp(
    `^  ${name}:\\n([\\s\\S]*?)(?=^  [a-z][a-z0-9-]*:|$(?![\\s\\S]))`,
    "m",
  ).exec(workflow.slice(workflow.indexOf("\njobs:\n") + 7));
  assert.ok(match, `Missing job ${name}`);
  return match[1];
}

function steps(body) {
  return [...body.matchAll(/^ {6}- name: ([^\n]+)\n([\s\S]*?)(?=^ {6}- name:|$(?![\s\S]))/gm)].map(
    ([text, name]) => ({ name, text }),
  );
}

function expression(value) {
  assert.match(value, /^[\s\w.='"!&|()/-]+$/, "Review newly introduced expression syntax");
  // The restricted GitHub boolean expressions used here have identical JS
  // semantics for the boolean inputs and exact string contexts in these cases.
  return new Function("github", "inputs", `return Boolean(${value});`);
}

function jobGate(body) {
  const match = /^ {4}if: >-\n\s+\$\{\{([\s\S]*?)\}\}/m.exec(body);
  assert.ok(match, "Deployment requires an explicit job-level condition");
  return expression(match[1].trim());
}

const verifyJob = job("verify-site");
const deployJob = job("deploy-cloudflare");
const verifySteps = steps(verifyJob);
const deploySteps = steps(deployJob);

function named(items, name) {
  const matches = items.filter((step) => step.name === name);
  assert.equal(matches.length, 1, `Expected exactly one ${name} step`);
  return matches[0].text;
}

test("Cloudflare publication is restricted to trusted master push or explicit master dispatch", () => {
  const allowed = jobGate(deployJob);
  for (const repository of ["castlemilk/tammy", "someone/tammy"])
    for (const ref of ["refs/heads/master", "refs/heads/feature", "refs/pull/48/merge"])
      for (const event_name of ["push", "pull_request", "workflow_dispatch", "workflow_run"])
        for (const deploy_cloudflare of [true, false, undefined]) {
          const expected =
            repository === "castlemilk/tammy" &&
            ref === "refs/heads/master" &&
            (event_name === "push" ||
              (event_name === "workflow_dispatch" && deploy_cloudflare === true));
          assert.equal(
            allowed({ repository, ref, event_name }, { deploy_cloudflare }),
            expected,
            JSON.stringify({ repository, ref, event_name, deploy_cloudflare }),
          );
        }
  assert.match(workflow, /deploy_cloudflare:\n(?: {8}[^\n]*\n)* {8}type: boolean\n/);
  assert.match(workflow, /deploy_cloudflare:\n(?: {8}[^\n]*\n)* {8}default: false\n/);
  assert.match(deployJob, /^ {4}needs: verify-site$/m);
});

test("both jobs require the isolated Cuttlefish identity and fork builds remain blocked", () => {
  for (const body of [verifyJob, deployJob]) {
    const labels = /^ {4}runs-on: \[([^\]]+)\]$/m
      .exec(body)?.[1]
      .split(",")
      .map((s) => s.trim());
    assert.deepEqual(labels, ["self-hosted", "linux", "ARM64", "tammy-linux-ci"]);
    assert.match(body, /test "\$\{RUNNER_OS\}" = Linux/);
    assert.match(body, /test "\$\{RUNNER_ARCH\}" = ARM64/);
    assert.match(body, /test "\$\(uname -m\)" = aarch64/);
    assert.match(body, /cf-vm-\*\) ;;/);
    assert.match(body, /RUNNER_NOT_CUTTLEFISH[^\n]*exit 1/);
  }
  const guard = /^ {4}if: \$\{\{([^\n]+)\}\}$/m.exec(verifyJob);
  assert.ok(guard);
  const allowed = expression(guard[1]);
  for (const head of ["castlemilk/tammy", "someone/tammy"])
    assert.equal(
      allowed(
        {
          event_name: "pull_request",
          repository: "castlemilk/tammy",
          event: { pull_request: { head: { repo: { full_name: head } } } },
        },
        {},
      ),
      head === "castlemilk/tammy",
    );
});

test("publication credentials are isolated to the deployment command", () => {
  const deploy = named(deploySteps, "Deploy the direct Worker with existing Cloudflare access");
  assert.match(deploy, /CLOUDFLARE_API_TOKEN: \$\{\{ secrets\.CLOUDFLARE_API_TOKEN \}\}/);
  assert.match(deploy, /CLOUDFLARE_ACCOUNT_ID: \$\{\{ vars\.CLOUDFLARE_ACCOUNT_ID \}\}/);
  assert.match(deploy, /run: node scripts\/site-release\.mjs cloudflare-deploy --output /);
  assert.doesNotMatch(
    workflow.replace(deploy, ""),
    /\bsecrets\.|\bCLOUDFLARE_API_TOKEN\b|\bCLOUDFLARE_ACCOUNT_ID\b/,
  );
  assert.doesNotMatch(
    workflow,
    /^\s*environment:/m,
    "Do not implicitly create a deployment environment",
  );
  assert.match(workflow, /^permissions:\n {2}contents: read\n/m);
  assert.doesNotMatch(workflow, /^\s*\w[\w-]*: write$/m);
});

test("master publication cannot be cancelled by newer workflow runs", () => {
  const top = workflow.slice(0, workflow.indexOf("\njobs:\n"));
  const match = /cancel-in-progress: \$\{\{([^\n]+)\}\}/.exec(top);
  assert.ok(match);
  const cancel = expression(match[1]);
  assert.equal(cancel({ ref: "refs/heads/master" }, {}), false);
  assert.equal(cancel({ ref: "refs/heads/feature" }, {}), true);
  assert.match(
    deployJob,
    /concurrency:\n {6}group: tammy-cloudflare-tammy-site-direct\n {6}cancel-in-progress: false\n/,
  );
});

test("deployment consumes this run's separately prepared direct artifact without rebuilding", () => {
  const download = named(deploySteps, "Download the verified direct artifact from this run");
  assert.match(download, /name: tammy-cloudflare-preparation-\$\{\{ github\.sha \}\}/);
  assert.doesNotMatch(download, /^\s+(github-token|repository|run-id):/m);
  const directUpload = named(verifySteps, "Retain the direct Cloudflare preparation artifact");
  assert.match(directUpload, /name: tammy-cloudflare-preparation-\$\{\{ github\.sha \}\}/);
  assert.match(verifyJob, /prepare --target cloudflare --output /);
  assert.match(verifyJob, /cloudflare-dry-run --output /);
  assert.doesNotMatch(deployJob, /\bpnpm build\b|site-release\.mjs (prepare|build|package)\b/);
  assert.match(
    named(deploySteps, "Install dependencies without deployment credentials"),
    /pnpm install --frozen-lockfile --filter sites-project\.\.\./,
  );
  const publishIndex = deploySteps.findIndex(
    (step) => step.name === "Deploy the direct Worker with existing Cloudflare access",
  );
  const verifyIndex = deploySteps.findIndex(
    (step) => step.name === "Recheck the revision and build before publication",
  );
  assert.ok(verifyIndex >= 0 && verifyIndex < publishIndex);
  assert.match(deploySteps[verifyIndex].text, /GITHUB_SHA/);
  assert.match(deploySteps[verifyIndex].text, /site-release\.mjs verify/);
});

test("actions are immutable pins and deployment evidence excludes raw logs", () => {
  const uses = [...workflow.matchAll(/^\s+uses: (.+)$/gm)].map((match) => match[1]);
  assert.ok(uses.length > 0);
  for (const action of uses) assert.match(action, /^[\w-]+\/[\w-]+@[a-f0-9]{40}$/);
  const uploads = deploySteps.filter((step) => /uses: actions\/upload-artifact@/.test(step.text));
  assert.equal(uploads.length, 1);
  assert.match(
    uploads[0].text,
    /path: \$\{\{ runner\.temp \}\}\/tammy-cloudflare-deployment\/deployment-record\.json\n/,
  );
  assert.doesNotMatch(uploads[0].text, /path:.*(?:\*|wrangler-output|logs)/);
  assert.match(uploads[0].text, /always\(\)/);
});

test("the actual artifact verification script rejects mismatched or tampered inputs", () => {
  const step = named(deploySteps, "Verify the downloaded revision and source archive");
  const match = /node --input-type=module <<'NODE'\n([\s\S]*?)\n {10}NODE/.exec(step);
  assert.ok(match, "Review changed artifact verification implementation");
  const script = match[1].replace(/^ {10}/gm, "");
  const root = mkdtempSync(path.join(tmpdir(), "tammy-ci-contract-"));
  const artifact = path.join(root, "tammy-cloudflare-artifact");
  mkdirSync(artifact);
  const sha = "a".repeat(40);
  const github = { repository: "castlemilk/tammy", commit_sha: sha };
  const archive = Buffer.from("test source archive bytes");
  const source = { target: "cloudflare", github, source_sha256: "source-digest" };
  const build = { ...source, build_sha256: "build-digest" };
  const release = {
    ...build,
    source_archive: {
      path: "source.tar.gz",
      sha256: createHash("sha256").update(archive).digest("hex"),
    },
  };
  const write = () => {
    for (const [name, value] of [
      ["source-provenance.json", source],
      ["build-provenance.json", build],
      ["release-manifest.json", release],
    ])
      writeFileSync(path.join(artifact, name), JSON.stringify(value));
    writeFileSync(path.join(artifact, "source.tar.gz"), archive);
  };
  const run = () =>
    execFileSync(process.execPath, ["--input-type=module", "-e", script], {
      env: { RUNNER_TEMP: root, GITHUB_SHA: sha },
      stdio: "pipe",
    });
  try {
    write();
    assert.doesNotThrow(run);
    source.github = { ...github, commit_sha: "b".repeat(40) };
    write();
    assert.throws(run, "Reject an artifact from a different revision");
    source.github = github;
    source.target = "sites";
    write();
    assert.throws(run, "Reject the separate Sites artifact");
    source.target = "cloudflare";
    write();
    writeFileSync(path.join(artifact, "source.tar.gz"), "tampered");
    assert.throws(run, "Reject source archive tampering before extraction");
    build.source_sha256 = "different";
    write();
    assert.throws(run, "Reject a build associated with another source");
    build.source_sha256 = source.source_sha256;
    build.build_sha256 = "different";
    write();
    assert.throws(run, "Reject a build inconsistent with the release manifest");
    build.build_sha256 = release.build_sha256;
    release.source_archive.path = "../unexpected.tar.gz";
    write();
    assert.throws(run, "Reject an unexpected source archive path");
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
