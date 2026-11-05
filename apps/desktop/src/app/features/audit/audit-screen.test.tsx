import { create } from "@bufbuild/protobuf";
import {
  JournalSchema,
  JournalSource,
  JournalState,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  BankStatementLineStatus,
  ListBankStatementLinesRequestSchema,
  ListBankStatementLinesResponseSchema,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
import {
  CivilDateSchema,
  MoneySchema,
  PageInfoSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  DocumentStatus,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  BasWorkpaperStatus,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
import { AuditScreen } from "./audit-screen";

const journalsCodec = createProtoMethodCodec({
  input: ListJournalsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListJournalsResponseSchema,
});

it("keeps available retained activity visible when another local projection is unavailable", async () => {
  const organisationId = "018f0000-0000-7000-8000-000000000004";
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.listJournals.mockImplementation(async () =>
    journalsCodec.encodeResponse(
      create(ListJournalsResponseSchema, {
        journals: [
          create(JournalSchema, {
            id: "018f0000-0000-7000-8000-000000000501",
            organisationId,
            version: 1n,
            state: JournalState.POSTED,
            source: JournalSource.MANUAL,
            postingDate: create(CivilDateSchema, { year: 2026, month: 8, day: 10 }),
            memo: "Office supplies paid personally",
            totalDebits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
            totalCredits: create(MoneySchema, { currencyCode: "AUD", minorUnits: 31900n }),
          }),
        ],
        page: create(PageInfoSchema, { returnedCount: 1 }),
      }),
    ),
  );

  render(
    <AuditScreen
      accounting={fixture.conveyor.accounting}
      banking={fixture.conveyor.banking}
      documents={fixture.conveyor.documents}
      reporting={fixture.conveyor.reporting}
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId,
      }}
    />,
  );

  expect(await screen.findByText("Office supplies paid personally")).toBeTruthy();
  expect(screen.getByText("Journal")).toBeTruthy();
  expect(screen.getByText(/\$319\.00 debits and credits/)).toBeTruthy();
  expect(
    screen.getByText(/does not replace exported audit-chain verification evidence/),
  ).toBeTruthy();
});

const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  organisationId: "018f0000-0000-7000-8000-000000000004",
};
const bankCodec = createProtoMethodCodec({
  input: ListBankStatementLinesRequestSchema,
  output: ListBankStatementLinesResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
});
const documentsCodec = createProtoMethodCodec({
  input: ListDocumentsRequestSchema,
  output: ListDocumentsResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4 * 1024 * 1024,
});
const basCodec = createProtoMethodCodec({
  input: GetCurrentBasDraftRequestSchema,
  output: GetCurrentBasDraftResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
});

function recordsFixture() {
  const fixture = createConveyorFixture();
  fixture.procedures.accounting.listJournals.mockResolvedValue(
    journalsCodec.encodeResponse(
      create(ListJournalsResponseSchema, {
        journals: [
          {
            id: "018f0000-0000-7000-8000-000000000501",
            version: 7n,
            memo: "Synthetic retained journal",
            state: JournalState.REVERSED,
            source: JournalSource.MANUAL,
            postingDate: { year: 2024, month: 5, day: 12 },
            totalDebits: { currencyCode: "AUD", minorUnits: 31900n },
          },
        ],
      }),
    ),
  );
  fixture.procedures.banking.listBankStatementLines.mockResolvedValue(
    bankCodec.encodeResponse(
      create(ListBankStatementLinesResponseSchema, {
        lines: [
          {
            id: "018f0000-0000-7000-8000-000000000502",
            version: 3n,
            statementImportId: "018f0000-0000-7000-8000-000000000503",
            matchReference: "User-entered reference, not a verified link",
            transactionDate: { year: 2024, month: 5, day: 12 },
            description: "Synthetic statement line",
            amount: { currencyCode: "AUD", minorUnits: -31900n },
            status: BankStatementLineStatus.RECONCILED,
          },
        ],
      }),
    ),
  );
  fixture.procedures.documents.listDocuments.mockResolvedValue(
    documentsCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents: [
          {
            id: "018f0000-0000-7000-8000-000000000504",
            version: 2n,
            sourceDisplayName: "synthetic.pdf",
            status: DocumentStatus.REVIEWED,
            createdAt: { seconds: 1715558400n },
            candidate: { supplierName: "Synthetic document", invoiceNumber: "SYN-001" },
          },
        ],
      }),
    ),
  );
  fixture.procedures.reporting.getCurrentBasDraft.mockResolvedValue(
    basCodec.encodeResponse(
      create(GetCurrentBasDraftResponseSchema, {
        workpaper: {
          id: "018f0000-0000-7000-8000-000000000505",
          version: 4n,
          status: BasWorkpaperStatus.DRAFT_NOT_LODGED,
          periodStart: { year: 2024, month: 4, day: 1 },
          periodEnd: { year: 2024, month: 6, day: 30 },
          createdAt: { seconds: 1715558401n },
          gstCredits1b: { currencyCode: "AUD", minorUnits: 0n },
        },
      }),
    ),
  );
  return fixture;
}
function renderAudit(fixture: ReturnType<typeof createConveyorFixture>, signedIn = true) {
  return render(
    <AuditScreen
      accounting={fixture.conveyor.accounting}
      banking={fixture.conveyor.banking}
      documents={fixture.conveyor.documents}
      reporting={fixture.conveyor.reporting}
      workspace={signedIn ? workspace : undefined}
    />,
  );
}

