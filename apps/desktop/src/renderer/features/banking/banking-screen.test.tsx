import { create } from "@bufbuild/protobuf";
import {
  BankStatementLineSchema,
  BankStatementLineStatus,
  CompleteBankReconciliationRequestSchema,
  CompleteBankReconciliationResponseSchema,
  GetBankingSummaryRequestSchema,
  GetBankingSummaryResponseSchema,
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
import { render, screen } from "@testing-library/react";
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

it("keeps matching distinct from reconciliation and only enables completion after matching", async () => {
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
    importBankStatement: vi.fn(),
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
  const user = userEvent.setup();

  render(
    <BankingScreen
      api={api}
      workspace={{
        workspaceId: "018f0000-0000-7000-8000-000000000001",
        userId: "018f0000-0000-7000-8000-000000000002",
        sessionId: "018f0000-0000-7000-8000-000000000003",
        organisationId,
      }}
    />,
  );

  expect(await screen.findByText("Officeworks INV-029847")).toBeTruthy();
  expect(screen.getByText("Unmatched")).toBeTruthy();
  const complete = screen.getByRole("button", { name: "Complete reconciliation" });
  expect((complete as HTMLButtonElement).disabled).toBe(true);
  await user.click(screen.getByRole("button", { name: "Match" }));
  expect(await screen.findByText("Matched")).toBeTruthy();
  expect((complete as HTMLButtonElement).disabled).toBe(false);
  await user.click(complete);
  expect(await screen.findByText("Reconciled")).toBeTruthy();
  expect(api.matchBankStatementLine).toHaveBeenCalledOnce();
  expect(api.completeBankReconciliation).toHaveBeenCalledOnce();
});
