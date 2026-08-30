import { createHash, randomUUID } from "node:crypto";
import { cp, link, lstat, open, readdir, readFile, realpath, rename, rm } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { isDeepStrictEqual } from "node:util";
import { inflateSync } from "node:zlib";

const MAX_PNG_BYTES = 16 * 1024 * 1024;
const MAX_TEXT_BYTES = 256 * 1024;
const SHA256 = /^[0-9a-f]{64}$/u;
const SHA40 = /^[0-9a-f]{40}$/u;
const BUILD = /^[1-9][0-9]*$/u;
const VERSION = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/u;
const UTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$/u;
const UUID_V7 = /^01900000-0000-7000-8000-00000000000[1-9ab]$/u;
const FIXTURE_PATH = "apps/desktop/release/macos/screenshots/fixture.json";
const EXPECTED_FILES = [
  "01-overview.png",
  "02-document-review.png",
  "03-journal-trial-balance.png",
  "04-bank-reconciliation.png",
  "05-bas-draft.png",
];
const CAPTIONS = [
  "See documents, banking and BAS work that needs attention at a glance.",
  "Review a fictional source document and its extracted accounting details.",
  "Trace a balanced journal through to the trial balance.",
  "Match and reconcile an imported fictional bank transaction.",
  "Prepare GST figures in a BAS draft that is clearly marked not lodged.",
];
const SHELL_ACCESSIBILITY_TEXT = [
  "Tammy",
  "Overview",
  "Documents",
  "Banking",
  "Chart of accounts",
  "Journals",
  "Trial balance",
  "GST & BAS",
  "Audit trail",
  "Settings",
  "Local data",
  "TB Tammy Business",
  "Local engine ready. Offline. No cloud required.",
];
const ACCESSIBILITY_TEXT = [
  [
    ...SHELL_ACCESSIBILITY_TEXT,
    "Overview",
    "Your local accounting workspace at a glance.",
    "Documents",
    "0",
    "1 reviewed this quarter",
    "Banking",
    "0",
    "0 unreconciled this quarter",
    "GST & BAS",
    "Draft",
    "Draft — not lodged",
    "Needs review",
    "Nothing needs review",
    "Your retained local workflows are up to date.",
  ],
  [
    ...SHELL_ACCESSIBILITY_TEXT,
    "Documents",
    "Retain source files, review locally extracted details, then approve them for later accounting.",
    "Choose File",
    "Upload document",
    "Needs review",
    "Harbour Office Supplies Pty Ltd",
    "WCS-2024Q4-001 · 12/05/2024",
    "$319.00",
    "Reviewed",
    "Source document",
    "wattle-office-supplies-invoice.pdf",
    "49 B · retained locally",
    "Reviewed",
    "Harbour Office Supplies Pty Ltd Invoice WCS-2024Q4-001 Subtotal $290.00 GST $29.00 Total $319.00",
    "Supplier",
    "Harbour Office Supplies Pty Ltd",
    "Invoice number",
    "WCS-2024Q4-001",
    "Date",
    "2024-05-12",
    "Subtotal",
    "290.00",
    "GST",
    "29.00",
    "Total",
    "319.00",
    "Review saved",
  ],
  [
    ...SHELL_ACCESSIBILITY_TEXT,
    "Trial balance",
    "Debit and credit balances as at 12/05/2024.",
    "Account",
    "Debit (AUD)",
    "Credit (AUD)",
    "3100Owner contributions",
    "$0.00",
    "$319.00",
    "6100Office expenses",
    "$319.00",
    "$0.00",
    "Total",
    "$319.00",
    "$319.00",
  ],
  [
    ...SHELL_ACCESSIBILITY_TEXT,
    "Banking",
    "Import local statement rows, confirm matches, then reconcile.",
    "Latest statement balance",
    "$681.00",
    "Statement import",
    "Business transaction account",
    "Opening balance",
    "1000.00",
    "CSV rows",
    "2024-05-12,HARBOUR OFFICE SUPPLIES WCS-2024Q4-001,-319.00",
    "Format: date, description, signed amount. Nothing is matched automatically.",
    "Import statement",
    "Statement lines",
    "0 unmatched · 0 unreconciled",
    "Complete reconciliation",
    "Date",
    "Description",
    "Amount",
    "State",
    "Action",
    "12/05/2024",
    "HARBOUR OFFICE SUPPLIES WCS-2024Q4-001",
    "-$319.00",
    "Reconciled",
  ],
  [
    ...SHELL_ACCESSIBILITY_TEXT,
    "GST & BAS",
    "A local workpaper from reviewed documents. Tammy does not lodge this BAS.",
    "Draft — not lodged",
    "Review locally when ready.",
    "Available in this build",
    "Tammy supports a local reviewed-document GST workpaper only.",
    "Unsupported in this build",
    "Complete BAS preparation, declaration, and lodgement are unavailable.",
    "Sales (G1)",
    "$0.00",
    "GST on sales (1A)",
    "$0.00",
    "GST credits (1B)",
    "$29.00",
    "Net GST refundable",
    "$29.00",
    "Reviewed source documents",
    "01/04/2024 – 30/06/2024",
    "1 source",
    "Date",
    "Supplier",
    "Invoice",
    "Gross",
    "GST credit",
    "12/05/2024",
    "Harbour Office Supplies Pty Ltd",
    "WCS-2024Q4-001",
    "$319.00",
    "$29.00",
    "Status: Draft — not lodged. This screen has no lodge, submit or declaration control.",
  ],
];
const EXPECTED_IDS = {
  bas: "01900000-0000-7000-8000-00000000000b",
  bankImport: "01900000-0000-7000-8000-000000000006",
  bankMatch: "01900000-0000-7000-8000-000000000007",
  bankReconciliation: "01900000-0000-7000-8000-000000000008",
  document: "01900000-0000-7000-8000-000000000009",
  documentReview: "01900000-0000-7000-8000-00000000000a",
  equityAccount: "01900000-0000-7000-8000-000000000002",
  expenseAccount: "01900000-0000-7000-8000-000000000001",
  journal: "01900000-0000-7000-8000-000000000003",
  journalCredit: "01900000-0000-7000-8000-000000000005",
  journalDebit: "01900000-0000-7000-8000-000000000004",
};

