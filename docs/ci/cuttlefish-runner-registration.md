# Tammy → Cuttlefish CI runner registration

**Status (6 November 2026):** Cuttlefish Linux runner capability confirmed in the owner's `skunkworq/cuttlefish` repository, but an **online runner registered to `castlemilk/tammy` is not yet verified**. A runner registered to `castlemilk/tautau` or any other repository **cannot** accept Tammy jobs.

## Exact runtime target

The maintained rig launcher `skunkworq/cuttlefish/scripts/macos/rig/rig-linux-runner.sh` provisions repository-scoped Ubuntu 24.04 Linux/ARM64 GitHub Actions containers on a Mac rig. Its entrypoint defaults to the `cuttlefish,linux,container` labels, alongside GitHub's default `self-hosted`, `Linux`, `ARM64` labels. Tammy's health job requires:

```yaml
runs-on: [self-hosted, Linux, ARM64, cuttlefish, container]
```

A Linux Cuttlefish runner **is not** a native macOS 14 ARM64 or Windows Server/Windows 11 host. Preserve those platform-specific release evidence jobs until equivalently qualified Cuttlefish-managed native runners are independently verified.

## Register the Tammy runner on an authorised rig

Use a maintained Cuttlefish rig that has the `gh` CLI authenticated with permission to create GitHub repository Actions runner registration tokens and is already configured with Docker:

```sh
cd ~/projects/cuttlefish
scripts/macos/rig/rig-linux-runner.sh start castlemilk/tammy
gh api repos/castlemilk/tammy/actions/runners \
  --jq '.runners[] | {name, status, busy, labels: [.labels[].name]}'
```

Do not print registration tokens or supply them to CI job inputs. The launcher creates a repo-specific Docker container, work/cache volumes and GitHub runner registration, and owns deregistration/restart. Use its watchdog only through the rig's normal service management, and never restart Docker or prune other repositories' runners mid-job.

If registration fails, inspect GitHub user permissions and the rig's Docker/runner state before making any workflow routing change. Cuttlefish's existing `castlemilk/tautau/docs/cuttlefish-ci.md` describes the same deployment pattern and ARM64/Docker constraints.

## Prove execution, not just configuration

1. The `Tammy Cuttlefish runner readiness` workflow is intentionally runner-exclusive. It requires `RUNNER_NAME` beginning `cuttlefish-linux-`, `Linux/ARM64`, `aarch64`, the `castlemilk/tammy` repository and Docker/Xvfb access.
2. Trigger the health workflow (or push its configured branch), open the completed job and inspect its runner name.
3. Only after this check succeeds should the Linux jobs in `foundation-ci.yml`, `tammy-site.yml` and `xero-roadmap-ci.yml` switch from GitHub-hosted Ubuntu to those exact runner labels.
4. Confirm full marketing-site typecheck/lint/tests/build and Linux Go race/desktop scenarios on the real rig. Keep native macOS/Windows workflows as separate release gates.
5. Do not allow untrusted fork pull-request code to execute on this repo-scoped runner while the rig mounts the shared Docker daemon. Restrict self-hosted pull-request jobs to branches in this repository, with read-only token permissions.

A pending job on a `self-hosted` label proves only that GitHub is waiting for a matching runner. **It is not proof the job ran on Cuttlefish.** Do not silently fall back to GitHub-hosted runners.

## Pending migration branch

[Runner probe #42076973768](https://github.com/castlemilk/tammy/actions/runs/42076973768) was **queued without an assigned runner** at the time of preparation. The repository-scoped `castlemilk/tammy` runner still needs to be registered and verified; neither a queued job nor labels alone proves execution.

The proposed workflow changes are staged in a draft PR: the existing Linux jobs for the desktop foundation, Tammy marketing site, and roadmap validation require exactly `[self-hosted, Linux, ARM64, cuttlefish, container]`, with no GitHub-hosted fallback and an explicit runner-name identity check. For security, the jobs refuse fork-originated PRs, because the Cuttlefish Linux runner has access to its rig's Docker daemon.

**Do not merge while the smoke job is queued.** Native macOS 14 ARM64 packaged E2E, Windows Server 2025 smoke, and the separately gated Windows 11 release workflow are intentionally unchanged. A Linux ARM64 container cannot supply evidence for those release platforms.

The pinned Node 24.18.0, Go 1.26.4, pnpm 11.15.0, Task 3.52.0 and existing test commands remain unchanged. After runner registration, the smoke and each Linux CI job must pass on the actual `cuttlefish-linux-*` runner before this routing becomes the main-branch configuration.
