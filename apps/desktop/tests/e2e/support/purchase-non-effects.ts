import { create } from "@bufbuild/protobuf";
import {
  BankStatementLineInputSchema,
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
  AuthenticationContextSchema,
  CommandContextSchema,
  MoneySchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  type PurchaseWorkspace,
  purchaseCommand,
  purchaseDate,
  purchaseProto,
} from "./purchase-payments-workflow";

const BANKING_SNAPSHOT_LIMIT = 200;

export interface PurchaseBankingSnapshot {
  readonly summary: {
    readonly importedLineCount: number;
    readonly unmatchedLineCount: number;
    readonly unreconciledLineCount: number;
    readonly latestClosingBalance: {
      readonly currencyCode: string;
      readonly minorUnits: string;
    } | null;
  };
  readonly lines: readonly {
    readonly id: string;
    readonly statementImportId: string;
    readonly organisationId: string;
    readonly version: string;
    readonly transactionDate: string;
    readonly description: string;
    readonly amount: {
      readonly currencyCode: string;
      readonly minorUnits: string;
    };
    readonly status: BankStatementLineStatus;
    readonly matchReference: string;
  }[];
}

function invariant(condition: boolean, code: string): asserts condition {
  if (!condition) throw new Error(code);
}

function isoDate(
  date: { readonly year: number; readonly month: number; readonly day: number } | undefined,
) {
  invariant(date !== undefined, "PURCHASE_BANKING_LINE_DATE_MISSING");
  return `${String(date.year).padStart(4, "0")}-${String(date.month).padStart(2, "0")}-${String(date.day).padStart(2, "0")}`;
}

export async function capturePurchaseBankingSnapshot(
  page: Parameters<typeof purchaseProto>[0],
  workspace: PurchaseWorkspace,
): Promise<PurchaseBankingSnapshot> {
  const authentication = create(AuthenticationContextSchema, {
    actorUserId: workspace.userId,
    sessionId: workspace.sessionId,
  });
  const summary = await purchaseProto(
    page,
    "banking",
    "getBankingSummary",
    GetBankingSummaryRequestSchema,
    GetBankingSummaryResponseSchema,
    create(GetBankingSummaryRequestSchema, {
      authentication,
      organisationId: workspace.organisationId,
    }),
  );
  invariant(
    summary.importedLineCount <= BANKING_SNAPSHOT_LIMIT,
    "PURCHASE_BANKING_SNAPSHOT_EXCEEDS_COMPLETE_LIMIT",
  );
  const listed = await purchaseProto(
    page,
    "banking",
    "listBankStatementLines",
    ListBankStatementLinesRequestSchema,
    ListBankStatementLinesResponseSchema,
    create(ListBankStatementLinesRequestSchema, {
      authentication,
      organisationId: workspace.organisationId,
      page: create(PageRequestSchema, { pageSize: BANKING_SNAPSHOT_LIMIT }),
    }),
  );
  invariant(
    listed.lines.length === summary.importedLineCount &&
      listed.page?.returnedCount === listed.lines.length &&
      !listed.page?.nextCursor,
    "PURCHASE_BANKING_SNAPSHOT_INCOMPLETE",
  );
  const lines = listed.lines.map((line) => {
    invariant(
      line.organisationId === workspace.organisationId && line.amount !== undefined,
      "PURCHASE_BANKING_LINE_SCOPE_OR_AMOUNT_INVALID",
    );
    return {
      id: line.id,
      statementImportId: line.statementImportId,
      organisationId: line.organisationId,
      version: line.version.toString(),
      transactionDate: isoDate(line.transactionDate),
      description: line.description,
      amount: {
        currencyCode: line.amount.currencyCode,
        minorUnits: line.amount.minorUnits.toString(),
      },
      status: line.status,
      matchReference: line.matchReference,
    };
  });
  return {
    summary: {
      importedLineCount: summary.importedLineCount,
      unmatchedLineCount: summary.unmatchedLineCount,
      unreconciledLineCount: summary.unreconciledLineCount,
      latestClosingBalance: summary.latestClosingBalance
        ? {
            currencyCode: summary.latestClosingBalance.currencyCode,
            minorUnits: summary.latestClosingBalance.minorUnits.toString(),
          }
        : null,
    },
    lines,
  };
}

