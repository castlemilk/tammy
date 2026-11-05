# Tammy funnel instrumentation: no-capture foundation

Status: implemented schema/attempt logic only. Issue [#33](https://github.com/castlemilk/tammy/issues/33). The public site currently ships **no analytics transport or consent manager**, and this PR does not initiate traffic tracking.

## Event architecture

`lib/growth/events.ts` exports a fixed allowlist and a fail-closed recorder. A caller can inject a testing publisher, but **no default transport, cookie, localStorage identifier, analytics SDK, session replay or external request is present**.

Consent states are `allowed`, `denied` and `unknown`. Unknown/denied always prevent emission. An allowed state **alone is insufficient**: an explicit approved publisher callback must also exist. This is intentional while privacy notice, retention, provider, user controls and legal basis remain undecided.

Event fields are exactly: `version`, `event`, `tool_id`, `page_key`, `outcome`, `consent_state`. Values come from fixed enums, not URLs or input values. Additional keys cause validation failure, including invoice number, customer name, ABN, financial amount, full referrer, error message, PDF bytes, file name and raw query text.

## Attempt semantics

- `begin(tool)` marks a meaningful start once per explicit in-memory attempt after consent and publisher confirmation.
- `complete(tool)` marks a genuinely successful usable result once; cannot occur without `begin`.
- `exportStarted` requires a begun attempt. `exportCompleted` requires an in-progress export. `exportFailed` records a typed failure without leaking an error string.
- `resetAttempt` is an explicit new task boundary; no persistent/cross-device identity.
- This is a schema helper, **not** user instrumentation deployed to any production sink. Before connecting it, test the actual invoice renderer and document how to confirm export completion in the browser.

## Before enabling real collection

1. Select the lawful/consented measurement model, provider, retention, operator access and privacy notice for the Australian site.
2. Implement a visible consent UI where needed and a working revocation/opt-out, with tests for denied, unknown, changed and network-off states.
3. Add a transport that accepts only the validated event payload, with no automatic form/URL/referrer capture. Validate server side as well.
4. Verify sensitive customer inputs never appear in analytics, logs, exception telemetry or SDK storage; use realistic synthetic data in network tests.
5. Document dashboard denominators, consent coverage, deduplication and the difference between tool completion and qualified paid interest.
6. Do not show paid conversion, paid pricing or activation metrics until real product entitlements and billing have launched.

The [growth measurement roadmap](https://github.com/castlemilk/tammy/blob/docs/tammy-growth-funnel-2026-11-05/docs/roadmap/growth-measurement-2026-11-05.md) supplies the full event policy, guardrails and review milestones.
