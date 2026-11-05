# Tammy Sites publication and custom-domain acceptance

Status: source preparation only, 5 November 2026. Issue [#32](https://github.com/castlemilk/tammy/issues/32). A passing build or correctly written DNS record **does not** prove that a Site was published.

## Observed domain state (read-only Vercel check)

- Domain: `tammy.tax`, purchased through Vercel, team `gamma systems`.
- Nameservers report `ns1.vercel-dns.com` and `ns2.vercel-dns.com`.
- Apex A records in the Vercel-managed zone: `162.159.143.30` and `172.66.3.26`, exactly as supplied by ChatGPT Sites in the current setup conversation.
- `_openai-site-verification` and `_cf-custom-hostname` TXT records were present.
- There is also Vercel's automatically created apex/wildcard ALIAS record. The Vercel config API recommended its own hosting IP and reported `misconfigured`, **because it checks configuration for a Vercel-hosted deployment**. Do not replace the ChatGPT Sites A records with Vercel's suggested IP unless the hosting decision actually changes.
- Custom-domain verification status **inside Sites** is not available from the connected Vercel API. External page fetching did not establish that the new host serves Tammy, nor that the latest site source is deployed.

## Publication process (not automated in this PR)

1. Open the Tammy Site in ChatGPT Sites and inspect its existing version and custom-domain status. Use the owner's **Settings → Add domain** / refresh controls.
2. Review stacked GitHub site PRs #13–#20 (and subsequent source changes) as a coherent release tree; resolve build/QA failures and inspect a preview. A GitHub PR is not the Sites project. Do not overwrite database changes blindly when publishing.
3. Confirm `tammy.tax` is accepted/verified by Sites, with HTTPS and correct root content. Only then prepare an atomically updated canonical URL release for `SITE_ORIGIN`, metadata, sitemap, robots, llms and RSS.
4. Test the intended built version in a browser, including anonymous invoice PDF download, the printable quote and representative Tools/Journal pages at 320px and desktop widths.
5. Deploy the reviewed version using **Sites publishing controls**, confirm audience/public access, and record the resulting version/URL.
6. Run `pnpm --dir apps/site site:preflight -- https://tammy.tax` from the release source. Check all failures and rerun after fixes.
7. Check old hosted URL handling. Where Sites supports an exact one-to-one redirect, map each path to the same path on the new host. Do not redirect all routes to the new homepage, and do not claim a redirect exists merely because an alternate domain is active.
8. Verify Google Search Console Domain property and submit the canonical sitemap. Inspect representative tool/blog URLs, indexing, source host duplicates and render-visible structured data. Capture observations rather than predict indexing dates.

## Hard acceptance

- Correct content served with HTTP 200 over HTTPS from `tammy.tax`; valid TLS and no unexpected cross-host redirects.
- Matching, absolute canonical for /, /tools, /tools/invoice-generator, /tools/quote-generator, /blog, plus all indexable URLs.
- Current sitemap, robots, RSS and structured data agree with the published domain.
- Invoice PDF actual-browser E2E; quote print or Save as PDF functionality with no customer-data upload.
- No production analytics collection before real consent/privacy implementation.
- Explicit evidence in issue #32: date, executed command, status matrix, Sites version, canonical result, representative mobile/browser test and Search Console property state.

## Tooling

`node scripts/live-site-preflight.mjs https://tammy.tax` is a **read-only** HTTP smoke test. It rejects redirects away from the target, non-200 pages, incorrect canonical URLs, noindex, mismatched robots/sitemap and duplicate/foreign-origin sitemap entries.

`node --test scripts/live-site-preflight.test.mjs` runs only deterministic mocks and is safe in CI. It does **not** simulate a real deployment or verify SSL ownership in the Sites dashboard.

### Blocking items

No Sites editor/publishing action is exposed by the connected GitHub/Vercel actions in this Chat-mode task. Production publication and Sites owner-side custom-domain activation remain pending. Preserve the original Sites hostname as the source canonical until those controls have confirmed an actual migration.
