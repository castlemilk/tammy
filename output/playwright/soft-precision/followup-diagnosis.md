# Follow-up: ASAR readiness, scrollbars and hosted CI

22 October 2026. This supersedes the earlier ASAR blocker and the initial scrollbar
finding interpretation. No original screenshot is replaced: all 41 PNG hashes still
match manifest.json. No concurrent checkout, cache history or financial behaviour changed.

## ASAR: pre-existing failure, scoped fix

The same real integration test was run at the original base
`aeda631070f9cb44289658c52128c595bf6133f7` in a temporary checkout with the unchanged locked
dependencies. It failed after 20,054ms. The final progress marker was exactly:

```
TAMMY_ASAR_HARNESS_START
TAMMY_ASAR_IMPORT_START
TAMMY_ASAR_IMPORT_DONE
TAMMY_ASAR_READY_WAIT
```

The fixture, ASAR test, production security source and descriptor-verification script
were byte-identical between that base and pushed styling SHA
`55d59ace3eed53c49b17bd59f344228e7147a5ed`. This is a pre-existing harness failure,
not a styling regression.

Two bounded probe batches separated process detachment, import ordering, sandbox
initialization and macOS activation. Moving the security import to top level or making
it synchronous did not resolve readiness. A macOS accessory activation policy reached
readiness and exited successfully in 931ms with sandboxing still enabled. The precise
WindowServer/AppKit internal mechanism is not claimed; the demonstrated failing point
is `app.whenReady()` under the headless fixture's ordinary activation mode.

The fix sets `app.setActivationPolicy("accessory")` at the macOS-only fixture entry.
This is an intentional background test application with no window or Dock presence.
It does not change the production application, sandbox, entitlements, protocol rules,
CSP or permissions. [Electron documents accessory activation](https://www.electronjs.org/docs/latest/api/app#appsetactivationpolicypolicy-macos)
as an application mode without a Dock icon or menu bar.

The real ASAR integration regression now asserts hidden Dock presence on macOS,
HTTP 200, exact archive content, content type and nosniff. It passed in 1.13s with
the original 20s harness bound unchanged. A subsequent full desktop run passed
**35 files / 394 tests**, using two workers and a bounded 10s ordinary-test timeout.
No new production dependencies or security permissions were added.

## Static audit: one real styling defect fixed, seven legacy choices remain

Runtime inspection disproved the initial "false positive" interpretation of the
scrollbar finding. The normal-mode computed `scrollbar-color` was `auto` on both the
root and a newly created overflow region. Inspection found that the two standards
properties had accidentally landed on `.border-border`, creating an opt-in baseline.

The declarations now live in the global `*` rule. The existing forced-colour system
fallback and WebKit thumb/track/hover/active rules remain. The packaged foundation
regression creates a new unclassed overflow region and verifies:

- root scrollbar colour is not the browser default in normal mode;
- a new region automatically receives the same scrollbar colours;
- forced-colour mode returns scrollbar colour to `auto`.

The assertions run inside the real package with CSP intact. CSSOM property assignment
avoids introducing prohibited inline style attributes. The updated package and both
packaged acceptance tests pass. These checks do not rely on the static parser.

The new strict audit has **7 findings, 0 unresolved owners**. See
[followup-static-ui-audit.json](followup-static-ui-audit.json). They are:

| Finding | Scope and decision |
| --- | --- |
| Six `form.novalidate-missing` findings | Existing chart, journals, banking, documents, setup and unlock forms retain native constraint validation plus domain/core checks. Actual runtime form `noValidate` is false and required controls remain required. Replacing these with a new validation system would change behaviour beyond this styling pass. |
| One `form.textarea-resize-missing` finding | The existing CSV textarea remains resizable so a user can expand long statement input. Removing that facility needs an equivalent expansion affordance. The shared max-width rule and responsive tests remain in force. |

These seven are deliberate, documented exceptions to the supplementary skill defaults;
they are not newly introduced styling regressions and are not a claim that strict audit
passes. The original 8-finding JSON remains historical evidence; the scrollbar finding
has been fixed rather than waived. The native form/CSV behaviour remains unchanged.

## Hosted CI at exact pushed styling SHA

[Run 39731599100](https://github.com/castlemilk/tammy/actions/runs/39731599100), associated
with exact SHA `55d59ace3eed53c49b17bd59f344228e7147a5ed`, completed as follows:

| Job | Result |
| --- | --- |
| macOS 14 arm64 packaged E2E, job 119667660644 | Passed |
| Contracts and local foundation, job 119667660844 | Failed in retained descriptor verification |
| Windows Server smoke only, job 119667660939 | Failed at the same descriptor verification |

Both failed jobs report `ERR_CHILD_PROCESS_STDIO_MAXBUFFER` for:

```
git diff --name-only 290815bebe4b13957dd33232d15ff9f08a9ce1c1 55d59ace3eed53c49b17bd59f344228e7147a5ed --
```

The descriptor manifest retains revision `290815be`. The script buffers the full
path list with Node's default 1,048,576-byte limit. Measured with streaming counts:

| Subject revision | Diff output bytes | Paths | Existing .pnpm-store paths |
| --- | ---: | ---: | ---: |
| Original base `aeda6310` | 3,600,564 | 24,074 | 23,293 |
| Styling SHA `55d59ace` | 3,604,553 | 24,145 | 23,293 |

Running the actual verifier function at the original base reproduced
`ERR_CHILD_PROCESS_STDIO_MAXBUFFER`, with stdout stopped at 1,048,576 bytes. This
independently confirms the CI failure predates this styling work. Raising the buffer
alone would not establish valid retained-descriptor provenance. The unrelated retained
revision/cache problem was left untouched; no history cleanup or cache content was added.
Repository-wide lint still has the separately reported existing formatting diagnostics.

## Final local verification and artifact provenance

- ASAR regression: passed; former base failure reproduced and recorded above.
- Full desktop suite: 394/394 passed, 46.65s, two workers.
- Desktop typecheck, scoped Biome and whitespace checks: passed.
- One renderer-only package rebuild: four signature/resource checks passed, with unchanged
  native core hash `a162de336a89b58f1575b6eb59d4ad88584495c5457205ff9b7d634ab67c55a9`.
- Both packaged acceptance tests: passed in 20.4s, including the new normal/forced-colour
  assertions, full synthetic workflows, offline probes, and core cleanup.
- Original 41 capture hashes: all preserved. No capture environment variable was supplied
  during this follow-up, so the regression run could not overwrite them.

The rebuilt renderer source is `e583c9c0cd958fc26dd26816e592cf0513ce58ba`.
Its app hash is `aead2ead5de9a5f0d904c057e137af423e28da84a0a1afce7fc12ae9120598ba`;
manifest hash is `e64c93dd3557b51f72c6a2522a2eab2febddbe4651f38f8c96bfc91c8b20560f`.
The following evidence commit also refines the test's synthetic DOM construction to
avoid inline style attributes; it does not change the renderer or production code.

A restricted read-only `ps` inventory was denied and was not retried. It did not block
diagnosis or verification. The Mac connection remained usable and all command outcomes
were checked before proceeding; no interrupted command was blindly repeated.

The PR remains a draft. No merge, release, deployment or unrelated CI/provenance rewrite
was performed. A new push receives its own hosted results; the completed statuses above
belong specifically to `55d59ace` and must not be reused as evidence for another SHA.
