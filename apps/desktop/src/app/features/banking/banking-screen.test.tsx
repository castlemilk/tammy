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
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";

import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { createConveyorFixture, renderWithConveyor as render } from "../../test/conveyor-fixtures";
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
const importCodec = createProtoMethodCodec({
  input: ImportBankStatementRequestSchema,
  maximumRequestBytes: 262_144,
  maximumResponseBytes: 32_768,
  output: ImportBankStatementResponseSchema,
});

const workspace = {
  workspaceId: "018f0000-0000-7000-8000-000000000001",
  userId: "018f0000-0000-7000-8000-000000000002",
  sessionId: "018f0000-0000-7000-8000-000000000003",
  organisationId: "018f0000-0000-7000-8000-000000000004",
};

function fixture() {
  const fixture = createConveyorFixture();
  let status = BankStatementLineStatus.UNMATCHED;
  const line = () =>
    create(BankStatementLineSchema, {
      id: "018f0000-0000-7000-8000-000000000301",
      statementImportId: "018f0000-0000-7000-8000-000000000302",
      organisationId: workspace.organisationId,
      version: status === BankStatementLineStatus.UNMATCHED ? 1n : 2n,
      transactionDate: create(CivilDateSchema, { year: 2026, month: 8, day: 10 }),
      description: "Officeworks INV-029847",
      amount: create(MoneySchema, { currencyCode: "AUD", minorUnits: -31_900n }),
      status,
    });
  fixture.procedures.banking.listBankStatementLines.mockImplementation(async () =>
    listCodec.encodeResponse(
      create(ListBankStatementLinesResponseSchema, {
        lines: [line()],
        page: create(PageInfoSchema, { returnedCount: 1 }),
      }),
    ),
  );
  fixture.procedures.banking.getBankingSummary.mockImplementation(async () =>
    summaryCodec.encodeResponse(
      create(GetBankingSummaryResponseSchema, {
        importedLineCount: 1,
        unmatchedLineCount: status === BankStatementLineStatus.UNMATCHED ? 1 : 0,
        unreconciledLineCount: status === BankStatementLineStatus.RECONCILED ? 0 : 1,
        latestClosingBalance: create(MoneySchema, { currencyCode: "AUD", minorUnits: 68_100n }),
      }),
    ),
  );
  fixture.procedures.banking.matchBankStatementLine.mockImplementation(
    async (frame: Uint8Array) => {
      const request = matchCodec.decodeRequest(frame);
      expect(request.lineId).toBe(line().id);
      expect(request.expectedVersion).toBe(1n);
      status = BankStatementLineStatus.MATCHED;
      return matchCodec.encodeResponse(
        create(MatchBankStatementLineResponseSchema, { line: line() }),
      );
    },
  );
  fixture.procedures.banking.completeBankReconciliation.mockImplementation(
    async (frame: Uint8Array) => {
      expect(reconcileCodec.decodeRequest(frame).organisationId).toBe(workspace.organisationId);
      status = BankStatementLineStatus.RECONCILED;
      return reconcileCodec.encodeResponse(
        create(CompleteBankReconciliationResponseSchema, {
          reconciledLineCount: 1,
          closingBalance: create(MoneySchema, { currencyCode: "AUD", minorUnits: 68_100n }),
        }),
      );
    },
  );
  return fixture;
}

it("keeps the complete large balance in a named keyboard-accessible region", async () => {
  const testFixture = fixture();
  testFixture.procedures.banking.getBankingSummary.mockResolvedValue(
    summaryCodec.encodeResponse(
      create(GetBankingSummaryResponseSchema, {
        importedLineCount: 1,
        unmatchedLineCount: 1,
        unreconciledLineCount: 1,
        latestClosingBalance: create(MoneySchema, {
          currencyCode: "AUD",
          minorUnits: 9_000_000_000_000_000n,
        }),
      }),
    ),
  );
  const user = userEvent.setup();
  render(<BankingScreen api={testFixture.conveyor.banking} workspace={workspace} />);
  const balance = await screen.findByRole("region", {
    name: "Latest imported statement balance",
  });
  await within(balance).findByText("$90,000,000,000,000.00", { exact: true });
  balance.focus();
  expect(document.activeElement).toBe(balance);
  await user.tab();
  expect(document.activeElement).not.toBe(balance);
  expect(testFixture.procedures.banking.importBankStatement).not.toHaveBeenCalled();
  expect(testFixture.procedures.banking.matchBankStatementLine).not.toHaveBeenCalled();
  expect(testFixture.procedures.banking.completeBankReconciliation).not.toHaveBeenCalled();
});

