# Tammy — Soft Precision design pack

Proposed art direction, generated 22 October 2026. These are design references, not implemented UI or evidence of production readiness.

## Start here
- [Artistic guidance](artistic-guidance.txt): colour, typography, spacing, components, motion and trust rules.
- [Image manifest](image-manifest.json): exact image dimensions and SHA-256 checksums.
- [Feature coverage](feature-coverage.txt): current versus planned screens and implementation cautions.

## Boards
1. [Today, transactions, receipt capture](01-soft-precision-views.png)
2. [Automation studio and invoice creation](02-automation-invoices.png)
3. [Design system and component states](03-design-system.png)
4. [Transparent paper-loop artwork](04-paper-loop.png)
5. [Cash flow and month-end](05-cash-flow-month-end.png)
6. [Mobile onboarding, connections, review and invoice activity](06-mobile-moments.png)
7. [Invoices, item details and payment allocations](07-sales-item-tracking.png)
8. [Documents, banking and local activity](08-documents-banking-activity.png)
9. [Chart of accounts, journals, trial balance and GST draft](09-accounting-core.png)
10. [Contacts, purchases, quotes/credits and reports](10-business-suite.png)
11. [Workspace, recovery, organisation and backup concepts](11-workspace-recovery.png)
12. [Opening balances, period close, reversal and interrupted import](12-corrections-interruptions.png)

13. [Local sign-in, supplier credits and import mapping](13-sign-in-credits-import.png)
14. [BAS review, internal transfers and migration recovery](14-bas-transfers-migration.png)

## Important scope correction
Boards 1–6 explored the visual direction before repository inspection. Their mobile, live-account connections, automation, forecasting and sending concepts are exploratory, NOT current capabilities. Boards 7–14 are informed by the current technical-state documentation and suite specification at commit aeda631070f9cb44289658c52128c595bf6133f7.

Tammy is a local-first AUD-only desktop app. Current banking is statement import and explicit matching, not live bank feeds. Current GST workpapers remain Draft — not lodged. Inventory/stock tracking, payment gateways, payroll and production ATO submission are excluded by the suite specification. Item tracking here means invoice/bill line detail, outstanding amounts, allocations and source evidence.

The concept boards must not override domain rules or imply unsupported features. Generated sample text, dates, account codes, badges and navigation need normalization against real fixtures. See feature-coverage.txt for known limitations.
