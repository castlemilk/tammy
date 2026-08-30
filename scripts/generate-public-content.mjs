import { open, readFile, rename, rm } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import path from "node:path";
import { validateMacOSStoreIdentity } from "./check-macos-store.mjs";
import { validateDataRemovalInventory } from "./macos-data-removal.mjs";

const policyError = (message) => new Error(`Invalid policy Markdown: ${message}`);
const semver = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-(?:(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$/;

function assertPlainPolicyText(value, label) {
  if (!value || /[<>!\[\]()*`_~\\]/.test(value)) throw policyError(`${label} contains unsupported Markdown syntax`);
}

function parseLinkUrl(value) {
  if (/\s/.test(value)) throw policyError("link URLs cannot contain whitespace");
  if (value.startsWith("https://")) {
    let url;
    try { url = new URL(value); } catch { throw policyError("HTTPS link URL is malformed"); }
    if (url.protocol !== "https:" || !url.hostname) throw policyError("link must use HTTPS");
    return value;
  }
  if (value.startsWith("mailto:")) {
    const email = value.slice(7);
    if (email !== "ben.ebsworth@gmail.com") throw policyError("mailto links must use the published support address without headers");
    return value;
  }
  throw policyError("links must use HTTPS or mailto URLs");
}

function parseInlines(value) {
  if (!value || /[<>]/.test(value) || value.includes("![")) throw policyError("raw HTML and images are not supported");
  if (/[_~\\]/.test(value)) throw policyError("unsupported Markdown syntax");
  const inlines = [];
  let cursor = 0;
  const appendText = (text) => {
    if (!text) return;
    const last = inlines.at(-1);
    if (last?.type === "text") last.value += text;
    else inlines.push({ type: "text", value: text });
  };
  while (cursor < value.length) {
    const marker = value[cursor];
    if (!"*`[\]()".includes(marker)) {
      const next = value.slice(cursor).search(/[\[\]()*`]/);
      const end = next === -1 ? value.length : cursor + next;
      appendText(value.slice(cursor, end));
      cursor = end;
    } else if ("]()".includes(marker)) {
      throw policyError("malformed inline syntax");
    } else if (marker === "*" || marker === "`") {
      const closing = value.indexOf(marker, cursor + 1);
      if (closing === -1 || closing === cursor + 1) throw policyError(`unclosed ${marker === "*" ? "emphasis" : "code span"}`);
      const content = value.slice(cursor + 1, closing);
      if (content.includes(marker)) throw policyError("nested inline syntax is not supported");
      inlines.push({ type: marker === "*" ? "emphasis" : "code", value: content });
      cursor = closing + 1;
    } else {
      const textEnd = value.indexOf("](", cursor + 1);
      const urlEnd = textEnd === -1 ? -1 : value.indexOf(")", textEnd + 2);
      const text = value.slice(cursor + 1, textEnd);
      if (urlEnd === -1 || !text || /[\[*`\]]/.test(text)) throw policyError("malformed link");
      inlines.push({ type: "link", text, href: parseLinkUrl(value.slice(textEnd + 2, urlEnd)) });
      cursor = urlEnd + 1;
    }
  }
  return inlines;
}

export function parsePolicyMarkdown(source) {
  if (typeof source !== "string" || source.includes("\r")) throw policyError("source must use LF line endings");
  const lines = source.split("\n");
  if (!/^# [^#].+$/.test(lines[0] ?? "")) throw policyError("exactly one H1 is required as the first line");
  assertPlainPolicyText(lines[0].slice(2), "H1");
  if (lines[1] !== "" || !/^Effective [^.]+\.$/.test(lines[2] ?? "") || lines[3] !== "") throw policyError("an effective-date paragraph is required after the H1");
  const effectiveDate = lines[2].slice(10, -1);
  assertPlainPolicyText(effectiveDate, "effective date");
  const sections = [];
  const headings = new Set();
  let section;
  for (let index = 4; index < lines.length; index += 1) {
    const line = lines[index];
    if (line === "") continue;
    const heading = /^## ([^#].+)$/.exec(line);
    if (heading) {
      if (headings.has(heading[1])) throw policyError("duplicate section heading");
      assertPlainPolicyText(heading[1], "H2 heading");
      headings.add(heading[1]);
      section = { heading: heading[1], blocks: [] };
      sections.push(section);
    } else {
      if (!section) throw policyError("content must appear in an H2 section");
      if (/^\s|^#{1,6}\s|^[+*]\s|^\d+[.)]\s|^>|^`{3,}|^~{3,}|\||^(?:-{3,}|={3,})$/.test(line)) throw policyError("unsupported Markdown syntax");
      if (line.startsWith("- ")) {
        const previous = section.blocks.at(-1);
        const list = previous?.kind === "list" ? previous : { kind: "list", items: [] };
        if (list !== previous) section.blocks.push(list);
        list.items.push(parseInlines(line.slice(2)));
      } else section.blocks.push({ kind: "paragraph", inlines: parseInlines(line) });
    }
  }
  if (sections.length === 0) throw policyError("at least one H2 section is required");
  if (lines.filter((line) => /^# /.test(line)).length !== 1) throw policyError("exactly one H1 is required");
  return { effectiveDate, sections };
}

export function generatePublicContent({ identity, privacy, desktopPackage, dataRemoval }) {
  validateMacOSStoreIdentity(identity);
  validateDataRemovalInventory(dataRemoval);
  if (!semver.test(desktopPackage?.version ?? "")) throw new Error("Desktop package version must be a semantic version");
  const content = {
    identity: { schemaVersion: identity.schemaVersion, appStoreName: identity.appStoreName, installedName: identity.installedName, bundleIdentifier: identity.bundleIdentifier, publisher: identity.publisher, supportEmail: identity.supportEmail, locale: identity.locale, primaryCategory: identity.primaryCategory, secondaryCategory: identity.secondaryCategory, minimumMacOSVersion: identity.minimumMacOSVersion, architectures: identity.architectures, copyright: identity.copyright, capabilityBoundary: identity.capabilityBoundary },
    marketingVersion: desktopPackage.version,
    policy: parsePolicyMarkdown(privacy),
    deletionGuidance: {
      containerDisplayPath: `~/${dataRemoval.containerRelativePath}`,
      groupContainerSuffix: dataRemoval.groupContainerSuffix,
      keychainServices: [...dataRemoval.keychainServices],
    },
  };
  return `export type PolicyInline =\n  | { readonly type: "text" | "emphasis" | "code"; readonly value: string }\n  | { readonly type: "link"; readonly text: string; readonly href: string };\n\nexport interface PolicySection {\n  readonly heading: string;\n  readonly blocks: readonly (\n    | { readonly kind: "paragraph"; readonly inlines: readonly PolicyInline[] }\n    | { readonly kind: "list"; readonly items: readonly (readonly PolicyInline[])[] }\n  )[];\n}\n\nexport const publicContent = ${JSON.stringify(content, null, 2)} as const;\n`;
}

const defaultFileSystem = { open, readFile, rename, rm };

async function createSiblingTemporaryFile(outputPath, fileSystem) {
  for (let attempt = 0; attempt < 8; attempt += 1) {
    const temporaryPath = path.join(path.dirname(outputPath), `.${path.basename(outputPath)}.tmp-${randomUUID()}`);
    try {
      return { handle: await fileSystem.open(temporaryPath, "wx"), temporaryPath };
    } catch (error) {
      if (error?.code !== "EEXIST") throw error;
    }
  }
  throw new Error("Unable to create a unique public-content temporary file");
}

async function replaceAtomically(outputPath, output, fileSystem) {
  let temporaryPath;
  let handle;
  try {
    ({ temporaryPath, handle } = await createSiblingTemporaryFile(outputPath, fileSystem));
    await handle.writeFile(output, "utf8");
    await handle.sync();
    await handle.close();
    handle = undefined;
    await fileSystem.rename(temporaryPath, outputPath);
    temporaryPath = undefined;
  } finally {
    if (handle) await handle.close().catch(() => {});
    if (temporaryPath) await fileSystem.rm(temporaryPath, { force: true }).catch(() => {});
  }
}

export async function run({ policyPath = "PRIVACY.md", identityPath = "apps/desktop/release/macos/store-identity.json", packagePath = "apps/desktop/package.json", dataRemovalPath = "apps/desktop/release/macos/data-removal.json", outputPath = "apps/site/content/public-content.generated.ts", fileSystem = defaultFileSystem } = {}) {
  const [privacy, identityText, packageText, dataRemovalText] = await Promise.all([fileSystem.readFile(policyPath, "utf8"), fileSystem.readFile(identityPath, "utf8"), fileSystem.readFile(packagePath, "utf8"), fileSystem.readFile(dataRemovalPath, "utf8")]);
  const output = generatePublicContent({ identity: JSON.parse(identityText), privacy, desktopPackage: JSON.parse(packageText), dataRemoval: JSON.parse(dataRemovalText) });
  try {
    if (await fileSystem.readFile(outputPath, "utf8") === output) return { written: false };
  } catch (error) {
    if (error?.code !== "ENOENT") throw error;
  }
  await replaceAtomically(outputPath, output, fileSystem);
  return { written: true };
}

if (import.meta.main) {
  const outputIndex = process.argv.indexOf("--output");
  if (outputIndex !== -1 && !process.argv[outputIndex + 1]) throw new Error("--output requires a path");
  await run(outputIndex === -1 ? {} : { outputPath: path.resolve(process.argv[outputIndex + 1]) });
}
