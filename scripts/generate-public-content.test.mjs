import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  generatePublicContent,
  parsePolicyMarkdown,
  run,
} from "./generate-public-content.mjs";

const identity = {
  schemaVersion: 1,
  appStoreName: "Tammy Accounting",
  installedName: "Tammy",
  bundleIdentifier: "com.tammy.desktop",
  publisher: "Gamma Systems Pty Ltd",
  supportEmail: "ben.ebsworth@gmail.com",
  locale: "en-AU",
  primaryCategory: "Finance",
  secondaryCategory: "Business",
  minimumMacOSVersion: "14.0",
  architectures: ["arm64"],
  copyright: "© 2026 Gamma Systems Pty Ltd",
  capabilityBoundary: { reporting: "preparation-only", atoLodgement: "not-lodged" },
};

const dataRemoval = {
  schemaVersion: 1,
  containerRelativePath: "Library/Containers/com.tammy.desktop",
  groupContainerSuffix: "com.tammy.desktop",
  keychainServices: [
    "com.tammy.workspace",
    "com.tammy.attempt-journal-anchor.v1",
    "com.tammy.audit-mirror",
    "com.tammy.sbr.production",
  ],
};

const privacy = `# Tammy privacy policy

Effective 30 August 2026.

## Data handled by Tammy

Use *local* records, \`Keychain\`, and [support](mailto:ben.ebsworth@gmail.com).

- An [HTTPS link](https://example.com/support)
- Plain text
`;

const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const generatorPath = path.join(repositoryRoot, "scripts/generate-public-content.mjs");

test("parses the intentionally small policy Markdown contract", () => {
  assert.deepEqual(parsePolicyMarkdown(privacy), {
    effectiveDate: "30 August 2026",
    sections: [
      {
        heading: "Data handled by Tammy",
        blocks: [
          {
            kind: "paragraph",
            inlines: [
              { type: "text", value: "Use " },
              { type: "emphasis", value: "local" },
              { type: "text", value: " records, " },
              { type: "code", value: "Keychain" },
              { type: "text", value: ", and " },
              { type: "link", text: "support", href: "mailto:ben.ebsworth@gmail.com" },
              { type: "text", value: "." },
            ],
          },
          {
            kind: "list",
            items: [
              [
                { type: "text", value: "An " },
                { type: "link", text: "HTTPS link", href: "https://example.com/support" },
              ],
              [{ type: "text", value: "Plain text" }],
            ],
          },
        ],
      },
    ],
  });
});

for (const [name, line] of [
  ["indented ATX heading", "  ## Not a section"],
  ["indented blockquote", "   > quoted"],
  ["indented alternate list", " + alternate"],
  ["indented numbered list", "  1. numbered"],
  ["indented code", "    code"],
  ["fenced code", "```"],
  ["table", "| header |"],
  ["thematic break", "  ---"],
  ["setext dash heading", "---"],
  ["setext equals heading", "==="],
  ["indented approved list", " - still unsupported"],
]) {
  test(`rejects ${name}`, () => {
    assert.throws(
      () => parsePolicyMarkdown(privacy.replace("Use *local*", `${line}\n\nUse *local*`)),
      /policy Markdown/i,
    );
  });
}

