# Tammy free-tools acquisition and paid-accounting funnel

**Snapshot:** 5 November 2026  
**Status:** Proposed strategy and delivery backlog, not implemented commercial capabilities  
**Primary buyer:** Australian non-employer sole trader or small service business, one AUD entity  
**Companion:** [Xero replacement roadmap](./xero-replacement-roadmap.md) and [Growth epic #31](https://github.com/castlemilk/tammy/issues/31)

## 1. North star

Tammy should be both a **genuinely useful free business-tools product** and a **paid local-first accounting product**. People should complete a small task without registration, come back when they have another task, and adopt persistent, auditable accounting when their needs cross beyond disconnected documents.

The growth metric is **supported small businesses doing real accounting tasks over time**, not raw calculator impressions. No paid subscription, price, customer account, hosted syncing, free workspace persistence or signed public desktop release is assumed to be available merely because it appears in this roadmap.

### Value exchange and product boundaries

| Layer | User promise | Examples | Status boundary |
| --- | --- | --- | --- |
| Anonymous free tools | Complete the task, inspect the result, export without signup | Invoice PDF, GST, ABN format, due dates, margin, break-even, cash runway | Seven tools are implemented in draft site source PRs #14–#20; live publication not independently verified |
| Optional return experience | Reuse earlier input with clear data custody and deletion | Saved customers/items, draft quotes, cash-flow worksheets, receipt lists | Proposed; start with explicit local save/export and recovery decisions |
| Paid Tammy Accounting | Persistent, reconciled, source-backed AUD books for a supported business | AR/AP, journal, bank reconciliation, GST/BAS drafts, P&L and accountant pack | G0 and G1 development/release gates; not public readiness |
| Optional connected services | Consented external convenience where approved | Bank-feed provider, payment gateway, approved BAS filing | G3 external gates; do not imply current availability |

**Do not make the public invoice generator useless to manufacture a paywall.** Paid differentiation is reliable accounting state, source reconciliation, repeatable processes, a provable audit trail and integrations after external acceptance.

## 2. Two delivery tracks, one coherent system

**Track A: acquisition and useful free software.** Own the public Tools and Journal experience, anonymous completion, printable/downloadable outputs, SEO, repeat-visit value and privacy-safe attribution.

**Track B: accounting depth and paid readiness.** Own encrypted recovery, opening balances, customer/supplier master, invoices/bills exceptions, bank match, reporting and Xero migration. The [Xero roadmap #30](https://github.com/castlemilk/tammy/issues/30) and [G0–G2 issues #22–#29](https://github.com/castlemilk/tammy/issues/22) are the release gates. Acquisition must not claim those routes are already supported.

**Indicative capacity allocation, only after planning and staffing:** 50% accounting correctness/recovery, 30% free tool utility/retention and 20% SEO/distribution. This is a prioritisation principle, not an approved resourcing plan.

## 3. The first personas and jobs

| Persona | First query / moment | Free outcome | Why paid accounting might matter |
| --- | --- | --- | --- |
| Solo consultant or contractor | “free Australian tax invoice generator” | Correctly treated invoice and downloadable PDF | Customer register, receipts/AR ageing, GST and bank matching |
| Tradie or small service firm | “quote template Australia” | Editable quote with terms and export | Convert quote to invoice, track suppliers and collect/reconcile receipts |
| GST-registered microbusiness | “how to remove GST from a price” | Standard 10% GST breakdown with source notes | Reviewable, consistent GST workpapers and report lineage |
| Shop or independent agency | “markup vs margin calculator” | Unit profit, markup, break-even scenario | Reports, period close, expense/bill allocations |
| Cash-constrained business | “cash runway calculator” | Honest first cash coverage estimate | Dated cash forecast backed by verified bank/AP/AR data |
| Xero customer considering a switch | “export invoices from Xero” | Verified migration preparation guide | Dry-run import, balance tie-out, independent restore and cutover |

Do not broaden to payroll, inventory or multi-currency until G4 domain acceptance and search-intent evidence justify it.

## 4. Real baseline, not a traffic estimate

**Draft site source includes seven free tools:** invoice generator, standard GST calculator, ABN checksum checker, invoice due-date calculator, profit-margin calculator, break-even calculator and cash-runway estimator. The Tools index has search/filter controls. The Journal has thirteen article entries in the latest PR #20 source.

These are **source-level implementation facts**, not evidence of a published site, real user traffic, repeat usage, Search Console indexing or paid conversions. Some features reside in a chain of draft PRs #13–#20; the live ChatGPT Sites project may not reflect them. Publisher-domain readiness for tammy.tax remains **unverified** in this planning snapshot. The canonical source is still the ChatGPT Sites host.

**Commercial baseline:** no verified paid checkout, pricing, paid accounts, billing provider, product entitlement or published Mac release. A marketing CTA may explain the product or accept legitimate future-interest signup only after that route actually exists.

## 5. User journeys with concrete handoffs

### Journey A: invoice → accountant-ready records

1. Visitor searches for an invoice template, lands on /tools/invoice-generator and completes a valid invoice anonymously.
2. Download is available immediately. The customer receives a real usable PDF, no signup requirement and no hidden data upload.
3. After export, offer a discreet, truthful “Save customer and draft locally” path only when there is a working, user-controlled persistence/export/delete solution.
4. After G0/G1 release, explain the upgrade: reuse contacts and invoice sources, keep AR/receipts and GST tied to the ledger, match bank transactions, export accountant-ready evidence.
5. On import into accounting, preview source IDs, matching, tax treatment and posting effects; explicit approval, idempotent retry and auditable reversals are mandatory.

### Journey B: business utility → repeat problem solving

1. Visitor calculates 10% GST on a quote, then moves into a real quote/invoice generator with the calculation preserved where appropriate.
2. A later visit uses a cash-runway estimate and a new dated 12-week forecast workbook with source assumptions and export.
3. Returning visitors can choose to retain templates, estimates and receipts in a disclosed local workspace without an account.
4. Paying becomes attractive only when the user needs linked invoice/bill/customer/bank/report truth, not to unlock another colour on a chart.

### Journey C: content → informed tool → paid interest

1. Each guide answers one Australian business question with primary ATO/business.gov.au sourcing, a reconciled worked example and stated exceptions.
2. It links to exactly the matching tool (not every tool); the tool explains its calculation and inputs.
3. The tool completion page offers a relevant next useful free task plus one honest accounting benefit.
4. Count qualified interest separately from paid activation, which only exists when the actual signed/recoverable product and payment flow launch.

## 6. Acceptance milestones, without fictional deadlines

| Gate | Outcome | Required evidence | Dependency |
| --- | --- | --- | --- |
| M0 — Reachable and measured | tammy.tax serves the correct site, canonicalised and observable | Live route smoke tests, TLS/DNS verification, Search Console, sitemap, consent-safe metrics baseline | [#32](https://github.com/castlemilk/tammy/issues/32), [#33](https://github.com/castlemilk/tammy/issues/33) |
| M1 — Useful tools | Complete invoice, quote, GST/ABN and dated cash utilities | Mobile/keyboard/PDF E2E, exact-cent golden tests, source-linked docs, anonymous outputs | [#34](https://github.com/castlemilk/tammy/issues/34), [#36](https://github.com/castlemilk/tammy/issues/36) |
| M2 — User-controlled repeat work | Explicit save/reuse with export, deletion and recovery | Multi-visit local scenario tests, no silent upload, graceful failure/restart | [#35](https://github.com/castlemilk/tammy/issues/35) |
| M3 — Honest paid handoff | Real accounting onboarding/entitlements for supported profile | G0 recovery, G1 invariants, signed release, billing/refund/support evidence | [#37](https://github.com/castlemilk/tammy/issues/37) plus [#22–#29](https://github.com/castlemilk/tammy/issues/22) |
| M4 — Repeatable growth | Data-led content and distribution cycles with guardrails | Query/page data, completion cohorts, documented experiments and rollback | [#38](https://github.com/castlemilk/tammy/issues/38) |

No calendar dates are assigned until the site and release constraints are understood.

## 7. Prioritised execution epics

| Order | Epic | Why now | Primary acceptance |
| --- | --- | --- | --- |
| 01 | [#32 Site + tammy.tax launch](https://github.com/castlemilk/tammy/issues/32) | Nothing converts if users cannot reach and index it | Correct hosted version, canonical site, Search Console and real route/PDF tests |
| 02 | [#33 Privacy-first instrumentation](https://github.com/castlemilk/tammy/issues/33) | Establish baseline before experiments | Consent-compliant event model, no financial PII, complete funnel denominators |
| 03 | [#34 Invoice → quote → optional save](https://github.com/castlemilk/tammy/issues/34) | Strong user task and natural path to AR | Anonymous PDF remains free; share tax model, working quotes and safe save offer |
| 04 | [#36 SEO content clusters](https://github.com/castlemilk/tammy/issues/36) | Useful discoverability and qualified task traffic | Intent/URL map, official sources, internal links, technical SEO audits |
| 05 | [#35 Repeat-use utilities/workspace](https://github.com/castlemilk/tammy/issues/35) | Increase retention before asking for money | Receipt/expense organiser, dated forecast, clear export/recovery/delete |
| 06 | [#37 Actual paid onboarding](https://github.com/castlemilk/tammy/issues/37) | Only when accounting can safely be trusted | G0/G1 product release, idempotent import and tested entitlements |
| 07 | [#38 Distribution and experimentation](https://github.com/castlemilk/tammy/issues/38) | Improve from real outcomes rather than keyword guesses | Verified task completion, consent and regression guardrails |

Machine-readable acceptance-level work is in [growth backlog CSV](./growth-backlog-2026-11-05.csv). The [SEO strategy](./seo-strategy-2026-11-05.md) and [measurement specification](./growth-measurement-2026-11-05.md) explain acquisition and conversion measurement.

## 8. Architecture to avoid two incompatible products

- Financial amounts in integer AUD minor units; GST, quantity and rounding policy shared with the existing validated invoice domain where feasible, not reimplemented in browser widgets.
- Distinguish anonymous calculated output from an **issued** accounting source. The free PDF does not create a Tammy ledger posting.
- User-owned local draft imports use immutable origin IDs, row/content hashes and explicit review decisions before entering actual accounting.
- The persistent paid ledger remains behind authenticated local Go core and desktop release gates; a browser free-workspace must not pretend to be its source of truth.
- No account/contact, invoice amount, business name, ABN, document bytes or GST result goes into analytic event payloads.
- Marketing claims come only from documented released capability, not roadmap images or draft PRs.
- A plan for payments does not imply a live Stripe subscription or a paid entitlement.
- “Local-first” means data location, recovery/export and exact opt-in networking are explained transparently.

## 9. Success metrics and guardrails

**North-star candidate, only after a public release:** weekly supported businesses completing at least one meaningful free or paid workflow, with precise unique-user definitions agreed under the privacy policy.

**Anonymous public tool funnel:** successful tool completions / genuine starts; successful PDF exports / export attempts; eligible organic sessions starting a relevant tool / eligible organic landings; opt-in reuse / exposed eligible completion, with consent-aware attribution.

**Paid funnel later:** qualified product interest / completed free tasks; actual activated organisations / qualified onboardings; trial-to-paid conversion and retention only after real plans, desktop release, billing and privacy-compliant identifiers exist.

**Guardrails:** failed calculations, invoice corruption, invalid GST/ABN claims, download failure, accessibility regressions, high support requests, unconsented tracking and lost client data must override acquisition goals.

Instrument only after consent and data-handling decisions are made. A “0 conversions” graph before there is a public paid offer is misleading; mark it “not launched,” not zero.

## 10. Roadmap review cadence

- **Weekly after launch:** Search Console query/URL trends, availability, top page errors, tool completions, anonymous export reliability, top customer questions; do not overinterpret small cohorts.
- **Monthly:** one user task-oriented release, source/regulatory content review, internal linking audit, return-use behaviour and failed flows.
- **At each G0–G2 milestone:** revisit the paid promise, remove unsupported claims, and expand allowed migration profiles only with an accepted fixture oracle and recovery evidence.
- **Quarterly:** judge new utility ideas by actual task demand, not keyword volume estimates alone; retire or merge redundant pages.

Review workstream mix using a simple weighted score: 30% direct user utility/completion, 25% natural accounting conversion fit, 20% validated search intent, 15% implementation leverage, 10% differentiation. **G0 correctness and security are hard gates, not scoreable trade-offs**.

## 11. Risks to actively reject

- Launching hundreds of pages that merely reword an ATO definition.
- Buying search traffic before the invoice generator exports reliably.
- Blocking PDF downloads to force signup.
- Moving canonical URLs to tammy.tax before both DNS and hosted Sites publication work.
- Collecting financial input data in analytics.
- Claiming free workspace sync, accountant access, BAS lodgement or bank feeds from concept screens.
- Counting Search Console impressions as paid demand.
- Investing in payroll/tax-return calculators before source accuracy and real user demand are demonstrated.

**Evidence / guidance:** [Google: helpful people-first content](https://developers.google.com/search/docs/fundamentals/creating-helpful-content), [Google: changing domains](https://developers.google.com/search/docs/crawling-indexing/site-move-with-url-changes), [Google: software-application structured data](https://developers.google.com/search/docs/appearance/structured-data/software-app).
