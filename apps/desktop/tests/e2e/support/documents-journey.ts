import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { create } from "@bufbuild/protobuf";
import { expect, type Page } from "@playwright/test";
import {
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  AuthenticationContextSchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentStatus,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { createProtoMethodCodec } from "../../../src/shared/proto-ipc";
import { invokeConveyorFrame } from "./conveyor";
import type { AuthenticatedAccountingWorkspace } from "./current-accounting-workflow";

export const documentsCapturePhase = process.env.TAMMY_DOCUMENTS_CAPTURE;
if (
  documentsCapturePhase !== undefined &&
  documentsCapturePhase !== "before" &&
  documentsCapturePhase !== "after"
) {
  throw new Error("DOCUMENTS_CAPTURE_PHASE_INVALID");
}
const documentsCodec = createProtoMethodCodec({
  input: ListDocumentsRequestSchema,
  output: ListDocumentsResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 4 * 1024 * 1024,
});
const journalsCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  output: ListJournalsResponseSchema,
  maximumRequestBytes: 16384,
  maximumResponseBytes: 1024 * 1024,
});
const names = ["Synthetic Cedar Supplies", "Synthetic Grove Studio", "Synthetic Bower Services"];

export async function runDocumentsReviewJourney(
  page: Page,
  workspace: AuthenticatedAccountingWorkspace,
) {
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("link", { name: "Documents", exact: true }).click();
  await expect(page.getByRole("button", { name: "Upload document", exact: true })).toBeEnabled();
  const journalIdsBefore = await journalIds(page, workspace);
  const originalCount = (await readDocuments(page, workspace)).length;
  const input = page.locator('input[type="file"]');
  await input.setInputFiles({
    name: "synthetic-unsupported.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("synthetic"),
  });
  await expect(page.getByRole("alert")).toContainText("Choose a PDF, PNG or JPEG");
  await capture(page, "01-import-validation");

  for (let i = 0; i < 3; i++) {
    const name = "DOC-SYN-00" + (i + 1) + ".pdf";
    await input.setInputFiles({ name, mimeType: "application/pdf", buffer: nativeTextPdf(i) });
    await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Upload document", exact: true })).toBeEnabled();
  }
  await input.setInputFiles({
    name: "synthetic-manual-expense.png",
    mimeType: "image/png",
    buffer: Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6iocAAAAASUVORK5CYII=",
      "base64",
    ),
  });
  await expect(
    page.getByRole("heading", { name: "synthetic-manual-expense.png", exact: true }),
  ).toBeVisible();
  await expect
    .poll(async () => (await readDocuments(page, workspace)).length)
    .toBe(originalCount + 4);

  const row = page
    .getByRole("table", { name: "Retained documents", exact: true })
    .getByRole("button", { name: /DOC-SYN-001\.pdf/ });
  await row.focus();
  await page.keyboard.press("Space");
  await expect(page.getByRole("heading", { name: "DOC-SYN-001.pdf", exact: true })).toBeVisible();
  await capture(page, "02-list-selected");
  await capture(page, "03-review-before-save", "review");

  const supplier = page.getByLabel("Supplier", { exact: true });
  await expect(supplier).toHaveValue(names[0] ?? "");
  await supplier.fill("Cancelled synthetic rename");
  await page.getByRole("button", { name: "Cancel changes", exact: true }).click();
  await expect(supplier).toHaveValue(names[0] ?? "");
  await page.getByLabel("Total", { exact: true }).fill("999.00");
  await page.getByRole("button", { name: "Save review", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("The review could not be saved");
  await expect(page.getByLabel("Total", { exact: true })).toHaveValue("999.00");
  const failed = (await readDocuments(page, workspace)).find(
    (d) => d.sourceDisplayName === "DOC-SYN-001.pdf",
  );
  expect(failed?.version).toBe(1n);
  expect(failed?.status).toBe(DocumentStatus.NEEDS_REVIEW);
  await capture(page, "04-review-error", "review");

  await page.getByLabel("Total", { exact: true }).fill("110.00");
  await page.getByRole("button", { name: "Save review", exact: true }).dblclick();
  await expect(page.getByRole("button", { name: "Review saved", exact: true })).toBeDisabled();
  await expect(supplier).toHaveAttribute("readonly", "");
  const saved = (await readDocuments(page, workspace)).find(
    (d) => d.sourceDisplayName === "DOC-SYN-001.pdf",
  );
  expect(saved?.version).toBe(2n);
  expect(saved?.status).toBe(DocumentStatus.REVIEWED);
  expect(await journalIds(page, workspace)).toEqual(journalIdsBefore);
  await capture(page, "05-review-confirmed", "review");

  const search = page.getByLabel("Search documents", { exact: true });
  await search.fill("DOC-SYN-001");
  await page.getByRole("button", { name: "Close review", exact: true }).click();
  await expect(row).toBeFocused();
  await expect(search).toHaveValue("DOC-SYN-001");
  await expect(page.getByRole("region", { name: "Document review", exact: true })).toHaveCount(0);
  await capture(page, "06-close-retains-search");
  await row.focus();
  await page.keyboard.press("Space");
  await expect(supplier).toHaveValue(names[0] ?? "");
  await search.fill("no synthetic document matches this");
  await expect(page.getByText("No matching documents", { exact: true })).toBeVisible();
  await capture(page, "07-search-empty");
  await page.getByRole("button", { name: "Clear search", exact: true }).click();
  await expect(search).toBeFocused();
  await page
    .getByRole("table", { name: "Retained documents", exact: true })
    .getByRole("button", { name: /synthetic-manual-expense\.png/ })
    .click();
  await expect(page.getByText("Image · manual entry", { exact: true })).toBeVisible();
  await capture(page, "08-manual-image", "review");

  await page.emulateMedia({ reducedMotion: "reduce" });
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 1000 });
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
        ),
      )
      .toBe(true);
    if (documentsCapturePhase !== "before") {
      const scroll = page.getByRole("region", {
        name: "Scrollable retained documents",
        exact: true,
      });
      await scroll.evaluate((node) => {
        node.scrollLeft = 0;
      });
      await scroll.focus();
      await page.keyboard.press("ArrowRight");
      await expect.poll(() => scroll.evaluate((node) => node.scrollLeft)).toBeGreaterThan(0);
      await expect(scroll).toHaveCSS("outline-style", "solid");
    }
    await capture(page, "09-document-list-" + width);
    await supplier.focus();
    await expect(supplier).toHaveCSS("outline-style", "solid");
    await capture(page, "10-manual-fields-" + width, "focus");
  }
  await page.setViewportSize({ width: 1440, height: 1100 });
  await page.getByRole("link", { name: "Overview", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("heading", { name: "Documents", exact: true })).toBeVisible();
  await page.goForward();
  await expect(page.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("table", { name: "Retained documents", exact: true })).toBeVisible();
  expect(await journalIds(page, workspace)).toEqual(journalIdsBefore);
  expect((await readDocuments(page, workspace)).length).toBe(originalCount + 4);
}

