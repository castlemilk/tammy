# Combined implementation and archival lint verification

22 October 2026. PR #5 remains a separate cleanup proposal from current default
branch `master` at `aeda631070f9cb44289658c52128c595bf6133f7`. Its final cleanup
commit is `5c628371522bf522cf466eda9590054d70f9d85a`.

Fresh remote reads confirmed that cleanup head and implementation head
`23109ae89ac50f18789d538aade640412f7cdfc6` were unchanged. Both owned checkouts were
clean. Existing reporting-worktree edits were preserved outside these branches.

A normal non-force merge produced
`a12ae0dcd88b39c5187b9d8da3544c503d15a169`, with both heads as parents. It merged
without conflicts. Neither pull request was merged into master, and the separate
cleanup branch was not rewritten or changed by this integration.

The nine archival-policy regressions now also run in the existing Ubuntu contracts
and Windows Server smoke Task scenarios, alongside descriptor/build/setup tests.
This adds no job, environment, security permission or timeout relaxation. Full
source lint retains recommended Biome rules; only the exact historical inventory
receives byte-preservation checks and matching raw-byte Git checkout attributes.

Local combined checks passed:

- 109/109 descriptor, setup, archival, Task and foundation-evidence tests (12.84s).
- Full repository lint, including all 55 archived byte hashes.
- Desktop TypeScript check and whitespace checks.

Canonical descriptors are generated from the next clean combined source commit,
then committed only as its immediate evidence-only child. Full pinned Buf coverage
and byte/hash verification remain mandatory. The final pushed SHA and its own
terminal hosted CI results are recorded in PR #4; earlier-head results are not
reused as passing evidence for the combined head.

All original 41 app captures and 55 archival output files remain byte-identical.
No producer or historical capture was rerun, no cache content or history was
removed, and no financial/security behaviour changed. The previously documented
seven supplementary UI-audit exceptions remain. Windows Server smoke does not
establish the separate Windows 11 release gate.