it("requires an explicit reviewed reference and keeps matching distinct from reconciliation", async () => {
  const testFixture = fixture();
  const user = userEvent.setup();
  render(<BankingScreen api={testFixture.conveyor.banking} workspace={workspace} />);
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
  expect(testFixture.procedures.banking.matchBankStatementLine).not.toHaveBeenCalled();
  expect(screen.queryByRole("form", { name: "Match review" })).toBeNull();

  await user.click(screen.getByRole("button", { name: "Match" }));
  expect((screen.getByLabelText("Reviewed source reference") as HTMLInputElement).value).toBe("");
  await user.type(screen.getByLabelText("Reviewed source reference"), "Receipt INV-029847");
  await user.dblClick(screen.getByRole("button", { name: "Confirm match" }));
  expect(await within(table).findByText("Matched")).toBeTruthy();
  const matchedFrame = testFixture.procedures.banking.matchBankStatementLine.mock.calls[0]?.[0];
  if (!matchedFrame) throw new Error("match request missing");
  expect(matchCodec.decodeRequest(matchedFrame).matchReference).toBe("Receipt INV-029847");
  expect(complete.disabled).toBe(false);

  await user.dblClick(complete);
  expect(await within(table).findByText("Reconciled")).toBeTruthy();
  expect(testFixture.procedures.banking.matchBankStatementLine).toHaveBeenCalledOnce();
  expect(testFixture.procedures.banking.completeBankReconciliation).toHaveBeenCalledOnce();
});

it("retains a failed match draft and reuses its command key on an unchanged retry", async () => {
  const testFixture = fixture();
  testFixture.procedures.banking.matchBankStatementLine.mockRejectedValueOnce(
    new Error("response interrupted"),
  );
  const user = userEvent.setup();
  render(<BankingScreen api={testFixture.conveyor.banking} workspace={workspace} />);

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
  await waitFor(() =>
    expect(testFixture.procedures.banking.matchBankStatementLine).toHaveBeenCalledTimes(2),
  );
  const requests = testFixture.procedures.banking.matchBankStatementLine.mock.calls.map(([frame]) =>
    matchCodec.decodeRequest(frame),
  );
  expect(requests[0]?.commandContext?.idempotencyKey).toBe(
    requests[1]?.commandContext?.idempotencyKey,
  );
});

it("returns keyboard review to its row or retained search when Escape closes the panel", async () => {
  const testFixture = fixture();
  const user = userEvent.setup();
  render(<BankingScreen api={testFixture.conveyor.banking} workspace={workspace} />);
  const match = await screen.findByRole("button", { name: "Match" });
  await user.click(match);
  expect(match.getAttribute("aria-expanded")).toBe("true");
  expect(match.getAttribute("aria-controls")).toBe("bank-match-review");
  await user.type(screen.getByLabelText("Reviewed source reference"), "Unconfirmed reference");
  await user.click(match);
  expect((screen.getByLabelText("Reviewed source reference") as HTMLInputElement).value).toBe(
    "Unconfirmed reference",
  );
  fireEvent.keyDown(screen.getByLabelText("Reviewed source reference"), {
    key: "Escape",
    isComposing: true,
  });
  expect(screen.getByRole("form", { name: "Match review" })).toBeTruthy();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("form", { name: "Match review" })).toBeNull();
  expect(document.activeElement).toBe(match);
  expect(match.getAttribute("aria-expanded")).toBe("false");
  await user.click(match);
  expect((screen.getByLabelText("Reviewed source reference") as HTMLInputElement).value).toBe("");
  const search = screen.getByRole("searchbox", { name: "Search statement lines" });
  await user.type(search, "No statement row matches");
  expect(await screen.findByText("No matching statement lines")).toBeTruthy();
  screen.getByLabelText("Reviewed source reference").focus();
  await user.keyboard("{Escape}");
  expect(document.activeElement).toBe(search);
  expect((search as HTMLInputElement).value).toBe("No statement row matches");
  expect(testFixture.procedures.banking.matchBankStatementLine).not.toHaveBeenCalled();
});

