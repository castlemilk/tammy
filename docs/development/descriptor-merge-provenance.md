# Descriptor evidence through an ordinary PR merge

Retained descriptors name the clean source commit that produced them. The next
commit contains only the generated descriptor/manifest pair. Generating evidence
does not alter that source revision, and verification rebuilds the complete
descriptor set with the pinned Buf version before comparing its bytes.

An ordinary GitHub merge adds a commit with two parents. Previously, verification
rejected that merge even when its entire tree was identical to the reviewed
evidence commit. This prevented the normal PR merge path from producing a
verifiable master revision.

The verifier accepts that additional commit shape only when all of these hold:

- The subject has exactly two parents. Its second parent is an evidence commit
  with exactly one parent: the manifest's source revision.
- The source-to-evidence diff is nonempty and contains only
  `compliance/contracts/descriptors.pb` and/or
  `compliance/contracts/descriptor-manifest.json`. Rename detection and submodule
  ignoring are disabled for this check so they cannot hide changed source paths.
- The merge's first parent is an ancestor of the source revision. A stale or
  unrelated base requires updating the branch and generating new evidence.
- The merge tree, evidence tree and checked-out HEAD tree have identical Git
  tree IDs. Extra merged files, mode changes, gitlink changes and conflict
  resolutions cannot pass through a clean worktree alone.
- The merge checkout is fully clean, including the retained pair and checked-out
  submodules. Canonical manifest, descriptor hash/schema, pinned Buf version and
  fresh-build byte equality checks still apply.

This rule is not recursive: nested and octopus merges, reversed parents and
merge-parent evidence commits are rejected. Squash and rebase merges are not
newly accepted by this rule. Choose the ordinary merge-commit method after the
branch is up to date and its final commit is the immediate evidence-only child.

The source and direct evidence-child workflows retain their allowance for local
changes to the retained pair while generating/verifying evidence. A direct child
must have exactly one parent. CI's existing explicit PR-head subject remains
supported when its checkout is GitHub's synthetic merge. The new HEAD-tree binding
applies specifically when the subject being verified is itself a merge; this
change does not claim a general binding of every subject override to HEAD.

Use the normal commands from a clean source commit:

```sh
TAMMY_SOURCE_REVISION=<source-commit> pnpm proto:descriptors:evidence
pnpm proto:descriptors:verify
# Commit only the generated retained pair, then verify again.
pnpm proto:descriptors:verify
```

`scripts/verify-descriptor-evidence.test.mjs` exercises real Git histories and a
fixture Buf executable for acceptance and adversarial ancestry/tree/configuration
cases. Canonical Buf verification on a real candidate remains a separate check.
Local passes do not qualify queued CI runners, authorize publication, or replace
the release gates in `AGENTS.md`.
