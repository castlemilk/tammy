import { createHash, randomBytes } from "node:crypto";
import { mkdir, readFile, rename, writeFile } from "node:fs/promises";
import path from "node:path";
import { create } from "@bufbuild/protobuf";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  AccountDesignation,
  AccountStatus,
  AccountType,
  CreateAccountRequestSchema,
  GetTrialBalanceRequestSchema,
  ListAccountsRequestSchema,
  ListJournalsRequestSchema,
  NormalBalance,
  PostManualJournalRequestSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  CompleteBankReconciliationRequestSchema,
  GetBankingSummaryRequestSchema,
  ImportBankStatementRequestSchema,
  ListBankStatementLinesRequestSchema,
  MatchBankStatementLineRequestSchema,
} from "@tammy/connect-client/tammy/v1/banking_pb.js";
import {
  AuthenticationContextSchema,
  CivilDateSchema,
  CommandContextSchema,
  MoneySchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  IngestDocumentRequestSchema,
  ListDocumentsRequestSchema,
  SaveDocumentReviewRequestSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  SignInRequestSchema,
  SignOutRequestSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  CreateOrganisationRequestSchema,
  GetOrganisationRequestSchema,
  GstBasis,
  GstReportingFrequency,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { GetAttentionSummaryRequestSchema } from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  CreateBasDraftRequestSchema,
  GetCurrentBasDraftRequestSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import {
  ConfirmRecoveryRequestSchema,
  CreateWorkspaceRequestSchema,
  LockWorkspaceRequestSchema,
  UnlockWorkspaceRequestSchema,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const bad = (
  message = "Check the supplied details and try again.",
  status = 400,
  code = "invalid_argument",
) => Object.assign(new Error(message), { status, code });
function required(value, max = 512) {
  if (typeof value !== "string" || !value.trim() || value.length > max) throw bad();
  return value;
}
function optionalText(value, max) {
  if (value === undefined || value === null) return "";
  if (typeof value !== "string" || value.length > max) throw bad();
  return value;
}
function id(value) {
  if (typeof value !== "string" || !UUID.test(value))
    throw bad("A valid operation or resource ID is required.");
  return value;
}
function version(value) {
  if (!(typeof value === "number" && Number.isSafeInteger(value)) && typeof value !== "string")
    throw bad();
  if (!/^[1-9][0-9]*$/.test(String(value))) throw bad();
  const n = BigInt(value);
  if (n > 18446744073709551615n) throw bad();
  return n;
}
function minor(value) {
  if (
    !(typeof value === "number" && Number.isSafeInteger(value)) &&
    !(typeof value === "string" && /^-?(0|[1-9][0-9]*)$/.test(value))
  )
    throw bad("Amounts must be whole cents.");
  const n = BigInt(value);
  if (n < -(2n ** 63n) || n > 2n ** 63n - 1n) throw bad("Amount is out of range.");
  return n;
}
function money(value) {
  return create(MoneySchema, { currencyCode: "AUD", minorUnits: minor(value) });
}
function outputMoney(value) {
  const n = value?.minorUnits ?? 0n;
  return n >= BigInt(Number.MIN_SAFE_INTEGER) && n <= BigInt(Number.MAX_SAFE_INTEGER)
    ? Number(n)
    : n.toString();
}
function date(value) {
  if (typeof value !== "string" || !/^\d{4}-\d{2}-\d{2}$/.test(value))
    throw bad("Use a valid date in YYYY-MM-DD format.");
  const [year, month, day] = value.split("-").map(Number);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  if (year < 1 || month < 1 || month > 12 || day < 1 || day > days[month - 1])
    throw bad("Use a valid date in YYYY-MM-DD format.");
  return create(CivilDateSchema, { year, month, day });
}
function dateOut(d) {
  return d
    ? `${String(d.year).padStart(4, "0")}-${String(d.month).padStart(2, "0")}-${String(d.day).padStart(2, "0")}`
    : null;
}
function today() {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: "Australia/Melbourne",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());
}
function uuid() {
  const b = randomBytes(16);
  let t = Date.now();
  for (let i = 5; i >= 0; i--) {
    b[i] = t & 255;
    t = Math.floor(t / 256);
  }
  b[6] = (b[6] & 15) | 0x70;
  b[8] = (b[8] & 63) | 0x80;
  return hexUuid(b);
}
function hexUuid(b) {
  const h = b.toString("hex");
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}
function derivedUuid(key, suffix) {
  const b = createHash("sha256").update(key).update(String(suffix)).digest().subarray(0, 16);
  Buffer.from(key.replaceAll("-", ""), "hex").copy(b, 0, 0, 6);
  b[6] = (b[6] & 15) | 0x70;
  b[8] = (b[8] & 63) | 0x80;
  return hexUuid(b);
}
function command(session, key) {
  return create(CommandContextSchema, {
    authentication: session.authentication,
    idempotencyKey: id(key),
  });
}
function secret(value) {
  return { utf8: new TextEncoder().encode(required(value, 1024)) };
}
async function core(fn) {
  try {
    return await fn();
  } catch (error) {
    if (error?.status) throw error;
    const failure = ConnectError.from(error);
    const code = failure.code;
    if (
      code === Code.PermissionDenied ||
      code === Code.Unauthenticated ||
      failure.rawMessage === "AUTHENTICATION_REQUIRED"
    )
      throw bad(
        "Your session is no longer valid. Unlock the workspace again.",
        401,
        "unauthenticated",
      );
    if (code === Code.NotFound)
      throw bad("The requested resource was not found.", 404, "not_found");
    if ([Code.FailedPrecondition, Code.AlreadyExists, Code.Aborted].includes(code))
      throw bad(
        "The request conflicts with the current state. Refresh and try again.",
        409,
        "conflict",
      );
    if (code === Code.InvalidArgument)
      throw bad(
        "The core rejected these details. Check them and try again.",
        400,
        "invalid_argument",
      );
    throw error;
  }
}
const page = { pageSize: 200 };
function pageInfo(count) {
  return { limit: 200, returnedCount: count, mayHaveMore: count === 200, cursorSupported: false };
}
const typeNames = {
  1: "asset",
  2: "liability",
  3: "equity",
  4: "income",
  5: "other_income",
  6: "expense",
  7: "other_expense",
  8: "contra",
};
const typeValues = {
  asset: AccountType.ASSET,
  liability: AccountType.LIABILITY,
  equity: AccountType.EQUITY,
  income: AccountType.REVENUE,
  expense: AccountType.EXPENSE,
};
function accountOut(a) {
  return {
    id: a.id,
    code: a.code,
    name: a.name,
    type: typeNames[a.type] ?? "unknown",
    normalBalance: a.normalBalance === NormalBalance.DEBIT ? "debit" : "credit",
    active: a.status === AccountStatus.ACTIVE,
    designation:
      a.designation === AccountDesignation.ORDINARY
        ? "ordinary"
        : a.designation === AccountDesignation.CONTROL
          ? "control"
          : "system",
  };
}
function journalOut(j, accounts = []) {
  const byId = new Map(accounts.map((a) => [a.id, a]));
  return {
    id: j.id,
    date: dateOut(j.postingDate),
    description: j.memo,
    reference: "",
    state: j.state === 2 ? "reversed" : "posted",
    lines: j.lines.map((l) => ({
      id: l.id,
      accountId: l.accountId,
      accountCode: byId.get(l.accountId)?.code ?? "",
      accountName: byId.get(l.accountId)?.name ?? "",
      description: l.description,
      debitMinor: outputMoney(l.debit),
      creditMinor: outputMoney(l.credit),
    })),
  };
}
function documentOut(d) {
  const c = d.candidate;
  return {
    id: d.id,
    version: d.version.toString(),
    sourceDisplayName: d.sourceDisplayName,
    status: d.status === 2 ? "reviewed" : "needs_review",
    supplierName: c?.supplierName ?? "",
    invoiceNumber: c?.invoiceNumber ?? "",
    date: dateOut(c?.documentDate),
    subtotalMinor: outputMoney(c?.subtotal),
    gstMinor: outputMoney(c?.gst),
    totalMinor: outputMoney(c?.total),
  };
}
function bankOut(l) {
  return {
    id: l.id,
    version: l.version.toString(),
    date: dateOut(l.transactionDate),
    description: l.description,
    amountMinor: outputMoney(l.amount),
    status: l.status === 3 ? "reconciled" : l.status === 2 ? "matched" : "unmatched",
  };
}
function basOut(w) {
  return {
    id: w.id,
    status: "draft_not_lodged",
    periodStart: dateOut(w.periodStart),
    periodEnd: dateOut(w.periodEnd),
    salesG1Minor: null,
    gstOnSales1aMinor: null,
    gstCredits1bMinor: outputMoney(w.gstCredits1b),
    netGstPayableMinor: null,
    limitations: [
      "Sales GST and net GST payable are not calculated by this reviewed-document workpaper.",
    ],
    sources: w.sources.map((s) => ({
      id: s.documentId,
      date: dateOut(s.documentDate),
      supplierName: s.supplierName,
      invoiceNumber: s.invoiceNumber,
      grossMinor: outputMoney(s.gross),
      gstCreditMinor: outputMoney(s.gstCredit),
    })),
  };
}
function docCandidate(body) {
  return {
    supplierName: optionalText(body.supplierName, 256),
    invoiceNumber: optionalText(body.invoiceNumber, 128),
    ...(body.date ? { documentDate: date(body.date) } : {}),
    subtotal: money(body.subtotalMinor ?? 0),
    gst: money(body.gstMinor ?? 0),
    total: money(body.totalMinor ?? 0),
  };
}

export async function createFacade({
  client,
  workspace,
  identity,
  organisation,
  dataRoot,
  mcpEnabled = false,
}) {
  await mkdir(dataRoot, { recursive: true, mode: 0o700 });
  const metadataPath = path.join(dataRoot, "web-workspace.json");
  let metadata;
  try {
    metadata = JSON.parse(await readFile(metadataPath, "utf8"));
    if (
      !metadata ||
      !UUID.test(metadata.workspaceId) ||
      !UUID.test(metadata.setupId) ||
      !["pending", "confirmed", "ready"].includes(metadata.stage) ||
      (metadata.stage === "ready" && !UUID.test(metadata.organisationId))
    )
      throw new Error("invalid metadata");
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
  async function save(value) {
    const temp = `${metadataPath}.tmp`;
    await writeFile(temp, JSON.stringify(value), { mode: 0o600 });
    await rename(temp, metadataPath);
    metadata = value;
  }
  let busy = false;
  let openedHere = false;
  async function exclusive(fn) {
    if (busy) throw bad("A workspace operation is already in progress.", 409);
    busy = true;
    try {
      return await fn();
    } finally {
      busy = false;
    }
  }
  async function signIn(username, password) {
    const result = await core(() =>
      identity.signIn(
        create(SignInRequestSchema, {
          username: required(username, 128),
          password: secret(password),
        }),
      ),
    );
    if (!result.user || !result.session) throw new Error("invalid sign in");
    return create(AuthenticationContextSchema, {
      actorUserId: result.user.id,
      sessionId: result.session.id,
    });
  }
  async function unlockWorkspace(passphrase) {
    await core(() =>
      workspace.unlockWorkspace(
        create(UnlockWorkspaceRequestSchema, {
          workspaceFile: { capabilityId: "local-workspace-file" },
          proof: { proof: { case: "passphrase", value: secret(passphrase) } },
        }),
      ),
    );
    openedHere = true;
  }
  async function authenticated(authentication) {
    const result = await core(() =>
      organisation.getOrganisation(
        create(GetOrganisationRequestSchema, {
          authentication,
          organisationId: metadata.organisationId,
        }),
      ),
    );
    if (!result.organisation) throw new Error("organisation missing");
    return {
      authentication,
      organisation: { id: result.organisation.id, name: result.organisation.displayName },
    };
  }
  const facade = {
    state: () =>
      !metadata
        ? { state: "setup" }
        : metadata.stage === "ready"
          ? { state: "locked" }
          : { state: "recovery", setupId: metadata.setupId },
    create: (body) =>
      exclusive(async () => {
        if (metadata) throw bad("A workspace already exists.", 409);
        const setupId = uuid();
        const result = await core(() =>
          client.createWorkspace(
            create(CreateWorkspaceRequestSchema, {
              setupId,
              destination: { capabilityId: "local-workspace-directory" },
              workspacePassphrase: secret(body.workspacePassphrase),
              administratorUsername: required(body.username, 128),
              administratorDisplayName: required(body.displayName, 128),
              administratorPassword: secret(body.administratorPassword),
            }),
          ),
        );
        if (!result.workspace?.id || !result.recoverySecret?.utf8?.length)
          throw new Error("workspace setup failed");
        await save({ workspaceId: result.workspace.id, setupId, stage: "pending" });
        openedHere = true;
        return { setupId, recoveryCode: new TextDecoder().decode(result.recoverySecret.utf8) };
      }),
    confirm: (body) =>
      exclusive(async () => {
        if (!metadata || metadata.stage === "ready" || id(body.setupId) !== metadata.setupId)
          throw bad("No matching setup is pending.", 409);
        const code = required(body.recoveryCode, 256)
          .replaceAll("-", "")
          .replaceAll(" ", "")
          .toUpperCase();
        const groups = code.match(/.{1,4}/g);
        if (!groups || groups.length < 2) throw bad("Enter the saved recovery code.");
        if (metadata.stage === "confirmed" && !openedHere)
          await unlockWorkspace(body.workspacePassphrase);
        if (metadata.stage === "pending") {
          await core(() =>
            workspace.confirmRecovery(
              create(ConfirmRecoveryRequestSchema, {
                setupId: metadata.setupId,
                confirmations: groups.slice(0, 2).map((value, groupIndex) => ({
                  groupIndex,
                  value: new TextEncoder().encode(value),
                })),
                ...(body.workspacePassphrase
                  ? {
                      terminalReplayProof: {
                        proof: { case: "passphrase", value: secret(body.workspacePassphrase) },
                      },
                    }
                  : {}),
              }),
            ),
          );
          await save({ ...metadata, stage: "confirmed" });
          openedHere = true;
        }
        const authentication = await signIn(body.username, body.administratorPassword);
        const result = await core(() =>
          client.createOrganisation(
            create(CreateOrganisationRequestSchema, {
              commandContext: {
                authentication,
                idempotencyKey: derivedUuid(metadata.setupId, "organisation"),
              },
              abn: required(body.abn, 11),
              legalName: required(body.businessLegalName, 160),
              displayName: required(body.businessDisplayName, 160),
              entityType: "AU_PRIVATE_COMPANY",
              gstBasis: GstBasis.NON_CASH,
              gstReportingFrequency: GstReportingFrequency.QUARTERLY,
              financialYearEndMonth: 6,
              activeTaxRuleBundle: {
                type: "tax_rule_bundle",
                id: "018f0000-0000-7000-8000-000000000022",
                revision: 1n,
                contentHash: Buffer.from(
                  "e2f9cde094db43c30260dc54f089a1ab835912e75e8c29add5c7a240e90497e4",
                  "hex",
                ),
              },
            }),
          ),
        );
        if (!result.organisation?.id) throw new Error("organisation missing");
        await save({ ...metadata, stage: "ready", organisationId: result.organisation.id });
        return {
          authentication,
          organisation: { id: result.organisation.id, name: result.organisation.displayName },
        };
      }),
    unlock: (body) =>
      exclusive(async () => {
        if (metadata?.stage !== "ready") throw bad("No completed workspace is available.", 409);
        await unlockWorkspace(body.workspacePassphrase);
        try {
          const authentication = await signIn(body.username, body.administratorPassword);
          return await authenticated(authentication);
        } catch (error) {
          await workspace.lockWorkspace(create(LockWorkspaceRequestSchema, {})).catch(() => {});
          openedHere = false;
          throw error;
        }
      }),
    logout: async (session) => {
      try {
        await identity.signOut(
          create(SignOutRequestSchema, { authentication: session.authentication }),
        );
      } finally {
        await workspace.lockWorkspace(create(LockWorkspaceRequestSchema, {})).catch(() => {});
        openedHere = false;
      }
    },
    accounts: async (session) => {
      const result = await core(() =>
        client.listAccounts(
          create(ListAccountsRequestSchema, {
            authentication: session.authentication,
            organisationId: session.organisation.id,
            page,
          }),
        ),
      );
      return { accounts: result.accounts.map(accountOut), page: pageInfo(result.accounts.length) };
    },
    createAccount: async (session, body) => {
      const type = typeValues[body.type];
      if (!type) throw bad("Choose an account type.");
      const result = await core(() =>
        client.createAccount(
          create(CreateAccountRequestSchema, {
            commandContext: command(session, body.idempotencyKey),
            organisationId: session.organisation.id,
            code: required(body.code, 32),
            name: required(body.name, 160),
            type,
            normalBalance: [AccountType.ASSET, AccountType.EXPENSE].includes(type)
              ? NormalBalance.DEBIT
              : NormalBalance.CREDIT,
            reportClassification: "custom.account",
            cashFlowClassification: "noncash",
          }),
        ),
      );
      return { account: accountOut(result.account) };
    },
    journals: async (session) => {
      const [result, chart] = await Promise.all([
        core(() =>
          client.listJournals(
            create(ListJournalsRequestSchema, {
              authentication: session.authentication,
              organisationId: session.organisation.id,
              page,
            }),
          ),
        ),
        facade.accounts(session),
      ]);
      return {
        journals: result.journals.map((j) => journalOut(j, chart.accounts)),
        page: pageInfo(result.journals.length),
      };
    },
    postJournal: async (session, body) => {
      const key = id(body.idempotencyKey);
      if (!Array.isArray(body.lines) || body.lines.length < 2 || body.lines.length > 1000)
        throw bad("Enter at least two journal lines.");
      const lines = body.lines.map((l, i) => ({
        clientLineId: derivedUuid(key, i),
        accountId: id(l.accountId),
        description: optionalText(l.description, 512),
        debit: money(l.debitMinor),
        credit: money(l.creditMinor),
      }));
      const result = await core(() =>
        client.postManualJournal(
          create(PostManualJournalRequestSchema, {
            commandContext: command(session, key),
            organisationId: session.organisation.id,
            postingDate: date(body.date),
            memo: optionalText(body.description, 512),
            lines,
          }),
        ),
      );
      const chart = await facade.accounts(session);
      return { journal: journalOut(result.journal, chart.accounts) };
    },
    trialBalance: async (session, query) => {
      const asOfDate = query?.get("asOfDate") || today();
      const result = await core(() =>
        client.getTrialBalance(
          create(GetTrialBalanceRequestSchema, {
            authentication: session.authentication,
            organisationId: session.organisation.id,
            asOfDate: date(asOfDate),
          }),
        ),
      );
      return {
        asOfDate,
        lines: result.lines.map((l) => ({
          accountId: l.account?.id ?? "",
          accountCode: l.code,
          accountName: l.name,
          debitMinor: outputMoney(l.debits),
          creditMinor: outputMoney(l.credits),
        })),
        totalDebitsMinor: outputMoney(result.totalDebits),
        totalCreditsMinor: outputMoney(result.totalCredits),
      };
    },
    documents: async (session) => {
      const result = await core(() =>
        client.listDocuments(
          create(ListDocumentsRequestSchema, {
            authentication: session.authentication,
            organisationId: session.organisation.id,
            page,
          }),
        ),
      );
      return {
        documents: result.documents.map(documentOut),
        page: pageInfo(result.documents.length),
      };
    },
    uploadDocument: async (session, body) => {
      const encoded = required(body.originalBase64, 700000);
      const original = Buffer.from(encoded, "base64");
      if (
        !original.length ||
        original.length > 512 * 1024 ||
        original.toString("base64") !== encoded
      )
        throw bad("Choose a PDF, PNG or JPEG no larger than 512 KiB.");
      const mime = body.mimeType;
      const valid =
        (mime === "application/pdf" && original.subarray(0, 5).toString() === "%PDF-") ||
        (mime === "image/png" &&
          original.subarray(0, 8).equals(Buffer.from("89504e470d0a1a0a", "hex"))) ||
        (mime === "image/jpeg" && original.subarray(0, 3).equals(Buffer.from("ffd8ff", "hex")));
      if (!valid) throw bad("The file does not match its PDF, PNG or JPEG type.");
      const extractedText = optionalText(body.extractedText, 32768);
      if (Buffer.byteLength(extractedText) > 32768) throw bad("Extracted text is too large.");
      const result = await core(() =>
        client.ingestDocument(
          create(IngestDocumentRequestSchema, {
            commandContext: command(session, body.idempotencyKey),
            organisationId: session.organisation.id,
            sourceDisplayName: required(body.sourceDisplayName, 255),
            mimeType: mime,
            original,
            extractedText,
            candidate: docCandidate(body.candidate ?? {}),
          }),
        ),
      );
      return { document: documentOut(result.document) };
    },
    reviewDocument: async (session, documentId, body) => {
      const result = await core(() =>
        client.saveDocumentReview(
          create(SaveDocumentReviewRequestSchema, {
            commandContext: command(session, body.idempotencyKey),
            documentId: id(documentId),
            expectedVersion: version(body.version),
            candidate: docCandidate(body),
          }),
        ),
      );
      return { document: documentOut(result.document) };
    },
    banking: async (session) => {
      const [result, summary] = await Promise.all([
        core(() =>
          client.listBankStatementLines(
            create(ListBankStatementLinesRequestSchema, {
              authentication: session.authentication,
              organisationId: session.organisation.id,
              page,
            }),
          ),
        ),
        core(() =>
          client.getBankingSummary(
            create(GetBankingSummaryRequestSchema, {
              authentication: session.authentication,
              organisationId: session.organisation.id,
            }),
          ),
        ),
      ]);
      return {
        lines: result.lines.map(bankOut),
        page: pageInfo(result.lines.length),
        summary: {
          importedLineCount: summary.importedLineCount,
          unmatchedLineCount: summary.unmatchedLineCount,
          unreconciledLineCount: summary.unreconciledLineCount,
          latestClosingBalanceMinor: outputMoney(summary.latestClosingBalance),
        },
      };
    },
    importBank: async (session, body) => {
      if (!Array.isArray(body.lines) || body.lines.length < 1 || body.lines.length > 1000)
        throw bad();
      const result = await core(() =>
        client.importBankStatement(
          create(ImportBankStatementRequestSchema, {
            commandContext: command(session, body.idempotencyKey),
            organisationId: session.organisation.id,
            openingBalance: money(body.openingBalanceMinor),
            lines: body.lines.map((l) => ({
              transactionDate: date(l.date),
              description: required(l.description, 256),
              amount: money(l.amountMinor),
            })),
          }),
        ),
      );
      return { id: result.statementImport?.id };
    },
    matchBank: async (session, lineId, body) => {
      const result = await core(() =>
        client.matchBankStatementLine(
          create(MatchBankStatementLineRequestSchema, {
            commandContext: command(session, body.idempotencyKey),
            lineId: id(lineId),
            expectedVersion: version(body.version),
            matchReference: required(body.reference, 128),
          }),
        ),
      );
      return { line: bankOut(result.line) };
    },
    reconcile: async (session, body) => {
      const result = await core(() =>
        client.completeBankReconciliation(
          create(CompleteBankReconciliationRequestSchema, {
            commandContext: command(session, body.idempotencyKey),
            organisationId: session.organisation.id,
          }),
        ),
      );
      return {
        reconciledLineCount: result.reconciledLineCount,
        closingBalanceMinor: outputMoney(result.closingBalance),
      };
    },
    bas: async (session) => {
      try {
        const result = await client.getCurrentBasDraft(
          create(GetCurrentBasDraftRequestSchema, {
            authentication: session.authentication,
            organisationId: session.organisation.id,
          }),
        );
        return { workpaper: result.workpaper ? basOut(result.workpaper) : null };
      } catch (error) {
        if (ConnectError.from(error).code === Code.NotFound) return { workpaper: null };
        return core(() => Promise.reject(error));
      }
    },
    createBas: async (session, body) => {
      const start = date(body.periodStart),
        end = date(body.periodEnd);
      if (start.year !== 2024 || end.year !== 2024 || body.periodStart > body.periodEnd)
        throw bad("Only the 2024 reviewed-document GST workpaper is supported.");
      const result = await core(() =>
        client.createBasDraft(
          create(CreateBasDraftRequestSchema, {
            commandContext: command(session, body.idempotencyKey),
            organisationId: session.organisation.id,
            periodStart: start,
            periodEnd: end,
          }),
        ),
      );
      return { workpaper: basOut(result.workpaper) };
    },
    overview: async (session) => {
      const day = today();
      const year = day.slice(0, 4);
      const result = await core(() =>
        client.getAttentionSummary(
          create(GetAttentionSummaryRequestSchema, {
            authentication: session.authentication,
            organisationId: session.organisation.id,
            asOfDate: date(day),
            reportingPeriod: { startDate: date(`${year}-01-01`), endDate: date(`${year}-12-31`) },
          }),
        ),
      );
      return {
        documentsNeedingReview: result.documentsNeedingReview,
        documentsReviewedInPeriod: result.documentsReviewedInPeriod,
        bankingLinesNeedingReconciliation: result.bankingLinesNeedingReconciliation,
        bankingLinesUnreconciledInPeriod: result.bankingLinesUnreconciledInPeriod,
        currentDraftBasWorkpapers: result.currentDraftBasWorkpapers,
        basStatus:
          result.basStatus === 2
            ? "draft_not_lodged"
            : result.basStatus === 3
              ? "outdated"
              : "not_created",
        attentionItems: result.attentionItems.map((item) => ({
          kind: item.kind === 1 ? "documents" : item.kind === 2 ? "banking" : "bas",
          label: item.label,
          resourceId: item.resource?.id,
        })),
      };
    },
    insights: async (session) => {
      const snapshot = await facade.overview(session);
      const insights = [];
      if (snapshot.documentsNeedingReview)
        insights.push({
          id: "documents",
          kind: "documents",
          title: "Documents need review",
          detail: `${snapshot.documentsNeedingReview} retained document(s) need review.`,
          count: snapshot.documentsNeedingReview,
        });
      if (snapshot.bankingLinesNeedingReconciliation)
        insights.push({
          id: "banking",
          kind: "banking",
          title: "Bank lines need attention",
          detail: `${snapshot.bankingLinesNeedingReconciliation} statement line(s) are unmatched.`,
          count: snapshot.bankingLinesNeedingReconciliation,
        });
      if (snapshot.currentDraftBasWorkpapers)
        insights.push({
          id: "bas",
          kind: "bas",
          title: "GST workpaper is a draft",
          detail: "This local workpaper has not been lodged.",
          count: snapshot.currentDraftBasWorkpapers,
        });
      return { insights };
    },
    capabilities: async () => ({
      api: {
        available: true,
        scope: "local",
        description: "Allowlisted local API backed by the accounting core.",
      },
      mcp: {
        available: mcpEnabled,
        transport: "stdio",
        access: "read-only",
        description: mcpEnabled
          ? "Read-only tools use an explicit local token and the active workspace."
          : "Set a local MCP token to enable read-only tools.",
      },
      intelligence: {
        available: true,
        description:
          "Rule-based attention derived from the current core snapshot; no AI predictions.",
      },
      reporting: {
        year: 2024,
        description:
          "Reviewed-document GST workpaper only. Full BAS and lodgement are unavailable.",
      },
    }),
  };
  return facade;
}
