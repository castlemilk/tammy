import { create } from "@bufbuild/protobuf";
import {
  BankStatementImportSchema,
  BankStatementLineSchema,
  BankStatementLineStatus,
  CompleteBankReconciliationRequestSchema,
  CompleteBankReconciliationResponseSchema,
  GetBankingSummaryRequestSchema,
  GetBankingSummaryResponseSchema,
  ImportBankStatementRequestSchema,
  ImportBankStatementResponseSchema,
  ListBankStatementLinesRequestSchema,
  ListBankStatementLinesResponseSchema,
  MatchBankStatementLineRequestSchema,
  MatchBankStatementLineResponseSchema,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
import {
  CivilDateSchema,
  MoneySchema,
  PageInfoSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { BankingScreen } from "./banking-screen";

const listCodec = createProtoMethodCodec({
  input: ListBankStatementLinesRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 262_144,
  output: ListBankStatementLinesResponseSchema,
});
const summaryCodec = createProtoMethodCodec({
  input: GetBankingSummaryRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: GetBankingSummaryResponseSchema,
});
const matchCodec = createProtoMethodCodec({
  input: MatchBankStatementLineRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 32_768,
  output: MatchBankStatementLineResponseSchema,
});
const reconcileCodec = createProtoMethodCodec({
  input: CompleteBankReconciliationRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 16_384,
  output: CompleteBankReconciliationResponseSchema,
});

function fixture() {
  const organisationId = "018f0000-0000-7000-8000-000000000004";
  let status = BankStatementLineStatus.UNMATCHED;
  const line = () =>
    create(BankStatementLineSchema, {
      id: "018f0000-0000-7000-8000-000000000301",
      statementImportId: "018f0000-0000-7000-8000-000000000302",
      organisationId,
      version: status === BankStatementLineStatus.UNMATCHED ? 1n : 2n,
      transactionDate: create(CivilDateSchema, { year: 2026, month: 8, day: 10 }),
      description: "Officeworks INV-029847",
      amount: create(MoneySchema, { currencyCode: "AUD", minorUnits: -31900n }),
      status,
    });
  const api = {
    importBankStatement: vi.fn<(frame: Uint8Array) => Promise<Uint8Array>>(),
    listBankStatementLines: vi.fn(async () =>
      listCodec.encodeResponse(
        create(ListBankStatementLinesResponseSchema, {
          lines: [line()],
          page: create(PageInfoSchema, { returnedCount: 1 }),
        }),
      ),
    ),
    getBankingSummary: vi.fn(async () =>
      summaryCodec.encodeResponse(
        create(GetBankingSummaryResponseSchema, {
          importedLineCount: 1,
          unmatchedLineCount: status === BankStatementLineStatus.UNMATCHED ? 1 : 0,
          unreconciledLineCount: status === BankStatementLineStatus.RECONCILED ? 0 : 1,
          latestClosingBalance: create(MoneySchema, { currencyCode: "AUD", minorUnits: 68100n }),
        }),
      ),
    ),
    matchBankStatementLine: vi.fn(async (frame: Uint8Array) => {
      const request = matchCodec.decodeRequest(frame);
      expect(request.lineId).toBe(line().id);
      expect(request.expectedVersion).toBe(1n);
      status = BankStatementLineStatus.MATCHED;
      return matchCodec.encodeResponse(
        create(MatchBankStatementLineResponseSchema, { line: line() }),
      );
    }),
    completeBankReconciliation: vi.fn(async (frame: Uint8Array) => {
      expect(reconcileCodec.decodeRequest(frame).organisationId).toBe(organisationId);
      status = BankStatementLineStatus.RECONCILED;
      return reconcileCodec.encodeResponse(
        create(CompleteBankReconciliationResponseSchema, {
          reconciledLineCount: 1,
          closingBalance: create(MoneySchema, { currencyCode: "AUD", minorUnits: 68100n }),
        }),
      );
    }),
  } satisfies Pick<
    TammyDesktopAPI,
    | "completeBankReconciliation"
    | "getBankingSummary"
    | "importBankStatement"
    | "listBankStatementLines"
    | "matchBankStatementLine"
  >;
  return api;
}
const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  organisationId: "018f0000-0000-7000-8000-000000000004",
};
const importCodec = createProtoMethodCodec({
  input: ImportBankStatementRequestSchema,
  maximumRequestBytes: 262_144,
  maximumResponseBytes: 32_768,
  output: ImportBankStatementResponseSchema,
});

it("requires an explicit reviewed reference and keeps matching distinct from reconciliation", async () => {
  const api = fixture();
  const user = userEvent.setup();
  render(<BankingScreen api={api} workspace={workspace} />);
  const table = await screen.findByRole("table", { name: "Statement lines" });
  expect(within(table).getByText("Unmatched")).toBeTruthy();
  const complete = screen.getByRole("button", {
    name: "Complete reconciliation",
  }) as HTMLButtonElement;
  expect(complete.disabled).toBe(true);
  await user.click(screen.getByRole("button", { name: "Match" }));
  expect(screen.getByLabelText("Reviewed source reference")).toBe(document.activeElement);
  expect(
    (screen.getByRole("button", { name: "Confirm match" }) as HTMLButtonElement).disabled,
  ).toBe(true);
  await user.type(screen.getByLabelText("Reviewed source reference"), "Cancelled reference");
  await user.click(screen.getByRole("button", { name: "Cancel match" }));
  expect(api.matchBankStatementLine).not.toHaveBeenCalled();
  expect(screen.queryByRole("form", { name: "Match review" })).toBeNull();
  await user.click(screen.getByRole("button", { name: "Match" }));
  expect((screen.getByLabelText("Reviewed source reference") as HTMLInputElement).value).toBe("");
  await user.type(screen.getByLabelText("Reviewed source reference"), "Receipt INV-029847");
  await user.dblClick(screen.getByRole("button", { name: "Confirm match" }));
  expect(await within(table).findByText("Matched")).toBeTruthy();
  const matchedFrame = api.matchBankStatementLine.mock.calls[0]?.[0];
  if (!matchedFrame) throw new Error("match request missing");
  expect(matchCodec.decodeRequest(matchedFrame).matchReference).toBe("Receipt INV-029847");
  expect(complete.disabled).toBe(false);
  await user.dblClick(complete);
  expect(await within(table).findByText("Reconciled")).toBeTruthy();
  expect(api.matchBankStatementLine).toHaveBeenCalledOnce();
  expect(api.completeBankReconciliation).toHaveBeenCalledOnce();
});

it("retains a failed match draft and reuses its command key on an unchanged retry", async () => {
  const api = fixture();
  api.matchBankStatementLine.mockRejectedValueOnce(new Error("response interrupted"));
  const user = userEvent.setup();
  render(<BankingScreen api={api} workspace={workspace} />);
  await user.click(await screen.findByRole("button", { name: "Match" }));
  await user.type(screen.getByLabelText("Reviewed source reference"), "Receipt INV-029847");
  await user.click(screen.getByRole("button", { name: "Confirm match" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("Your reference is still here"),
  );
  expect((screen.getByLabelText("Reviewed source reference") as HTMLInputElement).value).toBe(
    "Receipt INV-029847",
  );
  await user.click(screen.getByRole("button", { name: "Confirm match" }));
  await waitFor(() => expect(api.matchBankStatementLine).toHaveBeenCalledTimes(2));
  const requests = api.matchBankStatementLine.mock.calls.map(([frame]) =>
    matchCodec.decodeRequest(frame),
  );
  expect(requests[0]?.commandContext?.idempotencyKey).toBe(
    requests[1]?.commandContext?.idempotencyKey,
  );
});

it("keeps confirmed imports separate from refresh failures and blocks duplicate writes", async () => {
  const api = fixture();
  let accepted = false;
  api.importBankStatement.mockImplementation(async (frame: Uint8Array) => {
    const request = importCodec.decodeRequest(frame);
    expect(request.lines[0]?.amount?.minorUnits).toBe(-11000n);
    expect(request.openingBalance?.minorUnits).toBe(12345678901n);
    accepted = true;
    return importCodec.encodeResponse(
      create(ImportBankStatementResponseSchema, {
        statementImport: create(BankStatementImportSchema, {
          id: "018f0000-0000-7000-8000-000000000302",
          organisationId: workspace.organisationId,
          lineCount: 1,
          openingBalance: request.openingBalance,
          closingBalance: create(MoneySchema, { currencyCode: "AUD", minorUnits: 12345667901n }),
        }),
      }),
    );
  });
  const originalList = api.listBankStatementLines.getMockImplementation();
  if (!originalList) throw new Error("list fixture missing");
  api.listBankStatementLines.mockImplementation(async () => {
    if (accepted) throw new Error("refresh interrupted");
    return originalList();
  });
  const user = userEvent.setup();
  render(<BankingScreen api={api} workspace={workspace} />);
  await screen.findByRole("table", { name: "Statement lines" });
  await user.click(screen.getByRole("button", { name: "Import statement" }));
  const form = screen.getByRole("form", { name: "Statement import" });
  expect((screen.getByLabelText("Opening balance · AUD") as HTMLInputElement).value).toBe("");
  await user.type(screen.getByLabelText("Opening balance · AUD"), "123456789.01");
  await user.type(screen.getByLabelText("CSV rows"), "2024-05-12,Synthetic supplies,-110.00");
  await user.dblClick(within(form).getByRole("button", { name: "Import statement" }));
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("The action was saved"),
  );
  expect(screen.queryByRole("form", { name: "Statement import" })).toBeNull();
  expect(api.importBankStatement).toHaveBeenCalledOnce();
  expect(
    (screen.getByRole("button", { name: "Import statement" }) as HTMLButtonElement).disabled,
  ).toBe(true);
  expect(
    (screen.getByRole("button", { name: "Complete reconciliation" }) as HTMLButtonElement).disabled,
  ).toBe(true);
});