for (const [name, markdown] of [
  ["raw HTML", privacy.replace("Use", "<b>Use</b>" )],
  ["images", privacy.replace("Use", "![image](https://example.com/image.png) Use")],
  ["relative links", privacy.replace("mailto:ben.ebsworth@gmail.com", "/support")],
  ["scripts", privacy.replace("Use", "<script>alert(1)</script> Use")],
  ["duplicate headings", privacy.replace("## Data handled by Tammy", "## Data handled by Tammy\n\n## Data handled by Tammy")],
  ["unsupported syntax", privacy.replace("Use", "> Use")],
  ["malformed inline syntax", privacy.replace("Use", "Use ]")],
  ["underscore emphasis", privacy.replace("Use", "_Use_")],
  ["strikethrough", privacy.replace("Use", "~~Use~~")],
  ["backslash escapes", privacy.replace("Use", "\\Use")],
  ["malformed link URLs", privacy.replace("mailto:ben.ebsworth@gmail.com", "mailto:not an email")],
  ["mailto recipient other than support", privacy.replace("mailto:ben.ebsworth@gmail.com", "mailto:other@example.com")],
  ["mailto header payload", privacy.replace("mailto:ben.ebsworth@gmail.com", "mailto:ben.ebsworth@gmail.com?subject=hello")],
  ["raw HTML in H1", privacy.replace("Tammy privacy policy", "Tammy <b>privacy</b> policy")],
  ["unsupported syntax in effective date", privacy.replace("30 August 2026", "*30 August 2026*")],
  ["raw HTML in H2", privacy.replace("Data handled by Tammy", "Data <b>handled</b> by Tammy")],
  ["missing effective date", privacy.replace("Effective 30 August 2026.\n\n", "")],
]) {
  test(`rejects ${name}`, () => {
    assert.throws(() => parsePolicyMarkdown(markdown), /policy Markdown/i);
  });
}