it("announces and focuses a confirmed match after its original row action disappears", async () => {
  const testFixture = fixture();
  const user = userEvent.setup();
  render(<BankingScreen api={testFixture.conveyor.banking} workspace={workspace} />);
  await user.click(await screen.findByRole("button", { name: "Match" }));
  await user.type(screen.getByLabelText("Reviewed source reference"), "Reviewed synthetic receipt");
  await user.click(screen.getByRole("button", { name: "Confirm match" }));
  const confirmation = await screen.findByRole("region", { name: "Banking confirmation" });
  expect(document.activeElement).toBe(confirmation);
  expect(within(confirmation).getByRole("status").textContent).toContain(
    "no accounting entry or payment was created",
  );
  expect(screen.queryByRole("form", { name: "Match review" })).toBeNull();
  expect(
    (screen.getByRole("button", { name: "Complete reconciliation" }) as HTMLButtonElement).disabled,
  ).toBe(false);
  expect(testFixture.procedures.banking.matchBankStatementLine).toHaveBeenCalledOnce();
});

it("closes import by keyboard with retained rows while explicit cancel clears the draft", async () => {
  const testFixture = fixture();
  const user = userEvent.setup();
  render(<BankingScreen api={testFixture.conveyor.banking} workspace={workspace} />);
  const open = await screen.findByRole("button", { name: "Import statement" });
  await user.click(open);
  await user.type(screen.getByLabelText("Opening balance · AUD"), "1000.00");
  await user.type(
    screen.getByRole("textbox", { name: "CSV rows" }),
    "2024-05-12,Synthetic payment,10.00",
  );
  await user.keyboard("{Escape}");
  const returned = await screen.findByRole("button", { name: "Import statement" });
  await waitFor(() => expect(document.activeElement).toBe(returned));
  await user.click(returned);
  expect((screen.getByRole("textbox", { name: "CSV rows" }) as HTMLTextAreaElement).value).toBe(
    "2024-05-12,Synthetic payment,10.00",
  );
  await user.click(screen.getByRole("button", { name: "Cancel import" }));
  await user.click(await screen.findByRole("button", { name: "Import statement" }));
  expect((screen.getByRole("textbox", { name: "CSV rows" }) as HTMLTextAreaElement).value).toBe("");
  expect(testFixture.procedures.banking.importBankStatement).not.toHaveBeenCalled();
});

it("keeps a confirmed import separate from refresh failure and blocks another write", async () => {
  const testFixture = fixture();
  let accepted = false;
  testFixture.procedures.banking.importBankStatement.mockImplementation(async (frame) => {
    const request = importCodec.decodeRequest(frame);
    expect(request.lines[0]?.amount?.minorUnits).toBe(-11_000n);
    expect(request.openingBalance?.minorUnits).toBe(12_345_678_901n);
    accepted = true;
    return importCodec.encodeResponse(
      create(ImportBankStatementResponseSchema, {
        statementImport: create(BankStatementImportSchema, {
          id: "018f0000-0000-7000-8000-000000000302",
          organisationId: workspace.organisationId,
          lineCount: 1,
          openingBalance: request.openingBalance,
          closingBalance: create(MoneySchema, {
            currencyCode: "AUD",
            minorUnits: 12_345_667_901n,
          }),
        }),
      }),
    );
  });
  testFixture.procedures.banking.listBankStatementLines.mockImplementation(async () => {
    if (accepted) throw new Error("refresh interrupted");
    return listCodec.encodeResponse(
      create(ListBankStatementLinesResponseSchema, {
        lines: [],
        page: create(PageInfoSchema, { returnedCount: 0 }),
      }),
    );
  });
  const user = userEvent.setup();
  render(<BankingScreen api={testFixture.conveyor.banking} workspace={workspace} />);
  await screen.findByText("A calm start to reconciliation");
  await user.click(screen.getByRole("button", { name: "Import statement" }));
  const form = screen.getByRole("form", { name: "Statement import" });
  expect((screen.getByLabelText("Opening balance · AUD") as HTMLInputElement).value).toBe("");
  await user.type(screen.getByLabelText("Opening balance · AUD"), "123456789.01");
  await user.type(screen.getByLabelText("CSV rows"), "2024-05-12,Synthetic supplies,-110.00");
  await user.dblClick(within(form).getByRole("button", { name: "Import statement" }));

  expect(await screen.findByRole("status")).toHaveProperty(
    "textContent",
    expect.stringContaining("Statement imported"),
  );
  expect(await screen.findByRole("alert")).toHaveProperty(
    "textContent",
    expect.stringContaining("The action was saved"),
  );
  expect(screen.queryByRole("form", { name: "Statement import" })).toBeNull();
  expect(testFixture.procedures.banking.importBankStatement).toHaveBeenCalledOnce();
  expect(
    (screen.getByRole("button", { name: "Import statement" }) as HTMLButtonElement).disabled,
  ).toBe(true);
  expect(
    (screen.getByRole("button", { name: "Complete reconciliation" }) as HTMLButtonElement).disabled,
  ).toBe(true);
});
