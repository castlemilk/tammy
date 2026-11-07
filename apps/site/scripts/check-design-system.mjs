import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const site = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const design = await readFile(path.join(site, '../../DESIGN.md'), 'utf8');
const tokens = await readFile(path.join(site, 'app/tokens.css'), 'utf8');
const roles = {
  background: 'canvas',
  surface: 'paper',
  foreground: 'ink',
  primary: 'forest',
  'primary-deep': 'forest-deep',
  'muted-foreground': 'muted',
  border: 'rule',
  'field-border': 'field-border',
  muted: 'oat',
  'success-soft': 'sage',
  warning: 'warning',
  'warning-soft': 'warning-soft',
  danger: 'danger',
  apricot: 'apricot',
  butter: 'butter',
};
for (const [contract, runtime] of Object.entries(roles)) {
  const expected = design.match(
    new RegExp(`^  ${contract}: ['"](#[a-fA-F0-9]+)['"]`, 'm'),
  )?.[1];
  const actual = tokens.match(
    new RegExp(`--tammy-${runtime}:\\s*(#[a-fA-F0-9]+);`),
  )?.[1];
  assert.ok(
    expected && actual,
    `Missing token mapping: ${contract} → ${runtime}`,
  );
  assert.equal(
    actual.toLowerCase(),
    expected.toLowerCase(),
    `Token drift: ${contract}`,
  );
}
for (const file of ['app/landing.css', 'app/tools/tools.css']) {
  const css = await readFile(path.join(site, file), 'utf8');
  const aliases = [
    ...css.matchAll(
      /--[lt]-(?:ink|forest|paper|muted|rule|line|sans|body|serif|display):\s*([^;]+);/g,
    ),
  ];
  assert.ok(aliases.length > 4, `Missing route aliases: ${file}`);
  for (const [, value] of aliases)
    assert.match(
      value,
      /^var\(--tammy-[a-z-]+\)$/,
      `Independent route token in ${file}`,
    );
}
console.log(
  `Verified ${Object.keys(roles).length} accepted palette values and both route adapters.`,
);
