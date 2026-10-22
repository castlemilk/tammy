# BAS phase-three acceptance record

On 2026-10-23, BAS phase three (Tasks 1–11) is complete on clean source revision `9d36fbd05e5d2d4cc5fcadedce3b63c5fd5f3696`. The final quick gate, all sixteen packaged cases, independent export/provenance reconciliation, screenshot inspection and all 189 PDF-page reviews passed. Fresh complete identity/localproduct native race: `PASS: identity 85/85 and localproduct 121/121 cases with SQLCipher and -race; zero failures/skips; exit 0`. Independent final requirement/code-quality review and clean documentation commit: `independent requirement, code-quality and documentation reviews passed; ROOT committed the documentation, verified the clean worktree and unchanged accepted source trees, and retained the worktree for maintainer review`.

## Accepted source and package

- Clean repository source revision: `9d36fbd05e5d2d4cc5fcadedce3b63c5fd5f3696`; Go source tree: `9f152b047fa8676a26dea62b0af9a30d7575f823`; generated protobuf tree: `93e3d8c1452ef57ddc4b0841a3cf43c28ba7dae0`. The finance-owned source-reader directories and generated protobuf tree are unchanged since checkpoint `05477f1c`; identity and localproduct changed afterward and are covered by the fresh final native race below.
- Ordinary macOS arm64 package: `.tmp/electron-builder/ordinary/mac-arm64/Tammy.app`; verification record `.tmp/bas-acceptance/verification-2026-10-23/package-9d36fbd0-verification.json`, SHA-256 `74e0bb41a3c39ef421f792f046a71d8a1fa3ae8bf8126fc85fcbb6e2f5766f6e`. Executable digests: Tammy `b24a7efd2ec63c114839ea440f4756eaf714129e8ae586a581a63c0f4469a336`, Core `7474c18b995b181e414e8b6e3d82e253672afe34d82bbeafa5463fcdb436e526`, SBR helper `305bd9834a56573f420dbc6bb7224b3d2ecc22091aa30a4b1e11b6933bdedd82`, build manifest `c3174a1d32c612eaa20f456b99244221bd9dfd828b19a1de806d134da10c0a73`.
- Canonical packaged command: `rtk proxy env -u CI TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- task package:e2e`; log `/tmp/tammy-bas-closeout-full-package-settled-screenshots-2026-10-23.log`, SHA-256 `e556851c8806cb3aa1dce8a977407b3bbeee72dd2643df33decf402ef3d12062`. Result: all 16 cases passed in 12.8 minutes, zero failed/skipped/not-run/interrupted, zero retries, exit 0. Package integrity separately passed 7 checks with one expected Windows-only skip on Darwin arm64.
- The main journey completed the canonical-rule setup/legacy repair path, role/factor review and freeze, actual original and linked-revision PDF/CSV export, external records, restart/currentness and immutable frozen retrieval. The full suite also passed the existing foundation, invoice, purchase, reporting, and SBR guards.

## Final quick gate

Command: `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- task verify:quick`. The final quick gate passed on the same source: `/tmp/tammy-bas-closeout-final-quick-9d36fbd0-2026-10-23.log`, SHA-256 `62387a4fcb972bfe1dabad3c73cff89107766c2e07ad82983a61b4ab42798be7`, exit 0. It records 100 desktop files/1,228 passes; 757 script passes and one pre-existing conditional native-reproduction skip; 36 desktop-script tests; 32 invoice tests; 39 reporting tests; 32 local-API tests; default Core, type and generated-contract checks; lint with 135 warnings, 2 infos, and 0 errors. The single conditional script skip is separate from the packaged 16-case runtime, which had no skip.

## Prior native race evidence

Previously archived race-enabled native suite results provide module checkpoint evidence; the fresh final identity/localproduct run is recorded below:

