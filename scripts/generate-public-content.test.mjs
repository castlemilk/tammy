import assert from "node:assert/strict";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import {
  generatePublicContent,
  parsePolicyMarkdown,
  run,
} from "./generate-public-content.mjs";

const identity = {
  appStoreName: "Tammy Accounting",
  installedName: "Tammy",
  publisher: "Gamma Systems Pty Ltd",
  supportEmail: "ben.ebsworth@gmail.com",
  minimumMacOSVersion: "14.0",
  architectures: ["arm64"],
  capabilityBoundary: { reporting: "preparation-only", atoLodgement: "not-lodged" },
};

const privacy = `# Tammy privacy policy

Effective 30 August 2026.

## Data handled by Tammy

Use *local* records, \`Keychain\`, and [support](mailto:ben.ebsworth@gmail.com).

- An [HTTPS link](https://example.com/support)
- Plain text
`;

test("parses the intentionally small policy Markdown contract", () => {
  assert.deepEqual(parsePolicyMarkdown(privacy), {
    effectiveDate: "30 August 2026",
    sections: [
      {
        heading: "Data handled by Tammy",
        blocks: [
          {
            type: "paragraph",
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
            type: "list",
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

for (const [name, markdown] of [
  ["raw HTML", privacy.replace("Use", "<b>Use</b>" )],
  ["images", privacy.replace("Use", "![image](https://example.com/image.png) Use")],
  ["relative links", privacy.replace("mailto:ben.ebsworth@gmail.com", "/support")],
  ["scripts", privacy.replace("Use", "<script>alert(1)</script> Use")],
  ["duplicate headings", privacy.replace("## Data handled by Tammy", "## Data handled by Tammy\n\n## Data handled by Tammy")],
  ["unsupported syntax", privacy.replace("Use", "> Use")],
  ["malformed inline syntax", privacy.replace("Use", "Use ]")],
  ["malformed link URLs", privacy.replace("mailto:ben.ebsworth@gmail.com", "mailto:not an email")],
  ["missing effective date", privacy.replace("Effective 30 August 2026.\n\n", "")],
]) {
  test(`rejects ${name}`, () => {
    assert.throws(() => parsePolicyMarkdown(markdown), /policy Markdown/i);
  });
}

test("generates safe deterministic TypeScript from trusted JSON inputs and parsed policy", () => {
  const output = generatePublicContent({ identity, privacy, desktopPackage: { version: "0.1.0" } });
  assert.equal(output, generatePublicContent({ identity, privacy, desktopPackage: { version: "0.1.0" } }));
  assert.match(output, /export interface PolicySection/);
  assert.match(output, /export type PolicyInline/);
  assert.match(output, /"marketingVersion": "0.1.0"/);
  assert.match(output, /"effectiveDate": "30 August 2026"/);
  assert.doesNotMatch(output, /<script>|<b>|!\[/);
  for (const value of ["Tammy Accounting", "Gamma Systems Pty Ltd", "ben.ebsworth@gmail.com", "https://example.com/support"]) {
    assert.ok(output.includes(JSON.stringify(value)), `${value} must be a JSON string literal`);
  }
});

test("rejects an invalid desktop semantic version", () => {
  assert.throws(
    () => generatePublicContent({ identity, privacy, desktopPackage: { version: "0.1" } }),
    /semantic version/i,
  );
});

test("writes byte-stable generated content to an explicit temporary output path", async (context) => {
  const temporaryDirectory = await mkdtemp(path.join(process.env.TMPDIR ?? os.tmpdir(), "tammy-public-content-"));
  context.after(() => rm(temporaryDirectory, { recursive: true, force: true }));
  const outputPath = path.join(temporaryDirectory, "public-content.generated.ts");
  const policyPath = path.join(temporaryDirectory, "PRIVACY.md");
  const identityPath = path.join(temporaryDirectory, "store-identity.json");
  const packagePath = path.join(temporaryDirectory, "package.json");
  await Promise.all([
    import("node:fs/promises").then(({ writeFile }) => writeFile(policyPath, privacy)),
    import("node:fs/promises").then(({ writeFile }) => writeFile(identityPath, JSON.stringify(identity))),
    import("node:fs/promises").then(({ writeFile }) => writeFile(packagePath, JSON.stringify({ version: "0.1.0" }))),
  ]);

  await run({ policyPath, identityPath, packagePath, outputPath });
  const first = await readFile(outputPath, "utf8");
  await run({ policyPath, identityPath, packagePath, outputPath });
  assert.equal(await readFile(outputPath, "utf8"), first);
});
