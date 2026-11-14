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
| Canonical origin | `https://tammy.tax` since 14 November 2026 (`SITE_ORIGIN` in `lib/site-config.ts`). The desktop app's privacy and support links and the macOS App Store evidence tooling (`scripts/check-public-site.mjs`) still use the former Sites origin, which stays published; moving them is a separate desktop compatibility review |

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
node scripts/site-release.mjs vercel-verify-live --output /abs/tammy-vercel-output
pnpm --dir apps/site site:preflight -- https://tammy.tax
```

`vercel-verify-live` fetches `/`, `/tools`, `/sitemap.xml` and one hashed script from https://tammy.tax and requires each to match the deployed file byte for byte, retrying for about 30 seconds. It writes `live-verification.json` beside the record. A deployment that is READY but did not take the domain fails here, even though route and canonical checks would pass.

`vercel-deploy` rechecks the static files and routing against the build record and refuses any other file beside the output (the CLI would read a stray `vercel.json`). It then runs `vercel deploy --prebuilt --prod --format=json`. Locally it uses the signed-in Vercel CLI session; in GitHub Actions it requires `VERCEL_TOKEN`. It never takes a token as an argument and never logs in, and the CLI receives only an allowlist of environment variables (path, home, temp, locale, proxy and npm settings, plus the three `VERCEL_` values), so other tokens and the variables the CLI uses to detect AI agents never reach it. `npx` fetches the exact `vercel@56.4.1` from npm; it is not in the lockfile.

It writes `deployment-record.json` beside the output. The record is `published` only when the CLI's JSON reports a deployment ID, a `*.vercel.app` URL, `readyState: READY` and `target: production`. Anything else, including a CLI failure, is `outcome-uncertain` with the last 4,000 characters of CLI output kept (any token redacted). An existing record blocks a second attempt; inspect the project's deployments (`vercel ls tammy-site --scope document-analyser`) before retrying.

A production deployment moves every production domain of the project, including tammy.tax, at once.

## Acceptance

After each release, check the deployment URL and `https://tammy.tax` with sample data only:

- Every page in `public/sitemap.xml`, `/tools`, `/blog`, `/privacy`, `/support`, `sitemap.xml`, `robots.txt`, `llms.txt` and `rss.xml` return 200; a missing route returns 404; `/tools.html` and `/tools/` redirect to `/tools`.
- Real invoice and quote PDF downloads, changed and downloaded again, carry the changed data.
- The landing page at desktop and phone widths, no sideways scroll at 320px, no console errors.
- `pnpm --dir apps/site site:preflight -- https://tammy.tax` passes: every page's canonical, the sitemap and robots.txt use `https://tammy.tax`, with no redirect off the host.

## Domain and rollback

`tammy.tax` and `www.tammy.tax` are attached to the project; `www` redirects to the apex with 308. Vercel issues the certificate (the zone's CAA records allow Let's Encrypt).

Before the move, the apex had two A records pointing to the Sites custom hostname on Cloudflare: `172.66.3.26` and `162.159.143.30`, TTL 60. Removing them let the zone's default Vercel ALIAS serve the apex. The `_cf-custom-hostname` and `_openai-site-verification` TXT records stay, so the Sites hostname can be restored.

To roll back to Sites:

```sh
vercel dns add tammy.tax @ A 172.66.3.26 --scope document-analyser
vercel dns add tammy.tax @ A 162.159.143.30 --scope document-analyser
```

To roll back to an earlier Vercel release instead, promote its deployment: `vercel promote <deployment-url> --scope document-analyser`.

## Automatic releases from master

The **Tammy website** workflow has a `deploy-vercel` job. On every master push (or a manual run with `deploy_vercel: true`), `verify-site` also exports the `vercel` target, builds and verifies it, and uploads `tammy-vercel-preparation-<sha>`. `deploy-vercel` then:

1. checks it runs on the Cuttlefish lane;
2. downloads that exact artifact and rechecks its revision, target and hashes;
3. writes the reviewed output;
4. runs `vercel-deploy` with the token in that one step only;
5. runs `vercel-verify-live` to confirm tammy.tax serves those exact bytes;
6. runs `live-site-preflight.mjs https://tammy.tax`.

It never rebuilds, uploads only `deployment-record.json` and `live-verification.json`, and cannot be cancelled mid-publish.

The job is skipped until the repository is configured. One-time setup, by the owner:

1. Create a Vercel access token scoped to the gamma systems team (Vercel dashboard → Account Settings → Tokens), with an expiry you are comfortable rotating.
2. Store it without echoing it: `gh secret set VERCEL_TOKEN --repo castlemilk/tammy` and paste when prompted.
3. Set the two non-secret variables, which also switch the job on:
   `gh variable set VERCEL_ORG_ID --repo castlemilk/tammy --body team_xE5DMN3hIo8aPqyHNkBybg8r`
   `gh variable set VERCEL_PROJECT_ID --repo castlemilk/tammy --body prj_zlt5QLKUW3QrdkrHuHq7wkqVkU4j`

Until then, each tammy.tax release is run by hand with the commands above. A merge without the variables still publishes the Cloudflare Worker and leaves tammy.tax unchanged.