async function readDocuments(page: Page, workspace: AuthenticatedAccountingWorkspace) {
  const request = create(ListDocumentsRequestSchema, {
    authentication: create(AuthenticationContextSchema, {
      actorUserId: workspace.userId,
      sessionId: workspace.sessionId,
    }),
    organisationId: workspace.organisationId,
    page: create(PageRequestSchema, { pageSize: 200 }),
  });
  return documentsCodec.decodeResponse(
    await invokeConveyorFrame(
      page,
      "documents",
      "listDocuments",
      documentsCodec.encodeRequest(request),
    ),
  ).documents;
}
async function journalIds(page: Page, workspace: AuthenticatedAccountingWorkspace) {
  const request = create(ListJournalsRequestSchema, {
    authentication: create(AuthenticationContextSchema, {
      actorUserId: workspace.userId,
      sessionId: workspace.sessionId,
    }),
    organisationId: workspace.organisationId,
    page: create(PageRequestSchema, { pageSize: 200 }),
  });
  return journalsCodec
    .decodeResponse(
      await invokeConveyorFrame(
        page,
        "accounting",
        "listJournals",
        journalsCodec.encodeRequest(request),
      ),
    )
    .journals.map((j) => j.id)
    .sort();
}
async function capture(page: Page, name: string, anchor: "page" | "review" | "focus" = "page") {
  if (!documentsCapturePhase) return;
  const directory = path.resolve(
    import.meta.dirname,
    "../../../../../output/playwright/soft-precision-documents-2026-10-23",
    documentsCapturePhase,
  );
  await mkdir(directory, { recursive: true });
  if (anchor === "page")
    await page.getByRole("heading", { name: "Documents", exact: true }).scrollIntoViewIfNeeded();
  if (anchor === "review")
    await page
      .getByRole("region", { name: "Document review", exact: true })
      .scrollIntoViewIfNeeded();
  await writeFile(
    path.join(directory, name + ".png"),
    await page.screenshot({ animations: "disabled" }),
    { flag: "wx" },
  );
}

/** Small valid one-page ASCII PDF. Its real text operators drive the existing local extractor. */
function nativeTextPdf(index: number) {
  const lines = [
    names[index] ?? "Synthetic supplier",
    "Invoice DOC-SYN-00" + (index + 1),
    "Date 12/05/2024",
    ...Array.from({ length: 22 }, (_, n) => "Synthetic source detail line " + (n + 1)),
    "Subtotal $100.00",
    "GST $10.00",
    "Total $110.00",
  ];
  const stream = [
    "BT",
    "/F1 12 Tf",
    "36 790 Td",
    "16 TL",
    ...lines.map((line) => "(" + line + ") Tj T*"),
    "ET",
  ].join("\n");
  const objects = [
    "<< /Type /Catalog /Pages 2 0 R >>",
    "<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
    "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
    "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
    "<< /Length " + Buffer.byteLength(stream) + " >>\nstream\n" + stream + "\nendstream",
  ];
  let pdf = "%PDF-1.4\n";
  const offsets = [0];
  for (const [i, object] of objects.entries()) {
    offsets.push(Buffer.byteLength(pdf));
    pdf += i + 1 + " 0 obj\n" + object + "\nendobj\n";
  }
  const xref = Buffer.byteLength(pdf);
  pdf +=
    "xref\n0 6\n0000000000 65535 f \n" +
    offsets
      .slice(1)
      .map((o) => String(o).padStart(10, "0") + " 00000 n \n")
      .join("") +
    "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n" +
    xref +
    "\n%%EOF\n";
  return Buffer.from(pdf);
}
