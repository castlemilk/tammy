# Desktop feedback and Soft Precision completion

Date: 23 October 2026. Scope: the user's feedback on the running 216a1fbe Electron package.

## Approved references and scope

The user requested completion of the existing uplift. DESIGN.md and the Soft Precision pack at git revision 19533020422b6ea2f0c77dc3117a051483f9f9ce are the visual authority. Boards 03, 08, 09, 11, 13 and 14 guide components, working views, accounting, arrival/recovery, sign-in and BAS review. Boards 07 and 10 may guide already implemented invoice/purchase views; they do not authorise new business functionality. Generated examples and planned backup, live feed, transfer, automation or lodgement controls are not capabilities.

Retain the warm palette, forest actions, serif headings, tabular financial values and existing paper-loop arrival art. Complete the actual layouts and interaction states across every connected route, including setup, unlock, recovery, privacy, overview, documents, banking, invoices, accounts, journals, ledger empty state, trial balance, audit, BAS, SBR, settings and local diagnostics. Shared page/section/header primitives should replace divergent presentation. Tables own horizontal scrolling; forms retain natural vertical scrolling. Verify 320/390/900/1440 widths, focus, keyboard and reduced motion.

## Onboarding and secrets

Use the approved wider arrival/workspace layout with clear business and security groups. Keep workspace creation, recovery confirmation, sign-in and organisation creation on their existing named protobuf commands and preserve recovery secrecy. Password/passphrase fields show live, field-associated format feedback and explicit confirmation before creation. Mirror the actual Core format policy: NFC, 15–128 Unicode code points, valid printable Unicode and the 1,024-byte input bound. Preserve spaces; never log, cache or persist input secrets. Do not claim entropy, common-password or history checks performed by the client. Existing-password inputs must not reject a valid 15-code-point credential. External SBR credentials keep their own rules. All secret fields remain masked by default.

Normalise pasted ABN whitespace and conventional separators without truncating the clipboard. Reject letters, non-ASCII digits, wrong length and failed mod-89 checks with inline correction guidance before any workspace mutation. The user's example normalises to 52682863690 and passes the Core/ABR checksum; preserve those digits exactly. A checksum is not registry verification.

## ABN Lookup

The official API requires a registered GUID and no provider is configured. Provide an explicit browser lookup of the valid entered ABN at https://abr.business.gov.au/ABN/View?abn=CANONICAL. Tell the user that clicking shares that ABN with ABR in their browser, requires internet, and does not fill or verify organisation data in Tammy. No request occurs while typing. Keep renderer networking disabled. Main only opens exact approved public URLs or this fixed origin/path with one checksum-valid canonical ABN; reject lookalikes, extra parameters, fragments, credentials and other schemes. Keep manual ABR extracts as human-recorded evidence.

## Privacy and support

Use PRIVACY.md as the canonical policy consumed by both the site and app through the existing generated structured content. Expand actual data categories, local storage/imports, explicitly enabled local API/MCP recipients, retention/removal, immutable corrections, support and website handling, access/correction requests, complaints and security incident contact. Retain the existing Gamma Systems Pty Ltd and published email; do not invent addresses, telephone numbers, service-level promises or regulatory approval.

Explain RAM as Relationship Authorisation Manager, and distinguish business authority/machine credentials from DSP OSF, product registration and production access. State the current synthetic local readiness and preparation-only BAS boundary accurately. Privacy text cannot enable SBR production or certify compliance. Provide official ATO/RAM/ABR/OAIC resources. Full policy belongs on its own readable screen; Settings gets a compact summary with a working route to the full policy. Website source may change, but no external publication is included.

## Acceptance

Behaviour regressions must demonstrate RED/GREEN for paste normalisation, checksum preflight, live password format/confirmation and safe browser handoff. Existing accounting, authorization, exact money, provenance, currentness, export, idempotency and encrypted persistence assertions remain. Run quick/contracts/macOS gates on a clean committed revision, all packaged cases without retries, and an added onboarding/visual journey where useful. Review fresh screenshots for every connected screen and both narrow layouts, rebuild the exact package and leave it running for the user at setup. Preserve the user's existing local workspace; use isolated synthetic data for tests.
