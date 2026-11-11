# Tammy CI on the Cuttlefish GitHub runner pool

**Status: this GitHub Actions compatibility pool remains unqualified as observed on 10 November 2026.** The routing proposal is in [PR #46](https://github.com/castlemilk/tammy/pull/46). The [first readiness run](https://github.com/castlemilk/tammy/actions/runs/42076973768) queued without a runner. The [master website run at `fa5ade8a`](https://github.com/castlemilk/tammy/actions/runs/42777581689) also remains queued with no assigned runner. Master already routes the website to this pool; foundation and roadmap routing remain proposed here. The web application has its own hosted validation workflow, which this proposal does not migrate.

[PR #54](https://github.com/castlemilk/tammy/pull/54) proposes a separate native Cuttlefish workflow and has historical native-run evidence. That evidence does not qualify the GitHub Actions compatibility pool described here. Reconcile the two proposals before selecting a cutover; do not merge their different scheduling approaches indiscriminately.

## Chosen execution architecture

Use the maintained **Cuttlefish ephemeral GitHub Actions runner pool**, not a shared Docker-socket runner.

Source: private `skunkworq/cuttlefish`, `docs/runbooks/gha-runner-pool.md`, `scripts/macos/rig/rig-gha-runner-pool.sh`.

- Each job runs in a short-lived, isolated Linux ARM64 Micropod VM with a private Docker daemon and an ephemeral GitHub Actions JIT registration. A VM is destroyed after the job.
- A repo-unique lane label prevents a runner pool from accidentally accepting generic jobs belonging to other runner groups.
- The pool uses Cuttlefish's VM and pressure controls and its own one-job-per-VM cache lifecycle. Do not replace this with a generic `runs-on: self-hosted`.
- The original Ubuntu/Node/Go/pnpm/Task test commands remain pinned. Native platform tests remain separate.

**Exact Linux routing labels** (matching the pool install command):

```yaml
runs-on: [self-hosted, linux, ARM64, tammy-linux-ci]
```

**Expected GitHub runner identity:** `RUNNER_NAME` begins with `cf-vm-`; `RUNNER_OS=Linux`; `RUNNER_ARCH=ARM64`; `uname -m=aarch64`. The source code enforces this identity in every portable CI job and the standalone readiness check.

## One-time pool installation on Gamma or another authorised Cuttlefish rig

The following must run **on the rig**, with Cuttlefish's current Micropod environment, vmnet guard, GitHub administration credentials and the checked-out private Cuttlefish source available. It cannot be performed by editing GitHub workflows.

```sh
cd ~/projects/cuttlefish

# Ensure the maintained binary includes the ephemeral GitHub runner pool.
go build -o "$HOME/.cuttlefish/bin/cuttle-gha-pool" ./cmd/cuttle

# Create a persistent launchd pool for the Tammy repository; two VM slots.
scripts/macos/rig/rig-gha-runner-pool.sh install castlemilk/tammy \
  self-hosted,linux,ARM64,tammy-linux-ci 2

# Confirm launchd pool state, registered runners and actual job execution.
scripts/macos/rig/rig-gha-runner-pool.sh status castlemilk/tammy
gh api repos/castlemilk/tammy/actions/runners \
  --jq '.runners[] | {name, status, busy, labels: [.labels[].name]}'
tail "$HOME/.cuttlefish/logs/gha-runner-pool-castlemilk_tammy.log"
```

The pool's best credential is a fine-grained GitHub token restricted to **`castlemilk/tammy`** with **Administration: read/write** and **Actions: read**, stored in a mode-0600 local file and configured via `CUTTLE_POOL_TOKEN_FILE` for installation. The installed service reads the token file; never paste it into an issue, repository or workflow. If unavailable, the pool can use the rig owner's appropriately authorised `gh` login.

**Do not disable the vmnet guard to force a job through.** If pool pressure blocks VM admission or the GitHub API rejects registration, diagnose the local runner logs and credentials. The pool's controls are intentional; do not restart other users' Docker or Micropod workloads.

## CI migration policy

Portable Linux jobs proposed for Cuttlefish in PR #46:

| Workflow | Job | Expected Cuttlefish route |
| --- | --- | --- |
| `.github/workflows/foundation-ci.yml` | `contracts` | Go race tests, local desktop tests, Node/Buf contract verification |
| `.github/workflows/tammy-site.yml` | `verify-site` | TypeScript, lint, PDF and site tests, editorial/SEO checks, build |
| `.github/workflows/xero-roadmap-ci.yml` | `validate` | Roadmap evidence and backlog validation |
| `.github/workflows/cuttlefish-runner-readiness.yml` | `verify-cuttlefish` | Prove runner identity, Docker/Xvfb availability and repository |

These Linux jobs have **no GitHub-hosted fallback**. A queued run means no matching runner is accepting work; it is not a successful Cuttlefish run. The jobs reject pull requests from forks to protect the runner and its cached main-branch artefacts. Source checkouts use read-only GitHub tokens and no persisted credentials.

Native platform checks retain the current master configuration:
- `macos14-arm64-packaged`: hosted `macos-15` ARM64, displayed as macOS 15 packaged E2E. Its legacy job/artifact identifiers remain for evidence compatibility; they do not establish macOS 14 qualification. The master Dock/display-height fix is retained.
- `windows-server-x64-package-smoke`: Windows Server 2025 (not Windows 11 qualification).
- `foundation-windows11-e2e.yml`: separately enabled Windows 11 23H2 native self-hosted release gate.

**A Linux Micropod runner cannot replace those native OS checks.** Cuttlefish-managed native runners require separate physical/virtual OS qualification and actual registration.

## Prove the change before merging

1. Start/verify the repo-scoped pool on the rig; check registered names and labels.
2. Confirm the [Cuttlefish-only readiness workflow](https://github.com/castlemilk/tammy/actions/workflows/cuttlefish-runner-readiness.yml) **completes**, rather than just queues, and logs `RUNNER_VERIFIED_ON_CUTTLEFISH` on a `cf-vm-*` runner.
3. On the PR's exact head, obtain passing portable Linux CI for the foundation, website and roadmap. Verify the `runner_name` in GitHub job detail and Cuttlefish pool journal for each.
4. Confirm the native macOS 15 foundation and Windows Server smoke checks pass on the exact head. Full signed/notarized distribution and Windows 11 qualification remain separate release gates; foundation passes do not satisfy them.
5. Merge PR #46 only after those checks pass and confirm the next `master` jobs also report Cuttlefish runner names. Keep the original workflow semantics and timeouts unless real evidence justifies a change.

**Pending blocker:** GitHub's Chat connector can edit/check workflows and read run statuses, but it does not expose runner registration or a remote shell on the Cuttlefish rig. Starting the pool requires an authorised machine-side operation. Until this happens, PR #46 remains **draft** and must not reroute the remaining foundation or roadmap jobs. The already-routed website stays blocked pending its own actual pool execution. Track this in [issue #47](https://github.com/castlemilk/tammy/issues/47).
