# Tammy: direct Cloudflare Worker releases

The user explicitly requested direct Cloudflare deployment and deployment through CI on 7 November 2026. This runbook covers that route. The [Sites runbook](SITE-RELEASE-RUNBOOK.md) remains available for the existing ChatGPT Sites project; its earlier save-only scope does not prohibit the newly requested direct Worker deployment.

The direct target is `tammy-site-direct` on the selected account's existing `workers.dev` subdomain. Deploying it does not move `tammy.tax`, change DNS/canonicals, or publish a Sites version. Keep the current Sites project, public audience and hosting source manifests intact.

## Current evidence and blockers

Observed 7 November 2026; refresh before claiming readiness:

| Check | Observation |
| --- | --- |
| Local Wrangler authentication | `whoami` failed with HTTP 400 during token refresh, then reported not logged in |
| Local deployment environment | `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` absent |
| GitHub deployment configuration | Repository secrets, variables and environments were empty |
| Tammy Actions runners | Zero registered runners; site job queued |
| Direct Worker publication | Not performed by this preparation |

The missing existing Cloudflare credentials/account selection block local deployment. Missing CI credentials and the unregistered Tammy Cuttlefish pool block CI deployment. Code, tests, dry-run packaging and artifacts can be completed independently. Do not turn a queued job, successful build or successful dry run into a deployment claim.

The deploy wrapper does not log in or create credentials. Do not mint tokens, start login, persist local sessions, create accounts, expand permissions, register runners, or enable spending to work around these blockers. Continue only with existing approved access, a verified account and verified ownership/availability of the Worker name. If these are unavailable, retain the prepared candidate and report the exact remaining action.

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

Provide the verified `CLOUDFLARE_ACCOUNT_ID` to the deployment process through the approved execution environment. Locally, Wrangler can use an existing authorized session or `CLOUDFLARE_API_TOKEN`; GitHub Actions requires the token. Never include tokens in command arguments, files, output, provenance or artifacts. The wrapper is noninteractive and does not start login. The failed local session needs reauthorization through an approved normal Wrangler sign-in; its account and requested OAuth scopes are not yet known. CI credential configuration and Cuttlefish registration are separate access steps. With account/name ownership verified and the exact candidate unchanged:

```sh
node scripts/site-release.mjs cloudflare-deploy --output /absolute/path/to/tammy-cloudflare-deployment
```

The wrapper checks the direct config and provenance and invokes pinned Wrangler against the generated configuration. Retain `deployment-record.json`, source/build hashes, deployed version/deployment identifier and returned URL. The local output also includes `wrangler-output.jsonl`; CI uploads only the deployment record, not raw Wrangler output. The record marks publication only after Wrangler succeeds and returns the version/Worker URL; `live_verification` remains pending until the checks below are performed. A nonzero command or incomplete result is not a successful deployment. If the outcome is uncertain, inspect the existing Worker/deployment before retrying; do not blindly publish twice.

