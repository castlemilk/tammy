# Tammy invoice demo

This is an isolated test workspace containing fictional data. No invoice has been issued.

## Open the demo

Run:

```sh
rtk proxy "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/electron-builder/ordinary/mac-arm64/Tammy.app/Contents/MacOS/Tammy" --user-data-dir="/Users/benebsworth/projects/tammy/output/tammy-invoice-demo/user-data-company-template"
```

You can also run `rtk proxy zsh /Users/benebsworth/projects/tammy/output/tammy-invoice-demo/open-demo.sh` to open a separate macOS app instance. This helper should be used only when the demo is closed.

The packaged app may be rebuilt at the same location. Keep the profile directory to retain this demo.

## Test-only sign-in

- Email or username: `invoice-demo@example.test`
- Administrator password: `Tammy-demo-admin-2026!`
- Workspace passphrase: `Tammy-demo-workspace-2026!`

These credentials are intentionally stored here for this fictional demo only.

## What to inspect

1. Open **Documents** for three reviewed sample expenses. Their source PDFs are also in `sample-expenses/`.
2. Open **Invoices**, then **Open TEST-2026-001**. The saved draft links one row in **Invoice line items** to each reviewed expense. The **Reviewed expenses** table marks all three sources **Added** and prevents duplicate selection.
3. Verify $150.00 before GST, $10.50 GST and $160.50 total. Internal cost is $150.00 and profit before GST is $0.00.
4. Use **Export PDF** to save another draft copy. The seed run exports `TEST-2026-001-exported-from-Tammy.pdf`.

All names, addresses, identifiers and expense tax treatments are examples chosen for testing. No payment is required.

## Rerun seed verification

Run `rtk proxy zsh /Users/benebsworth/projects/tammy/output/tammy-invoice-demo/run-seed.sh`. It preserves this profile, reuses matching sample expenses and saves the same draft. It must not run while this demo is open. The script closes the app when finished.

## Company invoice template

The saved draft includes a sample company logo. Use **Manage invoice template** on the invoice page to change the logo, seller address, notes, payment instructions and payment terms. New invoices inherit saved defaults; **Apply saved template** updates only the editable invoice in view. The sample payment instructions are fictional and no payment is required.

## Preserved earlier profile

This demo uses `user-data-company-template`. The earlier `user-data` directory is preserved: it stalled during local engine startup after the ad hoc package rebuild. Its catalogue and authentication journal chains validated, but the startup cause remains unconfirmed. The fresh profile was tested through save, export and restart; no earlier authentication data was reset.
