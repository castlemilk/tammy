# Tammy product truth

Current-truth index reviewed against repository sources on 7 November 2026. Tammy serves Australian small-business owners and bookkeepers. Its native accounting workspace and public browser tools are separate products of the same visual system. This index describes implemented behaviour; it is not release qualification or evidence that tests ran in this design pass.

## Native Tammy

Tammy is development software: an Electron interface over a supervised Go core and a local SQLCipher workspace. The public site introduces the Mac application, but offers no public Mac download. There is no hosted Tammy business workspace or cloud sync. [Current technical state](docs/development/tech-state.md) is the detailed capability record; [public availability copy](apps/site/app/page.tsx) preserves the release boundary.

| Area | Connected behaviour and limits | Primary repo reference |
| --- | --- | --- |
| Workspace and accounts | Create/reopen an encrypted workspace, confirm a recovery code, sign in, set up the Australian organisation and chart of accounts, post balanced manual journals and read an as-of trial balance. Remaining Settings, opening-balance and period workflows are not all connected. | [Technical state](docs/development/tech-state.md), [accounting walkthrough](docs/development/local-accounting-walkthrough.md) |
| Documents and purchases | Retain local PDF/PNG/JPEG evidence, review extracted or manually entered data, confirm purchase allocations, post purchases and record dated payments. No cloud OCR, supplier register or complete bill/credit/refund workflow. | [Technical state](docs/development/tech-state.md), [purchase source contract](docs/development/purchase-reporting-sources.md) |
| Invoices and receipts | Save encrypted versioned drafts; issue immutable documents; export saved customer PDFs offline. **Issue atomically posts receivables, business income and basis-aware GST.** Record payment separately to reduce receivables and record the bank movement without duplicating sales. No email/delivery, payment collection, credit notes, refunds or recurring billing. | [Invoice builder](docs/development/invoice-builder.md), [Issue service](services/core/internal/invoices/service.go), [invoice accounting](services/core/internal/localproduct/invoice_income_sqlcipher.go) |
| Banking | Import statement rows, explicitly review matches, then complete a reconciliation once all rows are matched. No bank feeds or automated matching. | [Technical state](docs/development/tech-state.md) |
| Reporting and GST/BAS | Read posted income, receipts, balances and local activity; export income summaries and saved GST workpapers. Native workpapers retain figures, source references and warnings. **Draft — not lodged** remains the state; complete BAS preparation/freeze, declaration, PAYG and production ATO/SBR lodgement remain unsupported. | [Technical state](docs/development/tech-state.md), [BAS reporting rules](docs/development/bas-reporting-rules.md) |
| Local integrations | Opt-in, session-scoped local API/MCP access can read permitted data and stage an invoice proposal for human review. It cannot persist a draft, issue an invoice or post entries. Core backup/restore and audit components do not imply complete desktop workflows. | [Local API and MCP](docs/development/local-api-mcp.md), [Technical state](docs/development/tech-state.md) |

Legacy issued invoices created before accounting integration can remain **unposted**. Opening, reading or migrating them does not create financial entries. Review any earlier manual entry before choosing **Add to accounts**. The current Issue service calls `repos.Accounting.Post` in the document transaction; the [SQLCipher integration test](services/core/internal/localproduct/ledger_sqlcipher_integration_test.go) asserts exactly one `RECEIVABLE` journal after issue and idempotent replay. These sources were inspected, not executed, for this design pass.

## Independent browser tools

The [public catalogue](apps/site/lib/tool-library.ts) contains twelve tools. Inputs and calculations remain in the browser; explicit exports create local files. Browser tools do not connect to the native workspace, issue a native invoice, post a ledger entry, deliver a customer document or lodge a tax return. The hosted website itself is online. Guides and the Journal provide reading material, not accounting execution.

| Tool | Actual output and boundary | Primary repo reference |
| --- | --- | --- |
| Invoice generator | Local invoice PDF from validated details and simple taxable/GST-free lines. No automatic saved data, native Issue or customer delivery. | [Generator](apps/site/components/invoice-generator.tsx), [route limits](apps/site/app/tools/invoice-generator/page.tsx) |
| Quote generator | Local PDF, print fallback, and explicit JSON draft export/import. JSON is not encrypted; no customer acceptance, quote-to-invoice conversion or posting. | [Generator](apps/site/components/quote-generator.tsx), [route limits](apps/site/app/tools/quote-generator/page.tsx) |
| GST calculator | Add or extract standard 10% GST from an entered AUD amount. Does not decide tax treatment. | [Calculator](apps/site/components/gst-calculator.tsx) |
| Simpler BAS workpaper | Illustrative G1/1A/1B figures from already classified inputs and quarterly reference dates. Does not verify eligibility, attribution, adjustments or other labels; not ready to lodge. | [Workpaper](apps/site/components/bas-workpaper.tsx) |
| Sole trader tax checklist | Local printable preparation checklist. No personal-tax calculation, deduction assessment, transmission or lodgement. | [Checklist route](apps/site/app/tools/sole-trader-tax-checklist/page.tsx) |
| ABN checker | Local 11-digit format/checksum validation. Does not query the ABR or verify registration. | [Checker](apps/site/components/abn-checker.tsx) |
| Invoice due-date calculator | Calendar-day due date from an issue date and agreed terms. | [Calculator](apps/site/components/invoice-due-date-calculator.tsx) |
| Profit margin calculator | Gross profit, markup and margin from entered per-unit cost and price. Costs not entered are not estimated. | [Calculator](apps/site/components/profit-margin-calculator.tsx) |
| Break-even calculator | Required units/revenue from fixed costs, variable cost and price. A simple scenario, not a demand or tax model. | [Calculator](apps/site/components/break-even-calculator.tsx) |
| Receipt and expense organiser | Session worksheet with receipt attachments and CSV/JSON export/import. Reload clears it; exported JSON is unencrypted and excludes receipt files. No OCR, posting or decision that an amount is deductible/creditable. | [Organiser](apps/site/components/expense-organiser.tsx) |
| 12-week cash flow forecast | Dated receipts/payments scenario, weekly closing cash and CSV export. No bank connection, GST calculation or prediction of customer payment. | [Forecast](apps/site/components/cash-flow-forecast.tsx) |
| Cash runway calculator | Cash coverage at constant entered monthly rates; not a guarantee of liquidity. | [Calculator](apps/site/components/cash-runway-calculator.tsx) |

## Design and evidence boundary

[DESIGN.md](DESIGN.md) and the [public design system](apps/site/DESIGN-SYSTEM.md) own the established Soft Precision direction. The November consolidation changes public website/tools presentation and reuse; it does not redesign the native desktop or qualify a release. Generated art and the tools study are illustrative critique material, not evidence of product behaviour or an approved pixel comp. The local [component reference source](apps/site/design-system/preview.tsx) uses synthetic examples and performs no financial action. No native tests were run by this design pass.
