#!/bin/zsh
set -euo pipefail
cd /Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready
rtk proxy mise exec -- node --input-type=module - <<'JS'
import { build } from './node_modules/.pnpm/node_modules/esbuild/lib/main.js';
import { createRequire } from 'node:module';
const requireInvoicePackage = createRequire(new URL('./packages/invoices/package.json', import.meta.url));
await build({
  entryPoints: ['/Users/benebsworth/projects/tammy/output/tammy-invoice-demo/seed-demo.ts'],
  outfile: '/Users/benebsworth/projects/tammy/output/tammy-invoice-demo/seed-demo.mjs',
  bundle: true,
  platform: 'node',
  format: 'esm',
  packages: 'bundle',
  external: [
    '/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/apps/desktop/node_modules/@playwright/test/index.mjs',
  ],
  plugins: [{
    name: 'invoice-pdf-runtime',
    setup(build) {
      build.onResolve({ filter: /^@formepdf\/core$/ }, () => ({
        path: requireInvoicePackage.resolve('@formepdf/core'),
        external: true,
      }));
    },
  }],
});
JS
rtk proxy mise exec -- node /Users/benebsworth/projects/tammy/output/tammy-invoice-demo/seed-demo.mjs
