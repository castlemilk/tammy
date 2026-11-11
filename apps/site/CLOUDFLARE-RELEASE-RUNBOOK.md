# Tammy: direct Cloudflare Worker releases

The user explicitly requested direct Cloudflare deployment and deployment through CI on 7 November 2026. This runbook covers that route. The [Sites runbook](SITE-RELEASE-RUNBOOK.md) remains available for the existing ChatGPT Sites project; its earlier save-only scope does not prohibit the newly requested direct Worker deployment.

The direct target is `tammy-site-direct` on the selected account's existing `workers.dev` subdomain. Deploying it does not move `tammy.tax`, change DNS/canonicals, or publish a Sites version. Keep the current Sites project, public audience and hosting source manifests intact.

The subsequent [feature-led design review](../../docs/design/2026-11-07-site-feature-review.md), [landing refinement](../../docs/design/2026-11-07-landing-journey-refinement.md) and [Soft Precision system](../../docs/design/2026-11-07-soft-precision-system.md) were integrated in [PR48](https://github.com/castlemilk/tammy/pull/48) after the user's explicit merge/release instruction. [PR49](https://github.com/castlemilk/tammy/pull/49) then fixed a first-entry loading race found by live review. Its source `ab522aa0558f9710eee0668bcb814e2ed81814f5` published as version `2b7534d7-33cd-4a7f-a17a-fa3ac733e6e1`, independently read back at 100% traffic on 7 November 2026. These releases used the existing authorized Wrangler session, not CI. Refresh the active version before another release; historical receipts retain their own source/artifact identities.

The requested [tammy.tax migration plan](../../docs/release/cloudflare/tammy-tax-domain-plan.md) records the absent Cloudflare zone, missing authorized DNS inventory and exact routing prerequisites. Runner registration/qualification, CI credentials and DNS migration remain separate gates; the merge/release instruction did not authorize their setup.

## Initial deployment evidence and continuing setup gates

Historical first-publication record from 7 November 2026 (Australia/Melbourne, AEDT), superseded by the releases above. Its hashes and version still identify that original artifact. Refresh setup and active-version checks before each release:

| Check | Observation |
| --- | --- |
| Verified target | Gamma account `2132ccf47ceb5fff234c34d85490470a`; account subdomain `ben-ebsworth.workers.dev`; pre-deployment lookup returned Cloudflare 10007, Worker does not exist, for the fresh target |
| Approved local OAuth | User approved `account:read`, `workers_scripts:write` and Wrangler's automatically added `offline_access`; consent showed Account Read, Workers Scripts Write and Background Access on the intended account |
| Local authentication | Pinned Wrangler `whoami` exited 0 and confirmed the intended account with exactly those scopes; no scope expansion |
| Earlier authentication attempts | First attempt timed out after 120 seconds with refused localhost callback; same-scope retry rejected mismatched OAuth state (exit 1). Native input stopped when Chrome focus left the task; subsequent normal authorization succeeded |
| GitHub deployment configuration | Last check found empty repository secrets, variables and environments; CI credential setup remains unapproved |
| Tammy Actions runners | Last check found zero registered runners and a queued site job; registration remains unapproved |
| Built and published source | `56bd4ef4091a7df58e004b7ddb643e6f4c58cd7b` |
| Direct Worker publication | Pinned deployment wrapper exited 0; version `df005821-f3ae-451d-b728-610623a393ac` at [tammy-site-direct.ben-ebsworth.workers.dev](https://tammy-site-direct.ben-ebsworth.workers.dev/) |
| Live acceptance | HTTP 18/18, four fresh invoice/quote PDF downloads, desktop navigation and simulated 320px mobile checks passed. Physical touch and OS print dialog untested; original receipt retains its issuance-time `live_verification: pending` |

Local authentication, direct publication and the recorded live checks succeeded. Missing CI credentials and the unregistered Tammy Cuttlefish pool still independently block CI deployment. The approved local sign-in does not authorize either CI setup step. Publication is distinct from live acceptance and CI qualification.

The deploy wrapper remains noninteractive and does not start login. Reuse the authorized local session while valid. Do not extract or manually transfer credentials, mint API tokens, create accounts, expand scopes, register runners or enable spending as a workaround. Preserve the published candidate and its provenance. A later documentation/evidence commit has its own SHA and does not change which source produced the deployed build.

## Target contract

`prepare --target cloudflare` selects the direct source snapshot. The default target remains `sites`. The exported `.release-target.json` and source/build metadata record which target produced the output; do not relabel or reuse a Sites artifact as a direct artifact.

`TAMMY_DEPLOY_TARGET=cloudflare` selects `apps/site/wrangler.cloudflare.json` during the direct build. That source configuration declares:

- Worker name `tammy-site-direct`, `workers_dev: true` and `preview_urls: false`.
- `main: vinext/server/fetch-handler`, `nodejs_compat`, and the tested compatibility date `2026-05-15`.
- Assets binding `ASSETS` with `not_found_handling: none`; the Vite plugin supplies the generated asset directory.
- No routes/custom domains, D1/R2 resources, or account credentials.

The direct build excludes `sites()` and rejects Sites deployment metadata in its output. The source still retains the original hosting files and unchanged Sites configuration. The generated Worker config is `dist/server/wrangler.json`, with client assets under `dist/client`; deploy that exact verified output. Do not hand-edit generated config, use a floating Wrangler version, or run `wrangler deploy` against the generic Sites Worker name.

The Cloudflare plugin supports an explicit `configPath`; its generated configuration is used for deployment. Environments, when used, are selected at build time rather than by appending `--env` to a deployment of existing output. [Cloudflare Vite API](https://developers.cloudflare.com/workers/vite-plugin/reference/api/), [build-output configuration](https://developers.cloudflare.com/workers/vite-plugin/reference/migrating-from-wrangler-dev/).

## 1. Commit, attest and export the exact source

Use an isolated checkout and preserve concurrent work. Use root `package.json` toolchain pins, currently Node `24.18.0` and pnpm `11.15.0`; Wrangler is pinned at `4.92.0` in the site package.

Finish all source/documentation edits and commit them before generating descriptor evidence. Run the normal [descriptor attestation sequence](SITE-RELEASE-RUNBOOK.md#1-select-an-exact-github-candidate): set `TAMMY_SOURCE_REVISION` to the clean source HEAD, generate evidence, preserve the expected `descriptors.pb` bytes/SHA-256 for site-only changes, and run `pnpm proto:descriptors:verify`. Commit only `compliance/contracts/descriptor-manifest.json` and `compliance/contracts/descriptors.pb` as its immediate child, then verify again at that final HEAD. The final candidate is this evidence commit. Do not weaken the verifier; any later source edit needs new attestation.

From that clean final GitHub checkout:

```sh
node --test scripts/site-release.test.mjs
node scripts/site-release.mjs prepare --target cloudflare --output /absolute/path/to/tammy-cloudflare-candidate
```

Use a new or empty absolute output directory outside the source checkout. The exporter retains the full required workspace closure, including `packages/invoices`, root dependency/configuration inputs, frozen lockfile and patches. Copying only `apps/site` is invalid. Record the exact GitHub SHA and `source-provenance.json`; a PR's synthetic merge SHA and its head SHA are different candidates.

## 2. Validate and build the exported workspace

Run inside the exported candidate directory, stopping on failure:

```sh
pnpm install --frozen-lockfile --filter sites-project...
pnpm --dir apps/site typecheck
pnpm --dir packages/invoices typecheck
pnpm --dir apps/site lint
pnpm --dir packages/invoices test
pnpm --dir apps/site test
pnpm --dir apps/site editorial:lint
pnpm --dir apps/site seo:check
pnpm --dir apps/site site:preflight:test
pnpm build
node scripts/site-release.mjs verify
node scripts/site-release.mjs bundle --output /absolute/path/to/tammy-cloudflare-evidence
node scripts/site-release.mjs cloudflare-dry-run --output /absolute/path/to/tammy-cloudflare-dry-run
```

The source, build and release records must identify the direct target, Worker name and exact GitHub SHA. Retain source/build hashes, bundled evidence and `dry-run-record.json`; the dry-run directory also contains the packaged `worker/` files. CI uploads the dry-run record beside the direct preparation artifact, without raw logs. Inspect the generated config for the intended Worker name, no routes/custom domains, `workers_dev: true`, `preview_urls: false`, the relative client assets path and absent Sites metadata. The wrapper verifies config/provenance before dry run and deployment.

A dry run checks packaging without publishing. It does not establish account ownership, valid production authentication, an existing `workers.dev` subdomain, live routing, PDF behavior or Cuttlefish qualification. Retain both targets' artifacts separately when validating that the Sites release path still works.

## 3. Deploy using existing approved access

Before first deployment or enabling CI credentials, verify the intended Cloudflare account, existing account subdomain, and whether `tammy-site-direct` already exists. Inspect an existing Worker's routes and custom domains before overwriting it; do not infer ownership from its name or deploy into whichever account Wrangler selects by default. Stop if existing access cannot establish the target. A workers.dev-only receipt describes this command's requested triggers, not all pre-existing routes attached to a Worker. Compare the returned URL with the exact URL established from the verified account subdomain. Record the previous direct Worker version/deployment, if any. Supported read-only account lookups include [Worker scripts](https://developers.cloudflare.com/api/resources/workers/subresources/scripts/methods/list/), [account subdomain](https://developers.cloudflare.com/api/resources/workers/subresources/subdomains/methods/get/) and [custom domains](https://developers.cloudflare.com/api/resources/workers/subresources/domains/methods/list/). Existing routes are [zone-scoped](https://developers.cloudflare.com/api/resources/workers/subresources/routes/methods/list/); incomplete zone visibility cannot prove that a pre-existing Worker has no other routes. Do not broaden permissions to complete this check without approval.

Provide the verified `CLOUDFLARE_ACCOUNT_ID` to the deployment process through the approved execution environment. Locally, Wrangler can use an authorized local session or `CLOUDFLARE_API_TOKEN`; GitHub Actions requires the token. Keep normal session handling inside Wrangler. Do not copy credentials into command arguments, hand-managed files, output, provenance or artifacts. The wrapper is noninteractive and does not start login. CI credential configuration and Cuttlefish registration remain separate, unapproved access steps.

### Approved local sign-in procedure

The recorded local authorization is now successful. If that session later needs reauthorization within the approved account/scopes, use a fresh normal Wrangler sign-in in a task-owned terminal/browser context. From the validated candidate root, using its installed pinned Wrangler:

```sh
pnpm --dir apps/site exec wrangler login --scopes account:read workers_scripts:write
```

Wrangler adds `offline_access`; the approved consent is exactly Account Read, Workers Scripts Write and Background Access on the verified Gamma account. Use the consent page opened by the current live CLI process. Keep the normal localhost callback and PKCE/state checks intact. Do not reuse an expired callback, alter a state value, manually exchange a code, or bypass a mismatch. Authentication URLs, codes, state and credentials do not belong in release documentation or artifacts.

Wait for that CLI process to report success, then use normal `wrangler whoami` to confirm the authenticated account and refresh target ownership checks before publication. A consent click alone is insufficient. If the process times out, rejects state, or browser focus leaves the task, record the failed/pending result and stop native input; hand off a fresh normal sign-in in a controlled context. Earlier failed attempts remain historical failures; the subsequent successful account check and deployment receipt establish the current release status.

With a successful authorized session, account/name ownership verified and the exact candidate unchanged:

```sh
node scripts/site-release.mjs cloudflare-deploy --output /absolute/path/to/tammy-cloudflare-deployment
```

The wrapper checks the direct config and provenance and invokes pinned Wrangler against the generated configuration. Retain `deployment-record.json`, source/build hashes, deployed version/deployment identifier and returned URL. The local output also includes `wrangler-output.jsonl`; CI uploads only the deployment record, not raw Wrangler output. The record marks publication only after Wrangler succeeds and returns the version/Worker URL; `live_verification` remains pending until the checks below are performed. A nonzero command or incomplete result is not a successful deployment. If the outcome is uncertain, inspect the existing Worker/deployment before retrying; do not blindly publish twice.

Cloudflare supports external CI deployments using an account ID and API token with `wrangler deploy`. That is a different supported route from Sites publication. [Cloudflare GitHub Actions](https://developers.cloudflare.com/workers/ci-cd/external-cicd/github-actions/).

The account's `workers.dev` subdomain enables deployment without onboarding `tammy.tax`. Use the literal returned Worker URL rather than constructing an unverified hostname. [Cloudflare workers.dev](https://developers.cloudflare.com/workers/configuration/routing/workers-dev/).

## 4. CI deployment contract

The **Tammy website** workflow (`.github/workflows/tammy-site.yml`) groups both targets and separates validation/build/artifact preparation from the credential-bearing `deploy-cloudflare` job. Run titles identify the PR or branch and whether the run verifies or deploys. PR events validate branch changes; push events are restricted to `master`, avoiding duplicate branch-push runs. Existing path filters still apply. That job depends on `verify-site` and requires repository `castlemilk/tammy`, ref `refs/heads/master`, and either a push or manual `deploy_cloudflare: true`. `workflow_dispatch` defaults that boolean to `false`. Pull requests and manual runs against other branches cannot publish and never receive deployment credentials.

After the existing credentials and runner prerequisites are satisfied, an explicitly requested manual deployment uses:

```sh
gh workflow run tammy-site.yml --repo castlemilk/tammy --ref master -f deploy_cloudflare=true
```

The deploy job downloads `tammy-cloudflare-preparation-<GitHub SHA>` from the **same workflow run**, verifies the exact commit/provenance and deploys that artifact. It includes `source.tar.gz`, `dist/`, source/build/release manifests and the dry-run record. The Sites artifact remains `tammy-site-preparation-<GitHub SHA>`; the deployment record artifact is `tammy-cloudflare-deployment-<GitHub SHA>-<run attempt>`. Worker deployment concurrency is serialized as `tammy-cloudflare-tammy-site-direct` with cancellation disabled; do not cancel an active deployment and assume no publication occurred.

Both validation and deployment run on the existing Cuttlefish lane `[self-hosted, linux, ARM64, tammy-linux-ci]`, with `cf-vm-*` identity and `aarch64`. Preserve the fork guard, read-only GitHub token permissions and pinned toolchain. No GitHub-hosted fallback is part of this route. [PR #46](https://github.com/castlemilk/tammy/pull/46) and [issue #47](https://github.com/castlemilk/tammy/issues/47) track independent runner registration/qualification; do not merge, register, or weaken runner security as a release workaround.

Only the deployment step receives repository secret `CLOUDFLARE_API_TOKEN` and repository variable `CLOUDFLARE_ACCOUNT_ID`. No GitHub environment is created or required by this workflow. A workflow in the repository does not establish that the secret, variable or runner pool exists. Read the final run/job result and actual runner identity before calling CI qualified or automatic deployment operational. Missing setup remains an explicit blocker; creating credentials, access or runner infrastructure needs its own authorized scope.

After confirmed publication, `verify-direct-deployment.mjs` checks the exact receipt's repository, revision, account, Worker, URL and hashes, then requests twelve public routes. It requires the direct Worker to serve those routes itself while retaining the canonical origin imported from `lib/site-config.ts` (currently `https://tammy-accounting.castlemilk.chatgpt.site`). It writes a separate `live-verification.json`; it never rewrites `deployment-record.json` or republishes. CI retains only those two JSON files, and its summary distinguishes published, unhealthy and unconfirmed outcomes. A failed health check fails the job but does not roll back an already-published Worker. HTTP checks do not establish browser/PDF behavior or independently prove the active Cloudflare version; retain that readback separately.

Read-only setup inspection on 7 November 2026 found **zero registered repository runners**, **no repository Actions secrets**, and **no `CLOUDFLARE_ACCOUNT_ID` repository variable**. PR #48's `f3c426ab` site job was queued; its foundation jobs passed on Ubuntu and Windows while macOS was cancelled. This is a setup blocker, not successful Cuttlefish execution. PR #46 remains the independently gated pool rollout. The existing authorized local Wrangler session is a separate release path and is not a substitute for CI qualification.

## 5. Live acceptance and fallback

Verify the returned direct URL after a successful deployment, with sample data only:

- HTTP success, expected HTML and representative content for `/`, `/tools`, invoice and quote generators, `/blog`, `/privacy`, and a representative Journal article. Check CSS/JS/image/font/WASM asset responses and a missing route's behavior.
- In a real browser, create and download an invoice PDF and quote PDF, inspect the files, change inputs and download again. Verify repeated downloads contain the changed data and no stale result. Check print/Save as PDF where offered.
- Exercise navigation and the tools at desktop and narrow/mobile widths (including 320px), keyboard access and error states. Record physical-touch coverage separately.
- Preserve the source canonical origin and sitemap/robots/RSS values. The strict general preflight still expects the requested host to be canonical. For this specific direct Worker, run the receipt-bound acceptance command below, which explicitly expects the configured source canonicals and rejects redirects away from the Worker. Do not migrate canonicals to workers.dev to make a check pass.
- Confirm tammy.tax still serves the existing Sites deployment and that no DNS, routes, audience or Sites version was changed.

The current Sites deployment remains the live tammy.tax fallback during direct-Worker validation. A direct deployment cannot serve as proof of domain cutover. For a failed direct update, use the recorded prior direct deployment/version for a reviewed rollback through supported Cloudflare tooling; do not modify Sites to compensate.

```sh
node apps/site/scripts/verify-direct-deployment.mjs \
  --record /absolute/path/to/tammy-cloudflare-deployment/deployment-record.json \
  --revision EXACT_PUBLISHED_GITHUB_SHA \
  --output /absolute/path/to/tammy-cloudflare-deployment/live-verification.json
```

The output path must be new. Check the command exit status and retain the failures if acceptance fails; do not infer health merely from a successful publish receipt.

## Separate gate: moving tammy.tax

The ordinary Workers Custom Domain route requires an active Cloudflare zone in the deploying account. The observed tammy.tax DNS baseline is Vercel-managed. A free full-zone cutover therefore requires inventorying and reproducing **all** existing DNS records, then a separately approved nameserver migration to Cloudflare. This is not a registrar transfer. Preserve existing mail, verification and unrelated service records. The owner accepts ordinary disruption risk; a zero-downtime migration is not a release requirement. [Workers Custom Domains](https://developers.cloudflare.com/workers/configuration/routing/custom-domains/), [Cloudflare full-zone setup](https://developers.cloudflare.com/dns/zone-setups/full-setup/setup/).

Partial/CNAME zone setup is a Business/Enterprise option; do not introduce a paid plan for this release. Pointing a Vercel CNAME directly at the workers.dev hostname is not the supported Custom Domain shortcut. Keep tammy.tax and source canonicals unchanged until the account-owned zone, complete DNS migration, Worker domain attachment, TLS/routing and canonical metadata changes are separately reviewed and authorized. Then use the live acceptance checks in the [Sites canonical migration section](SITE-RELEASE-RUNBOOK.md#separate-reviewed-gate-canonical-migration-to-tammytax), applied to the direct deployment. [Partial-zone setup requirements](https://developers.cloudflare.com/dns/zone-setups/partial-setup/).

## Published release: 7 November 2026 (AEDT)

The [original deployment receipt](../../docs/release/cloudflare/deployments/df005821-f3ae-451d-b728-610623a393ac.json) is retained unchanged. Remote readback confirmed deployment creation on **7 November 2026 at 14:45:33 AEDT** (Cloudflare API timestamp `2026-11-07T03:45:33.521169Z`). The receipt's `live_verification: pending` remains its original issuance state; subsequent live checks are recorded separately below.

| Evidence | Recorded value |
| --- | --- |
| Publication | Pinned Wrangler `4.92.0` deployment wrapper exited 0; receipt status `published` |
| Worker / account | `tammy-site-direct` / `2132ccf47ceb5fff234c34d85490470a` |
| Prior target | Cloudflare 10007: Worker does not exist; fresh deployment, no prior direct version |
| Published source commit | `56bd4ef4091a7df58e004b7ddb643e6f4c58cd7b` |
| Attested source parent | `6601f360b60ee018a4bca65007c982dabe7de912` |
| Source SHA-256 | `62eadbbe654941ef295e65c0dea125e9d95b91efff2795bebc098e89e243f537` |
| Build SHA-256 | `45be0ac119deaee6b62a68c485135b174095c3426d8e1fa807cc1576959c961a` |
| Source archive SHA-256 | `1b368453d964b93ee0c14ff57e9372204ba914e190dcd48ff1e92fadc378fbd8` |
| Deployment ID | `e608f482-0393-4166-bd41-ec2101135c1f` |
| Active version / traffic | `df005821-f3ae-451d-b728-610623a393ac` / 100%, confirmed by remote readback |
| Remote deployment message | `GitHub 56bd4ef4091a7df58e004b7ddb643e6f4c58cd7b` |
| Published URL | <https://tammy-site-direct.ben-ebsworth.workers.dev/> |
| HTTP / assets / missing route checks | 18/18 passed at 14:47:02 AEDT: seven pages, genuine 404, seven assets with matching build hashes/MIME types, and sitemap/robots/RSS |
| HTTP evidence | [Public HTTP acceptance JSON](../../docs/release/cloudflare/verification/df005821-f3ae-451d-b728-610623a393ac-http.json); SHA-256 `119a6f48a3d70f9cc411aecdeb0067680b883e6a762f034a9dd7716c9858595d` |
| Canonical review | All seven pages and sitemap/robots/RSS retain the prior Sites origin; workers.dev canonical mismatch remains expected and separate from HTTP health |
| Repeated invoice/quote PDF downloads and browser checks | Four real downloads passed independent text/totals and rendered-layout/glyph review; each is one clean A4 page. Changed references/quantities/totals and distinct hashes establish fresh results. Desktop homepage → tools → invoice → quote → blog navigation and download success messages passed |
| Browser/PDF evidence | [Sanitized verification metadata](../../docs/release/cloudflare/verification/df005821-f3ae-451d-b728-610623a393ac-browser-pdf.json); SHA-256 `e88eea66f2d975dd1e6495375e94115599c8b7d5b3fe954be2760fa5fc8b4840`. PDF review recorded at 14:48:56 AEDT; original review JSON SHA-256 `37352ea4251b70d646a1c9c2be3530b09e0cd4343265f0ffadf2b8fed2c0ac3d` |
| Mobile / keyboard / physical-touch checks | Simulated 320×740 quote, invoice and blog: viewport and scroll width both 320, no horizontal overflow. Added a second quote item, then removed it using Enter. Physical touch and OS print dialog untested |
| Sites / tammy.tax readback | Native readback confirmed exact project `appgprj_6a93f33631748191b7c064c033f72b53`, active/public, latest version 12, live URL `https://tammy.tax`; no Sites/DNS/canonical changes performed |
| CI deployment / Cuttlefish qualification | Incomplete; CI credentials and runner registration remain unapproved |

The PDF cases used synthetic data only:

| Document | Quantity × unit price | GST | Total AUD | Bytes |
| --- | --- | --- | --- | --- |
| `LIVE-CF-001` | 2 × 100 | 20 | 220 | 35,547 |
| `LIVE-CF-002` | 3 × 100 | 30 | 330 | 35,596 |
| `LIVE-CF-QUOTE-001` | 3 × 125 | 0 | 375 | 35,790 |
| `LIVE-CF-QUOTE-002` | 4 × 125 | 0 | 500 | 35,573 |

Exact PDF hashes and review observations are retained in the sanitized metadata; PDF binaries and local filesystem paths are excluded.

This release records the unchanged `56bd4ef4…` build. The documentation checkout was `f609d469e82c5af1185ae6fb6f2ef2b12a4a7cfa` before this receipt update; that revision and subsequent documentation/evidence commits are not deployed source. Keep preparation manifests and the original receipt unchanged when recording follow-up evidence.

## 11 November 2026: CI credential preflight and readback

**CI token requirement.** The `CLOUDFLARE_API_TOKEN` repository secret must be an account-owned API token for account `2132ccf47ceb5fff234c34d85490470a` with exactly two permissions: (a) Workers Editor, scoped to the individual Worker `tammy-site-direct`, and (b) Workers Metadata Read-Only at the Workers product scope. The token needs no zone permissions. Without (b), Cloudflare returns code 10000 when Wrangler reads the account subdomain after upload, which is what failed the 11 November CI run.

**Preflight.** `site-release.mjs cloudflare-deploy` now reads two endpoints with the CI token before Wrangler runs: `GET /accounts/{account}/workers/subdomain` and `GET /accounts/{account}/workers/scripts/tammy-site-direct/subdomain`. The host is fixed to `api.cloudflare.com`; it cannot be overridden by environment or flag. If either read is not a success, the command stops, Wrangler is not invoked, and the error names the endpoint (`account-subdomain` or `script-subdomain`), the HTTP status and any numeric Cloudflare error codes. Tokens, headers and response bodies are never printed or written. The record has `status: "preflight-failed"` and `publication: "not-performed"`. The script-subdomain read requires an existing Worker, so the first publication of a Worker that does not yet exist cannot pass this preflight. Each preflight read retries once on HTTP 429, 5xx or a network/abort error; authorization and other 4xx answers are final. Local runs that rely on an existing Wrangler session without `CLOUDFLARE_API_TOKEN` skip both the preflight and the readback.

**Readback after any non-zero Wrangler exit.** The command reads `GET /accounts/{account}/workers/scripts/tammy-site-direct/deployments`, retrying once as above. The record becomes `published-unconfirmed`, with that version ID, only when the newest deployment has the message `GitHub <source commit>`, was created no earlier than the start of this `cloudflare-deploy` command, and has exactly one version at 100%. The start time is the `Date` header of the first successful preflight read, floored to the second; the local clock minus 5 seconds is used only when that header is absent or unparseable. Any other outcome, including a readback that fails, makes the record `outcome-uncertain`. Either way the job fails, and a readback error never replaces the original Wrangler error. The record's `readback` field states `matched`, `mismatch` or `unavailable`. Local runs without `CLOUDFLARE_API_TOKEN` skip the readback and record `unavailable`.

**HTTP acceptance for a `published-unconfirmed` record.** Cloudflare reports that version, but the live site has not been accepted. The record has no URL, so the verifier always probes the pinned direct origin. Run this against the record, with the revision set to the record's `github.commit_sha`:

```sh
node apps/site/scripts/verify-direct-deployment.mjs \
  --record /absolute/path/to/tammy-cloudflare-deployment/deployment-record.json \
  --revision EXACT_RECORD_GITHUB_COMMIT_SHA \
  --output /absolute/path/to/tammy-cloudflare-deployment/live-verification.json
```

The output carries `publication_status: "published-unconfirmed"` so the two states stay distinct. The workflow runs this verifier only after a successful deploy step, so an unconfirmed record needs this manual run.

**Observed live state on 11 November 2026.** The `tammy-site-direct` Worker has two deployments that the release process produced:

| Deployment time (UTC) | Version | Message | Notes |
| --- | --- | --- | --- |
| 2026-11-07T13:44:25Z | `3503f63e-8ed7-4676-b757-d5b88de7c58b` | `GitHub 8bb2c6d56393c55fc3d88fd527709f0f6f2688fa` | Earlier release |
| 2026-11-11T05:38:36Z | `7a9585f3-f1a0-4cae-ac1d-dec6968b96d4` | `GitHub fa5ade8a0ea33e455740bec24074d1d3b4e12ac6` | Published by CI run `42777581689`; its job reported failure after the upload, with no deployment receipt |

HTTP acceptance of the live Worker on 11 November 2026 passed for all 12 checked routes. That check read the public site without credentials, so it does not prove the CI token has the permissions above. The 11 November CI job exited 1 after publishing, so its record cannot be treated as a successful release receipt.

## Release checklist and record

Reusable template for the next release; the completed publication and verification record is above.

- [ ] Clean final evidence commit selected; descriptor hash and verification retained.
- [ ] Direct source export, full workspace install, required checks, build, provenance verification and dry run passed.
- [ ] Existing account, account subdomain and `tammy-site-direct` ownership/availability verified; existing credentials usable.
- [ ] Exact target/config and artifact hashes checked immediately before deployment.
- [ ] Real Cuttlefish run completed on the expected identity; CI secrets/branch gates verified without exposing values.
- [ ] Deployment succeeded with a recorded version/deployment identifier and literal URL.
- [ ] Live routes/assets, repeated invoice/quote PDFs, desktop/mobile/keyboard behavior verified; touch limits recorded.
- [ ] Sites identity/source manifests/live deployment, tammy.tax DNS and canonical configuration preserved.

| Evidence | Value |
| --- | --- |
| Date / operator / PR | |
| Attested source commit / final GitHub candidate | |
| Descriptor SHA-256 / verification | |
| Target / Worker name / verified account | `cloudflare` / `tammy-site-direct` / |
| Source / build / release manifests and hashes | |
| Tests / typechecks / lint / build / dry run | |
| CI run / job / final status / exact SHA | |
| Cuttlefish runner name / OS / architecture | |
| Previous direct deployment/version | |
| Deployment result path / new version or deployment ID | |
| Returned direct URL / live checks | |
| Repeated invoice/quote PDF and browser evidence | |
| Mobile / keyboard / physical-touch evidence | |
| Sites/tammy.tax unchanged evidence | |
| Blocker / exact remaining action / pending or failed outcome | |