function fail(code) {
  throw new Error(code);
}

function record(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function exactKeys(value, keys) {
  return (
    record(value) &&
    Object.keys(value).length === keys.length &&
    keys.every((key) => Object.hasOwn(value, key))
  );
}

function hash(value) {
  return createHash("sha256").update(value).digest("hex");
}

function collectStrings(value, output = []) {
  if (typeof value === "string") output.push(value);
  else if (Array.isArray(value)) for (const item of value) collectStrings(item, output);
  else if (record(value)) for (const item of Object.values(value)) collectStrings(item, output);
  return output;
}

export function scanScreenshotInputs(value, { allowSetupOnlyAbn = false } = {}) {
  for (const text of collectStrings(value)) {
    const lower = text.toLowerCase();
    if (
      lower.includes("gamma systems pty ltd") ||
      lower.includes("ben.ebsworth@gmail.com") ||
      lower.includes("wattle & co test pty ltd") ||
      lower.includes("workspace-passphrase-long-enough") ||
      lower.includes("administrator-password-long-enough") ||
      /-----begin (?:rsa |ec |encrypted )?private key-----/iu.test(text) ||
      /\bAKIA[0-9A-Z]{16}\b/u.test(text) ||
      /\b(?:recovery[ -]?code|machine[ -]?credential|access[ -]?token|private[ -]?key|client[ -]?secret|password)\b/iu.test(
        text,
      ) ||
      /\btfn\b/iu.test(text) ||
      /\b(?:bearer|token)\s+[A-Za-z0-9._~+/=-]{12,}\b/iu.test(text) ||
      /\b\d{3}[- ]?\d{3}[- ]?\d{3}\b/u.test(text) ||
      /\b\d{3}-\d{3}\b/u.test(text) ||
      (/\b\d{11}\b/u.test(text) && !(allowSetupOnlyAbn && text === "11000000560")) ||
      (/\b(?:INV[-:#][A-Z0-9-]{3,}|invoice\s+(?:(?:no\.?|number|#)\s*)?[A-Z0-9-]{3,})/iu.test(
        text,
      ) &&
        text !== "Invoice number" &&
        !text.includes("WCS-2024Q4-001"))
    ) {
      fail("APP_STORE_SCREENSHOT_INPUT_UNSAFE");
    }
  }
  return value;
}

export function validateScreenshotFixture(value) {
  if (
    !exactKeys(value, [
      "accounts",
      "banking",
      "bas",
      "business",
      "fictional",
      "fixtureId",
      "ids",
      "journal",
      "locale",
      "operator",
      "period",
      "schemaVersion",
      "screenshots",
      "sourceDocument",
    ]) ||
    value.schemaVersion !== 1 ||
    value.fixtureId !== "tammy-app-store-screenshots-en-au-v1" ||
    value.fictional !== true ||
    value.locale !== "en-AU" ||
    !exactKeys(value.business, ["abn", "displayName", "legalName"]) ||
    value.business.legalName !== "Wattle & Co Supplies Pty Ltd" ||
    value.business.displayName !== "Wattle & Co Supplies" ||
    !exactKeys(value.business.abn, [
      "display",
      "officialTestSourceUrl",
      "sourceRetrievedOn",
      "value",
    ]) ||
    value.business.abn.value !== "11000000560" ||
    value.business.abn.display !== false ||
    value.business.abn.officialTestSourceUrl !== null ||
    value.business.abn.sourceRetrievedOn !== null ||
    !exactKeys(value.operator, ["displayName", "username"]) ||
    value.operator.displayName !== "Avery Lawson" ||
    value.operator.username !== "admin@tammy.local" ||
    !exactKeys(value.period, ["end", "label", "postingDate", "start"]) ||
    !isDeepStrictEqual(value.period, {
      end: "2024-06-30",
      label: "2024 Q4",
      postingDate: "2024-05-12",
      start: "2024-04-01",
    }) ||
    !exactKeys(value.ids, Object.keys(EXPECTED_IDS)) ||
    !isDeepStrictEqual(value.ids, EXPECTED_IDS) ||
    !Object.values(value.ids).every((id) => UUID_V7.test(id))
  ) {
    fail("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
  }
  const expectedAccounts = [
    {
      cashFlowClassification: "noncash",
      code: "6100",
      name: "Office expenses",
      reportClassification: "profit_loss.manual",
      role: "expense",
    },
    {
      cashFlowClassification: "noncash",
      code: "3100",
      name: "Owner contributions",
      reportClassification: "balance_sheet.manual",
      role: "equity",
    },
  ];
  if (
    !isDeepStrictEqual(value.accounts, expectedAccounts) ||
    !exactKeys(value.journal, [
      "amountMinorUnits",
      "creditDescription",
      "debitDescription",
      "memo",
    ]) ||
    !isDeepStrictEqual(value.journal, {
      amountMinorUnits: "31900",
      creditDescription: "Owner contribution",
      debitDescription: "Office supplies",
      memo: "Office supplies paid personally",
    }) ||
    !exactKeys(value.sourceDocument, [
      "extractedText",
      "gstMinorUnits",
      "invoiceNumber",
      "mimeType",
      "sourceDisplayName",
      "subtotalMinorUnits",
      "supplierName",
      "syntheticPdfText",
      "totalMinorUnits",
    ]) ||
    value.sourceDocument.invoiceNumber !== "WCS-2024Q4-001" ||
    value.sourceDocument.supplierName !== "Harbour Office Supplies Pty Ltd" ||
    value.sourceDocument.sourceDisplayName !== "wattle-office-supplies-invoice.pdf" ||
    value.sourceDocument.mimeType !== "application/pdf" ||
    value.sourceDocument.subtotalMinorUnits !== "29000" ||
    value.sourceDocument.gstMinorUnits !== "2900" ||
    value.sourceDocument.totalMinorUnits !== "31900" ||
    value.sourceDocument.syntheticPdfText !==
      "%PDF-1.4\nTammy fictional screenshot fixture\n%%EOF" ||
    value.sourceDocument.extractedText !==
      "Harbour Office Supplies Pty Ltd Invoice WCS-2024Q4-001 Subtotal $290.00 GST $29.00 Total $319.00" ||
    !exactKeys(value.banking, [
      "closingBalanceMinorUnits",
      "lineAmountMinorUnits",
      "lineDescription",
      "matchReference",
      "openingBalanceMinorUnits",
    ]) ||
    !isDeepStrictEqual(value.banking, {
      closingBalanceMinorUnits: "68100",
      lineAmountMinorUnits: "-31900",
      lineDescription: "HARBOUR OFFICE SUPPLIES WCS-2024Q4-001",
      matchReference: "Reviewed fictional source document",
      openingBalanceMinorUnits: "100000",
    }) ||
    !exactKeys(value.bas, ["gstCreditsMinorUnits", "netGstPayableMinorUnits", "statusLabel"]) ||
    !isDeepStrictEqual(value.bas, {
      gstCreditsMinorUnits: "2900",
      netGstPayableMinorUnits: "-2900",
      statusLabel: "Draft — not lodged",
    }) ||
    !Array.isArray(value.screenshots) ||
    value.screenshots.length !== 5
  ) {
    fail("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
  }
  for (const [index, screenshot] of value.screenshots.entries()) {
    if (
      !exactKeys(screenshot, ["caption", "completeAccessibilityText", "filename"]) ||
      screenshot.filename !== EXPECTED_FILES[index] ||
      screenshot.caption !== CAPTIONS[index] ||
      !isDeepStrictEqual(screenshot.completeAccessibilityText, ACCESSIBILITY_TEXT[index])
    ) {
      fail("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
    }
  }
  try {
    scanScreenshotInputs(value, { allowSetupOnlyAbn: true });
  } catch {
    fail("APP_STORE_SCREENSHOT_FIXTURE_INVALID");
  }
  return value;
}

function crc32(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit += 1) crc = (crc >>> 1) ^ (0xedb88320 & -(crc & 1));
  }
  return (crc ^ 0xffffffff) >>> 0;
}

export function inspectPng(bytes) {
  if (
    !Buffer.isBuffer(bytes) ||
    bytes.length < 45 ||
    bytes.length > MAX_PNG_BYTES ||
    !bytes.subarray(0, 8).equals(Buffer.from("89504e470d0a1a0a", "hex"))
  ) {
    fail("APP_STORE_SCREENSHOT_PNG_INVALID");
  }
  let offset = 8;
  let header;
  const idat = [];
  let idatEnded = false;
  let ended = false;
  while (offset < bytes.length) {
    if (offset + 12 > bytes.length) fail("APP_STORE_SCREENSHOT_PNG_INVALID");
    const length = bytes.readUInt32BE(offset);
    const end = offset + 12 + length;
    if (length > MAX_PNG_BYTES || end > bytes.length) fail("APP_STORE_SCREENSHOT_PNG_INVALID");
    const type = bytes.toString("ascii", offset + 4, offset + 8);
    if (!/^[A-Za-z]{2}[A-Z][A-Za-z]$/u.test(type)) fail("APP_STORE_SCREENSHOT_PNG_INVALID");
    const data = bytes.subarray(offset + 8, offset + 8 + length);
    const expectedCrc = bytes.readUInt32BE(offset + 8 + length);
    if (crc32(bytes.subarray(offset + 4, offset + 8 + length)) !== expectedCrc) {
      fail("APP_STORE_SCREENSHOT_PNG_INVALID");
    }
    if (header === undefined) {
      if (type !== "IHDR" || length !== 13) fail("APP_STORE_SCREENSHOT_PNG_INVALID");
      header = {
        bitDepth: data[8],
        colorType: data[9],
        height: data.readUInt32BE(4),
        interlace: data[12],
        width: data.readUInt32BE(0),
      };
      if (
        header.width !== 1440 ||
        header.height !== 900 ||
        header.bitDepth !== 8 ||
        header.colorType !== 2 ||
        data[10] !== 0 ||
        data[11] !== 0 ||
        header.interlace !== 0
      ) {
        fail("APP_STORE_SCREENSHOT_PNG_INVALID");
      }
    } else if (type === "IHDR") fail("APP_STORE_SCREENSHOT_PNG_INVALID");
    if (type === "PLTE") fail("APP_STORE_SCREENSHOT_PNG_INVALID");
    if (!["IDAT", "IEND", "IHDR"].includes(type) && /^[A-Z]/u.test(type)) {
      fail("APP_STORE_SCREENSHOT_PNG_INVALID");
    }
    if (type === "IDAT") {
      if (idatEnded) fail("APP_STORE_SCREENSHOT_PNG_INVALID");
      idat.push(data);
    } else if (idat.length > 0 && type !== "IEND") idatEnded = true;
    if (type === "IEND") {
      if (length !== 0 || end !== bytes.length) fail("APP_STORE_SCREENSHOT_PNG_INVALID");
      ended = true;
    }
    offset = end;
  }
  if (!header || idat.length === 0 || !ended) fail("APP_STORE_SCREENSHOT_PNG_INVALID");
  const rowBytes = header.width * 3 + 1;
  const expectedPixels = rowBytes * header.height;
  const compressed = Buffer.concat(idat);
  let pixels;
  try {
    const inflated = inflateSync(compressed, { info: true, maxOutputLength: expectedPixels });
    if (inflated.engine.bytesWritten !== compressed.length) {
      fail("APP_STORE_SCREENSHOT_PNG_INVALID");
    }
    pixels = inflated.buffer;
  } catch {
    fail("APP_STORE_SCREENSHOT_PNG_INVALID");
  }
  if (pixels.length !== expectedPixels) fail("APP_STORE_SCREENSHOT_PNG_INVALID");
  for (let offset = 0; offset < pixels.length; offset += rowBytes) {
    if (pixels[offset] > 4) fail("APP_STORE_SCREENSHOT_PNG_INVALID");
  }
  return { ...header, sha256: hash(bytes) };
}

async function safeDirectory(directory, errorCode) {
  if (typeof directory !== "string" || !path.isAbsolute(directory)) fail(errorCode);
  const status = await lstat(directory).catch(() => fail(errorCode));
  if (!status.isDirectory() || status.isSymbolicLink()) fail(errorCode);
  return realpath(directory).catch(() => fail(errorCode));
}

async function stableRead(file, maximum, errorCode) {
  const before = await lstat(file, { bigint: true }).catch(() => fail(errorCode));
  if (!before.isFile() || before.isSymbolicLink() || before.size > BigInt(maximum)) fail(errorCode);
  const bytes = await readFile(file).catch(() => fail(errorCode));
  const after = await lstat(file, { bigint: true }).catch(() => fail(errorCode));
  if (
    before.dev !== after.dev ||
    before.ino !== after.ino ||
    before.size !== after.size ||
    before.mtimeNs !== after.mtimeNs ||
    bytes.length !== Number(before.size)
  ) {
    fail(errorCode);
  }
  return bytes;
}

export async function validateScreenshotManifest({
  candidateEventBytes,
  captureDirectory,
  fixture,
  fixtureBytes,
  storeMetadataBytes,
}) {
  validateScreenshotFixture(fixture);
  if (!Buffer.isBuffer(fixtureBytes) || !Buffer.isBuffer(storeMetadataBytes)) {
    fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
  }
  try {
    if (!isDeepStrictEqual(JSON.parse(fixtureBytes), fixture)) {
      fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
    }
  } catch {
    fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
  }
  const metadata = storeMetadataBytes.toString("utf8");
  let metadataOffset = -1;
  for (const [index, filename] of EXPECTED_FILES.entries()) {
    const expected = `\`${filename}\` — ${CAPTIONS[index]}`;
    const next = metadata.indexOf(expected);
    if (next <= metadataOffset) fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
    metadataOffset = next;
  }
  await safeDirectory(captureDirectory, "APP_STORE_SCREENSHOT_MANIFEST_INVALID");
  const allowed = new Set(["manifest.json"]);
  for (const filename of EXPECTED_FILES) {
    allowed.add(filename);
    allowed.add(filename.replace(/\.png$/u, ".accessibility.txt"));
  }
  const names = await readdir(captureDirectory).catch(() =>
    fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID"),
  );
  if (names.length !== allowed.size || names.some((name) => !allowed.has(name))) {
    fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
  }
  let manifest;
  try {
    manifest = JSON.parse(
      await stableRead(
        path.join(captureDirectory, "manifest.json"),
        MAX_TEXT_BYTES,
        "APP_STORE_SCREENSHOT_MANIFEST_INVALID",
      ),
    );
  } catch {
    fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
  }
  if (
    !exactKeys(manifest, [
      "buildNumber",
      "candidateEventPath",
      "candidateEventSha256",
      "captureArtifactKind",
      "capturedAt",
      "developmentSignedAppSha256",
      "dimensions",
      "distributionPackageSha256",
      "fixturePath",
      "fixtureSha256",
      "images",
      "locale",
      "marketingVersion",
      "productSourceCommit",
      "productSourceTree",
      "schemaVersion",
      "unsignedContentManifestSha256",
    ]) ||
    manifest.schemaVersion !== 1 ||
    manifest.locale !== "en-AU" ||
    !exactKeys(manifest.dimensions, ["height", "width"]) ||
    manifest.dimensions.width !== 1440 ||
    manifest.dimensions.height !== 900 ||
    manifest.fixturePath !== FIXTURE_PATH ||
    manifest.fixtureSha256 !== hash(fixtureBytes) ||
    !SHA40.test(manifest.productSourceCommit) ||
    !SHA40.test(manifest.productSourceTree) ||
    !VERSION.test(manifest.marketingVersion) ||
    !BUILD.test(manifest.buildNumber) ||
    !SHA256.test(manifest.unsignedContentManifestSha256) ||
    !SHA256.test(manifest.developmentSignedAppSha256) ||
    manifest.captureArtifactKind !== "development-signed-app" ||
    !UTC.test(manifest.capturedAt) ||
    !Number.isFinite(Date.parse(manifest.capturedAt)) ||
    !Array.isArray(manifest.images) ||
    manifest.images.length !== 5
  ) {
    fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
  }
  const unlinked = [
    manifest.distributionPackageSha256,
    manifest.candidateEventPath,
    manifest.candidateEventSha256,
  ].every((value) => value === null);
  if (!unlinked) {
    if (
      !Buffer.isBuffer(candidateEventBytes) ||
      !SHA256.test(manifest.distributionPackageSha256) ||
      typeof manifest.candidateEventPath !== "string" ||
      !new RegExp(
        `^docs/release/records/macos/${manifest.marketingVersion.replaceAll(".", "\\.")}/build-${manifest.buildNumber}/events/\\d{4}-\\d{2}-\\d{2}T\\d{2}-\\d{2}-\\d{2}\\.\\d{3}Z-candidate-built\\.json$`,
        "u",
      ).test(manifest.candidateEventPath) ||
      manifest.candidateEventSha256 !== hash(candidateEventBytes)
    ) {
      fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
    }
    let event;
    try {
      event = JSON.parse(candidateEventBytes);
    } catch {
      fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
    }
    if (
      !exactKeys(event, [
        "appSha256",
        "buildNumber",
        "kind",
        "marketingVersion",
        "packageSha256",
        "productSourceCommit",
        "productSourceTree",
        "unsignedContentManifestSha256",
      ]) ||
      event.kind !== "candidate-built" ||
      event.marketingVersion !== manifest.marketingVersion ||
      event.buildNumber !== manifest.buildNumber ||
      event.productSourceCommit !== manifest.productSourceCommit ||
      event.productSourceTree !== manifest.productSourceTree ||
      event.packageSha256 !== manifest.distributionPackageSha256 ||
      event.unsignedContentManifestSha256 !== manifest.unsignedContentManifestSha256 ||
      !SHA256.test(event.appSha256) ||
      event.appSha256 === manifest.developmentSignedAppSha256
    ) {
      fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
    }
  }
  const hashes = new Set();
  for (const [index, image] of manifest.images.entries()) {
    const definition = fixture.screenshots[index];
    const snapshotName = EXPECTED_FILES[index].replace(/\.png$/u, ".accessibility.txt");
    if (
      !exactKeys(image, [
        "accessibilitySnapshot",
        "accessibilitySnapshotSha256",
        "caption",
        "filename",
        "sha256",
      ]) ||
      image.filename !== EXPECTED_FILES[index] ||
      image.caption !== CAPTIONS[index] ||
      image.caption !== definition.caption ||
      image.accessibilitySnapshot !== snapshotName ||
      !SHA256.test(image.sha256) ||
      !SHA256.test(image.accessibilitySnapshotSha256)
    ) {
      fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
    }
    const imageBytes = await stableRead(
      path.join(captureDirectory, image.filename),
      MAX_PNG_BYTES,
      "APP_STORE_SCREENSHOT_MANIFEST_INVALID",
    );
    const png = inspectPng(imageBytes);
    if (png.sha256 !== image.sha256 || hashes.has(image.sha256)) {
      fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
    }
    hashes.add(image.sha256);
    const snapshotBytes = await stableRead(
      path.join(captureDirectory, snapshotName),
      MAX_TEXT_BYTES,
      "APP_STORE_SCREENSHOT_MANIFEST_INVALID",
    );
    if (hash(snapshotBytes) !== image.accessibilitySnapshotSha256) {
      fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
    }
    const snapshot = snapshotBytes.toString("utf8");
    try {
      scanScreenshotInputs(snapshot);
    } catch {
      fail("APP_STORE_SCREENSHOT_INPUT_UNSAFE");
    }
    const expectedSnapshot = `${definition.completeAccessibilityText.join("\n")}\n`;
    if (snapshot !== expectedSnapshot) {
      fail("APP_STORE_SCREENSHOT_MANIFEST_INVALID");
    }
  }
  return manifest;
}

async function exists(target) {
  return lstat(target).then(
    () => true,
    () => false,
  );
}

async function assertDestinationRoot(repositoryRoot, destinationRoot) {
  const suffix = ["apps", "desktop", "release", "macos", "screenshots"];
  if (
    typeof repositoryRoot !== "string" ||
    !path.isAbsolute(repositoryRoot) ||
    typeof destinationRoot !== "string" ||
    !path.isAbsolute(destinationRoot) ||
    destinationRoot !== path.join(repositoryRoot, ...suffix)
  ) {
    fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE");
  }
  const rootStatus = await lstat(repositoryRoot).catch(() =>
    fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE"),
  );
  if (!rootStatus.isDirectory() || rootStatus.isSymbolicLink()) {
    fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE");
  }
  const resolvedRoot = await realpath(repositoryRoot).catch(() =>
    fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE"),
  );
  if (resolvedRoot !== repositoryRoot) fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE");
  let current = repositoryRoot;
  for (const segment of suffix) {
    current = path.join(current, segment);
    const status = await lstat(current).catch(() =>
      fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE"),
    );
    if (!status.isDirectory() || status.isSymbolicLink()) {
      fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE");
    }
  }
  const resolvedDestination = await realpath(destinationRoot).catch(() =>
    fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE"),
  );
  if (
    resolvedDestination !== destinationRoot ||
    !resolvedDestination.startsWith(`${resolvedRoot}${path.sep}`)
  ) {
    fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE");
  }
}

async function syncTree(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const target = path.join(directory, entry.name);
    if (entry.isDirectory()) await syncTree(target);
    else {
      const file = await open(target, "r");
      try {
        await file.sync();
      } finally {
        await file.close();
      }
    }
  }
  const handle = await open(directory, "r");
  try {
    await handle.sync();
  } finally {
    await handle.close();
  }
}

async function syncDirectory(directory) {
  const handle = await open(directory, "r");
  try {
    await handle.sync();
  } finally {
    await handle.close();
  }
}

async function validateExistingSet(
  directory,
  fixture,
  fixtureBytes,
  storeMetadataBytes,
  candidateEventBytes,
) {
  try {
    await validateScreenshotManifest({
      candidateEventBytes,
      captureDirectory: directory,
      fixture,
      fixtureBytes,
      storeMetadataBytes,
    });
  } catch {
    fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE");
  }
}

function processIsAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    if (error?.code === "ESRCH") return false;
    return true;
  }
}

