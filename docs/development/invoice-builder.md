# Invoice builder

Tammy's desktop invoice builder creates customer-facing AUD invoice documents from manual line items, reviewed expenses, or an MCP-generated proposal. The Go core stores drafts and issued snapshots in the encrypted workspace. This is document invoicing, not a complete accounts-receivable subsystem.

## Launch and use

From the repository root:

```sh
mise exec -- task setup
mise exec -- task dev
```

Create or unlock a workspace, sign in, and complete the organisation profile. Open **Invoices** in the sidebar. Seller name and ABN come from the current organisation; enter the seller address, customer details, invoice number, issue/due dates, payment instructions and line items.

For expense-based lines, first upload a document in **Documents** and complete its review. The invoice expense picker uses only reviewed documents from the same organisation. Choose a source and review the description, quantity, cost, markup and selling price. Source costs do not establish the tax treatment of the onward sale: choose that explicitly. Changes to an invoice do not modify the source expense.

Enable the GST-registered checkbox only when the seller is registered. Tammy does not infer GST registration from an ABN or the organisation's GST accounting basis. Choose taxable, GST-free, input-taxed or out-of-scope treatment for each line. Prices and costs are ex-GST AUD amounts.

Save the draft before exporting. Draft PDFs are clearly marked as not issued. **Issue invoice** validates the seller/customer details and dates, then requires confirmation. Issued documents are immutable. If the organisation's legal name or ABN has changed since saving, save and review the draft again before issuing; the core will not silently replace the reviewed seller identity. Reopening the workspace retains saved drafts and issued documents; optimistic versions prevent silently overwriting another edit.

Use **Export PDF** to choose a destination in the native save dialog. Cancellation saves nothing. Internal costs, margins and source-document IDs are excluded from customer PDFs. Export does not send the document or store a payment.

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

Issuing currently performs no ledger posting, customer delivery, payment tracking, credit note, ATO/SBR submission or automatic recurring billing. Those workflows need separate implementation and verification; an issued document is not evidence of tax lodgement or App Store release.