| Scope | Result | Evidence |
| --- | --- | --- |
| `baspreparation` | `ok`, 673.627s | `/tmp/tammy-bas-closeout-native-bas-final-2026-10-22.log` |
| `purchases` | `ok`, 1440.481s | `/tmp/tammy-bas-closeout-purchases-final-2026-10-22.log`; sole accepted exclusion: `TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration` |
| `invoices`, `overview`, `app`, `authorisation`, `audit` | all `ok` | `/tmp/tammy-bas-closeout-native-support-final-2026-10-22.log` |
| `accounting`, `reporting`, `artefacts`, migrations, SQLCipher storage | all `ok` | `/tmp/tammy-bas-closeout-storage-source-final-2026-10-22.log` |
| `localproduct` real-TLS JSONL checkpoint | 118/118 tests passed, zero failures/skips; `ok` in 1182.803s (1182.805s package action) | `/tmp/tammy-bas-localproduct-05477f1c-2026-10-22.jsonl` |

The final unfiltered identity/localproduct race-enabled SQLCipher run is recorded as `PASS: identity 85/85 and localproduct 121/121 cases with SQLCipher and -race; zero failures/skips; exit 0`. Command: `rtk proxy env TMPDIR=/private/tmp GOFLAGS=-p=1 mise exec -- go test -json -race -tags tammy_sqlcipher ./internal/identity ./internal/localproduct -count=1 -timeout=60m`, from `services/core`; JSONL log: `/tmp/tammy-bas-native-identity-localproduct-final-9d36fbd0-2026-10-23.jsonl`. The run includes four regressions: `TestSQLCipherValidateAuthorizationProgressesWhileAssertTOTPOwnsIdentityMutex` verifies caller-transaction authorization progresses while a real TOTP assertion owns the identity mutex; `TestBeginBASReadAllowsConcurrentWriter` verifies retained BAS reads pin a deferred snapshot without blocking an independent writer; `TestGetCurrentBASDraftReadsWhileWriterIsHeld` verifies the legacy current-workpaper reader completes from WAL with a warmed encrypted connection; and `TestGetCurrentBASDraftRejectsSessionRevokedAfterPreflight` verifies `read_financial` is rechecked in the read snapshot after session revocation. The race run has no skip filter. The accepted purchase stress exclusion is recorded above; no new closeout regression is skipped.

## Packaged artifact manifest, independent checks, and visual review

The successful-run manifest is `.tmp/bas-acceptance/9d36fbd05e5d2d4cc5fcadedce3b63c5fd5f3696/19a6f3f3ab55f3f5-1792685338910-0/bas-success-artifact-manifest.json`, SHA-256 `c3ec196c5d24da1291ef6fbd9e6428dce9a98232c30f144d75639890db8876a8`. Its source revision matches the clean repository revision above. The independent checker log `.tmp/bas-acceptance/verification-2026-10-23/gates/independent-artifact-check-final-9d36fbd0.log`, SHA-256 `1063d15fe1f99464fc445054d88933a072fc314183ef96b29be1dcb3bd7ee759`, verified exact sizes and SHA-256 for all four exports and eight screenshots. It reconciled both CSVs against all 18 label expectations, exact traces, source coverage/provenance, exact frozen/calculation identities and hashes, distinct PAYGI profile-confirmation/current-notice evidence, retained revision reasons and all 18 predecessor deltas.

| Export | Bytes | SHA-256 | Frozen revision | Calculation revision |
| --- | ---: | --- | ---: | ---: |
| `bas-original.pdf` | 1,152,843 | `958abe3928ea8dcf78c5eaa0d425ceec5be21b89b005299a4ffd602433216c0c` | 1 | 1 |
| `bas-original.csv` | 130,960 | `7d677e17d0cf1926d76a7729d7fbac77d82990bf7faf72f14d3d7acdcfe3f879` | 1 | 1 |
| `bas-revision.pdf` | 1,777,442 | `dda1c1bde96993da94141ce4d0d47838361579b1721f40eb4ab24cedc5fcdef1` | 1 | 1 |
| `bas-revision.csv` | 223,466 | `a3a2801bdf5214a4d1caca7063caa1a440d76f6a377e96760feea57afd20143d` | 1 | 1 |

