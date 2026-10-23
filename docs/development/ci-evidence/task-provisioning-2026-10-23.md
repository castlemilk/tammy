# Separate macOS Task provisioning failure

This note concerns head `0a852811603ecf8c9c4e62c42cd23da012962adf`.
It is separate from the earlier [30-minute outer cancellation during SBR](macos-budget-2026-10-23.md).

## Original step evidence

- [PR run 39990974107, macOS job 120542322455](https://github.com/castlemilk/tammy/actions/runs/39990974107/job/120542322455)
  passed on attempt 1: 20 packaged cases in 17.5 minutes, SBR in 5.2 minutes,
  total job 26m52s. Its authenticated comparison source was
  `13cf72ea5acb0926eba0c93d6e7c106b840f8210`.
- [Push run 39990965028, original macOS job 120542291563](https://github.com/castlemilk/tammy/actions/runs/39990965028/job/120542291563)
  started at 16:56:44 UTC. **Install exact Task** started at 16:57:54 and was
  still the active step when GitHub cancelled the job at 17:41:44. Exact-version
  verification and all canonical development/package/application checks remained
  pending. No SBR or application test executed in this job. Terminal step logs
  were unavailable (404 BlobNotFound), so the underlying host/network cause is
  unproven. Earlier progress wording that inferred final-case execution from
  the job status was corrected after inspecting individual steps.
- A cancellation request reached an already completed run and made no change.
  After confirming terminal status, exactly one targeted macOS job retry was
  requested; the successful Ubuntu/Windows executions were retained, not rerun.
  [Attempt 2, job 120565185844](https://github.com/castlemilk/tammy/actions/runs/39990965028/job/120565185844)
  passed: 20 packaged cases in 16.2 minutes, SBR in 5.0 minutes, total job
  25m30s. Playwright retries remained zero. This is explicitly a provisioning
  retry, not a first-attempt all-green result.

These results apply only to that head and comparison source. They establish
successful ordinary packaged execution after provisioning; they do not prove
the original agent was healthy or that a different installer would repair an
unresponsive agent. Queue time, provisioning, application readiness and actual
test execution are distinct.

## Bounded acquisition correction

Previously this macOS step used `go install` without its own deadline, sharing
the entire acceptance envelope. Go compilation and its dependency graph are
unnecessary for acquiring the exact Task executable. The macOS step now uses
the [official Task 3.52.0 release](https://github.com/go-task/task/releases/tag/v3.52.0)
archive `task_darwin_arm64.tar.gz`, following the upstream
[binary installation option](https://taskfile.dev/docs/installation).

The archive's SHA256 is pinned to
`debfa9a3bc06fcb5d5550b44725ecd22bb305499ce057bd6d7a774db5cdaa7b8`.
It matches both release asset metadata and the official `task_checksums.txt`
asset (whose SHA256 is
`b51b3541b052339fd1bfbd7b2008a546a56f257e0a6808aaca66dfd7d69d5ac6`).
No token, new third-party action, mutable install script or tool-cache override
is used. GitHub's [reserved runner variables](https://docs.github.com/en/actions/reference/workflows-and-actions/variables)
are not overridden.

Acquisition gets a **five-minute step limit**, with a **10-second connection
limit**, **120-second transfer limit**, HTTPS-only redirects and zero download
retries. A fresh private directory avoids a cached-binary checksum bypass.
The checksum must pass before extracting only `task`; the result must be a
regular nonsymlink file and report exactly `3.52.0` before its directory is
published to PATH. Failure removes only that owned temporary directory and
leaves PATH unchanged. The following independent exact-version check remains.
The five-minute limit is deliberately separate from application readiness:
the real archive validation took 1.5 seconds locally, and prior successful
provisioning completed within the recorded setup interval. It is a bounded
network allowance, not an extra application timeout.

The measured 40-minute outer envelope, unchanged SBR 480-second deadline,
other per-scenario limits, worker count, zero Playwright retries, assertions,
artifact retention, command owners and permissions are preserved. Ubuntu and
Windows provisioning remain unchanged; Go remains available for Core builds.
No application, financial, authentication or release-policy change is included.

## Validation and limits

The new acquisition regression fails on the original workflow's absent step
limit. The corrected workflow passes **62 focused foundation/Task/workflow
tests** in 6.81 seconds. The executable failure-path regression uses the real
Unix checksum utility and verifies both a corrupt download and a transfer
error stop before extraction, retain the original PATH file and remove the
owned temporary directory. Windows runs the structural checks and explicitly
skips this macOS-only shell execution test.

The exact workflow installer was also executed once on local Apple silicon
against the official release, in an isolated temporary directory: checksum,
extraction, version and PATH publication passed in 1.5 seconds. This validates
tool acquisition only; it is not an application rebuild or a native suite.
Pinned actionlint 1.7.12 and repository lint pass; lint retains its existing
135 warnings and 12 infos. All 666 prior tracked output PNGs and 55 archived
files verify byte-identical. Renderer, Core, packaged fixtures and build-owner
scripts remain unchanged. Fresh hosted results for the corrected head are
recorded in draft PR #11. No merge,
ready, signing, upload or release operation is included.
