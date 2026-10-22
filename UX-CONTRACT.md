# Tammy visual migration behaviour contract

This records the UI consequences of the existing contracts; README.md,
docs/development/tech-state.md, protobuf contracts and core authorization remain the
sources of financial and security policy. Scope: Soft Precision styling, not a workflow rewrite.

| Capability | Canonical owner | Source of truth | Allowed variants | Verification |
| --- | --- | --- | --- | --- |
| Navigation | AppShell, Navigation, app-shell/router.ts | Existing product/API contract | Existing protected local routes | app.test.tsx, packaged current-workflows.spec.ts |
| Controls | components/ui/Button, Card, Badge; styles.css | Existing product/API contract | Solid, outline, ghost; native labelled fields | Renderer tests and real screenshots |
| Select/Listbox | Existing native select fields | Existing product/API contract | Operating-system popup and keyboard model | Renderer/journal tests, packaged journey |
| Date | Existing input type=date | Existing product/API contract | Native locale/calendar; contract civil dates | Renderer and packaged workflow assertions |
| Form | Existing feature form and domain codec | Existing product/API contract | Native constraints retained; server errors inline | Unit tests, document invalid-total retry journey |
| Scrollbar | styles.css global baseline | Existing product/API contract | Horizontal table scroll, natural page/form scroll | Narrow viewport assertions/screenshots |
| CRUD | Existing named protobuf methods and feature handlers | Existing product/API contract | Explicit review/post/match/reconcile; disabled pending actions | Packaged tests and core invariants |
| Table Selection | Existing document selection and journal detail navigation | Existing product/API contract | Local selection; no bulk scope | Keyboard, detail/back journeys |
| Toast | No global toast system in current app | Existing product/API contract | Inline persistent status/error only | Renderer tests |

The data projections remain bounded by their existing API page sizes. This migration
adds neither unbounded fetching nor new sort/filter persistence claims. A separate
pagination design is needed before extending these projections to larger datasets.

| Operation | Pending | Existing product/API contract | Confirmed outcome | Failure/recovery |
| --- | --- | --- | --- |
| Create workspace | Submit disabled | Existing product/API contract | Recovery code shown once | Keep form, inline error |
| Recovery/sign-in/organisation | Continue disabled | Existing product/API contract | Verified workspace/session/organisation, overview | Existing core contracts and inline error |
| Reopen | Unlock disabled | Existing product/API contract | New signed-in session, overview | Masked credentials, inline error |
| Document import | Retaining disabled | Existing product/API contract | Source retained in local document list | File validation or retain error |
| Document review | Saving disabled | Existing product/API contract | Reviewed status and disabled Review saved | Preserve edits, correct supplier length and retry |
| Add account | Saving disabled | Existing product/API contract | Existing chart reload | Cancel without write; existing error path |
| Journal | Posting disabled | Existing product/API contract | Posted read-only detail | Cancel without write; detail Back returns to list |
| Statement | Import/match/reconcile disabled pending | Existing product/API contract | Explicit matched/reconciled state | Existing row/domain validation; no auto-match |
| GST workpaper | Existing pending state | Existing product/API contract | Draft — not lodged, retained sources | Existing version/tax-period capability gates |

Keep all existing security boundaries, sensitive-value handling and native validation.
Do not add unsupported Undo, automated posting, external sends or production ATO submission.
Evidence is in output/playwright/soft-precision; never commit runtime traces, userData,
private logs, recovery codes or credentials. Screenshots of recovery must mask the code.
