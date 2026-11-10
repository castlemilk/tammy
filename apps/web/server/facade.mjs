import { createHash, randomBytes } from "node:crypto";
import { lstat, mkdir, readFile, rename, rm, writeFile } from "node:fs/promises";
import path from "node:path";
import { create } from "@bufbuild/protobuf";
import { timestampDate } from "@bufbuild/protobuf/wkt";
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
import {
  BasAttentionStatus,
  GetAttentionSummaryRequestSchema,
} from "@tammy/connect-client/tammy/v1/overview_pb.js";
import {
  CreateBasDraftRequestSchema,
  GetCurrentBasDraftRequestSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import {
  ConfirmRecoveryRequestSchema,
  CreateWorkspaceRequestSchema,
  GetWorkspaceStateRequestSchema,
  LockWorkspaceRequestSchema,
  UnlockWorkspaceRequestSchema,
  WorkspaceState,
} from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { createBundledTaxRuleSourceRef } from "../../desktop/src/shared/bundled-tax-rule.ts";
import { httpError } from "./errors.mjs";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const ISO_INSTANT = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z$/;
// The core discards a pending setup on the fifth failed recovery confirmation.
const SETUP_ATTEMPTS = 5;
// As in the desktop core client: creating a workspace derives keys and needs more than 5 seconds.
const WORKSPACE_CREATE_TIMEOUT_MS = 30_000;
// The core rate-limits unlock and sign-in at 5 failures, then refuses every attempt for 15 minutes.
const CREDENTIALS_REJECTED =
  "The workspace passphrase, username or administrator password is incorrect. After 5 failed attempts, wait 15 minutes before trying again.";
const PASSPHRASE_REJECTED =
  "The workspace passphrase is incorrect. After 5 failed attempts, wait 15 minutes before trying again.";
const SIGN_IN_REJECTED =
  "The username or administrator password is incorrect. After 5 failed attempts, wait 15 minutes before trying again.";
const PASSPHRASE_RULE =
  "The workspace passphrase must have 15 to 128 characters: letters, numbers, punctuation, symbols or spaces.";
const PASSWORD_RULE =
  "The administrator password must have 15 to 128 characters: letters, numbers, punctuation, symbols or spaces.";
// The gateway checks the secrets and the data folder first, so a remaining validation fault is a
// core state the request cannot fix.
const SETUP_REFUSED = "The core refused to create the workspace. Restart Tammy and try again.";
const DATA_FOLDER_IN_USE =
  "This data folder already holds a workspace that this web server has no record of. Stop the server, move the folder aside or set TAMMY_WEB_DATA_ROOT to a new folder, then start again.";
// A failed SignIn counts toward the core's limit of 5 in 15 minutes, which locks the administrator
// account and stops its live sessions working too. A takeover may use only part of that budget, so
// guessing cannot end the signed-in browser's session.
const TAKEOVER_ATTEMPTS = 3;
const SIGN_IN_WINDOW_MS = 15 * 60 * 1000;
const TAKEOVER_LIMITED = "Too many failed attempts. Wait 15 minutes before trying again.";
// The core's v1 secret rule (services/core/internal/workspace/keys.go): at most 1024 bytes of UTF-8,
// then 15 to 128 characters after NFC normalisation, each printable as Go defines it (letters,
// marks, numbers, punctuation, symbols and the ASCII space). The core checks the administrator
// password only after it has reserved the setup's files, so checking it here first keeps a rejected
// password from leaving a workspace file behind.
const PRINTABLE = /^[\p{L}\p{M}\p{N}\p{P}\p{S} ]*$/u;
function acceptedSecret(value) {
  if (typeof value !== "string" || Buffer.byteLength(value) > 1024) return false;
  const normalized = value.normalize("NFC");
  const count = [...normalized].length;
  return (
    count >= 15 &&
    count <= 128 &&
    Buffer.byteLength(normalized) <= 1024 &&
    PRINTABLE.test(normalized)
  );
}
// The code defaults to the documented code for the status, so a 409 always carries `conflict`.
const bad = (
  message = "Check the supplied details and try again.",
  status = 400,
  code = undefined,
) => httpError(message, status, code);
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
/**
 * The workspace and identity services return bare core faults, which reach Connect clients as
 * Code.Unknown with the fault name, such as AUTHENTICATION_REQUIRED, as the message.
 */
function coreFault(error) {
  return error?.status ? undefined : ConnectError.from(error).rawMessage;
}
/**
 * Map a core failure to a client-safe HTTP error. Calls that carry credentials or new secrets pass
 * `credentials` and `validation` messages, so a typing mistake is not reported as an expired session
 * or an internal failure.
 */
async function core(fn, { credentials, validation } = {}) {
  try {
    return await fn();
  } catch (error) {
    if (error?.status) throw error;
    const failure = ConnectError.from(error);
    const code = failure.code;
    if (credentials && failure.rawMessage === "AUTHENTICATION_REQUIRED")
      throw bad(credentials, 401, "unauthenticated");
    if (validation && failure.rawMessage === "VALIDATION")
      throw bad(validation, 400, "invalid_argument");
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
// Request input selects the type, so a Map keeps inherited names such as `constructor` out.
const typeValues = new Map([
  ["asset", AccountType.ASSET],
  ["liability", AccountType.LIABILITY],
  ["equity", AccountType.EQUITY],
  ["income", AccountType.REVENUE],
  ["expense", AccountType.EXPENSE],
]);
// Every BasAttentionStatus, named as in the proto. The current core's overview reports BAS
// preparation for the requested period: not_created, or action_needed through disputed. It no
// longer emits draft_not_lodged or outdated, which describe legacy GST drafts.
const basStatuses = new Map([
  [BasAttentionStatus.NOT_CREATED, "not_created"],
  [BasAttentionStatus.DRAFT_NOT_LODGED, "draft_not_lodged"],
  [BasAttentionStatus.OUTDATED, "outdated"],
  [BasAttentionStatus.ACTION_NEEDED, "action_needed"],
  [BasAttentionStatus.REVIEW_READY, "review_ready"],
  [BasAttentionStatus.FROZEN_FOR_MANUAL_HANDOFF, "frozen_for_manual_handoff"],
  [BasAttentionStatus.EXTERNAL_CONFIRMATION_RECORDED, "external_confirmation_recorded"],
  [BasAttentionStatus.DISPUTED, "disputed"],
]);
// Setups recorded before expiresAt and failedAttempts were kept have neither, and still load.
function validPendingFields(value) {
  return (
    (value.expiresAt === undefined ||
      (typeof value.expiresAt === "string" &&
        ISO_INSTANT.test(value.expiresAt) &&
        Number.isFinite(Date.parse(value.expiresAt)))) &&
    (value.failedAttempts === undefined ||
      (Number.isSafeInteger(value.failedAttempts) && value.failedAttempts >= 0))
  );
}
const isOpen = (state) =>
  state === WorkspaceState.UNAUTHENTICATED || state === WorkspaceState.AUTHENTICATED;
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
function legacyDocument(review) {
  const source = review.legacySource?.source;
  return source?.case === "document" ? source.value : undefined;
}
// The core's reconciled purchase format counts only posted purchases in GST credits (1B). A reviewed
// document that was never posted as a purchase is a separate provisional candidate, never a credit.
// The core lists every such document whatever its date, including undated ones, and
// legacyCandidateGst totals them. The web keeps those semantics, as the desktop does.
function basOut(w, documents = new Map()) {
  const reconciled = w.purchaseSourceFormat !== undefined;
  return {
    id: w.id,
    status: "draft_not_lodged",
    periodStart: dateOut(w.periodStart),
    periodEnd: dateOut(w.periodEnd),
    salesG1Minor: null,
    gstOnSales1aMinor: null,
    gstCredits1bMinor: outputMoney(w.gstCredits1b),
    netGstPayableMinor: null,
    candidateGstMinor: reconciled ? outputMoney(w.legacyCandidateGst) : null,
    // The core recomputes these on every read of the current draft.
    sourcesChanged: w.sourcesChanged,
    reviewWarnings: [...w.reviewWarnings],
    limitations: [
      "Sales (G1), GST on sales (1A) and net GST are calculated by the core but not shown in this web release.",
      ...(reconciled
        ? [
            "Provisional GST candidates are reviewed documents not yet posted as purchases, whatever their date. Their GST is not a claimed credit and entitlement remains subject to review. GST credits (1B) include only posted purchases, which this web release cannot create.",
          ]
        : []),
    ],
    sources: [
      ...w.sources.map((s) => ({
        id: s.documentId,
        date: dateOut(s.documentDate),
        supplierName: s.supplierName,
        invoiceNumber: s.invoiceNumber,
        grossMinor: outputMoney(s.gross),
        gstCreditMinor: outputMoney(s.gstCredit),
      })),
      ...w.purchaseSources.map((s) => ({
        id: s.documentId,
        date: dateOut(s.invoiceDate),
        supplierName: s.supplierName ?? "",
        invoiceNumber: s.invoiceNumber ?? "",
        grossMinor: outputMoney(s.allocatedGross),
        gstCreditMinor: outputMoney(s.claimedGst),
      })),
    ],
    candidates: w.purchaseReviews.flatMap((review) => {
      const reference = legacyDocument(review);
      if (!reference) return [];
      // Describe a candidate only from the reviewed document version that the workpaper retained.
      const document = documents.get(reference.documentId);
      const retained =
        document?.version === reference.reviewedVersion.toString() ? document : undefined;
      return [
        {
          id: reference.documentId,
          date: dateOut(review.invoiceDate),
          supplierName: retained?.supplierName ?? "",
          invoiceNumber: retained?.invoiceNumber ?? "",
          candidateGstMinor: outputMoney(review.unclaimedGst),
        },
      ];
    }),
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
  now: clock = Date.now,
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
      (metadata.stage === "ready" && !UUID.test(metadata.organisationId)) ||
      !validPendingFields(metadata)
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
  // The core session that the open workspace belongs to. Only its logout locks the workspace.
  let owner;
  let takeoverFailures = [];
  // Setup, unlock and logout run one at a time. A second setup or unlock request is refused while
  // another runs; a logout waits its turn, so it cannot lock a workspace that is being unlocked.
  let tail = Promise.resolve();
  function queued(fn) {
    const run = tail.then(() => fn());
    tail = run.catch(() => {});
    return run;
  }
  function exclusive(fn) {
    if (busy) return Promise.reject(bad("A workspace operation is already in progress.", 409));
    busy = true;
    return queued(fn).finally(() => {
      busy = false;
    });
  }
  async function lock() {
    owner = undefined;
    openedHere = false;
    await workspace.lockWorkspace(create(LockWorkspaceRequestSchema, {})).catch(() => {});
  }
  // The core keeps one workspace file in its data root and refuses to create over it.
  const workspaceFile = path.join(dataRoot, "core", "workspace", "tammy-workspace.db");
  function workspaceFileExists() {
    return lstat(workspaceFile).then(
      () => true,
      (error) => {
        if (error.code === "ENOENT") return false;
        throw error;
      },
    );
  }
  async function signIn(username, password, credentials = CREDENTIALS_REJECTED) {
    const result = await core(
      () =>
        identity.signIn(
          create(SignInRequestSchema, {
            username: required(username, 128),
            password: secret(password),
          }),
        ),
      { credentials },
    );
    if (!result.user || !result.session) throw new Error("invalid sign in");
    return create(AuthenticationContextSchema, {
      actorUserId: result.user.id,
      sessionId: result.session.id,
    });
  }
  async function unlockWorkspace(passphrase, credentials = CREDENTIALS_REJECTED) {
    if (typeof passphrase !== "string" || !passphrase) throw bad("Enter the workspace passphrase.");
    await core(
      () =>
        workspace.unlockWorkspace(
          create(UnlockWorkspaceRequestSchema, {
            workspaceFile: { capabilityId: "local-workspace-file" },
            proof: { proof: { case: "passphrase", value: secret(passphrase) } },
          }),
        ),
      { credentials },
    );
    openedHere = true;
  }
  // The catalogue state of the setup's workspace, or undefined when the core cannot report it.
  async function coreWorkspaceState() {
    try {
      const result = await workspace.getWorkspaceState(
        create(GetWorkspaceStateRequestSchema, { workspaceId: metadata.workspaceId }),
      );
      return result.workspace?.state;
    } catch {
      return undefined;
    }
  }
  /**
   * Forget a pending setup the web gives up on, after making sure the core has discarded it too.
   * The core has no cancel RPC. It discards a pending setup, closing it and deleting its files, when
   * a correct code arrives after the deadline or on the fifth failed confirmation inside its
   * 15-minute attempt window. But an incorrect code after the deadline is only counted, and the
   * core's attempt journal restarts empty with the web server (--development-memory-anchors) while
   * the web's count persists. Left behind, the old files would make the next setup fail, so the web
   * sends deliberately invalid confirmations until the core has discarded the setup. "0000" is
   * outside the Base32 recovery alphabet, so it can never match a recovery group.
   */
  async function discardPendingSetup() {
    const invalid = create(ConfirmRecoveryRequestSchema, {
      setupId: metadata.setupId,
      confirmations: [0, 1].map((groupIndex) => ({
        groupIndex,
        value: new TextEncoder().encode("0000"),
      })),
    });
    for (let attempt = 0; attempt < SETUP_ATTEMPTS; attempt++) {
      const rejected = await workspace.confirmRecovery(invalid).then(
        () => false,
        (error) => {
          if (coreFault(error) === "AUTHENTICATION_REQUIRED") return true;
          throw error;
        },
      );
      if (!rejected) throw new Error("an abandoned setup was confirmed");
    }
    // The core deletes a discarded setup's files before it answers. Some rejections do not count
    // toward that discard, so keep the setup record until the file is really gone.
    if (await workspaceFileExists())
      throw bad("The core has not discarded this setup yet. Restart Tammy, then try again.", 409);
    await rm(metadataPath, { force: true });
    metadata = undefined;
    openedHere = false;
  }
  /**
   * Another browser has the workspace open. SignIn checks the administrator credentials without
   * disturbing that browser unless they are correct, and a successful SignIn ends every other core
   * session itself. Locking afterwards keeps the workspace passphrase a requirement for the new
   * browser.
   */
  async function takeOver(username, password) {
    const now = clock();
    takeoverFailures = takeoverFailures.filter((at) => now - at < SIGN_IN_WINDOW_MS);
    if (takeoverFailures.length >= TAKEOVER_ATTEMPTS) throw bad(TAKEOVER_LIMITED, 429);
    try {
      await signIn(username, password);
    } catch (error) {
      if (error?.status === 401) takeoverFailures.push(now);
      throw error;
    }
    // As in the core, a successful sign-in clears the failures.
    takeoverFailures = [];
    await lock();
  }
  /**
   * The core rejects a pending confirmation with AUTHENTICATION_REQUIRED when the code is wrong, when
   * the deadline has passed, and when it already confirmed the setup before the web recorded that
   * (a terminal replay, which needs the workspace passphrase). Throws unless the setup was already
   * confirmed and is now open.
   */
  async function rejectedConfirmation(body) {
    const state = await coreWorkspaceState();
    if (state === WorkspaceState.LOCKED || isOpen(state)) {
      await save({
        workspaceId: metadata.workspaceId,
        setupId: metadata.setupId,
        stage: "confirmed",
      });
      openedHere = isOpen(state);
      if (!openedHere) await unlockWorkspace(body.workspacePassphrase, PASSPHRASE_REJECTED);
      return;
    }
    if (metadata.expiresAt !== undefined && clock() >= Date.parse(metadata.expiresAt)) {
      await discardPendingSetup();
      throw bad("This setup expired before it was confirmed. Start setup again.", 409);
    }
    const failedAttempts = (metadata.failedAttempts ?? 0) + 1;
    if (failedAttempts >= SETUP_ATTEMPTS) {
      await discardPendingSetup();
      throw bad(
        `This setup was discarded after ${SETUP_ATTEMPTS} incorrect recovery codes. Start setup again.`,
        409,
      );
    }
    await save({ ...metadata, failedAttempts });
    const remaining = SETUP_ATTEMPTS - failedAttempts;
    throw bad(
      `The recovery code does not match. ${remaining} ${remaining === 1 ? "attempt remains" : "attempts remain"} before this setup is discarded.`,
      400,
      "invalid_argument",
    );
  }
  async function confirmPendingSetup(body, groups) {
    let confirmed;
    try {
      confirmed = await workspace.confirmRecovery(
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
      );
    } catch (error) {
      if (coreFault(error) !== "AUTHENTICATION_REQUIRED") return core(() => Promise.reject(error));
      return rejectedConfirmation(body);
    }
    await save({
      workspaceId: metadata.workspaceId,
      setupId: metadata.setupId,
      stage: "confirmed",
    });
    // A terminal replay after a restart confirms without opening anything and reports LOCKED.
    openedHere = isOpen(confirmed.workspace?.state);
    if (!openedHere) await unlockWorkspace(body.workspacePassphrase, PASSPHRASE_REJECTED);
  }
  // Candidate rows carry document identity only; supplier details come from the bounded document
  // list. They are descriptive, so a failed lookup still returns the core's workpaper figures.
  async function basView(session, workpaper) {
    let documents = new Map();
    if (workpaper.purchaseReviews.some(legacyDocument)) {
      try {
        const list = await facade.documents(session);
        documents = new Map(list.documents.map((document) => [document.id, document]));
      } catch {
        // Keep the core figures without supplier details.
      }
    }
    return basOut(workpaper, documents);
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
  // The organisation's active tax-rule bundle reference, as the core stored it.
  async function taxRuleBundle(session) {
    const result = await core(() =>
      organisation.getOrganisation(
        create(GetOrganisationRequestSchema, {
          authentication: session.authentication,
          organisationId: session.organisation.id,
        }),
      ),
    );
    const bundle = result.organisation?.activeTaxRuleBundle;
    return bundle
      ? {
          id: bundle.id,
          revision: bundle.revision.toString(),
          contentHash: Buffer.from(bundle.contentHash).toString("hex"),
        }
      : null;
  }
  const facade = {
    state: () =>
      !metadata
        ? { state: "setup" }
        : metadata.stage === "ready"
          ? { state: "locked" }
          : {
              state: "recovery",
              setupId: metadata.setupId,
              ...(metadata.stage === "pending" && metadata.expiresAt
                ? { expiresAt: metadata.expiresAt }
                : {}),
            },
    create: (body) =>
      exclusive(async () => {
        if (metadata) throw bad("A workspace already exists.", 409);
        const username = required(body.username, 128);
        if (username.trim() !== username)
          throw bad("Remove the spaces at the start or end of the username.");
        const displayName = required(body.displayName, 128);
        if (!acceptedSecret(body.workspacePassphrase)) throw bad(PASSPHRASE_RULE);
        if (!acceptedSecret(body.administratorPassword)) throw bad(PASSWORD_RULE);
        // Without a setup record, a workspace file is one this server lost track of, such as a
        // setup that finished in the core after the request timed out.
        if (await workspaceFileExists()) throw bad(DATA_FOLDER_IN_USE, 409);
        const setupId = uuid();
        // The raw workspace client keeps the core's VALIDATION fault, which the desktop client
        // wrapper replaces with a generic message.
        const result = await core(
          () =>
            workspace.createWorkspace(
              create(CreateWorkspaceRequestSchema, {
                setupId,
                destination: { capabilityId: "local-workspace-directory" },
                workspacePassphrase: secret(body.workspacePassphrase),
                administratorUsername: username,
                administratorDisplayName: displayName,
                administratorPassword: secret(body.administratorPassword),
              }),
              { timeoutMs: WORKSPACE_CREATE_TIMEOUT_MS },
            ),
          { validation: SETUP_REFUSED },
        );
        if (!result.workspace?.id || !result.recoverySecret?.utf8?.length || !result.expiresAt)
          throw new Error("workspace setup failed");
        const expiresAt = timestampDate(result.expiresAt).toISOString();
        await save({
          workspaceId: result.workspace.id,
          setupId,
          stage: "pending",
          expiresAt,
          failedAttempts: 0,
        });
        openedHere = true;
        return {
          setupId,
          recoveryCode: new TextDecoder().decode(result.recoverySecret.utf8),
          expiresAt,
        };
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
        if (metadata.stage === "pending") await confirmPendingSetup(body, groups);
        else if (!openedHere) await unlockWorkspace(body.workspacePassphrase, PASSPHRASE_REJECTED);
        const authentication = await signIn(
          body.username,
          body.administratorPassword,
          SIGN_IN_REJECTED,
        );
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
              // The desktop's shared reference to the tax rules bundled with the core.
              activeTaxRuleBundle: createBundledTaxRuleSourceRef(),
            }),
          ),
        );
        if (!result.organisation?.id) throw new Error("organisation missing");
        await save({ ...metadata, stage: "ready", organisationId: result.organisation.id });
        owner = authentication.sessionId;
        return {
          authentication,
          organisation: { id: result.organisation.id, name: result.organisation.displayName },
        };
      }),
    unlock: (body) =>
      exclusive(async () => {
        if (metadata?.stage !== "ready") throw bad("No completed workspace is available.", 409);
        if (typeof body.workspacePassphrase !== "string" || !body.workspacePassphrase)
          throw bad("Enter the workspace passphrase.");
        const username = required(body.username, 128);
        const password = required(body.administratorPassword, 1024);
        // The core unlocks only a locked workspace, so an open one means another browser is in.
        if (isOpen(await coreWorkspaceState())) await takeOver(username, password);
        await unlockWorkspace(body.workspacePassphrase);
        try {
          const authentication = await signIn(username, password);
          const result = await authenticated(authentication);
          owner = authentication.sessionId;
          return result;
        } catch (error) {
          await lock();
          throw error;
        }
      }),
    logout: (session) =>
      queued(async () => {
        // After a takeover the workspace belongs to a newer session, which a lock would end.
        if (!owner || session?.authentication?.sessionId !== owner) return;
        try {
          await identity.signOut(
            create(SignOutRequestSchema, { authentication: session.authentication }),
          );
        } finally {
          await lock();
        }
      }),
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
      const type = typeValues.get(body.type);
      if (type === undefined) throw bad("Choose an account type.");
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
      let result;
      try {
        result = await client.getCurrentBasDraft(
          create(GetCurrentBasDraftRequestSchema, {
            authentication: session.authentication,
            organisationId: session.organisation.id,
          }),
        );
      } catch (error) {
        if (ConnectError.from(error).code === Code.NotFound) return { workpaper: null };
        return core(() => Promise.reject(error));
      }
      return { workpaper: result.workpaper ? await basView(session, result.workpaper) : null };
    },
    createBas: async (session, body) => {
      const start = date(body.periodStart),
        end = date(body.periodEnd);
      if (start.year !== 2024 || end.year !== 2024 || body.periodStart > body.periodEnd)
        throw bad(
          "The period must start and end within calendar 2024, with the start on or before the end.",
        );
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
      return { workpaper: await basView(session, result.workpaper) };
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
        basStatus: basStatuses.get(result.basStatus) ?? "unspecified",
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
    capabilities: async (session) => ({
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
          "Local GST drafts for periods within calendar 2024, showing GST credits (1B) and provisional candidates. The core calculates sales (G1), GST on sales (1A) and net GST, but the web app does not show them, and BAS preparation and lodgement are unavailable.",
        taxRuleBundle: await taxRuleBundle(session),
      },
    }),
  };
  return facade;
}