Frozen/calculation IDs and individual SHA-256 identities are recorded in the manifest and CSVs. The linked revision retains the exact reason: “Linked revision after a new Q3 taxable receipt; preserve the confirmed original handoff.” For clarity, the original pair is frozen `01a1645d-a75f-7783-88bb-3bdeb7021dbb` revision 1 (SHA-256 `c4aee2a6db5541fdddef8651f9686eecf80012f5e02de01ac66014f6dbd52b71`) and calculation `01a1645d-2efc-793c-bcda-4b649fa9a7fa` revision 1 (SHA-256 `cf0210cb8ba01352a587b35cf68acb9c3371b1409949d455a31ead888142c98d`). The linked revision pair is frozen `01a1645e-d232-7143-80a2-5eabe332d1f5` revision 1 (SHA-256 `b26bdfc65eb56fa3af3ab4d18ea3bc5961fcaa4b0dcd92ee851a8e8202be8e4d`) and calculation `01a1645e-a38a-70d3-a7b9-0efc6554c32b` revision 1 (SHA-256 `e814f555f2bad3da8122b488e694508e0cb6af09e9fc3e25af4e171dcf2e878e`). The automatic PDF inspection log `/tmp/tammy-bas-pdf-inspection-final-9d36fbd0-2026-10-23.log` reports 189 rendered pages (74 original, 115 revision) and zero rendering errors. All pages and all eight screenshots were visually reviewed; `inspection/visual-review-index.json` has SHA-256 `4ef2872e95588e42d47fcdf4b319204fcb01a09d020f83bd68c28cbcce353a91`. The index reports zero blank pages, clipped characters, invalid glyphs, or material findings. It records one nonblocking pagination note: revision appendix page 115 contains the final two JSON closing braces and footer; content is complete and legible, and the immutable export was retained unchanged. All eight screens were legible and meaningful, including the settled unverified external-history observation; no TOTP digits or credentials were visible.

The independent visual review is separately evidenced in `.tmp/bas-acceptance/9d36fbd05e5d2d4cc5fcadedce3b63c5fd5f3696/19a6f3f3ab55f3f5-1792685338910-0/inspection/` by the original 74-page review, revision pages 1–60 (`revision-pdf-pages-001-060-visual-review.json`, SHA-256 `b22d36f942c810e5ff27c40a220616ab100f40bddb83207d1989bbb03470db10`), revision pages 61–115, screenshot review, and summary. The CSV checker is not a substitute for PDF rendering or the packaged run; those gates passed separately as cited above.

## Exact original and linked-revision values

Values below are exact cents (with reported whole dollars in parentheses); `T2` is the exact rate, `T7` remains unset, and `5B` remains not applicable. The status-only rows have no exact numeric value.

| Label | Original | Linked revision | Change |
| --- | --- | --- | --- |
| G1 | KNOWN 16000/1¢ ($160) | KNOWN 18200/1¢ ($182) | +2200/1¢ |
| 1A | KNOWN 1000/1¢ ($10) | KNOWN 1200/1¢ ($12) | +200/1¢ |
| 1B | KNOWN 500/1¢ ($5) | KNOWN 500/1¢ ($5) | 0¢ |
| W1 | KNOWN 100000/1¢ ($1,000) | KNOWN 100000/1¢ ($1,000) | 0¢ |
| W2 | KNOWN 5000/1¢ ($50) | KNOWN 5000/1¢ ($50) | 0¢ |
| W3 | KNOWN_ZERO 0/1¢ ($0) | KNOWN_ZERO 0/1¢ ($0) | 0¢ |
| W4 | KNOWN_ZERO 0/1¢ ($0) | KNOWN_ZERO 0/1¢ ($0) | 0¢ |
| W5 | KNOWN 5000/1¢ ($50) | KNOWN 5000/1¢ ($50) | 0¢ |
| 4 | KNOWN 5000/1¢ ($50) | KNOWN 5000/1¢ ($50) | 0¢ |
| T1 | KNOWN 201099/1¢ ($2,010) | KNOWN 201099/1¢ ($2,010) | 0¢ |
| T2 | KNOWN 17/10 | KNOWN 17/10 | 0 rate units |
| T7 | UNSET | UNSET | state-only; exact value unavailable |
| T11 | KNOWN 3417/1¢ ($34) | KNOWN 3417/1¢ ($34) | 0¢ |
| 5A | KNOWN 3417/1¢ ($34) | KNOWN 3417/1¢ ($34) | 0¢ |
| 5B | NOT_APPLICABLE | NOT_APPLICABLE | state-only; exact value unavailable |
| 8A | KNOWN 9417/1¢ ($94) | KNOWN 9617/1¢ ($96) | +200/1¢ |
| 8B | KNOWN 500/1¢ ($5) | KNOWN 500/1¢ ($5) | 0¢ |
| 9 | KNOWN 8917/1¢ ($89), PAYABLE | KNOWN 9117/1¢ ($91), PAYABLE | +200/1¢ |

