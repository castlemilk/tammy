# Current technical state

This page is the source of truth for what the application exposes today. Design documents describe intent; this page describes the current development build.

## Capability status

| Area | Current state | Boundary |
| --- | --- | --- |
| Local workspace | Connected setup and reopen flow over a local SQLCipher database; workspace passphrase, one-time recovery-code confirmation, administrator sign-in, and session state are exercised. | Development uses process-memory security anchors and automatically resets only development attempt journals at core startup. |
| Organisation | Setup creates the sole Australian organisation with ABN, legal/display identity, Australia/Melbourne policy, AUD, non-cash GST, quarterly reporting, and a June year end. | Profile/security administration screens beyond initial setup are not connected. |
| Chart of accounts | A versioned AU small-business system/control chart is installed; ordinary accounts can be added. Protected accounts cannot be manually posted to. | Account update/archive UI and several accounting Settings routes remain deferred. |
| Journals and balances | Balanced two-sided manual journals, journal list/detail, and as-of trial balance are connected. | General ledger is currently a placeholder view. Opening-balance and period screens are not connected. |
| Documents | Local PDF, PNG, and JPEG intake up to 10 MiB, retained bytes, native PDF text extraction, candidate review, and reviewed-document list are connected. | There is no cloud OCR. Image text must be reviewed manually; supplier/payables workflow remains narrower than the broader design. |
| Banking | Signed CSV-style rows can be imported with an opening balance, explicitly matched, and completed as a reconciliation after all rows are matched. | This is a bounded local walkthrough, not bank-feed connectivity or automated matching. |
| GST and BAS | A build-version-pinned registry is connected before setup and on the current GST & BAS screen. It reports the 2024 Australian-business reviewed-document GST workpaper as available. | Complete BAS preparation, declaration, lodgement, and individual reporting remain unsupported. Workpaper status is always **Draft — not lodged**. |
| Overview and activity | Overview attention counts and a consolidated chronological activity screen read journals, banking lines, documents, and the current BAS draft. | The activity screen is a product projection, not a replacement for audit-chain export and verification evidence. |
| Backup, restore, and audit core | Encrypted backup/restore, authenticated recovery journals, audit-chain, and evidence-export components exist in the Go core and tests. | Their complete desktop workflows are not connected in this vertical slice. Do not infer UI availability from generated routes. |
| Packaging | Electron Forge development start, ad-hoc signed local macOS package, and packaged E2E commands exist. A separate fail-closed Mac App Store profile owns the bundle ID, icon, sandbox entitlements, privacy manifest, signing inputs, build number, repository check, and package orchestration. Apple Developer identifier `DXP9QHD7JH` and draft App Store Connect record `6800226692` now exist, with the safe product-page copy and categories seeded. | Apple distribution certificates/profiles, signed-build sandbox and privacy inspection, public privacy/support URLs, legal/export/price declarations, upload, and App Review remain external gates. |

## Architecture now

- Electron main supervises the bundled Go core and terminates the exact child process on shutdown.
- The renderer calls named protobuf methods through the preload boundary; Electron main forwards framed requests to the generated Connect handlers on an authenticated local transport.
- The Go core owns SQLCipher workspace opening, migrations, authorization, transactions, idempotency, accounting invariants, and local audit records.
- Module-owned repositories and typed ports preserve ownership boundaries. Files enter through bounded capability/byte interfaces rather than arbitrary renderer-supplied paths.
- Money is integer minor units in AUD; accounting and GST calculations avoid binary floating point.

## Development-only behavior

`pnpm desktop:start` passes `--development-memory-anchors` and a data root ending in `local-core-development`. The encrypted workspace and catalogue persist across normal restarts. Before each development core composition starts, the core removes only its two private attempt journals (`workspace-attempts.journal` and `identity-attempts.journal`) because their anchors are intentionally process-memory-only.

That reset prevents a stale development anchor from blocking restart. It also means development restart is not rate-limit durability evidence. Packaged mode uses `local-core` and does not request the development reset. Never copy this behavior into a production profile.

Local macOS packages are ad-hoc signed development artifacts. The current supported local evidence target is macOS arm64; hosted and external-platform claims require their own observed evidence.

## Deferred and external boundaries

Deferred repository work includes connecting the placeholder accounting/Settings routes and exposing the complete backup/restore and audit-evidence workflows. The repository-owned Mac App Store inputs and checks exist; the signed-build and external validation gates are listed in the [macOS App Store runbook](../release/macos-app-store.md).

External work includes Apple approval, legal/support/privacy completion, signing certificates and provisioning, ATO/SBR conformance, production credentials, and submission authorization. The Apple identifier and draft App Store record now exist; that setup does not replace the remaining external evidence.

## Design references

- [Local-first accounting and SBR design](../superpowers/specs/2026-07-19-tammy-local-first-accounting-sbr-design.md)
- [Core business accounting suite design](../superpowers/specs/2026-08-02-core-business-accounting-suite-design.md)
- [Accounting walkthrough UI design](../superpowers/specs/2026-08-09-accounting-tax-walkthrough-ui-design.md)
- [Application documentation and macOS release design](../superpowers/specs/2026-08-10-application-documentation-macos-release-design.md)
- [Current application documentation/release plan](../superpowers/plans/2026-08-10-application-documentation-macos-release.md)
- [macOS App Store release runbook](../release/macos-app-store.md)
