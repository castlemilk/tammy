#!/usr/bin/env bash
set -euo pipefail

export CI=true
corepack enable pnpm
corepack prepare pnpm@11.15.0 --activate
# The Linux foundation node may own the shared checkout as the node user.
git config --global --add safe.directory "$PWD"
node --test scripts/site-release.test.mjs scripts/site-ci.test.mjs

site_tmp="$(mktemp -d)"
trap 'rm -rf "$site_tmp"' EXIT
node scripts/site-release.mjs prepare --output "$site_tmp/sites-source"
(
  cd "$site_tmp/sites-source"
  pnpm install --frozen-lockfile --filter sites-project...
  pnpm --dir apps/site typecheck
  pnpm --dir apps/site lint
  pnpm --dir packages/invoices typecheck
  pnpm --dir packages/invoices test
  pnpm --dir apps/site test
  pnpm --dir apps/site editorial:lint
  pnpm --dir apps/site seo:check
  node --test apps/site/scripts/live-site-preflight.test.mjs apps/site/scripts/verify-direct-deployment.test.mjs
  pnpm build
  node scripts/site-release.mjs verify
  node scripts/site-release.mjs bundle --output "$site_tmp/sites-artifact"
)

node scripts/site-release.mjs prepare --target cloudflare --output "$site_tmp/cloudflare-source"
(
  cd "$site_tmp/cloudflare-source"
  pnpm install --frozen-lockfile --filter sites-project...
  pnpm build
  node scripts/site-release.mjs verify
  node scripts/site-release.mjs cloudflare-dry-run --output "$site_tmp/cloudflare-dry-run"
  node scripts/site-release.mjs bundle --output "$site_tmp/cloudflare-artifact"
  cp "$site_tmp/cloudflare-dry-run/dry-run-record.json" "$site_tmp/cloudflare-artifact/"
)

# Retain the two separately built, credential-free candidates on this run.
test -n "${CUTTLE_ARTIFACT_DIR:-}"
mkdir -p "$CUTTLE_ARTIFACT_DIR/sites" "$CUTTLE_ARTIFACT_DIR/cloudflare"
cp -R "$site_tmp/sites-artifact/." "$CUTTLE_ARTIFACT_DIR/sites/"
cp -R "$site_tmp/cloudflare-artifact/." "$CUTTLE_ARTIFACT_DIR/cloudflare/"
