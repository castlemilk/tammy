# Tammy growth measurement and privacy contract

**Status:** Design proposal. No tracker, account database, consent manager, Search Console connection or public paid product is presumed to exist.  
**Parent:** [Growth Roadmap #31](https://github.com/castlemilk/tammy/issues/31), [instrumentation epic #33](https://github.com/castlemilk/tammy/issues/33).

## 1. What to learn

A user who discovers an invoice tool, finishes a valid invoice and comes back next month is more informative than ten anonymous pageviews. Measurement must tell us:

- Are people finding the right page for their question?
- Can they successfully complete the free task, without account or card?
- Do they return or use a second useful tool where observability legitimately permits that inference?
- Which tasks lead to genuine interest in persistent accounting, **when a real interest route exists**?
- Are the tools accurate, accessible and reliable?

There is no meaningful paid conversion metric before a real paid offer, entitlement/billing flow, signed/recoverable accounting build and support policy exist.

## 2. Event schema: conservative by design

**Strict allowlist of fields** (versioned schema). Exclude free-form user inputs, document contents, calculations, business identity and all account/customer data.

| Field | Allowed values | Notes |
| --- | --- | --- |
| event | tool_view, tool_started, tool_completed, tool_error, export_started, export_completed, export_failed, save_offer_shown, save_offer_selected, next_tool_opened, accounting_interest_opened, accounting_interest_submitted, accounting_activated | Only emit events for actions that actually exist |
| version | 1 | Required schema version |
| tool_id | invoice, quote, gst, abn, due_date, margin, break_even, runway, cash_forecast, receipts, other_allowlisted | Fixed enum, never user-created |
| page_key | home, tools_index, tool_detail, blog_post, guide, accounting_info, availability | Approved route templates, not arbitrary URL path or query |
| channel | organic, direct, referral, campaign, unknown | Only where consent/legitimate attribution supports it |
| outcome | success, invalid_input, export_error, unavailable, network_error, unknown | Never include actual values or error message content |
| consent_state | allowed, denied, unknown | Must reflect actual consent behavior; unknown must not be treated as allowed |
| experiment | none or approved experiment ID | No arbitrary label or entered text |
| session_id | ephemeral random identifier *only after permissible consent* | Not a cross-device person identifier |
| event_time | timestamp or server receipt time | Apply minimisation/retention and review GDPR/APP obligations for target markets |

**Explicitly forbidden:** invoice number, ABN, business name, person/customer names, customer email, physical address, unit price, GST amount, account/bank balance, raw entered text, filename, file bytes, PDF, receipt content, tax code, browser query string that may contain details, recovery keys, full IP address stored for analytics, cross-site tracking IDs or session replay of form contents.

A tool can be used fully with analytics denied or unavailable. Do not store sensitive inputs in browser analytics SDK local storage or network traces. If only aggregate operational service logs are needed, define them separately from user-behaviour tracking.

## 3. Event semantics and deduplication

| Event | Trigger | Never trigger when |
| --- | --- | --- |
| tool_started | First meaningful editing/calculation action during a user attempt | Page merely loads or a user scrolls |
| tool_completed | Valid useful result has been shown or created | Form invalid, user clicks placeholder, export failed |
| export_started | User explicitly requests PDF/CSV/image export | Button appears |
| export_completed | Browser reports export bytes prepared and download initiated | Render promise rejected or only a server request was attempted |
| export_failed | Recoverable export error with reason enum | Diagnostic includes invoice data or private PDF bytes |
| save_offer_shown | Legitimate option to persist an already completed task is visibly presented | No save implementation exists |
| save_offer_selected | User knowingly chooses the working save path | A dismiss, close or info click |
| accounting_interest_opened | User opens truthful accounting information/waitlist page | Fake “subscribe” CTA or unavailable product |
| accounting_interest_submitted | Confirmed server acknowledgement after verified interest form exists | User merely focused an email field |
| accounting_activated | Supported signed desktop product completed initial safe setup | Unreleased builds, site pageviews, app installers downloaded |

Multiple recalculations from the same attempt should not count as repeated distinct completions unless the user deliberately begins another task. A session is an explicitly defined observability unit, not an approximation of unique human identity. Consent-off actions may produce **no measurable events**; dashboards must label consent coverage and unknown attribution.

## 4. Funnel definitions and denominators

| KPI | Definition | Reporting caveat |
| --- | --- | --- |
| Tool start rate | eligible observed sessions starting named tool / observed sessions landing on eligible route | Consent coverage and channel attribution may be incomplete |
| Tool completion rate | genuine completed attempts / genuine started attempts | Per-tool and device viewport; do not inflate by page load |
| Export reliability | successful export attempts / all export attempts | Explicitly separate validation, renderer, download initiation |
| Repeat utility use | observed sessions with >=2 distinct completed tool tasks / eligible observed sessions | Only where retention/session consent supports it |
| Save offer acceptance | successful opt-in saves / legitimate exposed completed tasks | Not defined before a real save feature exists |
| Qualified accounting interest | acknowledged expressions of interest / eligible completed free tasks | Not paid conversion; form must actually exist |
| True activation | new released, safe accounting organisation with verified first meaningful workflow / eligible supported onboardings | G0/G1 gate; not available while product unreleased |
| Trial-to-paid | paying active organisations / trial-eligible organisations, declared cohort window | Not defined until tested billing/cancellation and product launch |
| Organic search clicks | Search Console total clicks filtered by canonical landing URL/query segment | Aggregate publisher data, not joined to private accounts |

For tool analytics prefer **attempt** IDs generated locally within the page, discarded on close, and use them only with acceptable consent for deduplication. Do not send the attempted invoice ID. Report small cohorts as directional; suppress/aggregate segments that could expose individuals. Do not count a referral as organic without evidence.

## 5. Dashboard requirements

One weekly dashboard with:
- Search Console impressions, clicks, CTR, mean position by canonical URL and non-branded/branded query cohort, with counts and date windows.
- Site health: 200/404/redirect/canonical checks, sitemap URLs submitted/indexed, real Core Web Vitals sample if available.
- Tool funnel per tool: observed landing → started → completed → export started → export completed → optional save → actual accounting interest, with numerator, denominator, consent coverage and error categories.
- Return utility use, multi-tool paths, mobile versus desktop form errors, and recently changed source/article pages.
- Paid panels absent or labelled **not launched**, never zero bars that imply failed conversion.
- Regression guardrails: severe financial calculation bug, PDF corruption, security leakage, accessibility issue or incorrect ATO statement blocks experimentation regardless of conversion uplift.

Do not fabricate initial numbers or conversion forecasts; all values are baseline unknown until approved instrumentation and domain readiness.

## 6. Privacy, security and retention decisions to resolve before collecting

1. Determine whether analytics is strictly necessary, consent-based or disabled by default in the applicable jurisdiction, and reflect it in the privacy notice before deployment.
2. Choose provider/hosting and log retention with access boundaries. Proposed starting limit: short raw-event retention (e.g. <=90 days) pending documented privacy review, aggregated longer only where lawful and needed. This is **not** an existing retention setting.
3. Implement an event **schema allowlist**, network payload tests and explicit rejection of any unexpected keys. Ban SDK autocapture of form fields and query strings.
4. Verify IP/server logs cannot accidentally become permanent financial task analytics; redact errors and crash traces.
5. Provide clear opt-out/revocation and ensure calculators/export operate normally with analytics blocked or consent unknown.
6. Maintain a collection inventory, data controller/operator, access controls, deletion and incident response in the actual public privacy policy.
7. Audit analytics in browser developer tools using realistic synthetic invoices, ABNs, GST amounts, payee details and user rejections; expected transmitted financial values = **zero**.

## 7. Search Console setup runbook

- Verify tammy.tax property only after the domain serves the intended site. Search Console verification is not equivalent to publishing the site.
- Submit the actual new sitemap, inspect representative tool and Journal URLs, verify rendered canonical, robots, mobile HTML content and structured data.
- Export aggregate query/page results with reporting dates, region and device. Treat Google rounding/privacy thresholds and Search Console sampling/coverage limits explicitly.
- Never use a raw entered search query from Tammy's Tools filter as a telemetry field.
- Monitor the old Sites domain through the migration period and investigate indexable duplicates and incorrect redirects; do not erase useful old URLs before the new host is healthy.
- Store baseline decisions and evidence links in the related GitHub issue or runbook, not in a chart with unsourced numbers.

## 8. Experiment protocol

For each experiment: plain-language hypothesis, exposure cohort, exact UI change, primary completion metric, financial correctness and accessibility guardrails, outcome threshold, minimum observation period chosen **after** baseline traffic is known, and rollback action.

Prioritise reliability and mobile usability experiments before testing conversion nudges. Keep a single free task complete without a signup wall in every experiment. A “winner” must not increase corrupted PDF downloads, false tax labels, abandonment, or support tickets.

**Source reference:** [Google Search Console](https://support.google.com/webmasters/answer/9128668), [Google people-first content](https://developers.google.com/search/docs/fundamentals/creating-helpful-content).
