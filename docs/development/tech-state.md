# Current technical state

This page is the source of truth for what the application exposes today. Design documents describe intent; this page describes the current development build.

## Capability status

| Area | Current state | Boundary |
| --- | --- | --- |
| Local workspace | Connected setup and reopen flow over a local SQLCipher database; workspace passphrase, one-time recovery-code confirmation, administrator sign-in, and session state are exercised. | Development uses process-memory security anchors and automatically resets only development attempt journals at core startup. |
| Organisation | Setup creates the sole Australian organisation with ABN, legal/display identity, Australia/Melbourne policy, AUD, a choice of cash or non-cash GST, quarterly reporting, and a June year end. | Profile/security administration screens beyond initial setup are not connected. |
| Chart of accounts | A versioned AU small-business system/control chart is installed; ordinary accounts can be added. Protected accounts cannot be manually posted to. | Account update/archive UI and several accounting Settings routes remain deferred. |
| Journals and balances | Balanced two-sided manual journals, journal list/detail, and as-of trial balance are connected. | Opening-balance and period screens are not connected. |
| Purchases | A human confirms reviewed-source allocations before posting; immutable purchase postings and dated partial payments settle payables once. Cash GST follows eligible paid portions, non-cash GST is claimed once, and private/capital/evidence/legacy/deferral outcomes remain explicit. | No supplier register or broad bill approval, opening-payable settlement, supplier credits, refunds/reversals/corrections, bank matching, ageing, or complete BAS lifecycle. |
| Documents | Local PDF, PNG, and JPEG intake up to 10 MiB, retained bytes, native PDF text extraction, candidate review, and reviewed-document list are connected. | There is no cloud OCR. Image text must be reviewed manually; supplier/payables workflow remains narrower than the broader design. |
| Invoices | Editable line items and reviewed expense costs, explicit GST registration, exact AUD totals, encrypted versioned drafts, immutable issued invoices, and offline pdfcn-based PDFs are connected. MCP can stage a proposal for review in the editor. | Issue atomically posts business sales, receivables and basis-aware GST. Dated partial/full receipts reduce balances; older unposted invoices require explicit review and linking. Delivery, credit notes, refunds and recurring billing remain unavailable. Costs and margins never appear in customer PDFs. |
| Banking | Signed CSV-style rows can be imported with an opening balance, explicitly matched, and completed as a reconciliation after all rows are matched. | This is a bounded local walkthrough, not bank-feed connectivity or automated matching. |
| GST and BAS | Local 2024–2027 quarterly GST workpapers include invoice sales, reconciled purchase sources, typed legacy/unresolved/deferral review, retained provenance and explicit stale-source regeneration. PDF/CSV exports preserve the displayed saved identity, all bounded rows, exact figures and warnings. Cash purchase credits require eligible payment evidence. | BAS preparation/freeze, complete source coverage across all domains, PAYG, declaration, ATO/SBR lodgement, and individual reporting remain unsupported. Workpaper status is always **Draft — not lodged**. |
| Overview and activity | Overview shows complete posted revenue, invoice income, receipts and as-of balances, alongside attention counts; native PDF/CSV actions export the selected income summary with its independent balance date and revision. A consolidated chronological activity screen reads journals, banking lines, documents, and the current BAS draft. | Income export is a summary. The activity screen is a product projection, not a replacement for audit-chain export and verification evidence. |
| Local API and MCP | Opt-in native-menu pairing grants the current signed-in organisation's 14 read tools, one invoice proposal tool, and 10 MCP resources through an authenticated loopback broker and stdio adapter. | A private mode-0600 connection file carries session-scoped access. Revocation, sign-in, workspace unlock, or quit invalidates it. Lists are bounded to the first 200 entries (20 for invoices). Persistent writes remain unavailable; proposals are staged in memory for explicit human review and save. |
| Backup, restore, and audit core | Encrypted backup/restore, authenticated recovery journals, audit-chain, and evidence-export components exist in the Go core and tests. | Their complete desktop workflows are not connected in this vertical slice. Do not infer UI availability from generated routes. |
| SBR readiness | The macOS arm64 build has an authenticated, network-disabled simulator; TOTP-gated RAM machine-credential lifecycle UI; redacted readiness checks; and packaged fixture coverage. | EVTE is blocked pending externally issued signed registration, component, endpoint, service, and access inputs. There is no production SBR path and no BAS submit or lodge action. |
| Packaging | electron-vite produces explicit main, preload, and app bundles. electron-builder produces ordinary macOS arm64 and Windows x64 directory applications; local package E2E authenticates the app, ASAR, fuses, resources, runtime, and exact-process cleanup. A separate fail-closed Mac App Store profile owns the bundle ID, icon, sandbox entitlements, privacy manifest, signing inputs, build number, repository checks, and deterministic release root. Apple Developer identifier `DXP9QHD7JH` and draft App Store Connect record `6800226692` exist, with safe product-page copy and categories seeded. | Hosted target results and Apple certificates/profiles, signed-build inspection, legal/export/price declarations, upload, processing, and App Review remain external gates. |

