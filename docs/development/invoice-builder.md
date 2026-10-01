# Invoice builder

Tammy's desktop invoice builder creates customer-facing AUD invoice documents from manual line items, reviewed expenses, or an MCP-generated proposal. The Go core stores drafts and issued snapshots in the encrypted workspace. Issued invoices post sales, GST and receivables in the business ledger; dated receipts reduce their balances.

## Launch and use

From the repository root:

```sh
mise exec -- task setup
mise exec -- task dev
```

Create or unlock a workspace, sign in, and complete the organisation profile. Open **Invoices** in the sidebar. Seller name and ABN come from the current organisation; enter the seller address, customer details, invoice number, issue/due dates, payment instructions and line items.

Edit the invoice in the **Invoice line items** table. Enter description, quantity, private unit cost, selling price and GST treatment; line totals include GST. Costs are optional, but private profit remains unknown until every line has a cost, including an explicit zero when appropriate. The live summary shows subtotal, GST, total due, private cost and profit before GST. Partial input stays visible for correction before saving.

For expense-based lines, first upload a document in **Documents** and complete its review. The **Reviewed expenses** table uses only reviewed documents from the same organisation. Search by description, select one or more expenses, enter a markup percentage and choose **Add selected expenses**. Each selected expense becomes a quantity-one line linked to its original document, with the net expense as private cost and markup applied to its selling price. Already-linked expenses are marked **Added**; removing their invoice line makes them available again. Refreshing expenses preserves invoice edits.

Review selling price and GST treatment after importing expenses. Source GST does not establish the tax treatment of the onward sale: choose that explicitly. Editing private invoice costs does not modify the source expense or ledger. An invalid markup or selection exceeding the 50-line limit adds no expenses and preserves the selection for correction.

Enable the GST-registered checkbox only when the seller is registered. Tammy does not infer GST registration from an ABN or the organisation's GST accounting basis. Choose taxable, GST-free, input-taxed or out-of-scope treatment for each line. Prices and costs are ex-GST AUD amounts.

Save the draft before exporting. Draft PDFs are clearly marked as not issued. **Issue invoice** validates the seller/customer details and dates, then requires confirmation. Issue atomically posts business income and receivables; a closed accounting period leaves the saved draft unchanged. Issued documents are immutable. If the organisation's legal name or ABN has changed since saving, save and review the draft again before issuing; the core will not silently replace the reviewed seller identity. Reopening the workspace retains saved drafts and issued documents; optimistic versions prevent silently overwriting another edit.

Use **Export PDF** to choose a destination in the native save dialog. Save any edits first: export uses the saved version and stays disabled while the editor has unsaved changes. Cancellation saves nothing. Internal costs, profit and source-document IDs are excluded from customer PDFs. Export does not send the document or store a payment.

## Company template and logo

Choose **Manage invoice template** on the invoice page to save company defaults for seller address, payment instructions, notes and payment terms. Terms range from 0 to 365 days after the issue date; the initial default is 14 days. New invoices inherit the saved defaults. **Apply saved template** explicitly replaces those fields, the due date and logo on the editable draft in view. Customer details and line items remain as entered. Existing invoices retain their saved values when company defaults change.

Upload a static PNG or JPEG logo, then choose **Save template**. Logos may be up to 1 MiB, 4096 pixels per dimension and four million pixels. The preview shows the selected logo. **Remove logo** removes it from future defaults; **Remove invoice logo** removes it only from the current editable draft. Save the invoice to retain that change. Issued invoices retain their original logo and text.

The encrypted workspace stores immutable, organisation-scoped logo assets and versioned defaults. Upload validation checks the actual format, structure, decoded dimensions and pixels. PDF generation reads the saved invoice's asset, preserves its aspect ratio and uses only local image bytes. Template and logo management require the same admin/preparer authority as invoice writing; the MCP proposal schema remains unchanged.

## Business income, receipts and BAS

After issue, the invoice's accounting panel shows its posting, received amount and balance owing. **Record payment** accepts the actual receipt date, AUD amount, business bank account and reference. Partial payments retain their exact cents and line-level GST allocations. A receipt reduces receivables and records the bank movement without adding sales again. Overpayments, receipts before invoice issue, stale versions and closed-period postings are rejected. Private invoice costs remain pricing estimates and are never reposted as expenses.

**Overview** shows posted business revenue excluding GST, invoice income, payments received and invoices owing. Choose a quarter, financial year or custom period. **Balances as of** includes debt from earlier periods and subtracts receipts through that date. Totals read all retained sources. The invoice browser follows authenticated pages so older invoices remain available for review and payment recording.

Older issued documents created before accounting integration remain unposted. Review whether they were already entered manually, then use **Add to accounts** only when appropriate. Reads, migration and reopening do not create historical financial entries.