it("filters loaded records without issuing new requests and discloses exact retained metadata", async () => {
  const fixture = recordsFixture();
  renderAudit(fixture);
  const user = userEvent.setup();
  const list = await screen.findByRole("list", { name: "Retained activity records" });
  expect(within(list).getAllByRole("listitem")).toHaveLength(4);
  const filter = screen.getByRole("combobox", { name: "Filter loaded activity by record type" });
  await user.selectOptions(filter, "Banking");
  expect(within(list).getAllByRole("listitem")).toHaveLength(1);
  expect(
    within(list)
      .getAllByText("Reconciled")
      .filter((node) => node.closest("summary")),
  ).toHaveLength(1);
  const summary = within(list).getByText("Synthetic statement line").closest("summary");
  await user.click(summary as HTMLElement);
  expect(summary?.parentElement?.hasAttribute("open")).toBe(true);
  expect(screen.getByText("018f0000-0000-7000-8000-000000000503")).toBeTruthy();
  expect(screen.getByText("User-entered reference, not a verified link")).toBeTruthy();
  expect(screen.getByText("Record version").nextElementSibling?.textContent).toBe("3");
  await user.click(summary as HTMLElement);
  expect(summary?.parentElement?.hasAttribute("open")).toBe(false);
  const search = screen.getByRole("searchbox", { name: "Search loaded activity" });
  await user.type(search, "No such synthetic record");
  expect(screen.getByRole("heading", { name: "No matching loaded records" })).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Clear activity search" }));
  expect(document.activeElement).toBe(search);
  await user.selectOptions(filter, "Journal");
  expect(
    within(screen.getByRole("list", { name: "Retained activity records" }))
      .getAllByText("Reversed")
      .filter((node) => node.closest("summary")),
  ).toHaveLength(1);
  expect(
    within(screen.getByRole("list", { name: "Retained activity records" })).queryByText("Posted"),
  ).toBeNull();
  for (const procedure of [
    fixture.procedures.accounting.listJournals,
    fixture.procedures.banking.listBankStatementLines,
    fixture.procedures.documents.listDocuments,
    fixture.procedures.reporting.getCurrentBasDraft,
  ])
    expect(procedure).toHaveBeenCalledTimes(1);
  expect(
    journalsCodec.decodeRequest(
      fixture.procedures.accounting.listJournals.mock.calls[0]?.[0] ?? new Uint8Array(),
    ),
  ).toMatchObject({
    authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
    organisationId: workspace.organisationId,
    page: { pageSize: 200 },
  });
});

it("keeps unsupported status values explicit instead of inventing reviewed or reconciled state", async () => {
  const fixture = recordsFixture();
  fixture.procedures.banking.listBankStatementLines.mockResolvedValue(
    bankCodec.encodeResponse(
      create(ListBankStatementLinesResponseSchema, {
        lines: [
          {
            id: "018f0000-0000-7000-8000-000000000506",
            description: "Unknown bank status",
            status: 0,
          },
        ],
      }),
    ),
  );
  fixture.procedures.documents.listDocuments.mockResolvedValue(
    documentsCodec.encodeResponse(
      create(ListDocumentsResponseSchema, {
        documents: [
          {
            id: "018f0000-0000-7000-8000-000000000507",
            sourceDisplayName: "Unknown document status",
            status: 0,
          },
        ],
      }),
    ),
  );
  fixture.procedures.reporting.getCurrentBasDraft.mockResolvedValue(
    basCodec.encodeResponse(
      create(GetCurrentBasDraftResponseSchema, {
        workpaper: { id: "018f0000-0000-7000-8000-000000000508", status: 0 },
      }),
    ),
  );
  renderAudit(fixture);
  const list = await screen.findByRole("list", { name: "Retained activity records" });
  expect(
    within(list)
      .getAllByText("Unknown status")
      .filter((node) => node.closest("summary")),
  ).toHaveLength(3);
  expect(within(list).queryByText("Draft — not lodged")).toBeNull();
  expect(within(list).queryByText("Reviewed")).toBeNull();
  expect(within(list).queryByText("Unmatched")).toBeNull();
  expect(
    within(list)
      .getAllByText("Date unavailable")
      .filter((node) => node.closest("summary")),
  ).toHaveLength(3);
});

