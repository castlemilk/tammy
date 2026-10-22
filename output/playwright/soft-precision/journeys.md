# Soft Precision: real packaged journeys

Follow-up: [ASAR diagnosis, global-scrollbar correction and current CI evidence](followup-diagnosis.md). The original 41 capture files are preserved.

Further follow-up: [descriptor provenance correction and large-output regressions](descriptor-verification-followup.md).

Latest follow-up: [Windows dependency setup, runtime budgets and lint ownership inventory](windows-dependency-followup.md).

Combined checkpoint: [normal branch integration and combined verification](combined-verification.md).

Captured on the user's Mac, 22 October 2026, using the repository-owned Electron
Playwright fixture with a fresh isolated synthetic userData directory. These are real
rendered app screenshots, not generated concept boards. All retained records, users,
source files and amounts in this run are test fixtures. Recovery code pixels are masked.
No runtime traces, videos, userData, credentials, recovery secrets or private logs are
included here. The paper loop in the UI is an approved concept asset, not test evidence.

## Sources and isolation

- Repository: castlemilk/tammy; authenticated owner castlemilk, Git identity Ben Ebsworth.
- Original checkout: clean master at `aeda631070f9cb44289658c52128c595bf6133f7`.
- Active reporting work remains in its existing issue worktree. No other checkout was edited.
- Work lives in a separate checkout and `ui/soft-precision-journeys-2026-10-22` branch.
- No AGENTS.md was found in the checkout/ancestors. Personal frontend-design and
  design-taste-frontend guidance was read; the application behaviour contract takes
  precedence over marketing-only defaults. Shared renderer contracts and tests were inspected.
- Approved pack: `19533020422b6ea2f0c77dc3117a051483f9f9ce`; README, artistic guidance,
  feature coverage and actual pixels from boards 3, 8, 9 and 11 reviewed.
- The unrelated tracked `.pnpm-store` has no diff. Dependency cache went to a temporary
  directory. No history rewrite, deployment, release, credentials or permissions were added.

## Same journey and viewport

The first ten captures use the same synthetic records at 1440 × 1000 CSS pixels.
Workspace captures may be taller because full-page capture retains all form controls.
The before renderer is base `aeda6310`; final after renderer is `660774a1`.

| Before | After |
| --- | --- |
| [Documents](before/04-documents.png) | [Documents](after/04-documents.png) |
| [Banking](before/05-banking.png) | [Banking](after/05-banking.png) |
| [Chart](before/06-chart.png) | [Chart](after/06-chart.png) |
| [Trial balance](before/08-trial-balance.png) | [Trial balance](after/08-trial-balance.png) |
| [GST draft](before/09-gst-draft.png) | [GST draft](after/09-gst-draft.png) |

## Milestones, actions and observed results

| Captures | Actions and verified outcome |
| --- | --- |
| 01–03 | Create synthetic workspace and Australian organisation using actual required fields; mask the one-time recovery code; confirm; restart package; unlock and sign in; verify the same workspace with a new session. |
| 04–10 | Seed bounded test accounts, a balanced $319 journal, a signed statement line, reviewed document and draft GST workpaper through real named protobuf methods. Verify persisted versions, totals, idempotency and revisions in the existing test; navigate each real screen. No mock financial response replaces the core. |
| 11 | Select an unsupported synthetic text file. The app rejects it with visible file-type/size guidance. |
| 12 | Import a synthetic PNG; manually fill source details; submit a supplier name over the actual 256-character contract limit. The error appears and fields remain intact. |
| 13 | Correct the supplier name and double-click Save review. A single reviewed record is visible with $100 subtotal, $10 GST and $110 total; Review saved remains disabled. |
| 20–21 | Submit invalid CSV, observe guidance, correct it and import a -$110 statement line with an intentionally large synthetic opening balance. Completion remains disabled while the line is unmatched. |
| 22–23 | Explicitly match the line, then double-click completion. Reconciled states persist and completion disables; closing balance is $123,456,679.01. No automatic matching, feed or posting is implied. |
| 14 | Open Add account, type a draft, then Cancel. No account is written. Native select remains platform-owned. |
| 15–16 | Open an existing posted journal, verify both sides at $319, return to the list; open a new journal then Cancel. No extra journal is posted. |
| 17 | View consolidated local activity, then exercise browser Back and Forward. This product view does not claim audit-chain verification. |
| 18 | Use keyboard Tab from Overview to Documents. Focus is on the real link with a solid visible outline. |
| 19-* | Enable reduced motion; visit Documents, Banking and Trial balance at 320, 390 and 900px. Assert no page-level horizontal overflow; financial tables scroll within their surface. Verify zero navigation transition duration. |