**GST & BAS** includes invoice sales and retained source references. Non-cash GST uses issue dates; cash GST uses dated receipt allocations. Taxable, GST-free and input-taxed sales contribute to G1; out-of-scope lines do not. Saved workpapers retain their figures and show a source-change notice when regeneration is needed. Cash purchase credits require typed payment evidence, which the current reviewed-expense workflow does not retain; those credits are excluded with a warning. Workpapers remain drafts, without declaration or lodgement.

GST basis is captured at posting. Historical basis changes whose effective treatment cannot be reconstructed are rejected for review, rather than attributed using a guessed date. Invoice-before-payment is supported; deposits before issue, credit notes, refunds and invoice reversals require a later typed workflow.

## Generate a proposal through MCP

Follow [Local API and MCP](local-api-mcp.md) to enable and pair the current signed-in organisation. The relevant tools are:

- `tammy.invoices.list_cost_sources`: reviewed supplier references and cost amounts, without document bodies.
- `tammy.invoices.list_invoices`: the first 20 saved invoices, including private costs for the authorised accounting client.
- `tammy.invoices.propose_draft`: validates and stages an editable proposal in memory. It cannot save or issue an invoice.

The proposal takes the draft directly as its arguments, for example:

```json
{
  "number": "INV-001",
  "issueDate": "2026-09-30",
  "dueDate": "2026-10-14",
  "customerName": "Example customer",
  "customerAddress": "1 Example Street, Melbourne VIC 3000",
  "sellerAddress": "27 Cambridge Road, Mooroolbark VIC 3138",
  "gstRegistered": true,
  "notes": "",
  "paymentInstructions": "Bank transfer using the invoice number as reference",
  "lines": [
    {
      "description": "Consulting",
      "quantityMilli": 1500,
      "unitPriceMinor": 10005,
      "unitCostMinor": 4000,
      "taxTreatment": "taxable"
    }
  ]
}
```

`quantityMilli` represents thousandths of a unit: 1500 means 1.5. Money uses integer AUD cents, so 10005 means $100.05. `unitCostMinor`, `sourceDocumentId` and `customerAbn` are optional. A source ID must refer to a reviewed document in the current organisation; the client cannot select another organisation, authentication context or calculated totals.

The example calculates $150.08 ex-GST, $15.01 GST and $165.09 total, with a private $60.00 cost. Multiplication and GST round half-up per line using integer arithmetic. Drafts permit incomplete identity/date fields; issuing requires them. Limits are 1–50 lines, quantity 0.001–1000, per-unit price/cost up to $1,000,000.00, and invoice totals up to $10 billion.

In Tammy, choose **Use MCP draft**, review/edit all fields, then save explicitly. If there are unsaved edits, replacing them requires confirmation. Revoking access or changing the authenticated workspace clears staged proposals; proposals are not retained across a restart. Treat MCP-generated descriptions, prices and tax classifications as suggestions requiring review.

## PDF implementation

`packages/invoices` adapts pdfcn's Forme invoice template and components, with the upstream MIT notice retained in `THIRD_PARTY_NOTICES.md`. Rendering is offline in Electron main, from the saved Core record, with A4 pages, repeated table headers and actual page counts. The renderer cannot provide an arbitrary export path or inject precomputed totals.

The bundled font covers common Latin, Greek and Cyrillic text. Unsupported glyphs fail export explicitly instead of silently changing names or descriptions. The desktop preflights the actual saved document before issuing, so failed rendering leaves an editable draft. Wider script coverage is not yet supported.

## Test scenarios

```sh
mise exec -- task test:invoices
mise exec -- task integrations:test
mise exec -- task verify:full
mise exec -- task package:e2e
```

Focused tests cover integer calculations, validation, PDF pagination/privacy, editor behavior, native export and session revocation. Tagged SQLCipher invoice integration tests additionally cover authorisation, exact idempotent replay, stale versions, reviewed-source ownership and issued-document immutability. Packaged acceptance covers pairing over real HTTP/stdin MCP, human review, save, draft/issued PDF export and reopening an issued document after restart.

Company-template checks cover defaults-load races, explicit application, logo upload/removal, optimistic conflicts, malformed images, organisation isolation, an exact 1 MiB upload through the real TLS server, and unchanged invoice branding after defaults change. Packaged acceptance uploads a real transparent PNG, verifies embedded PDF images and payment instructions, changes the defaults, and reopens both the original invoice and a new draft after restart.

Customer delivery, credit notes, refunds, ATO/SBR submission and recurring billing remain unavailable. Issued sales and dated receipts are retained in the business ledger and local BAS workpapers; an issued document is not evidence of tax lodgement or App Store release.