Cloudflare supports external CI deployments using an account ID and API token with `wrangler deploy`. That is a different supported route from Sites publication. [Cloudflare GitHub Actions](https://developers.cloudflare.com/workers/ci-cd/external-cicd/github-actions/).

The account's `workers.dev` subdomain enables deployment without onboarding `tammy.tax`. Use the literal returned Worker URL rather than constructing an unverified hostname. [Cloudflare workers.dev](https://developers.cloudflare.com/workers/configuration/routing/workers-dev/).

## 4. CI deployment contract

`.github/workflows/tammy-site.yml` handles both targets and separates validation/build/artifact preparation from the credential-bearing `deploy-cloudflare` job. That job depends on `verify-site` and requires repository `castlemilk/tammy`, ref `refs/heads/master`, and either a push or manual `deploy_cloudflare: true`. `workflow_dispatch` defaults that boolean to `false`. Pull requests and manual runs against other branches cannot publish and never receive deployment credentials.

After the existing credentials and runner prerequisites are satisfied, an explicitly requested manual deployment uses:

```sh
gh workflow run tammy-site.yml --repo castlemilk/tammy --ref master -f deploy_cloudflare=true
```

The deploy job downloads `tammy-cloudflare-preparation-<GitHub SHA>` from the **same workflow run**, verifies the exact commit/provenance and deploys that artifact. It includes `source.tar.gz`, `dist/`, source/build/release manifests and the dry-run record. The Sites artifact remains `tammy-site-preparation-<GitHub SHA>`; the deployment record artifact is `tammy-cloudflare-deployment-<GitHub SHA>-<run attempt>`. Worker deployment concurrency is serialized as `tammy-cloudflare-tammy-site-direct` with cancellation disabled; do not cancel an active deployment and assume no publication occurred.

Both validation and deployment run on the existing Cuttlefish lane `[self-hosted, linux, ARM64, tammy-linux-ci]`, with `cf-vm-*` identity and `aarch64`. Preserve the fork guard, read-only GitHub token permissions and pinned toolchain. No GitHub-hosted fallback is part of this route. [PR #46](https://github.com/castlemilk/tammy/pull/46) and [issue #47](https://github.com/castlemilk/tammy/issues/47) track independent runner registration/qualification; do not merge, register, or weaken runner security as a release workaround.

Only the deployment step receives repository secret `CLOUDFLARE_API_TOKEN` and repository variable `CLOUDFLARE_ACCOUNT_ID`. No GitHub environment is created or required by this workflow. A workflow in the repository does not establish that the secret, variable or runner pool exists. Read the final run/job result and actual runner identity before calling CI qualified or automatic deployment operational. Missing setup remains an explicit blocker; creating credentials, access or runner infrastructure needs its own authorized scope.

## 5. Live acceptance and fallback

Verify the returned direct URL after a successful deployment, with sample data only:

- HTTP success, expected HTML and representative content for `/`, `/tools`, invoice and quote generators, `/blog`, `/privacy`, and a representative Journal article. Check CSS/JS/image/font/WASM asset responses and a missing route's behavior.
- In a real browser, create and download an invoice PDF and quote PDF, inspect the files, change inputs and download again. Verify repeated downloads contain the changed data and no stale result. Check print/Save as PDF where offered.
- Exercise navigation and the tools at desktop and narrow/mobile widths (including 320px), keyboard access and error states. Record physical-touch coverage separately.
- Preserve the source canonical origin and sitemap/robots/RSS values. The current strict live preflight expects the requested host to be canonical, so a `workers.dev` run will report mismatches. Record the canonical check separately; do not suppress failures or migrate canonicals to the Worker URL to make it pass.
- Confirm tammy.tax still serves the existing Sites deployment and that no DNS, routes, audience or Sites version was changed.

The current Sites deployment remains the live tammy.tax fallback during direct-Worker validation. A direct deployment cannot serve as proof of domain cutover. For a failed direct update, use the recorded prior direct deployment/version for a reviewed rollback through supported Cloudflare tooling; do not modify Sites to compensate.

## Separate gate: moving tammy.tax

The ordinary Workers Custom Domain route requires an active Cloudflare zone in the deploying account. The observed tammy.tax DNS baseline is Vercel-managed. A free full-zone cutover therefore requires inventorying and reproducing **all** existing DNS records, then a separately approved nameserver migration to Cloudflare. This is not a registrar transfer. Preserve existing mail, verification and unrelated service records. The owner accepts ordinary disruption risk; a zero-downtime migration is not a release requirement. [Workers Custom Domains](https://developers.cloudflare.com/workers/configuration/routing/custom-domains/), [Cloudflare full-zone setup](https://developers.cloudflare.com/dns/zone-setups/full-setup/setup/).

Partial/CNAME zone setup is a Business/Enterprise option; do not introduce a paid plan for this release. Pointing a Vercel CNAME directly at the workers.dev hostname is not the supported Custom Domain shortcut. Keep tammy.tax and source canonicals unchanged until the account-owned zone, complete DNS migration, Worker domain attachment, TLS/routing and canonical metadata changes are separately reviewed and authorized. Then use the live acceptance checks in the [Sites canonical migration section](SITE-RELEASE-RUNBOOK.md#separate-reviewed-gate-canonical-migration-to-tammytax), applied to the direct deployment. [Partial-zone setup requirements](https://developers.cloudflare.com/dns/zone-setups/partial-setup/).

## Release checklist and record

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
