import assert from "node:assert/strict";
import { execFile, spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { lstat, readFile, realpath } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import { fromJson, type JsonValue, toBinary } from "@bufbuild/protobuf";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-node";
import * as a from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  type Document,
  DocumentCandidateSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { IdentityService } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { WorkspaceService } from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { MACOS_RUNTIME_EGRESS_SANDBOX_PROFILE } from "../../../../../scripts/macos-runtime-egress.mjs";
import { capabilityInterceptor } from "../../../src/main/core-client";
import { type CoreReadiness, parseReadiness } from "../../../src/shared/readiness";
import { pollForNoCoreProcesses } from "../electron-lifecycle";
import type { PackagedLayout } from "../fixtures";
import { type CoreProcessInstancePin, findAuthenticatedCoreProcesses } from "../process-check";
import {
  type AuthenticatedAccountingWorkspace,
  CURRENT_WORKFLOW_ADMIN_PASSWORD,
  CURRENT_WORKFLOW_PASSPHRASE,
  CURRENT_WORKFLOW_USERNAME,
} from "./current-accounting-workflow";
import { purchaseDate, purchaseKey } from "./purchase-payments-workflow";

const root = fileURLToPath(new URL("../../../../../", import.meta.url));
export async function buildPurchaseOpeningFixture() {
  await promisify(execFile)(
    process.execPath,
    [path.join(root, "scripts/build-purchase-e2e-opening-fixture.mjs")],
    { cwd: root, maxBuffer: 1024 * 1024 },
  );
}

// Historical opening setup uses the existing native domain/repositories in an
// owned, closed synthetic workspace. The separate legacy payment uses actual
// authenticated commands in the verified packaged Core. Product review actions
// subsequently run through the UI; this introduces no renderer IPC or route.
export async function seedPackagedOpeningWithoutDocument(
  layout: PackagedLayout,
  userDataPath: string,
  workspace: AuthenticatedAccountingWorkspace,
  legacyDocument: Document,
): Promise<{
  readonly opening: a.PostOpeningConversionResponse;
  readonly settlement: a.Journal;
  readonly openingPayableId: string;
  readonly openingEquityCode: string;
  readonly legacyCashCode: string;
}> {
  if (
    path.basename(userDataPath) !== "user-data-primary" ||
    path.basename(path.dirname(userDataPath)) !== "electron-raw" ||
    (await realpath(userDataPath)) !== userDataPath
  )
    throw new Error("OPENING_FIXTURE_ROOT_INVALID");
  const executable = layout.coreExecutable;
  const info = await lstat(executable);
  if (
    !info.isFile() ||
    info.isSymbolicLink() ||
    createHash("sha256")
      .update(await readFile(executable))
      .digest("hex") !== layout.coreSha256
  )
    throw new Error("OPENING_FIXTURE_CORE_INVALID");
  const authority = { packagedExecutable: executable, coreSha256: layout.coreSha256 };
  const pins = new Map<number, CoreProcessInstancePin>();
  if ((await findAuthenticatedCoreProcesses(authority, pins)).length !== 0)
    throw new Error("OPENING_FIXTURE_CORE_ALREADY_RUNNING");
  const runNativeOpening = (
    targetUserDataPath: string,
    targetWorkspace: AuthenticatedAccountingWorkspace,
    passphrase: string,
  ) =>
    new Promise<{
      opening: a.PostOpeningConversionResponse;
      openingPayableId: string;
      openingEquityCode: string;
      outOfScopeTaxCodeId: string;
    }>((resolve, reject) => {
      const fixture = spawn(
        path.join(
          root,
          ".tmp/purchase-e2e-inspector",
          process.platform === "win32" ? "opening-fixture.exe" : "opening-fixture",
        ),
        [],
        { cwd: root, stdio: ["pipe", "pipe", "pipe"] },
      );
      let stdout = Buffer.alloc(0);
      let stderr = Buffer.alloc(0);
      let failed = false;
      const timer = setTimeout(() => {
        failed = true;
        fixture.kill("SIGKILL");
      }, 35_000);
      fixture.stdout.on("data", (bytes: Buffer) => {
        if (stdout.length + bytes.length > 1_048_576) {
          failed = true;
          fixture.kill("SIGKILL");
        } else stdout = Buffer.concat([stdout, bytes]);
      });
      fixture.stderr.on("data", (bytes: Buffer) => {
        if (stderr.length + bytes.length > 512) {
          failed = true;
          fixture.kill("SIGKILL");
        } else stderr = Buffer.concat([stderr, bytes]);
      });
      fixture.once("error", () => {
        clearTimeout(timer);
        reject(new Error("OPENING_FIXTURE_UNAVAILABLE"));
      });
      fixture.once("close", (code) => {
        clearTimeout(timer);
        if (failed || code !== 0) {
          const diagnostic = stderr.toString("utf8").trim();
          reject(
            new Error(
              /^PURCHASE_OPENING_FIXTURE_FAILED_(INPUT|INSTALLATION_KEY|HEADER|UNWRAP|DATABASE|OWNERSHIP|ACCOUNTS|OPENING|OUTPUT)$/.test(
                diagnostic,
              )
                ? diagnostic
                : "OPENING_FIXTURE_FAILED",
            ),
          );
          return;
        }
        try {
          const result = JSON.parse(stdout.toString("utf8"));
          const opening = fromJson(
            a.PostOpeningConversionResponseSchema,
            result.opening as JsonValue,
          );
          if (
            opening.conversion?.organisationId !== targetWorkspace.organisationId ||
            !opening.journal ||
            typeof result.openingPayableId !== "string" ||
            typeof result.openingEquityCode !== "string" ||
            typeof result.outOfScopeTaxCodeId !== "string"
          )
            throw new Error();
          resolve({
            opening,
            openingPayableId: result.openingPayableId,
            openingEquityCode: result.openingEquityCode,
            outOfScopeTaxCodeId: result.outOfScopeTaxCodeId,
          });
        } catch {
          reject(new Error("OPENING_FIXTURE_OUTPUT_INVALID"));
        }
      });
      fixture.stdin.on("error", () => {
        failed = true;
        fixture.kill("SIGKILL");
      });
      fixture.stdin.end(
        JSON.stringify({
          userDataPath: targetUserDataPath,
          workspaceId: targetWorkspace.workspaceId,
          organisationId: targetWorkspace.organisationId,
          passphrase,
        }),
      );
    });
  const databasePath = path.join(userDataPath, "local-core/core/workspace/tammy-workspace.db");
  const databaseDigest = async () =>
    createHash("sha256")
      .update(await readFile(databasePath))
      .digest("hex");
  const originalDigest = await databaseDigest();
  await assert.rejects(
    runNativeOpening(path.dirname(userDataPath), workspace, CURRENT_WORKFLOW_PASSPHRASE),
    /PURCHASE_OPENING_FIXTURE_FAILED_INPUT/,
  );
  await assert.rejects(
    runNativeOpening(userDataPath, workspace, "Wrong synthetic opening fixture passphrase"),
    /PURCHASE_OPENING_FIXTURE_FAILED_UNWRAP/,
  );
  await assert.rejects(
    runNativeOpening(
      userDataPath,
      { ...workspace, organisationId: purchaseKey() },
      CURRENT_WORKFLOW_PASSPHRASE,
    ),
    /PURCHASE_OPENING_FIXTURE_FAILED_OWNERSHIP/,
  );
  assert.equal(
    await databaseDigest(),
    originalDigest,
    "Rejected fixture inputs must preserve encrypted database bytes",
  );
  const nativeOpening = await runNativeOpening(
    userDataPath,
    workspace,
    CURRENT_WORKFLOW_PASSPHRASE,
  );
  const seededDigest = await databaseDigest();
  await assert.rejects(
    runNativeOpening(userDataPath, workspace, CURRENT_WORKFLOW_PASSPHRASE),
    /PURCHASE_OPENING_FIXTURE_FAILED_OWNERSHIP/,
  );
  assert.equal(
    await databaseDigest(),
    seededDigest,
    "An already seeded fixture must remain unchanged",
  );
  const args = ["--data-root", path.join(userDataPath, "local-core")];
  const child = spawn(
    process.platform === "darwin" ? "/usr/bin/sandbox-exec" : executable,
    process.platform === "darwin"
      ? ["-p", MACOS_RUNTIME_EGRESS_SANDBOX_PROFILE, executable, ...args]
      : args,
    {
      // Core treats parent stdin EOF as the request to shut down.
      stdio: ["pipe", "pipe", "pipe"],
      env: { HOME: process.env.HOME, PATH: process.env.PATH, TMPDIR: process.env.TMPDIR },
    },
  );
  const closed = new Promise<void>((resolve) => child.once("close", () => resolve()));
  let lifecycleEvent = "UNKNOWN";
  child.stderr.on("data", (bytes: Buffer) => {
    // Retain only the native lifecycle event enum, never raw diagnostics.
    const diagnostic = bytes.toString("utf8");
    const match = diagnostic.match(
      /"event"\s*:\s*"(configuration_failed|sbr_configuration_failed|composition_failed|server_construction_failed|server_start_failed|readiness_write_failed)"/,
    );
    if (match?.[1]) lifecycleEvent = match[1].toUpperCase();
    else if (diagnostic.includes("Operation not permitted")) lifecycleEvent = "SANDBOX_DENIED";
    else if (diagnostic.includes("No such file")) lifecycleEvent = "EXECUTABLE_MISSING";
  });
  const closeFixture = async () => {
    child.stdout.resume();
    child.stdin.end();
    let timer: ReturnType<typeof setTimeout> | undefined;
    const graceful = await Promise.race([
      closed.then(() => true),
      new Promise<false>((resolve) => {
        timer = setTimeout(() => resolve(false), 20_000);
      }),
    ]);
    clearTimeout(timer);
    if (!graceful) {
      child.kill("SIGKILL");
      await closed;
    }
    await pollForNoCoreProcesses({
      intervalMs: 100,
      timeoutMs: 5_000,
      query: () => findAuthenticatedCoreProcesses(authority, pins),
    });
    if (!graceful) throw new Error("OPENING_FIXTURE_CORE_FORCED_KILL");
  };
  try {
    const readiness = await new Promise<Readonly<CoreReadiness>>((resolve, reject) => {
      let bytes = Buffer.alloc(0);
      const timer = setTimeout(() => finish(new Error("OPENING_FIXTURE_STARTUP_TIMEOUT")), 30_000);
      const finish = (error?: Error, value?: Readonly<CoreReadiness>) => {
        clearTimeout(timer);
        child.stdout.removeListener("data", receive);
        child.removeListener("error", failed);
        child.removeListener("close", failed);
        if (error) reject(error);
        else if (value) resolve(value);
      };
      const failed = () =>
        finish(
          new Error(
            `OPENING_FIXTURE_STARTUP_FAILED_${lifecycleEvent}_${child.exitCode ?? child.signalCode ?? "SPAWN"}`,
          ),
        );
      const receive = (chunk: Buffer) => {
        if (bytes.length + chunk.length > 65_536) {
          finish(new Error("OPENING_FIXTURE_READINESS_INVALID"));
          return;
        }
        bytes = Buffer.concat([bytes, chunk]);
        if (!bytes.includes(10)) return;
        try {
          finish(undefined, parseReadiness(bytes));
        } catch {
          finish(new Error("OPENING_FIXTURE_READINESS_INVALID"));
        }
      };
      child.once("error", failed);
      child.once("close", failed);
      child.stdout.on("data", receive);
    });
    const matches = await findAuthenticatedCoreProcesses(authority, pins);
    if (!child.pid || !matches.some((match) => match.processId === child.pid))
      throw new Error("OPENING_FIXTURE_PROCESS_UNAUTHENTICATED");
    const transport = createConnectTransport({
      baseUrl: `https://127.0.0.1:${readiness.port}`,
      httpVersion: "1.1",
      defaultTimeoutMs: 15_000,
      nodeOptions: {
        ca: readiness.caPem,
        rejectUnauthorized: true,
        minVersion: "TLSv1.3",
        maxVersion: "TLSv1.3",
      },
      interceptors: [capabilityInterceptor(readiness.capability)],
    });
    const opened = await createClient(WorkspaceService, transport).unlockWorkspace({
      workspaceFile: { capabilityId: "local-workspace-file" },
      proof: {
        proof: {
          case: "passphrase",
          value: { utf8: new TextEncoder().encode(CURRENT_WORKFLOW_PASSPHRASE) },
        },
      },
    });
    if (opened.workspace?.id !== workspace.workspaceId)
      throw new Error("OPENING_FIXTURE_WORKSPACE_MISMATCH");
    const identity = createClient(IdentityService, transport);
    const signedIn = await identity.signIn({
      username: CURRENT_WORKFLOW_USERNAME,
      password: { utf8: new TextEncoder().encode(CURRENT_WORKFLOW_ADMIN_PASSWORD) },
    });
    if (signedIn.user?.id !== workspace.userId || !signedIn.session)
      throw new Error("OPENING_FIXTURE_PRINCIPAL_MISMATCH");
    const authentication = { actorUserId: signedIn.user.id, sessionId: signedIn.session.id };
    const accounting = createClient(a.AccountingService, transport);
    const chart = await accounting.listAccounts({
      authentication,
      organisationId: workspace.organisationId,
      page: { pageSize: 200 },
    });
    if (chart.page?.nextCursor) throw new Error("OPENING_FIXTURE_CHART_INCOMPLETE");
    const expense = chart.accounts.find((account) => account.code === "6101");
    const createdBank = await accounting.createAccount({
      commandContext: { authentication, idempotencyKey: purchaseKey() },
      organisationId: workspace.organisationId,
      code: "1011",
      name: "Historical operating cash",
      type: a.AccountType.ASSET,
      normalBalance: a.NormalBalance.DEBIT,
      reportClassification: "balance_sheet.cash",
      cashFlowClassification: "operating",
    });
    const bank = createdBank.account;
    if (
      !nativeOpening.outOfScopeTaxCodeId ||
      !bank ||
      !expense ||
      !legacyDocument.candidate ||
      legacyDocument.organisationId !== workspace.organisationId
    )
      throw new Error("OPENING_FIXTURE_SETTLEMENT_INPUT_MISSING");
    // An earlier-period legacy expense paid through an ordinary cash account.
    // It does not settle the separate opening payable or create a purchase credit.
    const settled = await accounting.postManualJournal({
      commandContext: { authentication, idempotencyKey: purchaseKey() },
      organisationId: workspace.organisationId,
      postingDate: purchaseDate("2026-10-02"),
      memo: "Retained earlier-period legacy supplier cash payment",
      lines: [
        {
          clientLineId: purchaseKey(),
          accountId: expense.id,
          debit: { currencyCode: "AUD", minorUnits: 11000n },
          credit: { currencyCode: "AUD", minorUnits: 0n },
          taxCodeId: nativeOpening.outOfScopeTaxCodeId,
          taxAmount: { currencyCode: "AUD", minorUnits: 0n },
          source: {
            type: "document",
            id: legacyDocument.id,
            revision: legacyDocument.version,
            contentHash: createHash("sha256")
              .update(toBinary(DocumentCandidateSchema, legacyDocument.candidate))
              .digest(),
          },
        },
        {
          clientLineId: purchaseKey(),
          accountId: bank.id,
          debit: { currencyCode: "AUD", minorUnits: 0n },
          credit: { currencyCode: "AUD", minorUnits: 11000n },
        },
      ],
    });
    if (!settled.journal) throw new Error("OPENING_FIXTURE_SETTLEMENT_MISSING");
    await identity.signOut({ authentication });
    return {
      opening: nativeOpening.opening,
      settlement: settled.journal,
      openingPayableId: nativeOpening.openingPayableId,
      openingEquityCode: nativeOpening.openingEquityCode,
      legacyCashCode: bank.code,
    };
  } finally {
    await closeFixture();
  }
}
