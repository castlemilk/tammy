# Descriptor provenance follow-up

22 October 2026. This follows [the original diagnosis](followup-diagnosis.md).
The UI captures and their manifest remain unchanged.

## Cause and scope

Ubuntu and Windows Server CI at `cc4e4373d46f3238d130e35cd94ca9a631bcb05a`
failed while comparing the retained descriptor revision `290815be` with the PR
subject. The historical comparison emits about 3.6 MB of paths, including 23,293
already tracked `.pnpm-store` paths. Node's buffered Git command stops at 1 MiB.
The same verifier failure was independently reproduced at the original app base
`aeda631070f9cb44289658c52128c595bf6133f7`.

That overflow also masks a real provenance mismatch: the retained source is neither
the subject nor its immediate parent with an evidence-only change. Increasing a
buffer or excluding cache paths would not make the old evidence valid.

The correction preserves the complete Git comparison and the existing policy:

- Diff and status output is consumed incrementally. Memory retained for a pending
  line is bounded to 1,048,576 characters; stderr is bounded to 32,768 characters
  with an explicit truncation marker. Output is drained and command completion is
  checked. Git failures retain their exit code, command and available diagnostics.
- Every path is checked. No cache, generated file, protobuf or directory filter is
  added. Source changes and dirty non-evidence paths still fail closed.
- A different subject must remain the immediate evidence-only child of the source.
  Canonical manifest, revision, pinned Buf version, descriptor validation, hash and
  exact fresh-build byte comparison remain mandatory.
- The stale retained evidence is refreshed using the existing complete pinned Buf
  build from the clean source commit. Its final evidence-only child records that
  source revision. No hand-edited provenance or narrowed descriptor input is used.

No cache files are removed, untracked or added. No history is rewritten. This does
not change financial behaviour, production permissions or the renderer.

## Regression evidence

The real-Git fixture creates 12,000 long cache-like paths with Git tree objects,
without checking out 12,000 files. Both its diff and dirty status exceed 1 MiB.
Against the original base verifier, both focused regressions failed with
`ERR_CHILD_PROCESS_STDIO_MAXBUFFER` (0/2 passed). Against the corrected verifier,
they reach the intended provenance and dirty-tree policy rejections.

Additional coverage preserves a real failing Git command's diagnostics and rejects
an internally consistent retained descriptor subset when the complete fresh build
contains another service. Existing valid evidence-child, canonical JSON, stale
bytes, revision, dirty-tree, build-lock and atomic-publication tests remain.

Local checkpoint: **89/89 tests passed** across descriptor verifier, descriptor
builder, Task contract and foundation-evidence tests (27.8s). Scoped Biome and
`git diff --check` passed. The new descriptor regressions are also part of both
the Ubuntu contracts and Windows Server smoke Task scenarios, immediately before
the real non-mutating descriptor verification. No new CI platform or release
permission is introduced.

Hosted results must be attributed to the final pushed evidence-child SHA. The PR
records those terminal results separately; the earlier macOS success and Ubuntu /
Windows failure are historical, not results for the corrected commit. Windows
Server smoke evidence does not establish the separate Windows 11 release gate.

The seven documented supplementary static UI audit exceptions remain unchanged:
six native-validation forms and one user-resizable CSV textarea. The global
scrollbar defect has already been corrected and verified in the real package.