function validateLockRecord(value, destinationRoot) {
  if (
    !exactKeys(value, ["destinationRoot", "ownerPid", "schemaVersion", "startedAt", "token"]) ||
    value.schemaVersion !== 1 ||
    value.destinationRoot !== destinationRoot ||
    !Number.isSafeInteger(value.ownerPid) ||
    value.ownerPid <= 0 ||
    !UTC.test(value.startedAt) ||
    !/^[0-9a-f-]{36}$/u.test(value.token)
  ) {
    fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
  }
  return value;
}

async function createLockCandidate(destinationRoot, onBoundary) {
  const token = randomUUID();
  const record = {
    destinationRoot,
    ownerPid: process.pid,
    schemaVersion: 1,
    startedAt: new Date().toISOString(),
    token,
  };
  const bytes = Buffer.from(`${JSON.stringify(record)}\n`);
  const writingPath = path.join(destinationRoot, `.promotion.lock.writing-${process.pid}-${token}`);
  const candidatePath = path.join(destinationRoot, `.promotion.lock.candidate-${token}`);
  const handle = await open(writingPath, "wx", 0o600).catch(() =>
    fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED"),
  );
  try {
    await handle.writeFile(bytes);
    await handle.sync();
  } finally {
    await handle.close();
  }
  await rename(writingPath, candidatePath);
  await syncDirectory(destinationRoot);
  await onBoundary("after-lock-candidate-fsync");
  return { bytes, candidatePath, record };
}

