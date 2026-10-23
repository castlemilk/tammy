# CI correction after the list-fidelity checkpoint

The original exact-head [PR run](https://github.com/castlemilk/tammy/actions/runs/39931866854)
and [push run](https://github.com/castlemilk/tammy/actions/runs/39931861959) at
1a9dcb50eab42c2d57ea4423210186bfb17aa23f failed. Neither run was retried.

- Ubuntu: 1,291 desktop cases passed, two App cases failed.
- Windows Server: 1,290 desktop cases passed, one platform case skipped and the
  same two App cases failed. This remains Server smoke, not Windows 11 evidence.
- macOS 14: script, SQLCipher, development and package checks passed. Packaged
  execution stopped at case four: BAS lifecycle waited for Open while the cached
  invoice detail was still active. Three packaged cases passed; 16 did not run.

The two App cases assumed that an editor opened immediately on first route entry.
They now explicitly choose New invoice before typing. Assertions still require
unsaved edits to survive a temporary Documents lookup, hidden fields to be inert,
no implicit save, and ephemeral values to be absent from session storage. The
session-replacement case additionally verifies that the old editor node is detached
and no customer field is accessible before entering the new blank draft.

The receipt helper now chooses Back to invoices before selecting another saved
record if a cached detail is active. It does not discard unsaved changes, dismiss a
confirmation, or bypass pending actions. One later template-reopen interaction uses
the same explicit Back step. Application navigation and financial code are unchanged.
The UX contract clarifies the existing cached-return behavior rather than changing it.

Local baselines reproduce both failures: the App file is 21 passed/two failed, and
the exact BAS case fails at the same Open locator after 36.9 seconds. Original
runtime artifacts remain private. With the corrected steps, all 23 App cases pass;
the complete native BAS lifecycle passes in 3.5 minutes, retaining receipt, blocked
PAYGI resolution, frozen handoff, linked revision and immutable-export assertions.
The affected invoice/purchase/BAS journey passes in 21.6 seconds.

A complete desktop run passes **1,293/1,293 in 109 files**, zero skips, in 125.18
seconds. TypeScript and lint pass with the existing 135 warnings/two infos. The
last evidence edit had also re-expanded one JSON array after its earlier format
pass. Only that owned metadata file is deterministically formatted; values and
all screenshot bytes are unchanged. No exclusion or test timeout is modified.

The verified ordinary application is unchanged from clean package source 08802fca.
No new package build or visual acceptance is claimed by this test-only correction.
Fresh hosted CI must be assessed at its new exact pushed head before starting the
invoice detail/payment implementation. The SBR diagnostic and release boundaries
remain held. Original failures, first-attempt images and all prior captures remain.