export async function seedPurchaseBankingReconciliationBaseline(
  page: Parameters<typeof purchaseProto>[0],
  workspace: PurchaseWorkspace,
): Promise<PurchaseBankingSnapshot> {
  const imported = await purchaseProto(
    page,
    "banking",
    "importBankStatement",
    ImportBankStatementRequestSchema,
    ImportBankStatementResponseSchema,
    create(ImportBankStatementRequestSchema, {
      commandContext: create(CommandContextSchema, purchaseCommand(workspace)),
      organisationId: workspace.organisationId,
      openingBalance: create(MoneySchema, { currencyCode: "AUD", minorUnits: 5000n }),
      lines: [
        create(BankStatementLineInputSchema, {
          transactionDate: purchaseDate("2026-08-30"),
          description: "Purchase non-effects reconciliation baseline",
          amount: create(MoneySchema, { currencyCode: "AUD", minorUnits: 1234n }),
        }),
      ],
    }),
  );
  invariant(
    imported.statementImport?.organisationId === workspace.organisationId &&
      imported.statementImport.lineCount === 1 &&
      imported.statementImport.closingBalance?.minorUnits === 6234n,
    "PURCHASE_BANKING_BASELINE_IMPORT_INVALID",
  );

  const listed = await purchaseProto(
    page,
    "banking",
    "listBankStatementLines",
    ListBankStatementLinesRequestSchema,
    ListBankStatementLinesResponseSchema,
    create(ListBankStatementLinesRequestSchema, {
      authentication: create(AuthenticationContextSchema, {
        actorUserId: workspace.userId,
        sessionId: workspace.sessionId,
      }),
      organisationId: workspace.organisationId,
      page: create(PageRequestSchema, { pageSize: BANKING_SNAPSHOT_LIMIT }),
    }),
  );
  invariant(
    listed.lines.length === 1 && listed.page?.returnedCount === 1 && !listed.page.nextCursor,
    "PURCHASE_BANKING_BASELINE_LINE_MISSING",
  );
  const line = listed.lines[0];
  invariant(
    line !== undefined &&
      line.organisationId === workspace.organisationId &&
      line.statementImportId === imported.statementImport.id &&
      line.version === 1n &&
      line.status === BankStatementLineStatus.UNMATCHED,
    "PURCHASE_BANKING_BASELINE_LINE_INVALID",
  );
  const matched = await purchaseProto(
    page,
    "banking",
    "matchBankStatementLine",
    MatchBankStatementLineRequestSchema,
    MatchBankStatementLineResponseSchema,
    create(MatchBankStatementLineRequestSchema, {
      commandContext: create(CommandContextSchema, purchaseCommand(workspace)),
      lineId: line.id,
      expectedVersion: line.version,
      matchReference: "PURCHASE-ACCEPTANCE-BASELINE-RECONCILIATION",
    }),
  );
  invariant(
    matched.line?.id === line.id &&
      matched.line.version === 2n &&
      matched.line.status === BankStatementLineStatus.MATCHED,
    "PURCHASE_BANKING_BASELINE_MATCH_INVALID",
  );
  const completed = await purchaseProto(
    page,
    "banking",
    "completeBankReconciliation",
    CompleteBankReconciliationRequestSchema,
    CompleteBankReconciliationResponseSchema,
    create(CompleteBankReconciliationRequestSchema, {
      commandContext: create(CommandContextSchema, purchaseCommand(workspace)),
      organisationId: workspace.organisationId,
    }),
  );
  invariant(
    completed.reconciledLineCount === 1 && completed.closingBalance?.minorUnits === 6234n,
    "PURCHASE_BANKING_BASELINE_RECONCILIATION_INVALID",
  );
  const snapshot = await capturePurchaseBankingSnapshot(page, workspace);
  invariant(
    snapshot.summary.importedLineCount === 1 &&
      snapshot.summary.unmatchedLineCount === 0 &&
      snapshot.summary.unreconciledLineCount === 0 &&
      snapshot.summary.latestClosingBalance?.minorUnits === "6234" &&
      snapshot.lines[0]?.id === line.id &&
      snapshot.lines[0]?.version === "3" &&
      snapshot.lines[0]?.status === BankStatementLineStatus.RECONCILED &&
      snapshot.lines[0]?.matchReference === "PURCHASE-ACCEPTANCE-BASELINE-RECONCILIATION",
    "PURCHASE_BANKING_BASELINE_RECONCILIATION_NOT_RETAINED",
  );
  return snapshot;
}