These rows are the checker’s expected exact values, not a substitute for its recorded output or the required visual review.

## Supported boundary and non-claims

The implemented local subset covers Simpler GST on matching monthly or quarterly statement periods, with cash or non-cash GST accounting, small/medium PAYGW, and notified-amount or notified-rate PAYGI paths without variation. Unknown, unsupported, unresolved and unclassified obligations remain visible blockers. PAYGI’s confirmed income-year tuple and PAYGW category/prefill are bound to the exact profile snapshot retained by the calculation; current PAYG notice and payroll evidence remain separate. Business income is a comparison only and never supplies PAYGI T1. Account TOTP enrollment is independent of SBR readiness; the known legacy bundled-rule-reference repair is explicit, administrator-only, version-checked, reasoned, future-dated and protected by a fresh factor. It does not rewrite frozen history.

External confirmations are user-recorded and unverified. This local workflow does not lodge or transmit a BAS to the ATO and makes no claim of ATO receipt or official acceptance.

## Final verification and review record

- Fresh identity/localproduct race result and source/tree verification: `PASS: identity 85/85 and localproduct 121/121 cases with SQLCipher and -race; zero failures/skips; exit 0`.
- Independent requirement/code-quality review and final documentation commit: `independent requirement, code-quality and documentation reviews passed; ROOT committed the documentation, verified the clean worktree and unchanged accepted source trees, and retained the worktree for maintainer review`.

Prior full race suites provide the module evidence listed above. The sole accepted purchase public-posting stress exclusion is `TestPurchaseReportingCaptureAccepts1000ModernRootsAndRejects1001BeforeHydration`; the fresh identity/localproduct run includes all four named concurrency/revocation regressions without a skip filter.

## Fresh native run details

The complete unfiltered command exited 0 on the accepted Go/protobuf trees. Counts include subtests; top-level function counts are shown separately. No test was skipped.

| Package | Passed cases | Top-level functions | Elapsed seconds |
| --- | ---: | ---: | ---: |
| `identity` | 85 | 57 | 562.670 |
| `localproduct` | 121 | 46 | 1164.130 |

The retained native JSONL SHA-256 is `6952a2b69dc5ed5b1cb166e1240cbd315118f16eb13094600196fa232ef6ba86`; its summary is `.tmp/bas-acceptance/verification-2026-10-23/native-identity-localproduct-final-9d36fbd0-summary.json` (SHA-256 `67c62d2f08fe69289667e2b2805ea92db4acc6258dd0e01508e01d440fb9a464`). Each of the four named new regressions has its own passing event in that log.

The packaged app is retained at `.tmp/electron-builder/ordinary/mac-arm64/Tammy.app`. Command logs remain at their cited `/tmp` paths, with hashed copies retained under `.tmp/bas-acceptance/verification-2026-10-23/gates`. Export files, renderings and review JSON are retained under the accepted source/run directory in `.tmp/bas-acceptance`. This local review evidence is preserved with the worktree. The later documentation-only commit does not change the accepted application, Core or protobuf source.
