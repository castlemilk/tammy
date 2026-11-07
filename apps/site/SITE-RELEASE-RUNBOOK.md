# Tammy: GitHub source to ChatGPT Sites

This runbook covers the **ChatGPT Sites** target: a reproducible GitHub release candidate, a saved Sites version, and separate Sites publication. Use the [Sites checklist](SITE-RELEASE-CHECKLIST.md) for that path. The user subsequently requested direct Cloudflare deployment and CI on 7 November 2026; follow the [direct Cloudflare release runbook](CLOUDFLARE-RELEASE-RUNBOOK.md) for that authorized route. Direct Worker publication does not publish a Sites version or move tammy.tax.

## Baseline and release boundaries

Observed on 6 November 2026; refresh these values before a later release:

| Item | Recorded value |
| --- | --- |
| GitHub repository / default branch | `castlemilk/tammy` / `master` |
| GitHub source at the start of this preparation | `01fe1d2b5db4232768e0327545f4b770a01d99f5` (PRs #40–#45 merged) |
| Existing Sites project | `appgprj_6a93f33631748191b7c064c033f72b53` |
| Current live Sites version | 12 |
| Published source revision | `fca980735b975ee77624a8312a5caad0f847f936` |
| Live URL / audience | `https://tammy.tax` / public |
| Custom hostname / provider / SSL | Previously confirmed active in Sites |
| Source canonical origin | `https://tammy-accounting.castlemilk.chatgpt.site` |
| Storage bindings | `d1: null`, `r2: null` |

`get_site` confirmed the existing project, public audience, live URL and version 12 during this preparation. These observations do not establish that the latest GitHub source is live. DNS is not the current release blocker. Vercel manages the domain, but its hosting-specific configuration recommendations are not instructions to replace Sites records.

The 6 November **Sites preparation** was save-only: source, tests, build output and documentation, without production publication. That boundary still applies to the Sites preparation flow described here. The later direct Cloudflare request authorizes a separate Worker deployment through its own runbook; it does not authorize Sites publication, credential creation, runner registration, permission changes, or DNS/audience/canonical migration. Saving an unpublished Sites version still requires existing approved source access and confirmation that pushing cannot trigger publication.

## What is automated

`.github/workflows/tammy-site.yml` watches the site, shared invoice package, root dependency/configuration inputs, patches, release scripts and its own workflow. It checks the source and prepares a downloadable release artifact on the isolated Cuttlefish lane. It has read-only repository permissions and no Sites credentials or Sites publication step. Its separate direct Cloudflare target/job is documented in the [direct release runbook](CLOUDFLARE-RELEASE-RUNBOOK.md#4-ci-deployment-contract).

The runner contract is `[self-hosted, linux, ARM64, tammy-linux-ci]`, with `RUNNER_NAME` matching `cf-vm-*` and `uname -m` reporting `aarch64`. Fork pull requests do not execute on this pool. Local validation is useful evidence, but does not qualify the Cuttlefish runner.

[Draft PR #46](https://github.com/castlemilk/tammy/pull/46) and [issue #47](https://github.com/castlemilk/tammy/issues/47) track the runner installation and qualification. At the baseline check, the site job was queued and the Tammy pool had not been registered. Keep this release PR draft until the pool is online and this PR's exact revision has passed on Cuttlefish. Do not merge PR #46, register a pool, weaken its security checks, or add a GitHub-hosted fallback as part of release preparation.

No supported unattended GitHub Actions-to-Sites publishing/authentication integration was verified in the available Sites tools. `publish_on_push` is an owner-only, time-windowed private-publication capability; Tammy is public, and that option must remain disabled. Do not invent API endpoints, copy account tokens into Actions, or treat a GitHub push as a Sites deployment.

## 1. Select an exact GitHub candidate

Use an isolated checkout. Preserve concurrent working trees and open pull requests. Fetch the current branch, review the source diff and record a full commit SHA. A PR workflow may build GitHub's synthetic merge revision: the recorded candidate SHA must identify the revision actually tested, not an assumed PR head.

Commit all intended source changes before preparation. Keep generated release output outside the checkout. The exporter uses committed source and refuses dirty inputs rather than quietly assigning a commit SHA to uncommitted work.

Before exporting, refresh the normal descriptor attestation against that clean source commit. `DESCRIPTOR_SOURCE_REVISION_MISMATCH` means the retained descriptor manifest does not attest the current source revision; a successful site build does not satisfy this repository check. Finish **all source and documentation changes first**, then run from the GitHub checkout with the pinned toolchain:

```sh
TAMMY_DESCRIPTOR_SHA_BEFORE="$(shasum -a 256 compliance/contracts/descriptors.pb)"
TAMMY_SOURCE_REVISION="$(git rev-parse HEAD)" pnpm proto:descriptors:evidence
test "$TAMMY_DESCRIPTOR_SHA_BEFORE" = "$(shasum -a 256 compliance/contracts/descriptors.pb)"
pnpm proto:descriptors:verify
git diff -- compliance/contracts
git add -- compliance/contracts/descriptor-manifest.json compliance/contracts/descriptors.pb
git diff --cached --name-only
git commit -m "Refresh descriptor evidence for release source"
pnpm proto:descriptors:verify
git rev-parse HEAD
```

Stop on any command failure. For this site-only release, `descriptors.pb` must retain its expected bytes and SHA-256; investigate any hash change rather than accepting it as metadata refresh. Review the staged paths before committing: the immediate child commit may contain **only** `compliance/contracts/descriptor-manifest.json` and `compliance/contracts/descriptors.pb`. The normal verifier accepts that evidence-only child of the attested source commit. Do not weaken its revision check or describe descriptor evidence as site-test coverage.

The final release candidate SHA is the evidence commit, and verification must pass again at that final HEAD. Export from it. Any subsequent source or documentation edit requires a new clean source commit, evidence refresh and verification before preparing another candidate.

From the final GitHub candidate checkout, substitute an absolute, unused output path:

```sh
node scripts/site-release.mjs prepare --output /absolute/path/to/tammy-release-candidate
```

The release source is a self-contained workspace. Copying only `apps/site` is invalid because its `@tammy/invoices` dependency is `workspace:*`. The exported closure includes the site, `packages/invoices`, the root dependency manifest, frozen lockfile, workspace settings, patches and shared TypeScript configuration. Its root build wrapper preserves the nested application layout, stages the resulting Worker at the root, and exposes the existing hosting manifest to the Sites helpers. Site identity and null storage bindings remain unchanged.

`source-provenance.json` records the exact GitHub candidate SHA and exported file hashes. The root export also includes `.npmrc`, the release scripts/tests and `tsconfig.base.json`. Retain this manifest with the source/build artifacts. It describes preparation, not a saved Sites revision or successful deployment.

## 2. Validate the exported workspace

Use the repository-pinned Node.js and pnpm versions: currently Node `24.18.0` and pnpm `11.15.0`. Install from the frozen lockfile in the exported workspace; do not regenerate it merely to make an export install.

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
node scripts/site-release.mjs bundle --output /absolute/path/to/tammy-release-evidence
```

Run `node --test scripts/site-release.test.mjs` from the GitHub candidate as well. `pnpm build` invokes the release wrapper: it verifies source, builds the nested site, stages root `dist/` and writes `build-provenance.json`. `verify` checks the exported source and built-file hashes. `bundle` creates `source.tar.gz`, the source/build provenance manifests, `release-manifest.json` and `dist/` in the external artifact directory. Preserve command results and hashes in the release checklist. If source changes after validation, prepare and validate a new candidate.

The Worker build must include its entrypoint, client assets, hosting metadata and the invoice/quote PDF runtime assets. A build succeeding from the full developer workspace does not prove that an exported source workspace can build independently. CI checks and retained evidence must use the exported workspace.

Local preview/browser checks should exercise invoice and quote creation, actual PDF downloads, printable output, representative Tools/Journal pages and navigation at desktop and 320px widths. Record whether these were performed; unit tests and HTTP preflight mocks are not browser evidence.

## 3. Package a local candidate with the supported Sites helper

Use the installed Sites plugin's scripts; do not vendor or reverse-engineer its authentication. Resolve `<plugin-root>` to the installed directory containing `scripts/` and `skills/`, and use an absolute archive path outside the source tree.

From the exported source root, the supported local build helper is:

```text
node <plugin-root>/scripts/build-site.mjs
```

The repository wrapper verifies the candidate and invokes the installed plugin's supported `package-site.mjs` helper:

```text
node scripts/site-release.mjs package --sites-plugin /absolute/path/to/sites-plugin --output /absolute/path/to/tammy-worker-package
```

It writes `worker.tar.gz` and `worker-manifest.json`, including the archive hash and an explicit unsaved/null Sites commit. The output path must be external to the source tree. Retain the archive hash, source/build provenance and archive entry verification. A local Worker archive is a prepared candidate. It is not yet eligible for `save_site_version` using the GitHub SHA: the configured Sites source branch must contain the exact reconciled source used for that archive.

Do not modify or replace a verified archive while a save is pending. Do not upload source code in place of a Worker deployment archive.

## 4. Open the existing Sites source (access gate)

Read the current Sites skill and native tool contracts when executing this step; helper locations and responses can change. Use `get_site` with the existing project ID and confirm its identity, public audience and current version. Never create a replacement Site to bypass source access.

The current preparation found no source repository credential in `get_site`. No credential was created, and no source push or save was attempted. If there is no already approved, valid source connection, stop this step and report that exact blocker. Obtaining new access requires a separately authorized supported Sites operation; do not create persistent tokens, account secrets or permissions.

With existing authorized access, first confirm there is no active publish-on-push window. A false or omitted acceptance value does not cancel an earlier window. If that cannot be established, stop before pushing and reconcile the existing publication status. This preparation may save an unpublished version; it may not publish one.

Open an empty directory through the supported source helper:

```text
node <plugin-root>/scripts/site-workflow.mjs --project-id appgprj_6a93f33631748191b7c064c033f72b53
```

Launch with a TTY. After `Ready for Site workflow JSON on stdin (input is hidden).`, send one newline-terminated JSON object containing the returned `credential` object and omit `archivePath`. Keep credentials only in session memory; never place them in a command argument, file, environment dump, Git configuration or log. When `auth_mode` is `work_proxy`, use its remote URL with ordinary Git/helper authentication and do not configure a token or auth header.

Retain the complete successful opening result, including `checkout_path`, `project_id` and `commit_sha`. Continue in that returned checkout. The GitHub repository and the configured Sites repository are separate source histories.

## 5. Reconcile source without overwriting concurrent work

Before editing the Sites checkout, compare the opened source against the exported GitHub candidate and the last recorded release mapping. Preserve any Sites-only changes; review and port them into GitHub or resolve the differences explicitly. Do not blindly copy a directory over the checkout or run a destructive `rsync --delete`.

The first release with this workspace layout needs an explicit migration review: old site-root files may need to move under `apps/site`, while the root becomes the workspace/build wrapper. Review additions, moves and removals together. Remove obsolete files only when the review proves they belong to the superseded site layout. Preserve `.git`, checkout-local authentication/runtime state and unrelated work.

Review both hosting manifests and retain the exact project ID, existing fields and null D1/R2 bindings. Record the GitHub candidate SHA in the release provenance. The eventual Sites commit SHA will generally differ because it commits the exported/reconciled source to a different repository.

Install dependencies and rerun checks/builds on the reconciled source. If reconciliation changes application source, first review and commit the equivalent change in GitHub, then produce a new candidate and provenance; do not label a different tree as the tested GitHub candidate.

## 6. Synchronize, package, and save without deployment

Run the same source helper in the opened checkout with the complete previous opening result as `source`, the authorized credential object, ordered command arrays for any remaining checks/build, and an absolute `archivePath`. The framework build command is `["node", "<plugin-root>/scripts/build-site.mjs"]`. Only omit checks/builds already completed successfully on unchanged source.

The helper owns Git preparation, commit/push, source-HEAD verification and packaging. If its push is rejected because remote source advanced, preserve the local commit, reconcile the new remote state, then rebuild. Never force-push or reuse an archive built before the reconciliation.

After successful completion, pass the helper's returned `project_id`, `commit_sha` and `archive` unchanged to the native `save_site_version` operation. Its `commit_sha` must match the current HEAD of the configured Sites source branch and the source used to build the archive. Do not substitute the GitHub commit.

Record the saved version ID/number and returned source revision/archive metadata. Read back the saved version. **Stop here.** Do not call `deploy_site_version`, `deploy_private_site_version`, `save_version_and_deploy_private`, or enable publish-on-push during this preparation. A saved version number is not evidence of publication.

If save access is blocked, retain the verified local source/build/archive, its hashes, the PR URL and this checklist. Report the missing source connection/save step without claiming a saved version exists.

## 7. Production publication is a separate release action

After explicit authorization to publish, refresh Site state and the intended saved version. Verify that its source revision and archive hashes match the reviewed release record and that its audience remains public. For Tammy's public audience, use the native `deploy_site_version` operation for that saved version. Do not change audience to use a private-only convenience operation.

If deployment is non-terminal (`pending`, `building`, `publishing`), poll its returned deployment ID until terminal. Continue an existing deployment instead of starting duplicates. Record the final native status and URL. A failed deployment is a failure even when the source build or save succeeded.

Then perform live HTTP and browser checks. For a canonical migration release, run:

```sh
pnpm --dir apps/site site:preflight -- https://tammy.tax
```

This read-only check rejects off-host redirects, non-200 responses, incorrect canonicals, noindex, and inconsistent/duplicate/foreign-origin sitemap entries. It does not verify actual PDF interactions, mobile layout, source provenance or Search Console ownership. Record and investigate every failure.

## Separate reviewed gate: canonical migration to tammy.tax

Domain/provider/SSL activation has already been confirmed, but source canonical migration remains separate from this preparation. Leave `SITE_ORIGIN`, sitemap, robots, llms, RSS, metadata and structured data unchanged in the preparation PR. Consequently, the strict `tammy.tax` canonical preflight is expected to report mismatches until a reviewed migration is deployed; do not suppress those failures or call them a pass.

For the migration, refresh live hostname/TLS/routing evidence, review a single coherent change to `apps/site/lib/site-config.ts` and all materialized/generated metadata, and update matching tests. Search the repository for the old hostname: desktop privacy/support URL contracts and release-evidence tooling also refer to it and need their own compatibility review. Do not silently change those contracts with a site copy operation.

Deploy the reviewed migration through the same source/save path, then require:

- HTTPS 200 responses and no unexpected cross-host redirects for `/`, `/tools`, invoice and quote generators, `/blog`, `/privacy`, and every required preflight route.
- Absolute canonicals, sitemap, robots, RSS and structured data consistently using the intended origin.
- Anonymous invoice and quote PDF downloads/print flow checked in a real browser with sample data and no customer-data upload.
- Desktop and narrow/mobile navigation/layout checked; record physical-touch coverage separately.
- Old-host behavior recorded. Where supported, use exact path-preserving redirects; do not claim that a redirect exists merely because both hosts serve content.
- Search Console Domain property verification and sitemap submission recorded by an authorized operator. Do not predict indexing dates.
- No production analytics collection introduced without the real consent/privacy implementation.

Retain release evidence with the PR and [issue #32](https://github.com/castlemilk/tammy/issues/32): GitHub/Sites revisions, saved version, archive hashes, runner identity, check results, publication status, live route matrix and browser observations.