test("generates safe deterministic TypeScript from trusted JSON inputs and parsed policy", () => {
  const output = generatePublicContent({ identity, privacy, desktopPackage: { version: "0.1.0" }, dataRemoval });
  assert.equal(output, generatePublicContent({ identity, privacy, desktopPackage: { version: "0.1.0" }, dataRemoval }));
  assert.match(output, /export interface PolicySection/);
  assert.match(output, /export type PolicyInline/);
  assert.match(output, /readonly kind: "paragraph"/);
  assert.match(output, /"kind": "list"/);
  assert.doesNotMatch(output, /"type": "paragraph"|"type": "list"/);
  assert.match(output, /"marketingVersion": "0.1.0"/);
  assert.match(output, /"schemaVersion": 1/);
  assert.match(output, /"effectiveDate": "30 August 2026"/);
  assert.match(output, /"containerDisplayPath": "~\/Library\/Containers\/com\.tammy\.desktop"/);
  assert.match(output, /"groupContainerSuffix": "com\.tammy\.desktop"/);
  for (const service of dataRemoval.keychainServices) assert.ok(output.includes(JSON.stringify(service)));
  assert.doesNotMatch(output, /com\.tammy\.sbr\.(?:development|simulator)|com\.tammy\.workspace\.dev/);
  assert.doesNotMatch(output, /<script>|<b>|!\[/);
  for (const value of ["Tammy Accounting", "Tammy", "com.tammy.desktop", "Gamma Systems Pty Ltd", "ben.ebsworth@gmail.com", "en-AU", "Finance", "Business", "14.0", "arm64", "© 2026 Gamma Systems Pty Ltd", "preparation-only", "not-lodged", "https://example.com/support"]) {
    assert.ok(output.includes(JSON.stringify(value)), `${value} must be a JSON string literal`);
  }
});

for (const [name, changedIdentity] of [
  ["schema version", { ...identity, schemaVersion: 2 }],
  ["extra top-level field", { ...identity, extra: "drift" }],
  ["bundle identifier", { ...identity, bundleIdentifier: "com.example.tammy" }],
  ["architecture", { ...identity, architectures: ["x64"] }],
  ["nested capability", { ...identity, capabilityBoundary: { ...identity.capabilityBoundary, reporting: "lodged" } }],
  ["nested capability extra field", { ...identity, capabilityBoundary: { ...identity.capabilityBoundary, extra: "drift" } }],
]) {
  test(`rejects canonical identity drift: ${name}`, () => {
    assert.throws(
      () => generatePublicContent({ identity: changedIdentity, privacy, desktopPackage: { version: "0.1.0" }, dataRemoval }),
      /MACOS_STORE_IDENTITY_INVALID/,
    );
  });
}

for (const version of ["0.1", "01.1.0", "1.01.0", "1.0.01", "1.0.0-", "1.0.0-alpha..1", "1.0.0+build..1"]) {
  test(`rejects invalid desktop semantic version ${version}`, () => {
  assert.throws(
    () => generatePublicContent({ identity, privacy, desktopPackage: { version }, dataRemoval }),
    /semantic version/i,
  );
  });
}

test("writes byte-stable generated content to an explicit temporary output path", async (context) => {
  const temporaryDirectory = await mkdtemp(path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-public-content-"));
  context.after(() => rm(temporaryDirectory, { recursive: true, force: true }));
  const outputPath = path.join(temporaryDirectory, "public-content.generated.ts");
  const policyPath = path.join(temporaryDirectory, "PRIVACY.md");
  const identityPath = path.join(temporaryDirectory, "store-identity.json");
  const packagePath = path.join(temporaryDirectory, "package.json");
  const dataRemovalPath = path.join(temporaryDirectory, "data-removal.json");
  await Promise.all([
    writeFile(policyPath, privacy),
    writeFile(identityPath, JSON.stringify(identity)),
    writeFile(packagePath, JSON.stringify({ version: "0.1.0" })),
    writeFile(dataRemovalPath, JSON.stringify(dataRemoval)),
  ]);

  assert.deepEqual(await run({ policyPath, identityPath, packagePath, dataRemovalPath, outputPath }), { written: true });
  const first = await readFile(outputPath, "utf8");
  const firstStat = await stat(outputPath);
  assert.deepEqual(await run({ policyPath, identityPath, packagePath, dataRemovalPath, outputPath }), { written: false });
  assert.equal(await readFile(outputPath, "utf8"), first);
  assert.equal((await stat(outputPath)).ino, firstStat.ino, "unchanged output is not replaced");

  await writeFile(policyPath, privacy.replace("Plain text", "Changed text"));
  assert.deepEqual(await run({ policyPath, identityPath, packagePath, dataRemovalPath, outputPath }), { written: true });
  assert.notEqual((await stat(outputPath)).ino, firstStat.ino, "changed output is atomically replaced");
  assert.match(await readFile(outputPath, "utf8"), /Changed text/);
});

test("CLI --check never creates or changes generated output", async (context) => {
  const temporaryDirectory = await mkdtemp(
    path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-public-content-check-"),
  );
  context.after(() => rm(temporaryDirectory, { recursive: true, force: true }));
  const outputPath = path.join(temporaryDirectory, "public-content.generated.ts");

  const missing = spawnSync(
    process.execPath,
    [generatorPath, "--check", "--output", outputPath],
    { cwd: repositoryRoot, encoding: "utf8" },
  );
  assert.notEqual(missing.status, 0);
  await assert.rejects(stat(outputPath), { code: "ENOENT" });

  await writeFile(outputPath, "stale-content\n");
  const stale = spawnSync(
    process.execPath,
    [generatorPath, "--check", "--output", outputPath],
    { cwd: repositoryRoot, encoding: "utf8" },
  );
  assert.notEqual(stale.status, 0);
  assert.equal(await readFile(outputPath, "utf8"), "stale-content\n");
});

test("cleans up an exclusively-created sibling temporary file after rename failure", async (context) => {
  const temporaryDirectory = await mkdtemp(path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-public-content-"));
  context.after(() => rm(temporaryDirectory, { recursive: true, force: true }));
  const policyPath = path.join(temporaryDirectory, "PRIVACY.md");
  const identityPath = path.join(temporaryDirectory, "store-identity.json");
  const packagePath = path.join(temporaryDirectory, "package.json");
  const dataRemovalPath = path.join(temporaryDirectory, "data-removal.json");
  const outputPath = path.join(temporaryDirectory, "public-content.generated.ts");
  await Promise.all([writeFile(policyPath, privacy), writeFile(identityPath, JSON.stringify(identity)), writeFile(packagePath, JSON.stringify({ version: "0.1.0" })), writeFile(dataRemovalPath, JSON.stringify(dataRemoval))]);
  const cleaned = [];
  const fileSystem = {
    async open(filePath, flags) {
      assert.equal(flags, "wx");
      return { async writeFile() {}, async sync() {}, async close() {} };
    },
    readFile,
    async rename() { throw new Error("rename failed"); },
    async rm(filePath) { cleaned.push(filePath); },
  };
  await assert.rejects(() => run({ policyPath, identityPath, packagePath, dataRemovalPath, outputPath, fileSystem }), /rename failed/);
  assert.equal(cleaned.length, 1);
  assert.equal(path.dirname(cleaned[0]), temporaryDirectory);
});

test("retries a temporary-name collision before atomically replacing output", async (context) => {
  const temporaryDirectory = await mkdtemp(path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-public-content-"));
  context.after(() => rm(temporaryDirectory, { recursive: true, force: true }));
  const policyPath = path.join(temporaryDirectory, "PRIVACY.md");
  const identityPath = path.join(temporaryDirectory, "store-identity.json");
  const packagePath = path.join(temporaryDirectory, "package.json");
  const dataRemovalPath = path.join(temporaryDirectory, "data-removal.json");
  const outputPath = path.join(temporaryDirectory, "public-content.generated.ts");
  await Promise.all([writeFile(policyPath, privacy), writeFile(identityPath, JSON.stringify(identity)), writeFile(packagePath, JSON.stringify({ version: "0.1.0" })), writeFile(dataRemovalPath, JSON.stringify(dataRemoval))]);
  let attempts = 0;
  const renamed = [];
  const fileSystem = {
    async open(filePath) {
      attempts += 1;
      if (attempts === 1) {
        const error = new Error("exists");
        error.code = "EEXIST";
        throw error;
      }
      return { async writeFile() {}, async sync() {}, async close() {} };
    },
    readFile,
    async rename(source, destination) { renamed.push([source, destination]); },
    async rm() {},
  };
  assert.deepEqual(await run({ policyPath, identityPath, packagePath, dataRemovalPath, outputPath, fileSystem }), { written: true });
  assert.equal(attempts, 2);
  assert.deepEqual(renamed[0].slice(1), [outputPath]);
});

test("canonical repository policy, identity, and removal inventory generate deterministic public content", async () => {
  const [canonicalPrivacy, canonicalIdentity, desktopPackage, canonicalDataRemoval] = await Promise.all([
    readFile("PRIVACY.md", "utf8"),
    readFile("apps/desktop/release/macos/store-identity.json", "utf8"),
    readFile("apps/desktop/package.json", "utf8"),
    readFile("apps/desktop/release/macos/data-removal.json", "utf8"),
  ]);
  for (const heading of ["## Publisher and scope", "## Website and hosting", "## Support email", "## Retention and deletion", "## Reporting boundary"]) assert.ok(canonicalPrivacy.includes(heading));
  for (const claim of ["Gamma Systems Pty Ltd", "ben.ebsworth@gmail.com", "Local records remain on your Mac until you remove them", "does not lodge ATO or SBR submissions"]) assert.ok(canonicalPrivacy.includes(claim));
  const input = { identity: JSON.parse(canonicalIdentity), privacy: canonicalPrivacy, desktopPackage: JSON.parse(desktopPackage), dataRemoval: JSON.parse(canonicalDataRemoval) };
  assert.equal(generatePublicContent(input), generatePublicContent(input));
});

for (const [name, changedDataRemoval] of [
  ["additional path", { ...dataRemoval, extraPath: "Library/Application Support/Other" }],
  ["changed container path", { ...dataRemoval, containerRelativePath: "Library/Containers/com.example.other" }],
  ["additional service", { ...dataRemoval, keychainServices: [...dataRemoval.keychainServices, "com.example.sentinel"] }],
  ["development service", { ...dataRemoval, keychainServices: dataRemoval.keychainServices.map((service) => service === "com.tammy.sbr.production" ? "com.tammy.sbr.development" : service) }],
]) {
  test(`rejects invented public deletion guidance: ${name}`, () => {
    assert.throws(
      () => generatePublicContent({ identity, privacy, desktopPackage: { version: "0.1.0" }, dataRemoval: changedDataRemoval }),
      /MACOS_DATA_REMOVAL_INVENTORY_INVALID/,
    );
  });
}