The 320px document capture initially exposed cramped amount controls. The final renderer
stacks those fields and the final PNG was re-inspected. Dense tables remain sans/tabular;
serif headings and artwork stay outside the ledger data. The screenshots were inspected
for readable values, source context, status, focus and control geometry.

## Verification

Commands run with pinned Node 24.18.0 and pnpm 11.15.0 via mise/corepack.

| Check | Result |
| --- | --- |
| `pnpm desktop:typecheck` | Passed after final source changes. |
| `pnpm exec biome check apps/desktop/src/renderer apps/desktop/tests/e2e/current-workflows.spec.ts` | Passed, 46 files. |
| `git diff --check` | Passed. |
| `pnpm --dir apps/desktop exec vitest run src/renderer --maxWorkers=2 --testTimeout=10000` | 17 files, 36 tests passed. One earlier default-5s run timed out on a typing-heavy setup test while packaging ran; bounded workers and a 10s timeout resolved it. |
| `pnpm desktop:test` | 393/394 passed. The remaining ASAR Electron harness timed out waiting for app readiness at 20s; isolated rerun reproduced it. Renderer, preload, main security unit tests and core interoperability otherwise passed. |
| Ordinary local Electron package | Built; 4/4 signature/resource checks passed; source/package manifest and SQLCipher resource authentication passed. Ad-hoc development package only. |
| Final packaged `pnpm --dir apps/desktop e2e --output /tmp/tammy-soft-precision-final-run-1` | 2/2 passed in 14.7s: full current workflow plus offline first-run/security/privacy/back/focus checks. No console/page errors; fixture verified core shutdown without an orphan. |
| `pnpm lint` | Failed with 861 existing repository-wide formatting diagnostics, including retained JSON evidence under output/tammy-income-bas-exports-2026-10-06*. Those unrelated files were left untouched. |
| Supplementary strict static UI audit | Initial 8 findings are recorded in static-ui-audit.json. Six concern existing native form validation; one concerns textarea resizing; one was a real global-scrollbar defect, corrected in the follow-up. The initial false-positive interpretation of that scrollbar finding was wrong. Native validation and CSV editing behaviour are deliberately preserved in this visual-only migration. This is not a strict-audit compliance claim. |

The baseline packaged workflow passed in 8.9s. Final native core hash is unchanged:
`a162de336a89b58f1575b6eb59d4ad88584495c5457205ff9b7d634ab67c55a9`.
Final captured renderer package uses source `660774a198b3baaa06d631a7c4036fa7f884eea6`,
app hash `fd72d91b1dc5a16c7b4b1eb2f86181b868db716b189fd0bbc749f7f2dc47f81b`,
manifest hash `a5d64ec8d58a2fe94d1fea9ebd933e7a729535b8a358d9907aa9dc1bb06f467a`.
The original 41 captures retain this renderer provenance. The follow-up separately verifies a global-scrollbar correction; it does not overwrite these capture files.

Use a fresh output directory for each isolated packaged run. Reusing the earlier directory
produced a fail-closed workspace-create error consistent with retained security anchors;
a fresh directory resolved it. Security/reset behaviour was not changed. A dirty-source
package was also correctly rejected before committing the styling milestone.

## Limits and follow-up

This pass styles the connected workspace/recovery/sign-in/organisation, overview,
documents, explicit CSV banking, chart, journals, trial balance, draft GST and local
activity surfaces. Invoices and other planned suite workflows remain separate. No live
feeds, stock tracking, gateways, automatic posting or ATO submission were introduced.
The existing candidate contract does not reject subtotal/total mismatches; this styling
pass does not invent a financial validation rule. The retry fixture tests an actual rule.

No release, Windows, screen-reader or production-readiness claim is made. Hosted CI is a
separate result from the local checks above. See the draft PR for hosted status.

## Library delivery

Confirmed persistent Library files, also retained as repository PNGs:

- Before Documents: `libfile_df2cd86e02b48191bcc7e0950f6c1f57` (version 0).
- Final after Documents: `libfile_6e9aa066151c81918773a626a89f7063` (version 1).
- Final GST draft: `libfile_e27b0767e3bc81919b70d129a88d595e` (version 1).

See manifest.json for dimensions and SHA-256 hashes of every capture. Library metadata
is in library-delivery.json; no runtime logs are required to review this evidence.
