# Invoice income and BAS acceptance

Validated on 4 October 2026 using the ordinary macOS arm64 package built from clean committed source revision `c9e2631b715f892c4284cbc2693a29496ac15d8d`. All four packaged end-to-end tests passed in 5.6 minutes.

## Verified behavior

- Issuing an invoice atomically posts receivables, revenue excluding GST, and basis-aware GST. Drafts contribute no income. Private costs remain pricing metadata; payments do not post revenue a second time.
- Overview reports complete posted business revenue, invoice income, dated receipts, and invoices owing. Income periods and the balance date are independent, so earlier unpaid invoices remain visible.
- Non-cash GST sales follow issue dates. Cash GST sales follow actual receipt dates and exact line allocations. BAS drafts retain invoice references, dates, GST basis, saved figures, and source-change notices; regeneration updates the selected period.
- Older issued invoices remain unposted until explicitly reviewed and added to accounts. Authenticated pagination reaches invoices beyond the first 200, while totals aggregate all retained sources.
- Existing encrypted workspaces upgrade on authenticated opening. Real TLS restart/unlock tests retained an issued invoice, its journal and payment balances, and saved BAS sources, then regenerated and read a new draft. Locked startup and an incorrect passphrase left database bytes unchanged.
- Current-schema opens retain shared access under an active writer. An older workspace held open elsewhere, missing files, wrong keys, altered migration checksums, and newer schemas fail closed. Deterministic tests cover source removal immediately before SQLite opening without recreating an empty workspace.

## Packaged accounting examples

The non-cash acceptance invoice is issued on 30 September 2026 for $600.33 excluding GST, $60.04 GST, and $660.37 total. Q3 income and receivables reflect the posting; its BAS shows G1 $660.37 and 1A $60.04. A $330.18 receipt on 2 October leaves $330.19 owing. Q4 Overview shows no additional invoice income, the receipt and remaining balance; Q4 BAS adds no sales or GST from that receipt. Restart retains the posting and receipt.

The cash acceptance invoice contains taxable, GST-free, and out-of-scope lines: $170.00 excluding GST, $10.00 GST, and $180.00 total. Issue posts $170.00 revenue and deferred GST, with no unpaid cash-basis BAS sales. A $90.00 receipt contributes G1 $80.00 and 1A $5.00. Final settlement and regeneration produce G1 $160.00 and 1A $10.00, excluding the out-of-scope line. Q4 receipts total $180.00, income is not duplicated, and the invoice balance is zero. Restart retains both receipts, exact allocations, and BAS figures.

The accounting workflow also exercises reviewed expenses, the invoice table at desktop and compact sizes, company-logo/default changes, draft and issued PDF exports, immutable invoice branding, real HTTP/stdin MCP proposals, balanced journals, and trial balances.

## Validation results

| Check | Result |
| --- | --- |
| Desktop Vitest | 802 tests across 75 files passed |
| Repository and desktop script tests | 780 passed, one opt-in SQLCipher reproducibility check skipped, no failures |
| Invoice calculation/PDF tests | 32 passed |
| Local API tests | 32 passed |
| Generated-client contract tests | 24 passed |
| Desktop, invoice, local API and generated-client TypeScript | Passed |
| Protobuf formatting, lint, breaking compatibility, generation and contract checks | Passed |
| Go default suite | Passed |
| SQLCipher race coverage | All packages passed across the full run and focused reruns |
| Final SQLCipher, app and local-product race suites | Passed; 3.917s, 27.115s and 21.665s respectively |
| Full final workspace regression suite | Passed; 104.902s |
| Packaged accounting and invoice workflows | Passed; 16.6s |
| Packaged first-run journey | Passed; 1.6s |
| Packaged cash invoice/receipts/BAS/restart | Passed; 11.3s |
| Packaged SBR simulator regression | Passed; 5.1 minutes |
| Package signature, fuses, ASAR/core/resource checks | Seven passed; Windows check skipped on macOS |
| Repository lint and whitespace | No errors; 23 existing lint warnings and two infos |
| Independent implementation and upgrade review | No outstanding findings |

The initial full race run hit the default ten-minute package timeout while running real Argon2 security tests in identity and workspace. Both passed with a thirty-minute test budget: 580.657s and 674.430s. Security parameters were unchanged. A test helper exited before consuming its profile descriptor; the fixture now reads that descriptor as the real helper does, and focused repetition plus the race rerun passed. Production SBR behavior was unchanged, and the normal packaged SBR test passed on the final build.

Regression tests reproduce and cover delayed BAS loading overwriting edited period dates, timestamp-prefix and clock-ordering mistakes in current draft selection, the production migration target, and existing-workspace upgrade admission. Durable committed save order survives replay, VACUUM, and restart.

## Packaged artifact and evidence

- App SHA-256: `df609fad244d0ddfa96fcee317823af15f7795e87d0013a74596ccbb137b63ab`
- Core SHA-256: `2b5832407c0740ecae498b9cde29a87fccede4388bf122f14cbe2f34ab45f915`
- Manifest SHA-256: `a6f95f7753651536f2bd364466e210d24d55ace84d91e2eea3111b122008ee72`

The package is `.tmp/electron-builder/ordinary/mac-arm64/Tammy.app`. Screenshots are retained under `.tmp/invoice-income-bas-e2e/` and `.tmp/invoice-cost-table-e2e/`; these generated artifacts are intentionally ignored by Git. The final cash/non-cash BAS and Overview screenshots were visually inspected: totals, reporting labels, dates and source references are readable without overlap. Five screenshots and their hashes are also preserved at `/Users/benebsworth/projects/tammy/output/tammy-invoice-income-bas/`. Validation logs use `/tmp/tammy-income-…`, including `e2e-final`, `package-final`, `desktop-tests-final`, `scripts-final`, `go-default-final`, `go-race-final`, `go-race-recheck`, `upgrade-race-final`, and `workspace-upgrade-regression`.

## Current limits

This is local GST workpaper support for 2024–2027, without full BAS declaration/lodgement or a company-tax-return workflow. Cash purchase credits require payment evidence that the reviewed-expense flow does not retain; those credits are excluded with a visible warning. Deposits before issue, credit notes, refunds, and invoice reversals need separate typed workflows.

BAS rejects periods exceeding 1,000 sales sources or 1,000 purchase sources rather than truncating totals. The invoice accounting panel displays the latest 200 receipts; payment counts, balances, and root business totals include all receipts. Historical GST-basis changes or missing version evidence require review instead of guessed attribution.

The existing fictional sample workspace remains preserved with its three reviewed expenses, company template/logo, and editable $160.50 draft. Acceptance used separate disposable profiles.