it("shows decode failures without hiding the available record families", async () => {
  const fixture = recordsFixture();
  fixture.procedures.documents.listDocuments.mockResolvedValue(new Uint8Array([0]));
  renderAudit(fixture);
  const list = await screen.findByRole("list", { name: "Retained activity records" });
  expect(within(list).getAllByRole("listitem")).toHaveLength(3);
  expect(screen.getByRole("alert").textContent).toContain("Unavailable sources: Documents");
  expect(screen.getByRole("alert").textContent).toContain("may be incomplete");
});

it("distinguishes healthy empty responses from all unavailable sources", async () => {
  const fixture = recordsFixture();
  fixture.procedures.accounting.listJournals.mockResolvedValue(
    journalsCodec.encodeResponse(create(ListJournalsResponseSchema, {})),
  );
  fixture.procedures.banking.listBankStatementLines.mockResolvedValue(
    bankCodec.encodeResponse(create(ListBankStatementLinesResponseSchema, {})),
  );
  fixture.procedures.documents.listDocuments.mockResolvedValue(
    documentsCodec.encodeResponse(create(ListDocumentsResponseSchema, {})),
  );
  fixture.procedures.reporting.getCurrentBasDraft.mockResolvedValue(
    basCodec.encodeResponse(create(GetCurrentBasDraftResponseSchema, {})),
  );
  const view = renderAudit(fixture);
  expect(await screen.findByRole("heading", { name: "No retained activity yet" })).toBeTruthy();
  expect(screen.queryByRole("alert")).toBeNull();
  view.unmount();
  const failed = recordsFixture();
  for (const procedure of [
    failed.procedures.accounting.listJournals,
    failed.procedures.banking.listBankStatementLines,
    failed.procedures.documents.listDocuments,
    failed.procedures.reporting.getCurrentBasDraft,
  ])
    procedure.mockRejectedValue(new Error("Synthetic read failure"));
  renderAudit(failed);
  expect(await screen.findByRole("heading", { name: "Activity unavailable" })).toBeTruthy();
  expect(screen.queryByRole("heading", { name: "No retained activity yet" })).toBeNull();
});

it("labels loading and does not make requests without a workspace", async () => {
  const fixture = recordsFixture();
  fixture.procedures.accounting.listJournals.mockReturnValue(new Promise(() => {}));
  const view = renderAudit(fixture);
  expect(await screen.findByRole("heading", { name: "Loading retained records" })).toBeTruthy();
  view.unmount();
  const signedOut = recordsFixture();
  renderAudit(signedOut, false);
  expect(screen.getByRole("heading", { name: "Activity unavailable" })).toBeTruthy();
  expect(signedOut.procedures.accounting.listJournals).not.toHaveBeenCalled();
});

it("retains every cent when displaying signed int64 boundary amounts", async () => {
  const fixture = recordsFixture();
  fixture.procedures.accounting.listJournals.mockResolvedValue(
    journalsCodec.encodeResponse(
      create(ListJournalsResponseSchema, {
        journals: [
          {
            id: "018f0000-0000-7000-8000-000000000509",
            version: 1n,
            memo: "Synthetic maximum journal",
            state: JournalState.POSTED,
            totalDebits: { currencyCode: "AUD", minorUnits: 9223372036854775807n },
          },
        ],
      }),
    ),
  );
  fixture.procedures.banking.listBankStatementLines.mockResolvedValue(
    bankCodec.encodeResponse(
      create(ListBankStatementLinesResponseSchema, {
        lines: [
          {
            id: "018f0000-0000-7000-8000-000000000510",
            version: 1n,
            description: "Synthetic minimum statement line",
            amount: { currencyCode: "AUD", minorUnits: -9223372036854775808n },
            status: BankStatementLineStatus.UNMATCHED,
          },
        ],
      }),
    ),
  );
  renderAudit(fixture);
  const list = await screen.findByRole("list", { name: "Retained activity records" });
  expect(within(list).getByText("$92,233,720,368,547,758.07 debits and credits")).toBeTruthy();
  expect(within(list).getByText("−$92,233,720,368,547,758.08")).toBeTruthy();
});
