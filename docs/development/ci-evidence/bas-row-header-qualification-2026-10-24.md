# BAS row-header qualification correction

Failed review head: `53dbd5f4dc9fdeb947c134989e195fcc55ee5dd2`. Its original automatic push [40051674411](https://github.com/castlemilk/tammy/actions/runs/40051674411) and PR [40051677304](https://github.com/castlemilk/tammy/actions/runs/40051677304) finished with four passing Linux/Windows jobs and two failing macOS jobs. These failures are retained, not counted as green or erased by a retry.

## Demonstrated cause and narrow correction

The approved UI block changed BAS label cells to semantic `th scope="row"`. The pre-existing financial linked-revision journey still filtered for a `cell` named G1 and indexed cells as if that label were a data cell. Both original macOS jobs failed on that exact locator while expecting `18200/1`; no amount mismatch was observed. This stale locator was missed in the initial focused pass.

The correction finds the label by `rowheader`, requires exactly one matching row and exactly five data cells, then checks data-cell indexes 1/2 for exact cents /reported whole dollars. G1 expectations remain `18200/1` and `182`; label 9 remains `9117/1` and `91`. No financial amount, arithmetic, timeout, retry, source lookup, permission, approval or export assertion is weakened. No application source changes or package rebuild are required for this test-only correction.

Original [push macOS 120745496366](https://github.com/castlemilk/tammy/actions/runs/40051674411/job/120745496366): main packaged suite 3 passed /1 failed in 3.9m; failing case 3.1m. Original [PR macOS 120745504838](https://github.com/castlemilk/tammy/actions/runs/40051677304/job/120745504838): 3 passed /1 failed in 3.4m; failing case 2.9m. Remaining main cases were not executed after the first failure; they are not counted as skipped or passed. Separate development workflow passed in both jobs.

The corrected single existing BAS acceptance case passed locally in **3.4m, one passed /zero skipped /zero retried** against the same verified ordinary package from clean application source `fe0d0123bfb1ffcc933fc2b38cdd064f917844f4`. Its exact cash/PAYGI, frozen handoff, linked revision, PDF/CSV export, cancellation and immutable-record assertions remain. Full runtime hashes are in the BAS capture folder. Runtime app bytes and all 40 new captures /860 prior PNGs remain unchanged. Private runtime logs and any authentication material are not committed.

## Original queue/setup/execution measurements

| Original job | Queue | Setup before canonical | Canonical execution |
| --- | --- | --- | --- |
| Push Ubuntu 120745496400 | 2s | 107s | 447s |
| PR Ubuntu 120745505026 | 1s | 89s | 341s |
| Push macOS 120745496366 | 3s | 76s | 677s |
| PR macOS 120745504838 | 2s | 49s | 472s |

Both Ubuntu jobs passed 1,341 tests /111 files, typecheck, lint (701 files, 135 warnings /12 infos), descriptor checks and all 55 retained archive hashes. Suites took 247.38s /186.88s. ASAR files passed in 4,074ms /4,057ms; measured harness lifecycles were 4,029.588ms /4,023.821ms. Body entry was 231.745ms /289.159ms, security import 62.856ms /47.056ms, readiness plus profile validation 3,493.856ms /3,496.195ms, protocol setup 10.927ms /9.375ms, and exit request through close 187.847ms /148.826ms. The earlier ASAR deadline failure did not recur; these successful executions do not demonstrate its cause or a repair.

Both Windows Server jobs passed 1,340 tests /one expected macOS-only skip /111 files and typecheck; ASAR files passed in 573ms /1,338ms. Windows suites took 388.72s /384.44s. This is Server smoke, not Windows 11 release qualification.

A normal scoped source push will use the existing automatic workflow for the corrected head. No manual dispatch, failed-job retry, new workflow/monitor, broader rebuild or gate weakening. Draft /merge /publication holds, unresolved ASAR reliability, configured audit findings and platform/product qualification boundaries remain.
