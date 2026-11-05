# Exact-head CI selector follow-up

Original review head **af46e7290ac79bc1cb47583cc24b9e2b11a7729b** completed both
Ubuntu and both Windows Server jobs successfully. Both original macOS jobs stopped
at the same stale Overview readiness assertion in `current-workflows.spec.ts`:
`getByText('Nothing needs review', { exact: true }).first()`.

- [PR run 39967410778, macOS job 120460790570](https://github.com/castlemilk/tammy/actions/runs/39967410778/job/120460790570): five packaged cases passed, the sixth failed, 14 did not run.
- [Push run 39967403940, macOS job 120460767223](https://github.com/castlemilk/tammy/actions/runs/39967403940/job/120460767223): same failure; five passed and 14 did not run.

No manual retry, workflow dispatch or timeout change is used. The full original
logs/artifacts remain on those runs. This note retains the relevant selector and
outcome without copying runtime/private logs or authentication material into Git.

The new Overview deliberately says **No additional review items** and directs the
user to the separate BAS status. The old unqualified **Your retained local
workflows are up to date** assertion no longer describes the supported UI. The
test now scopes its checks to **Local attention**, checks the new empty-list
message and explanation, retains the reviewed-document count, and explicitly
checks BAS's **Action needed** status. All invoice, tax, payment, template,
restart/authentication and native financial assertions remain intact.

The corresponding readiness marker in the separate signed-screenshot spec is
aligned too. Its strict pinned accessibility fixture and validation policy are
**not changed**, regenerated or claimed qualified. They contain older shell/text
expectations and that signed capture workflow is not run in this visual block.
There is no signing, profile, release-gate or distribution change.

With the test-only correction, the **original complete invoice/purchase/BAS case
passes locally in 38.0 seconds** (36.1-second body), zero retries, against the same
clean final package at **74e147431c950bb094dba57f1386bd3ed3840020**. Both restarts
complete in this run. This observed pass does not establish the cause of the
earlier local second-unlock failure; that original evidence remains retained. The
older local SBR startup discrepancy also remains recorded and paused.

No runtime code, image bytes or financial behavior change in this correction.
Fresh exact-head hosted results must be assessed after the normal push. The PR's
evidence note will record their terminal outcome without relabelling screenshot
source or replacing these original failed runs.
