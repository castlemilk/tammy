# Tammy: Vercel releases for tammy.tax

On 14 November 2026 the owner asked for tammy.tax to move to Vercel. The domain is registered with Vercel and its DNS is Vercel's, so the move needs no nameserver change. Since then Vercel serves `https://tammy.tax`. The ChatGPT Sites project keeps serving its own origin (`https://tammy-accounting.castlemilk.chatgpt.site`) and is the rollback target. The direct Cloudflare Worker keeps publishing to workers.dev from CI.

## Target

| Item | Value |
| --- | --- |
| Vercel team | gamma systems, slug `document-analyser`, ID `team_xE5DMN3hIo8aPqyHNkBybg8r` |
| Project | `tammy-site`, ID `prj_zlt5QLKUW3QrdkrHuHq7wkqVkU4j` |
| Build | `TAMMY_DEPLOY_TARGET=vercel`: vinext static export (`output: "export"`). Every route is prerendered HTML; invoice and quote PDFs are generated in the browser. No server code is deployed |
| Output | Build Output API v3, prebuilt: `.vercel/output/static/` plus `config.json`. Nothing is built on Vercel |
| CLI | `vercel@56.4.1` through `npx`, credentials from the environment only |
| Canonical origin | Unchanged: `https://tammy-accounting.castlemilk.chatgpt.site`. Moving it to tammy.tax is a separate reviewed change (see [Sites runbook](SITE-RELEASE-RUNBOOK.md#separate-reviewed-gate-canonical-migration-to-tammytax)) |

`site-release.mjs vercel-output` copies the verified `dist/client` into `.vercel/output/static`, leaving out hidden build metadata such as `.vite/`, and refuses any byte that differs from `build-provenance.json`. Its `config.json` uses the routes that `vercel build` 56.4.1 generates for `cleanUrls: true` and `trailingSlash: false`: `/x.html`, `/x/` and `/index` redirect with 308, each `x.html` is served at `/x`, `.rsc` payloads are served as `text/x-component`, `/_next/static/*` is cached as immutable, and misses get `404.html` with status 404. The routes live in `VERCEL_ROUTES` in `scripts/site-release.mjs`; change them only with a matching test.

## Release

Commit and attest first, exactly as in [AGENTS.md](../../AGENTS.md): finish source and documentation, commit, run `proto:descriptors:evidence` with `TAMMY_SOURCE_REVISION` set to that commit, commit only the manifest as its immediate child, verify again. The candidate is the evidence commit, pushed to GitHub.

From a clean checkout of the candidate, with the pinned toolchain (`mise exec --`):

```sh
node --test scripts/site-release.test.mjs scripts/site-ci.test.mjs
node scripts/site-release.mjs prepare --target vercel --output /abs/tammy-vercel-source
cd /abs/tammy-vercel-source
pnpm install --frozen-lockfile --filter sites-project...
pnpm --dir apps/site typecheck && pnpm --dir apps/site lint && pnpm --dir apps/site test
pnpm --dir apps/site editorial:lint && pnpm --dir apps/site seo:check
pnpm build
node scripts/site-release.mjs verify
node scripts/site-release.mjs vercel-output --output /abs/tammy-vercel-output
VERCEL_ORG_ID=team_xE5DMN3hIo8aPqyHNkBybg8r VERCEL_PROJECT_ID=prj_zlt5QLKUW3QrdkrHuHq7wkqVkU4j \
  node scripts/site-release.mjs vercel-deploy --output /abs/tammy-vercel-output
```

`vercel-deploy` rechecks the static files and routing against the build record and refuses any other file beside the output (the CLI would read a stray `vercel.json`). It then runs `vercel deploy --prebuilt --prod --format=json`. Locally it uses the signed-in Vercel CLI session; in GitHub Actions it requires `VERCEL_TOKEN`. It never takes a token as an argument and never logs in, and the CLI receives only an allowlist of environment variables (path, home, temp, locale, proxy and npm settings, plus the three `VERCEL_` values), so other tokens and the variables the CLI uses to detect AI agents never reach it. `npx` fetches the exact `vercel@56.4.1` from npm; it is not in the lockfile.

It writes `deployment-record.json` beside the output. The record is `published` only when the CLI's JSON reports a deployment ID, a `*.vercel.app` URL, `readyState: READY` and `target: production`. Anything else, including a CLI failure, is `outcome-uncertain` with the last 4,000 characters of CLI output kept (any token redacted). An existing record blocks a second attempt; inspect the project's deployments (`vercel ls tammy-site --scope document-analyser`) before retrying.

A production deployment moves every production domain of the project, including tammy.tax, at once.

## Acceptance

After each release, check the deployment URL and `https://tammy.tax` with sample data only:

- Every page in `public/sitemap.xml`, `/tools`, `/blog`, `/privacy`, `/support`, `sitemap.xml`, `robots.txt`, `llms.txt` and `rss.xml` return 200; a missing route returns 404; `/tools.html` and `/tools/` redirect to `/tools`.
- Real invoice and quote PDF downloads, changed and downloaded again, carry the changed data.
- The landing page at desktop and phone widths, no sideways scroll at 320px, no console errors.
- The canonical tags still name the Sites origin until the canonical migration is reviewed.

## Domain and rollback

`tammy.tax` and `www.tammy.tax` are attached to the project; `www` redirects to the apex with 308. Vercel issues the certificate (the zone's CAA records allow Let's Encrypt).

Before the move, the apex had two A records pointing to the Sites custom hostname on Cloudflare: `172.66.3.26` and `162.159.143.30`, TTL 60. Removing them let the zone's default Vercel ALIAS serve the apex. The `_cf-custom-hostname` and `_openai-site-verification` TXT records stay, so the Sites hostname can be restored.

To roll back to Sites:

```sh
vercel dns add tammy.tax @ A 172.66.3.26 --scope document-analyser
vercel dns add tammy.tax @ A 162.159.143.30 --scope document-analyser
```

To roll back to an earlier Vercel release instead, promote its deployment: `vercel promote <deployment-url> --scope document-analyser`.

## Not automatic yet

Merging to master publishes the Cloudflare Worker, not Vercel. Automatic Vercel releases need a Vercel token stored as the GitHub secret `VERCEL_TOKEN` and a CI job that runs the commands above on the Cuttlefish runner. Creating that token and secret is a separate owner decision. Until then, each tammy.tax release is run by hand with the commands above.
