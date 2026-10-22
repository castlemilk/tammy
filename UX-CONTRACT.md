# Tammy visual migration behaviour contract

This records the UI consequences of the existing contracts; README.md,
docs/development/tech-state.md, protobuf contracts and core authorization remain the
sources of financial and security policy. Scope: Soft Precision styling, not a workflow rewrite.

| Capability | Canonical owner | Allowed variants | Verification |
| --- | --- | --- | --- |
| Navigation | AppShell, Navigation, app-shell/router.ts | Existing protected local routes | app.test.tsx, packaged current-workflows.spec.ts |
| Controls | components/ui/Button, Card, Badge; styles.css | Solid, outline, ghost; native labelled fields | Renderer tests and real screenshots |
| Select/Listbox | Existing native select fields | Operating-system popup and keyboard model | Renderer/journal tests, packaged journey |
| Date | Existing input type=date | Native locale/calendar; contract civil dates | Renderer and packaged workflow assertions |
| Form | Existing feature form and domain codec | Native constraints retained; server errors inline | Unit tests, document invalid-total retry journey |
| Scrollbar | styles.css global baseline | Horizontal table scroll, natural page/form scroll | Narrow viewport assertions/screenshots |
| CRUD | Existing named protobuf methods and feature handlers | Explicit review/post/match/reconcile; disabled pending actions | Packaged tests and core invariants |
| Table Selection | Existing document selection and journal detail navigation | Local selection; no bulk scope | Keyboard, detail/back journeys |
| Toast | No global toast system in current app | Inline persistent status/error only | Renderer tests |

The data projections remain bounded by their existing API page sizes. This migration
adds neither unbounded fetching nor new sort/filter persistence claims. A separate
pagination design is needed before extending these projections to larger datasets.

| Operation | Pending | Confirmed outcome | Failure/recovery |
| --- | --- | --- | --- |
| Create workspace | Submit disabled | Recovery code shown once | Keep form, inline error |
| Recovery/sign-in/organisation | Continue disabled | Verified workspace/session/organisation, overview | Existing core contracts and inline error |
| Reopen | Unlock disabled | New signed-in session, overview | Masked credentials, inline error |
| Document import | Retaining disabled | Source retained in local document list | File validation or retain error |
| Document review | Saving disabled | Reviewed status and disabled Review saved | Preserve edits, correct totals and retry |
| Add account | Saving disabled | Existing chart reload | Cancel without write; existing error path |
| Journal | Posting disabled | Posted read-only detail | Cancel without write; detail Back returns to list |
| Statement | Import/match/reconcile disabled pending | Explicit matched/reconciled state | Existing row/domain validation; no auto-match |
| GST workpaper | Existing pending state | Draft — not lodged, retained sources | Existing version/tax-period capability gates |

Keep all existing security boundaries, sensitive-value handling and native validation.
Do not add unsupported Undo, automated posting, external sends or production ATO submission.
Evidence is in output/playwright/soft-precision; never commit runtime traces, userData,
private logs, recovery codes or credentials. Screenshots of recovery must mask the code.
