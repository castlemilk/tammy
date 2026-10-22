# Local accounting walkthrough

This walkthrough exercises the current development UI. It is not a production, lodgement, or external-conformance procedure.

## 1. Start the application

From the repository root:

```sh
rtk mise exec -- pnpm desktop:start
```

The first build may take several minutes because it builds the vendored SQLCipher-backed Go core. Keep the owning terminal open.

## 2. Create the encrypted workspace

On **Create your local workspace**, enter:

- an administrator display name and username;
- the business legal name, display name, and a checksum-valid 11-digit ABN;
- a workspace passphrase and administrator password that satisfy the displayed minimums.

Choose **Create local workspace**. Tammy creates the workspace only inside its approved local application-data capability; the renderer does not choose a raw database path.

## 3. Save and confirm recovery

Tammy displays a one-time recovery code. Store it outside this development workspace, then choose **I saved my recovery code**. The current setup flow confirms the requested recovery groups, signs in the administrator, creates the organisation, and installs the pinned AU rules and chart.

This is setup recovery-code confirmation, not a demonstration of every break-glass or restore workflow. Do not put a real recovery code in issues, fixtures, screenshots, or logs.

## 4. Reopen and sign in

Close the app normally and start it again. The encrypted workspace persists beneath Electron's `local-core-development` data root. Enter the workspace passphrase and administrator credentials on **Unlock your workspace**.

Development startup intentionally resets only its private workspace/identity attempt journals because their anchors live in memory. It does not delete the SQLCipher database, catalogue, installation key, organisation, or accounting records. This behavior is development-only and is not packaged rate-limit evidence.

## 5. Inspect the organisation and chart

Setup creates one AU private-company organisation using Australia/Melbourne, AUD, non-cash GST, quarterly reporting, a June year end, and the retained AU GST rule bundle.

Open **Accounting → Chart of accounts**. The installed chart includes protected bank, receivable/payable, current/deferred/evidence/adjustment GST, earnings, retained earnings, and opening-equity accounts. Add at least two ordinary accounts before creating a manual journal; protected system/control accounts cannot be selected for manual posting.

## 6. Post and inspect a journal

Open **Accounting → Journals**, choose **New journal**, select different ordinary debit and credit accounts, enter a date, positive AUD amount, and description, then post.

The resulting detail must show equal debit and credit totals. Return to the journal list to confirm the retained posted entry. The separate General ledger route is currently a placeholder and should not be used as evidence of a connected ledger-detail UI.

Open **Accounting → Trial balance**, choose an as-of date on or after the journal date, and confirm total debits equal total credits. The displayed account balances should reflect the journal without changing it.

## 7. Retain and review a document

Open **Documents** and choose a local PDF, PNG, or JPEG no larger than 10 MiB. PDF text is extracted locally where supported; images do not use a cloud OCR service. Review the proposed supplier, invoice, date, subtotal, GST, and total, correct them if necessary, and save the review.

The retained document remains local. A reviewed purchase document within the chosen reporting period becomes an eligible source for the BAS draft.

## 8. Import, match, and reconcile banking

Open **Banking**. Enter an opening balance and one or more rows in:

```text
YYYY-MM-DD,description,signed amount
```

Import the rows, explicitly **Match** each one, and observe that matched is distinct from reconciled. **Complete reconciliation** remains disabled until every imported line is matched; after completion, the summary and line state show reconciliation separately.

This walkthrough uses pasted local rows. It does not claim bank feeds, automatic matching, or external financial-institution connectivity.

## 9. Create a BAS draft

Open **GST & BAS**, select a period containing the reviewed document, and choose **Create local draft**. Review G1, 1A, 1B, net GST, and the retained source list.

The status must read **Draft — not lodged**. The screen has no declaration, lodge, submit, or ATO transport action. Reconciliation state does not convert a local draft into a lodged BAS.

## 10. Trace local activity

Open **Audit trail**. The current product view combines retained journals, banking lines, documents, and the current BAS draft into a chronological activity list. Cross-check identifiers, dates, amounts, and states against their source screens.

This screen is a convenient activity projection. It does not replace the Go core's cryptographic audit-chain verification or an exported evidence package, whose complete desktop workflow is deferred.

## Resetting the walkthrough

Normal restart is the preferred recovery path and preserves data. If a developer deliberately needs a blank local workspace, first stop Electron and the Go core, then move the entire Electron `local-core-development` directory to a separately named backup location. Do not remove individual database, key, catalogue, audit, or journal files: partial deletion can correctly fail closed.

The application itself automatically handles the development-only attempt-journal reset described above; manually deleting those journals while the core is running is unsupported.
