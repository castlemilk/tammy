# Original hosted row-name failure and focused correction

Exact head `6ad30c5e708235b18883d55fc48b6588b4b2cd55` had four successful
Ubuntu/Windows jobs and two failed original macOS jobs, each on attempt 1:

| Original execution | Result |
| --- | --- |
| [Push 40028904933 /120671939602](https://github.com/castlemilk/tammy/actions/runs/40028904933/job/120671939602) | Five passed, case six failed in 31.0s, 14 not run |
| [PR 40028912826 /120671966645](https://github.com/castlemilk/tammy/actions/runs/40028912826/job/120671966645) | Five passed, case six failed in 33.0s, 14 not run |

Case six is the existing invoice/purchase/BAS workflow. The missed assertion at
`current-workflows.spec.ts:1729–1730` matched the entire row's old accessible name.
The new semantic description button contributes its explicit action name, making
the actual row name `30/09/2026 Open journal: Invoice INV-MCP-001 $660.37 Posted`.
This is a test adaptation missed during the initial clickable-row migration.

The original [PR artifact](https://github.com/castlemilk/tammy/actions/runs/40028912826/artifacts/12534801528)
was downloaded once and authenticated against its supplied SHA256
`eaa28beefa4d67f0eb86f38a88a5fb8383b20a9aa31ad14377c837352b8e57b5`.
Only its inspected [safe synthetic failure frame](ci-row-followup/original-pr-failure.png)
is retained here. It shows the correct invoice, date, AUD 660.37 and Posted state.
The accessibility context was inspected only for that synthetic invoice row;
the archive, raw context, traces, runtime logs and authentication/setup material
are not copied into the repository.

The correction locates the unique row containing the exact `Open journal:
Invoice INV-MCP-001` button, requires exactly one row, then checks the date,
amount and Posted cells individually with exact accessible names and visibility.
The preceding native `expectInvoiceJournal` assertion still verifies exact income
60033, receivable 66037 and GST 6004 minor units. No financial assertion is removed,
and no application handler, timeout, retry, worker or job policy changes.

The existing component case also verifies date, amount and state in the semantic
description button's own retained row before keyboard activation. All **10 Journals
component tests pass in 2.38s**. The single affected native invoice/purchase/BAS
case passes **22.4s /23.6s including runner**, zero retries, against the existing
verified ordinary runtime `ffc2fdaa7e8a74092d78025107df4a620516c369`.
It was not rebuilt: application source is unchanged. The test-only working harness
hash and original artifact/frame provenance are in [evidence.json](ci-row-followup/evidence.json).

Desktop typecheck and repository lint pass: **667 files, 135 existing warnings,
12 infos**, all **55 archives** verified. Every **786 previously tracked output
PNG** remains byte-identical; the one newly retained safe failure frame makes the
expected total **787**. Original before/reference/after and Library deliveries
remain unchanged. Exact corrected-head hosted qualification is still required and
will be recorded in the draft PR rather than inferred from this local pass.

No retry or manual dispatch of the failed hosted runs was requested. Their original
results remain failed; new ordinary push/PR executions will qualify the correction.
Draft, merge and publication holds and the previously documented financial,
audit and platform limits remain.