function validateReclaimRecord(value, destinationRoot) {
  if (
    !exactKeys(value, [
      "destinationRoot",
      "ownerPid",
      "schemaVersion",
      "startedAt",
      "targetLockSha256",
      "targetLockToken",
      "token",
    ]) ||
    value.schemaVersion !== 1 ||
    value.destinationRoot !== destinationRoot ||
    !Number.isSafeInteger(value.ownerPid) ||
    value.ownerPid <= 0 ||
    !UTC.test(value.startedAt) ||
    !SHA256.test(value.targetLockSha256) ||
    !/^[0-9a-f-]{36}$/u.test(value.targetLockToken) ||
    !/^[0-9a-f-]{36}$/u.test(value.token)
  ) {
    fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
  }
  return value;
}

async function recoverReclaimMarker(destinationRoot, reclaimPath) {
  let bytes;
  let marker;
  try {
    bytes = await stableRead(reclaimPath, MAX_TEXT_BYTES, "APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
    marker = validateReclaimRecord(JSON.parse(bytes), destinationRoot);
  } catch (error) {
    if (error?.code === "ENOENT") return;
    if (!(await exists(reclaimPath))) return;
    fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
  }
  if (processIsAlive(marker.ownerPid)) fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
  const quarantine = path.join(destinationRoot, `.promotion.lock.reclaim-stale-${randomUUID()}`);
  try {
    await rename(reclaimPath, quarantine);
  } catch (error) {
    if (error?.code === "ENOENT") return;
    fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
  }
  const moved = await stableRead(
    quarantine,
    MAX_TEXT_BYTES,
    "APP_STORE_SCREENSHOT_PROMOTION_LOCKED",
  );
  if (!moved.equals(bytes)) {
    await rename(quarantine, reclaimPath).catch(() => {});
    fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
  }
  await syncDirectory(destinationRoot);
  await rm(quarantine);
  await syncDirectory(destinationRoot);
}

async function createReclaimMarker(destinationRoot, reclaimPath, existing, existingBytes) {
  const token = randomUUID();
  const record = {
    destinationRoot,
    ownerPid: process.pid,
    schemaVersion: 1,
    startedAt: new Date().toISOString(),
    targetLockSha256: hash(existingBytes),
    targetLockToken: existing.token,
    token,
  };
  const bytes = Buffer.from(`${JSON.stringify(record)}\n`);
  const writingPath = path.join(destinationRoot, `.promotion.lock.reclaim-writing-${token}`);
  const candidatePath = path.join(destinationRoot, `.promotion.lock.reclaim-candidate-${token}`);
  const handle = await open(writingPath, "wx", 0o600).catch(() =>
    fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED"),
  );
  try {
    await handle.writeFile(bytes);
    await handle.sync();
  } finally {
    await handle.close();
  }
  await rename(writingPath, candidatePath);
  await syncDirectory(destinationRoot);
  try {
    await link(candidatePath, reclaimPath);
  } catch (error) {
    await rm(candidatePath, { force: true }).catch(() => {});
    if (["EEXIST", "ENOENT"].includes(error?.code)) return undefined;
    fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
  }
  await syncDirectory(destinationRoot);
  await rm(candidatePath);
  await syncDirectory(destinationRoot);
  return { bytes, record };
}

async function removeOwnedReclaimMarker(destinationRoot, reclaimPath, bytes) {
  const completedPath = path.join(
    destinationRoot,
    `.promotion.lock.reclaim-complete-${randomUUID()}`,
  );
  await rename(reclaimPath, completedPath).catch(() =>
    fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED"),
  );
  const moved = await stableRead(
    completedPath,
    MAX_TEXT_BYTES,
    "APP_STORE_SCREENSHOT_PROMOTION_LOCKED",
  );
  if (!moved.equals(bytes)) {
    await rename(completedPath, reclaimPath).catch(() => {});
    fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
  }
  await syncDirectory(destinationRoot);
  await rm(completedPath);
  await syncDirectory(destinationRoot);
}

async function sameInode(left, right) {
  const [leftStatus, rightStatus] = await Promise.all([
    lstat(left, { bigint: true }).catch(() => undefined),
    lstat(right, { bigint: true }).catch(() => undefined),
  ]);
  return (
    leftStatus !== undefined &&
    rightStatus !== undefined &&
    leftStatus.dev === rightStatus.dev &&
    leftStatus.ino === rightStatus.ino
  );
}

async function acquirePromotionLock(destinationRoot, onBoundary) {
  const lockPath = path.join(destinationRoot, ".promotion.lock");
  const reclaimPath = path.join(destinationRoot, ".promotion.lock.reclaim");
  const candidate = await createLockCandidate(destinationRoot, onBoundary);
  for (let attempt = 0; attempt < 200; attempt += 1) {
    await recoverReclaimMarker(destinationRoot, reclaimPath);
    try {
      await link(candidate.candidatePath, lockPath);
      await syncDirectory(destinationRoot);
      await onBoundary("after-lock-publication");
      if (!(await sameInode(candidate.candidatePath, lockPath))) {
        fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
      }
      await rm(candidate.candidatePath);
      return { bytes: candidate.bytes, lockPath };
    } catch (error) {
      if (error?.code !== "EEXIST") fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
      let existingBytes;
      let existing;
      try {
        existingBytes = await stableRead(
          lockPath,
          MAX_TEXT_BYTES,
          "APP_STORE_SCREENSHOT_PROMOTION_LOCKED",
        );
        existing = validateLockRecord(JSON.parse(existingBytes), destinationRoot);
      } catch {
        fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
      }
      if (processIsAlive(existing.ownerPid)) fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
      const reclaim = await createReclaimMarker(
        destinationRoot,
        reclaimPath,
        existing,
        existingBytes,
      );
      if (!reclaim) continue;
      await onBoundary("after-reclaim-marker-publication");
      const [currentLock, currentMarker] = await Promise.all([
        stableRead(lockPath, MAX_TEXT_BYTES, "APP_STORE_SCREENSHOT_PROMOTION_LOCKED"),
        stableRead(reclaimPath, MAX_TEXT_BYTES, "APP_STORE_SCREENSHOT_PROMOTION_LOCKED"),
      ]);
      if (!currentLock.equals(existingBytes) || !currentMarker.equals(reclaim.bytes)) {
        fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
      }
      await rename(candidate.candidatePath, lockPath);
      await syncDirectory(destinationRoot);
      await onBoundary("after-lock-publication");
      await removeOwnedReclaimMarker(destinationRoot, reclaimPath, reclaim.bytes);
      return { bytes: candidate.bytes, lockPath };
    }
  }
  fail("APP_STORE_SCREENSHOT_PROMOTION_LOCKED");
}

async function releasePromotionLock(lock, onBoundary) {
  const directory = path.dirname(lock.lockPath);
  const token = randomUUID();
  const releaseClaim = path.join(directory, `.promotion.lock.release-claim-${token}`);
  const releasedPath = path.join(directory, `.promotion.lock.released-${token}`);
  const current = await stableRead(
    lock.lockPath,
    MAX_TEXT_BYTES,
    "APP_STORE_SCREENSHOT_PROMOTION_LOCKED",
  );
  if (!current.equals(lock.bytes)) return;
  await link(lock.lockPath, releaseClaim);
  await syncDirectory(directory);
  await onBoundary("after-release-claim-publication");
  if (!(await sameInode(lock.lockPath, releaseClaim))) return;
  await rename(lock.lockPath, releasedPath);
  await syncDirectory(directory);
  await onBoundary("after-release-lock-rename");
  await rm(releasedPath);
  await rm(releaseClaim);
  await syncDirectory(directory);
}

async function recoverPromotion(
  destinationRoot,
  fixture,
  fixtureBytes,
  storeMetadataBytes,
  candidateEventBytes,
) {
  const canonical = path.join(destinationRoot, "en-AU");
  const siblings = await readdir(destinationRoot);
  const backups = siblings.filter((name) => name.startsWith(".en-AU.backup-"));
  const staging = siblings.filter((name) => name.startsWith(".en-AU.staging-"));
  if (backups.length > 1) fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE");
  const canonicalExists = await exists(canonical);
  if (canonicalExists) {
    await validateExistingSet(
      canonical,
      fixture,
      fixtureBytes,
      storeMetadataBytes,
      candidateEventBytes,
    );
  }
  const backupPaths = backups.map((name) => path.join(destinationRoot, name));
  const stagingPaths = staging.map((name) => path.join(destinationRoot, name));
  for (const target of [...backupPaths, ...stagingPaths]) {
    await validateExistingSet(
      target,
      fixture,
      fixtureBytes,
      storeMetadataBytes,
      candidateEventBytes,
    );
  }
  if (backups.length === 1) {
    const backup = backupPaths[0];
    if (!canonicalExists) {
      await rename(backup, canonical);
      await syncDirectory(destinationRoot);
    } else {
      await rm(backup, { recursive: true });
      await syncDirectory(destinationRoot);
    }
  }
  for (const target of stagingPaths) {
    await rm(target, { recursive: true });
    await syncDirectory(destinationRoot);
  }
}

export async function promoteScreenshotSet({
  candidateEventBytes,
  destinationRoot,
  fixture,
  fixtureBytes,
  onBoundary = async () => {},
  repositoryRoot,
  sourceDirectory,
  storeMetadataBytes,
}) {
  await assertDestinationRoot(repositoryRoot, destinationRoot);
  if (typeof onBoundary !== "function") fail("APP_STORE_SCREENSHOT_DESTINATION_UNSAFE");
  await safeDirectory(destinationRoot, "APP_STORE_SCREENSHOT_DESTINATION_UNSAFE");
  await validateScreenshotManifest({
    candidateEventBytes,
    captureDirectory: sourceDirectory,
    fixture,
    fixtureBytes,
    storeMetadataBytes,
  });
  const lock = await acquirePromotionLock(destinationRoot, onBoundary);
  const staging = path.join(destinationRoot, `.en-AU.staging-${randomUUID()}`);
  let backup;
  try {
    await recoverPromotion(
      destinationRoot,
      fixture,
      fixtureBytes,
      storeMetadataBytes,
      candidateEventBytes,
    );
    await cp(sourceDirectory, staging, {
      dereference: false,
      errorOnExist: true,
      force: false,
      recursive: true,
      verbatimSymlinks: true,
    });
    await syncTree(staging);
    await onBoundary("after-staging-fsync");
    await validateScreenshotManifest({
      candidateEventBytes,
      captureDirectory: staging,
      fixture,
      fixtureBytes,
      storeMetadataBytes,
    });
    const canonical = path.join(destinationRoot, "en-AU");
    if (await exists(canonical)) {
      await validateExistingSet(
        canonical,
        fixture,
        fixtureBytes,
        storeMetadataBytes,
        candidateEventBytes,
      );
      backup = path.join(destinationRoot, `.en-AU.backup-${randomUUID()}`);
      await onBoundary("before-backup-rename");
      await rename(canonical, backup);
      await onBoundary("after-backup-rename");
      await syncDirectory(destinationRoot);
      await onBoundary("after-backup-parent-fsync");
    }
    await onBoundary("before-staging-rename");
    await rename(staging, canonical);
    await onBoundary("after-staging-rename");
    await syncDirectory(destinationRoot);
    await onBoundary("after-canonical-parent-fsync");
    await validateScreenshotManifest({
      candidateEventBytes,
      captureDirectory: canonical,
      fixture,
      fixtureBytes,
      storeMetadataBytes,
    });
    await onBoundary("after-canonical-revalidation");
    if (backup !== undefined) {
      await rm(backup, { recursive: true });
      await onBoundary("after-backup-removal");
      await syncDirectory(destinationRoot);
      await onBoundary("after-final-parent-fsync");
    }
    return canonical;
  } catch (error) {
    if (await exists(staging)) {
      try {
        await validateExistingSet(
          staging,
          fixture,
          fixtureBytes,
          storeMetadataBytes,
          candidateEventBytes,
        );
        await rm(staging, { recursive: true });
      } catch {
        // Preserve an unvalidated directory for operator inspection; never guess that it is ours.
      }
    }
    throw error;
  } finally {
    await releasePromotionLock(lock, onBoundary).catch(() => {});
  }
}

async function main() {
  const args = process.argv.slice(2);
  if (args.length !== 2 || args[0] !== "--source" || !path.isAbsolute(args[1])) {
    fail("APP_STORE_SCREENSHOT_ARGUMENTS_INVALID");
  }
  const repositoryRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
  const destinationRoot = path.join(repositoryRoot, "apps/desktop/release/macos/screenshots");
  const fixtureFile = path.join(destinationRoot, "fixture.json");
  const fixtureBytes = await readFile(fixtureFile);
  const fixture = JSON.parse(fixtureBytes);
  const storeMetadataBytes = await readFile(
    path.join(repositoryRoot, "apps/desktop/release/macos/store-metadata.md"),
  );
  const canonical = await promoteScreenshotSet({
    destinationRoot,
    fixture,
    fixtureBytes,
    repositoryRoot,
    sourceDirectory: args[1],
    storeMetadataBytes,
  });
  process.stdout.write(`${JSON.stringify({ canonical })}\n`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    process.stderr.write(
      `${error instanceof Error ? error.message : "APP_STORE_SCREENSHOT_FAILED"}\n`,
    );
    process.exitCode = 1;
  });
}