## Architecture now

- Electron main supervises the bundled Go core and terminates the exact child process on shutdown.
- The app calls the fixed window.conveyor bridge. Its system, workspace, organisation, accounting, banking, documents, purchases, reporting, and sbr modules expose only committed query/command methods; the preload provides no stream, event, Node, or raw transport primitive.
- Electron main authorizes the exact live main frame before routing a Conveyor call and forwards typed protobuf requests to the generated Connect handlers over the authenticated local transport.
- Electron main owns opt-in local API pairing, a fixed read allowlist, and a bounded invoice-proposal operation. The stdio MCP adapter uses the private connection file to call the loopback broker; it does not expose raw Core transport or renderer access. See [Local API and MCP](local-api-mcp.md) and [Invoice builder](invoice-builder.md).
- The Go core owns SQLCipher workspace opening, migrations, authorization, transactions, idempotency, accounting invariants, and local audit records.
- Module-owned repositories and typed ports preserve ownership boundaries. Files enter through bounded capability/byte interfaces rather than arbitrary renderer-supplied paths.
- Money is integer minor units in AUD; accounting and GST calculations avoid binary floating point.
- Purchases owns immutable posting, payment, evidence, attribution, deferral, and legacy-review facts. Reporting reads the complete purchase-owned source snapshot through purchases.ReportingPort rather than Documents pages. See [purchase reporting source contract](purchase-reporting-sources.md).
- Income/BAS exports acquire and validate immutable Core snapshots in main, retain the captured session across asynchronous work, and use a bounded mode-0600 atomic native writer shared with invoice export. CSV preserves Unicode; PDF explicitly rejects unsupported text. See [packaged export acceptance](income-bas-exports-acceptance-2026-10-06.md).

electron-vite writes `apps/desktop/out/main/index.js`, `apps/desktop/out/preload/index.cjs`, and `apps/desktop/out/renderer/index.html`. Ordinary Builder outputs are `.tmp/electron-builder/ordinary/mac-arm64/Tammy.app` and `.tmp/electron-builder/ordinary/win-unpacked/Tammy.exe`; deterministic Mac App Store runs use `.tmp/macos-release/<version>/build-<number>/` instead.

## Development-only behavior

`pnpm desktop:start` passes `--development-memory-anchors` and a data root ending in `local-core-development`. The encrypted workspace and catalogue persist across normal restarts. Before each development core composition starts, the core removes only its two private attempt journals (`workspace-attempts.journal` and `identity-attempts.journal`) because their anchors are intentionally process-memory-only.

That reset prevents a stale development anchor from blocking restart. It also means development restart is not rate-limit durability evidence. Packaged mode uses `local-core` and does not request the development reset. Never copy this behavior into a production profile.

Local macOS packages are ad-hoc signed development artifacts. The current supported local evidence target is macOS arm64; hosted and external-platform claims require their own observed evidence.

## Deferred and external boundaries

Deferred repository work includes connecting the remaining accounting/Settings routes and exposing the complete backup/restore and audit-evidence workflows. Company EOFY preparation and the company tax return remain backlog work. The repository-owned Mac App Store inputs and checks exist; the signed-build and external validation gates are listed in the [macOS App Store runbook](../release/macos-app-store.md).

External work includes Apple signing, upload, processing, and review; ATO/SBR production lodgement; DSP registration; conformance; production credentials; and submission authorization. The Apple identifier and draft App Store record exist; that setup does not replace the remaining external evidence.

For SBR, the authenticated simulator proves only local credential and transport-state behavior. EVTE cannot launch until its signed external evidence is installed and accepted. Actual ATO credential-component integration follows under a separately approved implementation after issuance; editable repository examples cannot enable it. See [Local SBR readiness](sbr-local-readiness.md).

## Design references

- [Local-first accounting and SBR design](../superpowers/specs/2026-07-19-tammy-local-first-accounting-sbr-design.md)
- [Core business accounting suite design](../superpowers/specs/2026-08-02-core-business-accounting-suite-design.md)
- [Accounting walkthrough UI design](../superpowers/specs/2026-08-09-accounting-tax-walkthrough-ui-design.md)
- [Application documentation and macOS release design](../superpowers/specs/2026-08-10-application-documentation-macos-release-design.md)
- [Current application documentation/release plan](../superpowers/plans/2026-08-10-application-documentation-macos-release.md)
- [macOS App Store release runbook](../release/macos-app-store.md)
