# macOS CI envelope diagnosis

The original [push run 39984087065](https://github.com/castlemilk/tammy/actions/runs/39984087065/job/120518623043)
and [comparison run 39984093577](https://github.com/castlemilk/tammy/actions/runs/39984093577/job/120518646518)
both ran on their first attempt. Their commits (`2464afa6` and GitHub merge
`9551cfa4`) have the identical Git tree
`3acc85f2109576154a69e856611254ebac995de3`. This rules out a source-tree
difference between the two runs. Original timestamps are retained in the JSON
beside this note; no raw process logs, credentials or authentication material
are retained here.

| Phase | Successful push | Cancelled comparison |
| --- | ---: | ---: |
| Queue before job start | 7s | 3s |
| Runner/dependency/Task provisioning | 2m23s | 2m35s |
| Toolchain, script tests, SQLCipher reproducibility and Go race test | 2m09s | 2m45s |
| Development build and acceptance | 1m52s | 2m39s |
| Ordinary package, verification and test preparation | 2m49s | 3m34s |
| First 19 packaged cases | 11m27s | 14m22s |
| Final SBR project, including its fixture | 5m04s, passed | 4m21s observed, then outer cancellation |

Queue delay is excluded from GitHub's job execution budget. The comparison's
ordinary test interval was approximately 25% slower; individual case durations
were broadly slower too. The available evidence supports runner throughput
variation, not an isolated observed SBR hang. Physical host contention was not
instrumented, so it is not claimed as a proven cause. No application assertion
failed before cancellation. The interrupted SBR project's finer internal phase
is unavailable because the outer cancellation skipped artifact retention.

SBR deliberately obtains fresh 30-second TOTP counters across credential and
simulator operations. Those waits qualify anti-replay behavior. It already has
an eight-minute test deadline. The packaged global default is two minutes;
existing scenario overrides, including the fifteen-minute BAS and seven-minute
pagination cases, remain unchanged. The successful push completed all 20 cases,
including SBR. The
comparison completed all 19 ordinary cases, including the Documents keyboard
correction, but had only about 4m20s left when its final SBR project began.
That is less than even the successful run's observed duration.

There are two Core/helper builds: development acceptance, then independently
authenticated ordinary packaging. `scripts/build-core.mjs` deliberately uses
a fresh private cache including the standard library. This change preserves
that isolation and both supported entry-path checks; it does not introduce
artifact reuse, shared-cache authority, fewer builds or skipped verification.

## Scoped correction

The defect is an outer job envelope that can expire before a supported test
gets its existing deadline. The slow comparison reached its final project
25m54s after job start. Adding its unchanged eight-minute limit and a two-minute
failure-evidence reserve requires **35m54s**. Rounding that measured requirement
to a finite five-minute envelope gives **40 minutes** for the macOS job only.

The regression fails against the original 30-minute workflow. It reads the
recorded slow-run timestamps and actual SBR test deadline, requires the test
and artifact reserve to fit, and caps the envelope at 40 minutes. Existing
workflow tests continue to pin the original command ownership, all test
projects, zero retries, one worker, stop-on-first-failure behavior, permissions,
artifact handling, signing boundary and other platforms' budgets. The macOS
eight-minute inner limit is explicitly asserted unchanged. A real SBR hang
will therefore fail that test and can retain evidence before the outer job
expires. No SBR result or release qualification is inferred from the budget
calculation itself.

No application, security, assertion, SBR fixture, helper timeout, per-test
timeout, worker count or CI job count changes. No local full rebuild or manual
workflow retry is needed: targeted workflow regressions and lint validate the
edit locally; a normal scoped push uses the existing CI path for fresh native
evidence. The original cancelled/failed runs and every screenshot remain
unchanged. Draft, merge and publication holds remain in force.

Local validation: the budget regression fails on the original workflow and
passes after the correction. All 60 targeted foundation/workflow/Task tests
pass (7.22s), as do pinned actionlint 1.7.12 and repository lint (135 existing
warnings, 12 infos). The first actionlint invocation stopped before execution
because sandbox DNS could not reach the Go module proxy; the network-enabled
invocation passed. All 666 prior tracked PNGs and 55 archived files were verified
byte-identical. Renderer, Core, packaged tests/fixtures and build-owner scripts
are unchanged. Fresh hosted outcomes are recorded against the exact pushed
revision in draft PR #11, without relabeling the original evidence above.
