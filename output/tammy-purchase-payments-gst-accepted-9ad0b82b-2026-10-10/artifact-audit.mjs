import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { existsSync } from 'node:fs';
import { mkdir, readFile, readdir, stat, writeFile } from 'node:fs/promises';
import { createRequire, registerHooks, stripTypeScriptTypes } from 'node:module';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const [root, directory] = process.argv.slice(2);
assert.ok(root && directory && path.isAbsolute(root) && path.isAbsolute(directory));
const manifest = JSON.parse(await readFile(path.join(directory, 'build-manifest.json'), 'utf8'));
assert.match(manifest.source_revision, /^[0-9a-f]{40}$/u);
assert.equal(manifest.source_dirty, false);
const packageLog = await readFile(path.join(directory, 'package-e2e.log'), 'utf8');
const packagePins = JSON.parse(packageLog.split('\n').find(line => line.startsWith('{"appExecutable"')));
assert.equal(packagePins.sourceRevision, manifest.source_revision);
assert.equal(packagePins.coreSha256, manifest.core_sha256);
const { hashAppBundle } = await import(pathToFileURL(path.join(root, 'scripts/capture-app-store-screenshots.mjs')).href);
assert.equal(createHash('sha256').update(await readFile(packagePins.appExecutable)).digest('hex'), packagePins.appSha256);
const appBundleSha256 = await hashAppBundle(path.resolve(path.dirname(packagePins.appExecutable), '../..'));
assert.equal(createHash('sha256').update(await readFile(packagePins.coreExecutable)).digest('hex'), packagePins.coreSha256);
assert.equal(createHash('sha256').update(await readFile(path.join(directory, 'build-manifest.json'))).digest('hex'), packagePins.manifestSha256);
const sbr = JSON.parse(await readFile(path.join(directory, 'sbr-e2e-result.json'), 'utf8'));
assert.equal(sbr.source_revision, manifest.source_revision);
assert.equal(sbr.playwright_status, 'PASSED');
assert.equal(sbr.socket_violations, 0);
assert.equal(sbr.core_orphans, 0);
assert.equal(sbr.helper_orphans, 0);
const nonEffects = [];
for (const kind of ['cash', 'non-cash', 'cash-mixed']) {
  const record = JSON.parse(await readFile(path.join(directory, kind, 'reconciliation.json'), 'utf8'));
  assert.deepEqual(record.bankingAfter, record.bankingBefore);
  assert.equal(record.bankingBefore.lines.length, 1);
  assert.equal(record.bankingBefore.lines[0].version, '3');
  assert.equal(record.bankingBefore.lines[0].status, 3);
  assert.equal(record.sbrSimulatorTransportCount, 0);
  nonEffects.push({ kind, unchangedBanking: true, sbrSimulatorTransportCount: 0 });
}
const requireDesktop = createRequire(path.join(root, 'apps/desktop/package.json'));
const generated = pathToFileURL(path.join(root, 'packages/connect-client/src/gen/')).href;
registerHooks({ resolve(specifier, context, nextResolve) {
  try { return nextResolve(specifier, context); } catch (error) {
    if (context.parentURL?.startsWith(generated) && specifier.startsWith('.') && specifier.endsWith('.js')) {
      const candidate = new URL(specifier.replace(/\.js$/u, '.ts'), context.parentURL);
      if (existsSync(fileURLToPath(candidate))) return nextResolve(candidate.href, context);
    }
    throw error;
  }
}});
const compiled = '/private/tmp/tammy-phase2-artifact-audit-modules-2026-10-10';
await mkdir(compiled, { recursive: true, mode: 0o700 });
const support = path.join(root, 'apps/desktop/tests/e2e/support');
for (const name of ['reporting-export-artifacts', 'purchase-pdf-provenance', 'purchase-export-assertions']) {
  let source = stripTypeScriptTypes(await readFile(path.join(support, `${name}.ts`), 'utf8'));
  source = source.replaceAll('"./reporting-export-artifacts.js"', '"./reporting-export-artifacts.mjs"');
  source = source.replaceAll('"./purchase-pdf-provenance.js"', '"./purchase-pdf-provenance.mjs"');
  for (const dependency of ['csv-parse/sync', 'pdfjs-dist/legacy/build/pdf.mjs']) {
    source = source.replaceAll(JSON.stringify(dependency), JSON.stringify(pathToFileURL(requireDesktop.resolve(dependency)).href));
  }
  await writeFile(path.join(compiled, `${name}.mjs`), source, { mode: 0o600 });
}
const { assertPurchaseExportArtifacts } = await import(pathToFileURL(path.join(compiled, 'purchase-export-assertions.mjs')).href);
const { parseReportCsv, readPdfPages } = await import(pathToFileURL(path.join(compiled, 'reporting-export-artifacts.mjs')).href);
const { fromBinary } = requireDesktop('@bufbuild/protobuf');
const { BasWorkpaperSchema } = await import(pathToFileURL(requireDesktop.resolve('@tammy/connect-client/tammy/v1/tax_pb.js')).href);
async function walk(base) {
  const files = [];
  for (const entry of await readdir(base, { withFileTypes: true })) {
    const item = path.join(base, entry.name);
    if (entry.isDirectory()) files.push(...await walk(item));
    else if (entry.isFile()) files.push(item);
    else throw new Error('NON_REGULAR_ACCEPTANCE_ARTIFACT');
  }
  return files.sort();
}
const originals = (await walk(directory)).filter(file => !path.basename(file).startsWith('independent-') && !file.includes('/visual/'));
const comparisons = [];
for (const sidecar of originals.filter(file => file.endsWith('.core.pb'))) {
  const stem = sidecar.slice(0, -'.core.pb'.length);
  const csv = `${stem}.csv`;
  const pdf = path.basename(stem) === 'restarted' ? path.join(path.dirname(stem), 'retained-stale.pdf') : `${stem}.pdf`;
  assert.ok(existsSync(csv) && existsSync(pdf), `missing artifact pair ${stem}`);
  const bas = fromBinary(BasWorkpaperSchema, await readFile(sidecar));
  await assertPurchaseExportArtifacts(csv, pdf, bas);
  const parsed = await parseReportCsv(csv);
  const pages = await readPdfPages(pdf);
  assert.equal((await stat(csv)).mode & 0o777, 0o600);
  assert.equal((await stat(pdf)).mode & 0o777, 0o600);
  comparisons.push({
    csv: path.relative(directory, csv), pdf: path.relative(directory, pdf), core: path.relative(directory, sidecar),
    workpaperId: bas.id, version: bas.version.toString(), claimedGstMinor: bas.gstCredits1b?.minorUnits.toString(),
    candidateGstMinor: bas.legacyCandidateGst?.minorUnits.toString() ?? null,
    sourceRows: bas.purchaseSources.length, reviewRows: bas.purchaseReviews.length, supportRows: bas.legacyPurchaseSupport.length,
    csvRows: parsed.rows.length, pages: pages.length,
  });
}
assert.equal(comparisons.length, 18, 'all eighteen purchase export acquisitions must have retained Core comparisons');
const csvFiles = originals.filter(file => file.endsWith('.csv'));
assert.equal(csvFiles.length, comparisons.length, 'every actual purchase CSV must be compared');
const artifacts = [];
for (const file of originals) {
  const bytes = await readFile(file);
  artifacts.push({ file: path.relative(directory, file), bytes: bytes.length, sha256: createHash('sha256').update(bytes).digest('hex') });
}
const result = { auditedAt: new Date().toISOString(), sourceRevision: manifest.source_revision, directory,
  comparisonCount: comparisons.length, distinctPdfCount: new Set(comparisons.map(value => value.pdf)).size,
  distinctPdfPages: comparisons.filter(value => !value.csv.endsWith('/restarted.csv')).reduce((sum, value) => sum + value.pages, 0),
  artifacts, comparisons, packagePins, appBundleSha256, nonEffects, sbr };
await writeFile(path.join(directory, 'independent-inspection.json'), `${JSON.stringify(result, null, 2)}\n`, { mode: 0o600 });
console.log(JSON.stringify({ comparisons: result.comparisonCount, pdfs: result.distinctPdfCount, pages: result.distinctPdfPages, artifacts: artifacts.length }));
