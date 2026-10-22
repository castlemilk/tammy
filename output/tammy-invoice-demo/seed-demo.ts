import assert from "node:assert/strict";
import { randomBytes } from "node:crypto";
import { mkdir, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { _electron, expect } from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/apps/desktop/node_modules/@playwright/test/index.mjs";
import { create, fromBinary, toBinary } from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/node_modules/@bufbuild/protobuf/dist/esm/index.js";
import { AnySchema } from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/node_modules/@bufbuild/protobuf/dist/esm/wkt/index.js";
import {
  DocumentStatus,
  IngestDocumentRequestSchema,
  IngestDocumentResponseSchema,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
  SaveDocumentReviewRequestSchema,
  SaveDocumentReviewResponseSchema,
} from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/packages/connect-client/src/gen/tammy/v1/documents_pb.ts";
import {
  InvoiceState,
  ListInvoicesRequestSchema,
  ListInvoicesResponseSchema,
  SaveInvoiceDraftRequestSchema,
  SaveInvoiceDraftResponseSchema,
} from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/packages/connect-client/src/gen/tammy/v1/invoices_pb.ts";
import { invoiceDraftToProto } from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/apps/desktop/src/shared/invoices.ts";
import { renderInvoicePdf } from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/packages/invoices/src/pdf.ts";
import { pdfStreams } from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/packages/invoices/test-support/pdf-text.ts";
import { pngLogo } from "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/packages/invoices/test-support/logo-fixtures.ts";

const root = "/Users/benebsworth/projects/tammy/output/tammy-invoice-demo";
const profile = path.join(root, "user-data-company-template");
const executable = "/Users/benebsworth/projects/tammy/.worktrees/issue/sbr-registration-ready/.tmp/electron-builder/ordinary/mac-arm64/Tammy.app/Contents/MacOS/Tammy";
const username = "invoice-demo@example.test";
const password = "Tammy-demo-admin-2026!";
const passphrase = "Tammy-demo-workspace-2026!";
const sample = JSON.parse(await readFile("/Users/benebsworth/projects/tammy/output/pdf/example-invoice-2026-10-01.json", "utf8"));
const expenses = [
  { name: "Office stationery", supplier: "Example Stationery Pty Ltd", number: "SAMPLE-STATIONERY-001", filename: "01-office-stationery.pdf", subtotal: 8000, gst: 800 },
  { name: "Courier delivery", supplier: "Example Courier Pty Ltd", number: "SAMPLE-COURIER-001", filename: "02-courier-delivery.pdf", subtotal: 2500, gst: 250 },
  { name: "Reference subscription", supplier: "Example Reference Library Pty Ltd", number: "SAMPLE-REFERENCE-001", filename: "03-reference-subscription.pdf", subtotal: 4500, gst: 0 },
];
const errors: string[] = [];
const stages: string[] = [];
const viewportChecks: { width: number; height: number; pageScrollWidth: number; pageClientWidth: number; tableScrollWidth: number; tableClientWidth: number }[] = [];
let application: Awaited<ReturnType<typeof _electron.launch>> | undefined;
let applicationProcess: ReturnType<Awaited<ReturnType<typeof _electron.launch>>["process"]> | undefined;
let page: Awaited<ReturnType<Awaited<ReturnType<typeof _electron.launch>>["firstWindow"]>>;

function stage(message: string) {
  stages.push(message);
  console.log(message);
}

function uuidV7() {
  const bytes = randomBytes(16);
  let milliseconds = Date.now();
  for (let index = 5; index >= 0; index--) {
    bytes[index] = milliseconds & 255;
    milliseconds = Math.floor(milliseconds / 256);
  }
  bytes[6] = (bytes[6] & 15) | 112;
  bytes[8] = (bytes[8] & 63) | 128;
  const value = bytes.toString("hex");
  return `${value.slice(0, 8)}-${value.slice(8, 12)}-${value.slice(12, 16)}-${value.slice(16, 20)}-${value.slice(20)}`;
}

async function launch() {
  application = await _electron.launch({ executablePath: executable, args: [`--user-data-dir=${profile}`], chromiumSandbox: true, offline: true, timeout: 30_000 });
  applicationProcess = application.process();
  applicationProcess.stdout?.on("data", (value) => process.stdout.write(value));
  applicationProcess.stderr?.on("data", (value) => process.stderr.write(value));
  page = await application.firstWindow();
  page.setDefaultTimeout(15_000);
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("console", (message) => { if (message.type() === "error") errors.push(message.text()); });
  await page.waitForLoadState("domcontentloaded");
}

async function close() {
  if (!application) return;
  const owned = application;
  application = undefined;
  const process = applicationProcess;
  applicationProcess = undefined;
  if (!process || process.exitCode !== null || process.signalCode !== null) return;
  const exited = new Promise<void>((resolve) => process.once("close", () => resolve()));
  await owned.evaluate(({ app }) => { setTimeout(() => app.quit(), 250); });
  void owned.close().catch(() => undefined);
  let timer: ReturnType<typeof setTimeout> | undefined;
  try {
    await Promise.race([exited, new Promise<never>((_, reject) => { timer = setTimeout(() => reject(new Error("DEMO_GRACEFUL_QUIT_TIMEOUT")), 20_000); })]);
  } finally { clearTimeout(timer); }
}

async function unlock() {
  await page.getByLabel("Workspace passphrase").fill(passphrase);
  await page.getByLabel("Email or username").fill(username);
  await page.getByLabel("Administrator password").fill(password);
  await page.getByRole("button", { name: "Unlock workspace", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
}

async function verifySavedEditor() {
  const lineItems = page.getByRole("table", { name: "Invoice line items", exact: true });
  const reviewedExpenses = page.getByRole("table", { name: "Reviewed expenses", exact: true });
  await expect(lineItems).toBeVisible();
  await expect(lineItems.getByRole("row")).toHaveCount(4);
  await expect(lineItems.getByText("Linked reviewed expense · internal only", { exact: true })).toHaveCount(3);
  await expect(reviewedExpenses).toBeVisible();
  await expect(reviewedExpenses.getByRole("row")).toHaveCount(4);
  await expect(reviewedExpenses.getByText("Added", { exact: true })).toHaveCount(3);
  for (const expense of expenses) {
    const checkbox = reviewedExpenses.getByRole("checkbox", { name: `Select ${expense.supplier} · ${expense.number}`, exact: true });
    await expect(checkbox).toBeDisabled();
    await expect(checkbox).not.toBeChecked();
  }
  await expect(page.getByTestId("invoice-subtotal")).toContainText("$150.00");
  await expect(page.getByTestId("invoice-gst")).toContainText("$10.50");
  await expect(page.getByTestId("invoice-total")).toContainText("$160.50");
  await expect(page.getByTestId("invoice-cost")).toContainText("$150.00");
  await expect(page.getByTestId("invoice-margin")).toContainText("$0.00");
  await expect(page.getByTestId("invoice-margin")).toContainText("Profit (ex GST)");
}

async function captureEditorViewport(width: number, height: number, filename: string) {
  await application!.evaluate(({ BrowserWindow }, dimensions) => {
    BrowserWindow.getAllWindows()[0].setContentSize(dimensions.width, dimensions.height);
  }, { width, height });
  await expect.poll(() => page.evaluate(() => ({ width: innerWidth, height: innerHeight }))).toEqual({ width, height });
  const dimensions = await page.evaluate(() => {
    const lineTable = document.querySelector('table[aria-label="Invoice line items"]')!;
    const scrollContainer = lineTable.parentElement!;
    return { width: innerWidth, height: innerHeight, pageScrollWidth: document.documentElement.scrollWidth, pageClientWidth: document.documentElement.clientWidth, tableScrollWidth: scrollContainer.scrollWidth, tableClientWidth: scrollContainer.clientWidth };
  });
  assert.ok(dimensions.pageScrollWidth <= dimensions.pageClientWidth, `Page overflow at ${width}×${height}: ${JSON.stringify(dimensions)}`);
  viewportChecks.push(dimensions);
  await page.screenshot({ path: path.join(root, filename), fullPage: true });
  stage(`Captured ${width}×${height} saved editor; page horizontal overflow absent, table scroll ${dimensions.tableScrollWidth}/${dimensions.tableClientWidth}.`);
}

async function authority() {
  const workspace = await page.evaluate(() => {
    const serialized = sessionStorage.getItem("tammy.session.active");
    if (!serialized) throw new Error("DEMO_SESSION_MISSING");
    return JSON.parse(serialized);
  });
  return { authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId }, organisationId: workspace.organisationId };
}

async function rpc(module: string, method: string, input: any, output: any, value: any) {
  const request = toBinary(AnySchema, create(AnySchema, { typeUrl: `type.googleapis.com/${input.typeName}`, value: toBinary(input, create(input, value)) }));
  const response = await page.evaluate(async ({ module, method, request }) => {
    const envelope = await (window as any).conveyor.invoke(`conveyor:${module}`, method, Uint8Array.from(request));
    if (!envelope.ok) throw new Error(`${method}:${envelope.error?.code ?? "UNKNOWN"}`);
    return Array.from(envelope.data as Uint8Array);
  }, { module, method, request: Array.from(request) });
  const envelope = fromBinary(AnySchema, Uint8Array.from(response));
  assert.equal(envelope.typeUrl, `type.googleapis.com/${output.typeName}`);
  return fromBinary(output, envelope.value) as any;
}

const money = (minorUnits: number) => ({ currencyCode: "AUD", minorUnits: BigInt(minorUnits) });
const command = (authentication: any) => ({ authentication, idempotencyKey: uuidV7() });
const listDocuments = (context: any) => rpc("documents", "listDocuments", ListDocumentsRequestSchema, ListDocumentsResponseSchema, { ...context, page: { pageSize: 100 } });
const listInvoices = (context: any) => rpc("accounting", "listInvoices", ListInvoicesRequestSchema, ListInvoicesResponseSchema, { ...context, page: { pageSize: 100 } });

await mkdir(path.join(root, "sample-expenses"), { recursive: true });
await writeFile(path.join(root, "README.md"), `# Tammy invoice demo\n\nThis is an isolated test workspace containing fictional data. No invoice has been issued.\n\n## Open the demo\n\nRun:\n\n\`\`\`sh\nrtk proxy "${executable}" --user-data-dir="${profile}"\n\`\`\`\n\nYou can also run \`rtk proxy zsh /Users/benebsworth/projects/tammy/output/tammy-invoice-demo/open-demo.sh\` to open a separate macOS app instance. This helper should be used only when the demo is closed.\n\nThe packaged app may be rebuilt at the same location. Keep the profile directory to retain this demo.\n\n## Test-only sign-in\n\n- Email or username: \`${username}\`\n- Administrator password: \`${password}\`\n- Workspace passphrase: \`${passphrase}\`\n\nThese credentials are intentionally stored here for this fictional demo only.\n\n## What to inspect\n\n1. Open **Documents** for three reviewed sample expenses. Their source PDFs are also in \`sample-expenses/\`.\n2. Open **Invoices**, then **Open TEST-2026-001**. The saved draft links one row in **Invoice line items** to each reviewed expense. The **Reviewed expenses** table marks all three sources **Added** and prevents duplicate selection.\n3. Verify $150.00 before GST, $10.50 GST and $160.50 total. Internal cost is $150.00 and profit before GST is $0.00.\n4. Use **Export PDF** to save another draft copy. The seed run exports \`TEST-2026-001-exported-from-Tammy.pdf\`.\n\nAll names, addresses, identifiers and expense tax treatments are examples chosen for testing. No payment is required.\n\n## Rerun seed verification\n\nRun \`rtk proxy zsh /Users/benebsworth/projects/tammy/output/tammy-invoice-demo/run-seed.sh\`. It preserves this profile, reuses matching sample expenses and saves the same draft. It must not run while this demo is open. The script closes the app when finished.\n`);

try {
  await launch();
  await page.waitForFunction(() => document.querySelector("h1")?.textContent?.trim());
  const heading = await page.locator("h1").first().textContent();
  if (heading?.includes("Unlock")) {
    await unlock();
    stage("Unlocked existing isolated demo workspace.");
  } else {
    await page.evaluate(() => { history.replaceState(null, "", "/setup/workspace"); dispatchEvent(new PopStateEvent("popstate")); });
    await expect(page.getByRole("heading", { name: "Create your local workspace" })).toBeVisible();
    await page.getByLabel("Your name").fill("Invoice Demo");
    await page.getByLabel("Email or username").fill(username);
    await page.getByLabel("Business legal name").fill(sample.sellerName);
    await page.getByLabel("Business display name").fill("Wattle & Co Test");
    await page.getByLabel("ABN", { exact: true }).fill(sample.sellerAbn);
    await page.getByLabel("Workspace passphrase").fill(passphrase);
    await page.getByLabel("Administrator password").fill(password);
    await page.getByRole("button", { name: "Create local workspace", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Save your recovery code" })).toBeVisible();
    // Never read, log, or capture the recovery code in this throwaway demo.
    await page.getByRole("button", { name: "I saved my recovery code" }).click();
    await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
    stage("Created isolated fictional demo workspace.");
  }

  let context = await authority();
  const existingDocuments = await listDocuments(context);
  const documentIds: string[] = [];
  for (const [index, expense] of expenses.entries()) {
    const sourceDisplayName = `${expense.name} - example expense.pdf`;
    let document = existingDocuments.documents.find((value: any) => value.sourceDisplayName === sourceDisplayName);
    const candidate = { supplierName: expense.supplier, invoiceNumber: expense.number, documentDate: { year: 2026, month: 10, day: 1 }, subtotal: money(expense.subtotal), gst: money(expense.gst), total: money(expense.subtotal + expense.gst) };
    const original = await renderInvoicePdf({ ...sample, number: expense.number, sellerName: expense.supplier, sellerAbn: "11000000560", sellerAddress: `${index + 10} Example Supplier Road\nMelbourne VIC 3000`, customerName: sample.sellerName, customerAddress: sample.sellerAddress, lines: [{ description: expense.name, quantityMilli: 1000, unitPriceMinor: expense.subtotal, taxTreatment: expense.gst ? "taxable" : "gst-free" }], notes: "Synthetic source expense for testing only. Amounts and tax treatment are examples.", paymentInstructions: "Example expense. No payment required.", state: "draft" });
    await writeFile(path.join(root, "sample-expenses", expense.filename), original);
    if (!document) {
      document = (await rpc("documents", "ingestDocument", IngestDocumentRequestSchema, IngestDocumentResponseSchema, { commandContext: command(context.authentication), organisationId: context.organisationId, sourceDisplayName, mimeType: "application/pdf", original, extractedText: `${expense.supplier}\n${expense.number}\n${expense.name}\nSubtotal AUD ${(expense.subtotal / 100).toFixed(2)}\nGST AUD ${(expense.gst / 100).toFixed(2)}\nTotal AUD ${((expense.subtotal + expense.gst) / 100).toFixed(2)}\nExample data for testing only`, candidate })).document;
    }
    if (document.status !== DocumentStatus.REVIEWED) {
      document = (await rpc("documents", "saveDocumentReview", SaveDocumentReviewRequestSchema, SaveDocumentReviewResponseSchema, { commandContext: command(context.authentication), documentId: document.id, expectedVersion: document.version, candidate })).document;
    }
    assert.equal(document.status, DocumentStatus.REVIEWED);
    assert.equal(document.candidate.subtotal.minorUnits, BigInt(expense.subtotal));
    assert.equal(document.candidate.gst.minorUnits, BigInt(expense.gst));
    documentIds.push(document.id);
    stage(`Retained and reviewed ${expense.name}: AUD ${((expense.subtotal + expense.gst) / 100).toFixed(2)}.`);
  }

  const { sellerName, sellerAbn, state, ...draft } = sample;
  draft.lines = draft.lines.map((line: any, index: number) => ({ ...line, sourceDocumentId: documentIds[index] }));
  const invoices = await listInvoices(context);
  const existing = invoices.invoices.find((value: any) => value.draft?.number === sample.number);
  if (existing) assert.equal(existing.state, InvoiceState.DRAFT, "Never overwrite an issued invoice");
  const saved = (await rpc("accounting", "saveInvoiceDraft", SaveInvoiceDraftRequestSchema, SaveInvoiceDraftResponseSchema, { commandContext: command(context.authentication), organisationId: context.organisationId, invoiceId: existing?.id ?? "", expectedVersion: existing?.version ?? 0n, draft: invoiceDraftToProto(draft) })).invoice;
  assert.equal(saved.state, InvoiceState.DRAFT);
  assert.equal(saved.totalMinor, 16050n);
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await page.getByRole("button", { name: "Open TEST-2026-001", exact: true }).click();
  await expect(page.getByLabel("Customer name", { exact: true })).toHaveValue(sample.customerName);
  await verifySavedEditor();
  await page.getByRole("button", { name: "Manage invoice template", exact: true }).click();
  await page.getByRole("textbox", { name: "Template seller address", exact: true }).fill(sample.sellerAddress);
  await page.getByRole("textbox", { name: "Default notes", exact: true }).fill(sample.notes);
  await page.getByRole("textbox", { name: "Default payment instructions", exact: true }).fill(sample.paymentInstructions);
  await page.getByLabel("Payment terms (days)", {exact:true}).fill("14");
  const logo = pngLogo();
  await writeFile(path.join(root, "sample-company-logo.png"), Buffer.from(logo.dataBase64, "base64"));
  if (await page.getByText("No company logo selected.", {exact:true}).isVisible()) {
    await page.getByLabel("Company logo", {exact:true}).setInputFiles(path.join(root, "sample-company-logo.png"));
    await expect(page.getByRole("img", {name:"Company logo preview",exact:true})).toBeVisible();
  }
  await page.getByRole("button", {name:"Save template",exact:true}).click();
  await expect(page.getByText("Company template saved.",{exact:true})).toBeVisible();
  await page.getByRole("button", {name:"Apply saved template",exact:true}).click();
  await expect(page.getByRole("img",{name:"Invoice logo",exact:true})).toBeVisible();
  await captureEditorViewport(1440,900,"company-template.png");
  await page.getByRole("button", {name:"Close template editor",exact:true}).click();
  await page.getByRole("button", { name: "Save draft", exact: true }).click();
  await expect(page.getByText("Draft saved.", { exact: true })).toBeVisible();
  const exportedPdf = path.join(root, "TEST-2026-001-exported-from-Tammy.pdf");
  await application!.evaluate(({ dialog }, filePath) => { dialog.showSaveDialog = async () => ({ canceled: false, filePath }); }, exportedPdf);
  await page.getByRole("button", { name: "Export PDF", exact: true }).click();
  await expect(page.getByText("Invoice PDF saved.", { exact: true })).toBeVisible();
  const exportedBytes = await readFile(exportedPdf);
  assert.match(exportedBytes.toString("latin1"), /\/Subtype\s*\/Image\b/);
  const exported = pdfStreams(exportedBytes);
  for (const expected of ["DRAFT", sample.number, sample.customerName, "150.00", "10.50", "160.50", "Page 1 of 1"]) assert.ok(exported.includes(expected), `PDF missing ${expected}`);
  for (const id of documentIds) assert.ok(!exported.includes(id));
  assert.doesNotMatch(exported, /unitCostMinor|marginMinor|sourceDocumentId|Internal cost|Margin|Profit/);
  assert.ok(exported.includes(sample.paymentInstructions));
  stage("Saved the linked draft through Tammy and exported its real bundled-engine PDF.");
  await close();
  await launch();
  await unlock();
  context = await authority();
  const reopenedDocuments = (await listDocuments(context)).documents;
  assert.equal(reopenedDocuments.length, 3);
  assert.ok(reopenedDocuments.every((value: any) => value.status === DocumentStatus.REVIEWED));
  const reopened = (await listInvoices(context)).invoices.find((value: any) => value.id === saved.id);
  assert.equal(reopened.state, InvoiceState.DRAFT);
  assert.equal(reopened.subtotalMinor, 15000n);
  assert.equal(reopened.gstMinor, 1050n);
  assert.equal(reopened.totalMinor, 16050n);
  assert.deepEqual(reopened.draft.lines.map((line: any) => line.sourceDocumentId), documentIds);
  await page.getByRole("link", { name: "Documents", exact: true }).click();
  for (const expense of expenses) {
    await page.getByRole("button", { name: new RegExp(`${expense.supplier} ${expense.number}`) }).click();
    await expect(page.getByRole("heading", { name: `${expense.name} - example expense.pdf`, exact: true })).toBeVisible();
    await expect(page.getByLabel("Supplier", { exact: true })).toHaveValue(expense.supplier);
    await expect(page.getByRole("button", { name: "Review saved", exact: true })).toBeDisabled();
  }
  await page.screenshot({ path: path.join(root, "reviewed-expenses.png"), fullPage: true });
  await page.getByRole("link", { name: "Invoices", exact: true }).click();
  await page.getByRole("button", { name: "Open TEST-2026-001", exact: true }).click();
  await expect(page.getByLabel("Customer name", { exact: true })).toHaveValue(sample.customerName);
  await expect(page.getByLabel("Customer name", { exact: true })).toBeEnabled();
  await verifySavedEditor();
  await captureEditorViewport(1440, 900, "saved-draft.png");
  await captureEditorViewport(1024, 700, "saved-draft-1024x700.png");
  assert.deepEqual(errors, []);
  stage("Restart verified: all three expenses remain reviewed and TEST-2026-001 remains an editable saved draft.");
  await expect(page.getByRole("img",{name:"Invoice logo",exact:true})).toBeVisible();
  await writeFile(path.join(root, "verification.json"), `${JSON.stringify({ verifiedAt: new Date().toISOString(), profile, executable, invoiceNumber: sample.number, invoiceId: saved.id, state: "draft", sourceDocumentIds: documentIds, subtotalMinor: 15000, gstMinor: 1050, totalMinor: 16050, expenseReviewPersisted: true, draftReopened: true, companyTemplateSaved: true, invoiceLogoPersisted: true, invoicePdfLogoEmbedded: true, semanticTablesVerified: true, linkedExpenseReselectionDisabled: true, viewportChecks, exportedPdf, consoleErrors: errors, stages }, null, 2)}\n`);
  await writeFile(path.join(root,"README.md"), `${await readFile(path.join(root,"README.md"),"utf8")}\n## Company invoice template\n\nThe saved draft includes a sample company logo. Use **Manage invoice template** on the invoice page to change the logo, seller address, notes, payment instructions and payment terms. New invoices inherit saved defaults; **Apply saved template** updates only the editable invoice in view. The sample payment instructions are fictional and no payment is required.\n`);
  await writeFile(path.join(root,"README.md"), `${await readFile(path.join(root,"README.md"),"utf8")}\n## Preserved earlier profile\n\nThis demo uses \`user-data-company-template\`. The earlier \`user-data\` directory is preserved: it stalled during local engine startup after the ad hoc package rebuild. Its catalogue and authentication journal chains validated, but the startup cause remains unconfirmed. The fresh profile was tested through save, export and restart; no earlier authentication data was reset.\n`);
  await rm(path.join(root, "seed-failure.txt"), { force: true });
} catch (error) {
  stage(`FAILED: ${error instanceof Error ? error.message : String(error)}`);
  await writeFile(path.join(root, "seed-failure.txt"), stages.join("\n"));
  throw error;
} finally {
  await close();
}
stage("Closed Tammy gracefully. Persistent demo profile is ready to reopen.");
