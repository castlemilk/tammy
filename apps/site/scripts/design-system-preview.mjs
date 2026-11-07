import { mkdir, readFile, readdir, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { build } from 'vite';

const siteRoot = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '..',
);
const outDir = path.resolve(
  process.argv[2] ?? path.join(siteRoot, '../../output/design-system'),
);
const assetDir = path.join(siteRoot, 'dist/client');
const cssFiles = (await readdir(assetDir, { recursive: true }))
  .filter((name) => name.endsWith('.css'))
  .sort();
if (!cssFiles.length)
  throw new Error('Build the site before generating its component reference.');
const siteCss = (
  await Promise.all(
    cssFiles.map((name) => readFile(path.join(assetDir, name), 'utf8')),
  )
).join('\n');
await mkdir(outDir, { recursive: true });
await build({
  root: siteRoot,
  configFile: false,
  define: { 'process.env.NODE_ENV': JSON.stringify('production') },
  build: {
    outDir,
    emptyOutDir: false,
    lib: {
      entry: path.join(siteRoot, 'design-system/preview.tsx'),
      name: 'TammyDesignPreview',
      formats: ['iife'],
      fileName: () => 'preview.js',
      cssFileName: 'preview',
    },
  },
});
await writeFile(path.join(outDir, 'site.css'), siteCss);
await writeFile(
  path.join(outDir, 'index.html'),
  `<!doctype html><html lang="en-AU"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Tammy Soft Precision — component reference</title><link rel="stylesheet" href="site.css"><link rel="stylesheet" href="preview.css"></head><body><div id="root"></div><script src="preview.js"></script></body></html>`,
);
console.log(`Local component reference: ${path.join(outDir, 'index.html')}`);
