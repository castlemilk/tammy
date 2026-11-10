import assert from "node:assert/strict";
import { mkdir, mkdtemp, readFile, rm, stat, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { create } from "@bufbuild/protobuf";
import { timestampFromMs } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import {
  DocumentSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { BasAttentionStatus } from "@tammy/connect-client/tammy/v1/overview_pb.js";
import { PurchaseReviewStatus } from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import {
  BasPurchaseReviewKind,
  BasPurchaseSourceFormat,
  BasWorkpaperSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { WorkspaceState } from "@tammy/connect-client/tammy/v1/workspace_pb.js";
import { BUNDLED_TAX_RULE_SOURCE } from "../../desktop/src/shared/bundled-tax-rule.ts";
import { createFacade } from "./facade.mjs";

const ids = {
  workpaper: "018f0000-0000-7000-8000-000000000001",
  organisation: "018f0000-0000-7000-8000-000000000002",
  reviewed: "018f0000-0000-7000-8000-000000000003",
  changed: "018f0000-0000-7000-8000-000000000004",
  posted: "018f0000-0000-7000-8000-000000000005",
  workspace: "018f0000-0000-7000-8000-000000000006",
  setup: "018f0000-0000-7000-8000-000000000007",
  later: "018f0000-0000-7000-8000-000000000008",
  undated: "018f0000-0000-7000-8000-000000000009",
  user: "018f0000-0000-7000-8000-00000000000a",
  session: "018f0000-0000-7000-8000-00000000000b",
};
const session = {
  authentication: { actorUserId: ids.setup, sessionId: ids.workspace },
  organisation: { id: ids.organisation, name: "Example" },
};
const aud = (minorUnits) => ({ currencyCode: "AUD", minorUnits });
const day = (month, dayOfMonth, year = 2024) => ({ year, month, day: dayOfMonth });
const deadline = Date.parse("2024-05-12T03:15:00.000Z");
const pending = (fields = {}) => ({
  workspaceId: ids.workspace,
  setupId: ids.setup,
  stage: "pending",
  ...fields,
});
const confirmation = {
  setupId: ids.setup,
  recoveryCode: "ABCD-EFGH-IJKL",
  workspacePassphrase: "workspace-passphrase-long-enough",
  username: "admin@tammy.local",
  administratorPassword: "administrator-password-long-enough",
  businessLegalName: "Tammy Business Pty Ltd",
  businessDisplayName: "Tammy Business",
  abn: "51824753556",
};
// The workspace and identity services return bare faults: Code.Unknown with the fault name.
const fault = (name) => new ConnectError(name, Code.Unknown);
const conflict = (message) => ({ status: 409, code: "conflict", message });
const metadataFile = (dataRoot) => path.join(dataRoot, "web-workspace.json");
// Where the core keeps the workspace it is creating or has created.
async function leaveWorkspaceFile(dataRoot) {
  await mkdir(path.join(dataRoot, "core", "workspace"), { recursive: true });
  await writeFile(path.join(dataRoot, "core", "workspace", "tammy-workspace.db"), "");
}

async function withFacade(dependencies, run, metadata) {
  const dataRoot = await mkdtemp(path.join(os.tmpdir(), "tammy-web-facade-"));
  try {
    if (metadata) await writeFile(metadataFile(dataRoot), JSON.stringify(metadata));
    const start = () => createFacade({ ...dependencies, dataRoot });
    await run(await start(), { dataRoot, start });
  } finally {
    await rm(dataRoot, { recursive: true, force: true });
  }
}

function document(id, version, supplierName, invoiceNumber) {
  return create(DocumentSchema, {
    id,
    organisationId: ids.organisation,
    version,
    status: 2,
    sourceDisplayName: "receipt.pdf",
    candidate: { supplierName, invoiceNumber, total: aud(31900n), gst: aud(2900n) },
  });
}

function legacyCandidate(documentId, reviewedVersion, unclaimed, invoiceDate = day(5, 12)) {
  return {
    legacySource: {
      source: {
        case: "document",
        value: {
          documentId,
          reviewedVersion,
          originalContentHash: new Uint8Array(32),
          reviewedSnapshotHash: new Uint8Array(32),
        },
      },
    },
    status: PurchaseReviewStatus.UNRESOLVED_LEGACY,
    kind: BasPurchaseReviewKind.OLD_LEGACY_DOCUMENT,
    ...(invoiceDate ? { invoiceDate } : {}),
    eligibleGst: aud(unclaimed),
    unclaimedGst: aud(unclaimed),
    reason: "Legacy source needs review of eligibility, settlement and prior GST claims.",
  };
}

/** A workspace client that rejects every confirmation, as the core does for a wrong code. */
function rejectingWorkspace(state = WorkspaceState.PENDING_RECOVERY) {
  const confirmations = [];
  return {
    confirmations,
    confirmRecovery: async (request) => {
      confirmations.push(
        request.confirmations.map(({ value }) => new TextDecoder().decode(value)).join("-"),
      );
      throw fault("AUTHENTICATION_REQUIRED");
    },
    getWorkspaceState: async () => ({ workspace: { id: ids.workspace, state } }),
  };
}

test("reviewed documents stay provisional GST candidates outside GST credits (1B)", async () => {
  const workpaper = create(BasWorkpaperSchema, {
    id: ids.workpaper,
    organisationId: ids.organisation,
    periodStart: day(4, 1),
    periodEnd: day(6, 30),
    salesG1: aud(0n),
    gstOnSales1a: aud(0n),
    gstCredits1b: aud(500n),
    netGstPayable: aud(-500n),
    purchaseSourceFormat: BasPurchaseSourceFormat.RECONCILED_V1,
    legacyCandidateGst: aud(3900n),
    purchaseSources: [
      {
        documentId: ids.posted,
        invoiceDate: day(5, 1),
        allocatedGross: aud(5500n),
        claimedGst: aud(500n),
        supplierName: "Posted Supplies",
        invoiceNumber: "P-1",
      },
    ],
    purchaseReviews: [
      legacyCandidate(ids.reviewed, 2n, 2900n),
      legacyCandidate(ids.changed, 2n, 1000n),
    ],
  });
  let documentReads = 0;
  const client = {
    getCurrentBasDraft: async () => create(GetCurrentBasDraftResponseSchema, { workpaper }),
    listDocuments: async () => {
      documentReads++;
      return create(ListDocumentsResponseSchema, {
        documents: [
          document(ids.reviewed, 2n, "Officeworks", "INV-1"),
          // Reviewed again after the draft, so its current details are not the retained source.
          document(ids.changed, 3n, "Changed Supplier", "INV-2"),
        ],
      });
    },
  };
  await withFacade({ client }, async (facade) => {
    const { workpaper: result } = await facade.bas(session);
    assert.equal(documentReads, 1);
    assert.equal(result.gstCredits1bMinor, 500);
    assert.equal(result.candidateGstMinor, 3900);
    assert.equal(result.salesG1Minor, null);
    assert.equal(result.netGstPayableMinor, null);
    assert.equal(result.sourcesChanged, false);
    assert.deepEqual(result.reviewWarnings, []);
    assert.deepEqual(result.sources, [
      {
        id: ids.posted,
        date: "2024-05-01",
        supplierName: "Posted Supplies",
        invoiceNumber: "P-1",
        grossMinor: 5500,
        gstCreditMinor: 500,
      },
    ]);
    assert.deepEqual(result.candidates, [
      {
        id: ids.reviewed,
        date: "2024-05-12",
        supplierName: "Officeworks",
        invoiceNumber: "INV-1",
        candidateGstMinor: 2900,
      },
      {
        id: ids.changed,
        date: "2024-05-12",
        supplierName: "",
        invoiceNumber: "",
        candidateGstMinor: 1000,
      },
    ]);
    assert.equal(result.limitations.length, 2);
    assert.match(result.limitations[0], /calculated by the core but not shown/);
    assert.match(result.limitations[1], /not yet posted as purchases, whatever their date/);
    assert.match(result.limitations[1], /not a claimed credit/);
  });
});

test("candidates of any date, including undated ones, are listed with the core's total", async () => {
  // The core lists every reviewed document that is not a posted purchase, whatever its date, and
  // totals them in legacyCandidateGst. The web keeps that rather than filtering by period.
  const workpaper = create(BasWorkpaperSchema, {
    id: ids.workpaper,
    organisationId: ids.organisation,
    periodStart: day(4, 1),
    periodEnd: day(6, 30),
    gstCredits1b: aud(0n),
    purchaseSourceFormat: BasPurchaseSourceFormat.RECONCILED_V1,
    legacyCandidateGst: aud(102950n),
    sourcesChanged: true,
    reviewWarnings: [
      "3 retained purchase review rows include legacy candidates, evidence suspense, exclusions or future claims. Review the complete typed purchase details before relying on this draft.",
    ],
    purchaseReviews: [
      legacyCandidate(ids.reviewed, 2n, 2900n),
      legacyCandidate(ids.later, 2n, 100000n, day(9, 1, 2025)),
      legacyCandidate(ids.undated, 2n, 50n, null),
    ],
  });
  const client = {
    getCurrentBasDraft: async () => create(GetCurrentBasDraftResponseSchema, { workpaper }),
    listDocuments: async () =>
      create(ListDocumentsResponseSchema, {
        documents: [
          document(ids.reviewed, 2n, "In period", "IN-1"),
          document(ids.later, 2n, "Next year", "NY-1"),
          document(ids.undated, 2n, "Undated", "UD-1"),
        ],
      }),
  };
  await withFacade({ client }, async (facade) => {
    const { workpaper: result } = await facade.bas(session);
    assert.deepEqual(
      result.candidates.map(({ supplierName, date, candidateGstMinor }) => [
        supplierName,
        date,
        candidateGstMinor,
      ]),
      [
        ["In period", "2024-05-12", 2900],
        ["Next year", "2025-09-01", 100000],
        ["Undated", null, 50],
      ],
    );
    assert.equal(result.candidateGstMinor, 102950);
    assert.equal(result.gstCredits1bMinor, 0);
    assert.equal(result.sourcesChanged, true);
    assert.deepEqual(result.reviewWarnings, workpaper.reviewWarnings);
  });
});

test("legacy reviewed-document workpapers keep their GST credit sources", async () => {
  const workpaper = create(BasWorkpaperSchema, {
    id: ids.workpaper,
    organisationId: ids.organisation,
    periodStart: day(4, 1),
    periodEnd: day(6, 30),
    gstCredits1b: aud(2900n),
    sources: [
      {
        documentId: ids.reviewed,
        documentDate: day(5, 12),
        supplierName: "Officeworks",
        invoiceNumber: "INV-1",
        gross: aud(31900n),
        gstCredit: aud(2900n),
      },
    ],
  });
  const client = {
    getCurrentBasDraft: async () => create(GetCurrentBasDraftResponseSchema, { workpaper }),
    listDocuments: async () => assert.fail("legacy sources need no document lookup"),
  };
  await withFacade({ client }, async (facade) => {
    const { workpaper: result } = await facade.bas(session);
    assert.equal(result.gstCredits1bMinor, 2900);
    assert.equal(result.candidateGstMinor, null);
    assert.deepEqual(result.candidates, []);
    assert.deepEqual(result.sources, [
      {
        id: ids.reviewed,
        date: "2024-05-12",
        supplierName: "Officeworks",
        invoiceNumber: "INV-1",
        grossMinor: 31900,
        gstCreditMinor: 2900,
      },
    ]);
    assert.equal(result.limitations.length, 1);
  });
});

test("GST drafts are limited to calendar 2024 with the start on or before the end", async () => {
  await withFacade({ client: {} }, async (facade) => {
    for (const [periodStart, periodEnd] of [
      ["2023-12-31", "2024-03-31"],
      ["2024-10-01", "2025-01-01"],
      ["2024-06-30", "2024-04-01"],
    ])
      await assert.rejects(facade.createBas(session, { periodStart, periodEnd }), {
        status: 400,
        message:
          "The period must start and end within calendar 2024, with the start on or before the end.",
      });
  });
});

test("the overview names every BAS attention status the core reports", async () => {
  let status = BasAttentionStatus.ACTION_NEEDED;
  const client = {
    getAttentionSummary: async () => ({
      documentsNeedingReview: 0,
      documentsReviewedInPeriod: 0,
      bankingLinesNeedingReconciliation: 0,
      bankingLinesUnreconciledInPeriod: 0,
      currentDraftBasWorkpapers: 1,
      basStatus: status,
      attentionItems: [],
    }),
  };
  await withFacade({ client }, async (facade) => {
    for (const [value, name] of [
      [BasAttentionStatus.UNSPECIFIED, "unspecified"],
      [BasAttentionStatus.NOT_CREATED, "not_created"],
      [BasAttentionStatus.ACTION_NEEDED, "action_needed"],
      [BasAttentionStatus.REVIEW_READY, "review_ready"],
      [BasAttentionStatus.FROZEN_FOR_MANUAL_HANDOFF, "frozen_for_manual_handoff"],
      [BasAttentionStatus.EXTERNAL_CONFIRMATION_RECORDED, "external_confirmation_recorded"],
      [BasAttentionStatus.DISPUTED, "disputed"],
    ]) {
      status = value;
      assert.equal((await facade.overview(session)).basStatus, name);
    }
  });
});

test("account types are looked up without inherited object keys", async () => {
  const client = { createAccount: async () => assert.fail("an invalid type reached the core") };
  await withFacade({ client }, async (facade) => {
    for (const type of ["constructor", "__proto__", "toString", "hasOwnProperty", 4, undefined])
      await assert.rejects(
        facade.createAccount(session, {
          idempotencyKey: "018f0000-0000-7000-8000-0000000000aa",
          code: "6100",
          name: "Office expenses",
          type,
        }),
        { status: 400, code: "invalid_argument", message: "Choose an account type." },
      );
  });
});

test("workspace state conflicts carry the documented conflict code", async () => {
  await withFacade({}, async (facade) => {
    await assert.rejects(facade.unlock({}), conflict("No completed workspace is available."));
    await assert.rejects(
      facade.confirm({ setupId: ids.setup }),
      conflict("No matching setup is pending."),
    );
  });
  await withFacade(
    {},
    async (facade) => {
      await assert.rejects(facade.create({}), conflict("A workspace already exists."));
    },
    {
      workspaceId: ids.workspace,
      setupId: ids.setup,
      stage: "ready",
      organisationId: ids.organisation,
    },
  );
  const started = Promise.withResolvers();
  const workspace = {
    createWorkspace: () =>
      new Promise((resolve) => {
        started.resolve(() =>
          resolve({
            workspace: { id: ids.workspace },
            recoverySecret: { utf8: new TextEncoder().encode("ABCD-EFGH") },
            expiresAt: timestampFromMs(deadline),
          }),
        );
      }),
  };
  const details = {
    workspacePassphrase: "workspace-passphrase-long-enough",
    username: "admin@tammy.local",
    displayName: "Tammy Admin",
    administratorPassword: "administrator-password-long-enough",
  };
  await withFacade({ workspace }, async (facade) => {
    const first = facade.create(details);
    await assert.rejects(
      facade.create(details),
      conflict("A workspace operation is already in progress."),
    );
    // Finish the core call once the first create has reached it.
    (await started.promise)();
    assert.equal((await first).recoveryCode, "ABCD-EFGH");
  });
});

test("setup keeps the core's confirmation deadline and returns it", async () => {
  const calls = [];
  const workspace = {
    createWorkspace: async (request, options) => {
      calls.push({ request, options });
      return {
        workspace: { id: ids.workspace },
        recoverySecret: { utf8: new TextEncoder().encode("ABCD-EFGH-IJKL") },
        expiresAt: timestampFromMs(deadline),
      };
    },
  };
  await withFacade({ workspace }, async (facade, { dataRoot }) => {
    const created = await facade.create({
      workspacePassphrase: "workspace-passphrase-long-enough",
      username: "admin@tammy.local",
      displayName: "Tammy Admin",
      administratorPassword: "administrator-password-long-enough",
    });
    assert.deepEqual(created, {
      setupId: created.setupId,
      recoveryCode: "ABCD-EFGH-IJKL",
      expiresAt: "2024-05-12T03:15:00.000Z",
    });
    // Key derivation can outlast the transport's 5-second default.
    assert.deepEqual(calls[0].options, { timeoutMs: 30_000 });
    assert.deepEqual(JSON.parse(await readFile(metadataFile(dataRoot), "utf8")), {
      workspaceId: ids.workspace,
      setupId: created.setupId,
      stage: "pending",
      expiresAt: "2024-05-12T03:15:00.000Z",
      failedAttempts: 0,
    });
    assert.deepEqual(facade.state(), {
      state: "recovery",
      setupId: created.setupId,
      expiresAt: "2024-05-12T03:15:00.000Z",
    });
  });
});

test("setup checks the secrets and the data folder before the core reserves anything", async () => {
  let creates = 0;
  const workspace = {
    createWorkspace: async () => {
      creates++;
      throw fault("VALIDATION");
    },
  };
  const details = {
    workspacePassphrase: "workspace-passphrase-long-enough",
    username: "admin@tammy.local",
    displayName: "Tammy Admin",
    administratorPassword: "administrator-password-long-enough",
  };
  const rule = (name) =>
    `The ${name} must have 15 to 128 characters: letters, numbers, punctuation, symbols or spaces.`;
  await withFacade({ workspace }, async (facade, { dataRoot }) => {
    // The core's rule: 15 to 128 printable characters after NFC normalisation.
    for (const workspacePassphrase of [
      "fourteen-chars",
      "x".repeat(129),
      "tab\tseparated-passphrase",
      undefined,
    ])
      await assert.rejects(facade.create({ ...details, workspacePassphrase }), {
        status: 400,
        code: "invalid_argument",
        message: rule("workspace passphrase"),
      });
    // An e and a combining acute accent normalise to one character, so this is 129 characters.
    for (const administratorPassword of [
      "fourteen-chars",
      "e\u0301".repeat(129),
      "no-break\u00a0space-password",
    ])
      await assert.rejects(facade.create({ ...details, administratorPassword }), {
        status: 400,
        message: rule("administrator password"),
      });
    await assert.rejects(facade.create({ ...details, username: " admin@tammy.local" }), {
      status: 400,
      message: "Remove the spaces at the start or end of the username.",
    });
    assert.equal(creates, 0);
    // 128 characters after normalisation, and only then sent to the core. A validation fault the
    // checks above cannot explain is not blamed on the secrets.
    await assert.rejects(
      facade.create({ ...details, administratorPassword: "e\u0301".repeat(128) }),
      {
        status: 400,
        code: "invalid_argument",
        message: "The core refused to create the workspace. Restart Tammy and try again.",
      },
    );
    assert.equal(creates, 1);
    // A workspace file without a setup record is one this server lost track of.
    await leaveWorkspaceFile(dataRoot);
    await assert.rejects(
      facade.create(details),
      conflict(
        "This data folder already holds a workspace that this web server has no record of. Stop the server, move the folder aside or set TAMMY_WEB_DATA_ROOT to a new folder, then start again.",
      ),
    );
    assert.equal(creates, 1);
    assert.deepEqual(facade.state(), { state: "setup" });
  });
});

test("incorrect recovery codes count down across restarts and discard the setup on the fifth", async () => {
  const workspace = rejectingWorkspace();
  const now = () => deadline - 60_000;
  await withFacade(
    { workspace, now },
    async (first, { dataRoot, start }) => {
      // Metadata from before expiresAt and failedAttempts were kept still loads and counts.
      assert.deepEqual(first.state(), { state: "recovery", setupId: ids.setup });
      await assert.rejects(first.confirm(confirmation), {
        status: 400,
        code: "invalid_argument",
        message:
          "The recovery code does not match. 4 attempts remain before this setup is discarded.",
      });
      assert.equal(JSON.parse(await readFile(metadataFile(dataRoot), "utf8")).failedAttempts, 1);
      // The count is persisted, so a restart cannot reset it.
      const facade = await start();
      for (const remaining of ["3 attempts remain", "2 attempts remain", "1 attempt remains"])
        await assert.rejects(facade.confirm(confirmation), {
          status: 400,
          message: `The recovery code does not match. ${remaining} before this setup is discarded.`,
        });
      assert.deepEqual(workspace.confirmations, Array(4).fill("ABCD-EFGH"));
      await assert.rejects(
        facade.confirm(confirmation),
        conflict("This setup was discarded after 5 incorrect recovery codes. Start setup again."),
      );
      // Deliberately invalid confirmations make sure the core discarded the setup and its files.
      assert.deepEqual(workspace.confirmations.slice(5), Array(5).fill("0000-0000"));
      await assert.rejects(stat(metadataFile(dataRoot)), { code: "ENOENT" });
      assert.deepEqual(facade.state(), { state: "setup" });
    },
    pending(),
  );
});

test("a rejected confirmation after the deadline discards the setup", async () => {
  let now = deadline - 1;
  const workspace = rejectingWorkspace();
  await withFacade(
    { workspace, now: () => now },
    async (facade, { dataRoot }) => {
      assert.deepEqual(facade.state(), {
        state: "recovery",
        setupId: ids.setup,
        expiresAt: "2024-05-12T03:15:00.000Z",
      });
      await assert.rejects(facade.confirm(confirmation), {
        status: 400,
        message:
          "The recovery code does not match. 2 attempts remain before this setup is discarded.",
      });
      now = deadline;
      await assert.rejects(
        facade.confirm(confirmation),
        conflict("This setup expired before it was confirmed. Start setup again."),
      );
      assert.deepEqual(workspace.confirmations.slice(2), Array(5).fill("0000-0000"));
      await assert.rejects(stat(metadataFile(dataRoot)), { code: "ENOENT" });
      assert.deepEqual(facade.state(), { state: "setup" });
    },
    pending({ expiresAt: "2024-05-12T03:15:00.000Z", failedAttempts: 2 }),
  );
});

test("a discard that leaves the setup's file behind keeps the setup record", async () => {
  const workspace = rejectingWorkspace();
  await withFacade(
    { workspace, now: () => deadline - 60_000 },
    async (facade, { dataRoot }) => {
      // Some rejections do not count toward the core's discard, so the file can outlive them.
      await leaveWorkspaceFile(dataRoot);
      await assert.rejects(
        facade.confirm(confirmation),
        conflict("The core has not discarded this setup yet. Restart Tammy, then try again."),
      );
      assert.deepEqual(workspace.confirmations.slice(1), Array(5).fill("0000-0000"));
      assert.equal(JSON.parse(await readFile(metadataFile(dataRoot), "utf8")).failedAttempts, 4);
      assert.deepEqual(facade.state(), {
        state: "recovery",
        setupId: ids.setup,
        expiresAt: "2024-05-12T03:15:00.000Z",
      });
    },
    pending({ expiresAt: "2024-05-12T03:15:00.000Z", failedAttempts: 4 }),
  );
});

test("pending metadata with invalid deadline or attempt fields is refused", async () => {
  for (const fields of [{ expiresAt: "tomorrow" }, { failedAttempts: -1 }, { failedAttempts: "2" }])
    await assert.rejects(
      withFacade({}, () => assert.fail("invalid metadata loaded"), pending(fields)),
      /invalid metadata/,
    );
});

function confirmingClients(confirmRecovery, calls) {
  return {
    workspace: {
      confirmRecovery,
      getWorkspaceState: async () => ({
        workspace: { id: ids.workspace, state: WorkspaceState.LOCKED },
      }),
      unlockWorkspace: async (request) => {
        calls.push(`unlock:${new TextDecoder().decode(request.proof.proof.value.utf8)}`);
        return {};
      },
    },
    identity: {
      signIn: async () => {
        calls.push("signIn");
        return { user: { id: ids.user }, session: { id: ids.session } };
      },
    },
    client: {
      createOrganisation: async (request) => {
        calls.push(request);
        return { organisation: { id: ids.organisation, displayName: "Tammy Business" } };
      },
    },
  };
}

test("a terminal replay that reports a locked workspace is unlocked before sign-in", async () => {
  const calls = [];
  const confirmRecovery = async (request) => {
    calls.push(`confirm:${request.terminalReplayProof ? "with proof" : "without proof"}`);
    // After a restart the core confirms the committed setup without opening it.
    return { workspace: { id: ids.workspace, state: WorkspaceState.LOCKED } };
  };
  await withFacade(
    confirmingClients(confirmRecovery, calls),
    async (facade, { dataRoot }) => {
      const result = await facade.confirm(confirmation);
      assert.deepEqual(result.organisation, { id: ids.organisation, name: "Tammy Business" });
      const [organisationRequest] = calls.splice(3);
      assert.deepEqual(calls, [
        "confirm:with proof",
        "unlock:workspace-passphrase-long-enough",
        "signIn",
      ]);
      // New organisations use the current bundled tax rules, not the known legacy reference.
      const bundle = organisationRequest.activeTaxRuleBundle;
      assert.equal(bundle.id, BUNDLED_TAX_RULE_SOURCE.id);
      assert.equal(bundle.revision, BUNDLED_TAX_RULE_SOURCE.revision);
      assert.equal(
        Buffer.from(bundle.contentHash).toString("hex"),
        BUNDLED_TAX_RULE_SOURCE.contentHashHex,
      );
      assert.deepEqual(JSON.parse(await readFile(metadataFile(dataRoot), "utf8")), {
        workspaceId: ids.workspace,
        setupId: ids.setup,
        stage: "ready",
        organisationId: ids.organisation,
      });
    },
    pending({ expiresAt: "2024-05-12T03:15:00.000Z" }),
  );
});

test("a setup the core already confirmed is finished with the passphrase, not counted", async () => {
  const calls = [];
  const confirmRecovery = async () => {
    calls.push("confirm");
    throw fault("AUTHENTICATION_REQUIRED");
  };
  await withFacade(
    { ...confirmingClients(confirmRecovery, calls), now: () => deadline + 60_000 },
    async (facade) => {
      const { workspacePassphrase: _, ...withoutPassphrase } = confirmation;
      await assert.rejects(facade.confirm(withoutPassphrase), {
        status: 400,
        message: "Enter the workspace passphrase.",
      });
      assert.deepEqual(facade.state(), { state: "recovery", setupId: ids.setup });
      calls.length = 0;
      await facade.confirm(confirmation);
      assert.deepEqual(calls.slice(0, 2), ["unlock:workspace-passphrase-long-enough", "signIn"]);
    },
    pending({ expiresAt: "2024-05-12T03:15:00.000Z", failedAttempts: 4 }),
  );
});

test("wrong credentials are reported as credentials, not as an expired session", async () => {
  let locks = 0;
  const workspace = {
    unlockWorkspace: async (request) => {
      if (new TextDecoder().decode(request.proof.proof.value.utf8) !== "right-passphrase")
        throw fault("AUTHENTICATION_REQUIRED");
      return {};
    },
    lockWorkspace: async () => {
      locks++;
      return {};
    },
  };
  const identity = {
    signIn: async () => {
      throw fault("AUTHENTICATION_REQUIRED");
    },
  };
  const message =
    "The workspace passphrase, username or administrator password is incorrect. After 5 failed attempts, wait 15 minutes before trying again.";
  await withFacade(
    { workspace, identity },
    async (facade) => {
      const credentials = {
        workspacePassphrase: "wrong-passphrase",
        username: "admin@tammy.local",
        administratorPassword: "administrator-password-long-enough",
      };
      await assert.rejects(facade.unlock(credentials), {
        status: 401,
        code: "unauthenticated",
        message,
      });
      await assert.rejects(
        facade.unlock({ ...credentials, workspacePassphrase: "right-passphrase" }),
        { status: 401, code: "unauthenticated", message },
      );
      // A failed sign-in closes the workspace it opened.
      assert.equal(locks, 1);
    },
    {
      workspaceId: ids.workspace,
      setupId: ids.setup,
      stage: "ready",
      organisationId: ids.organisation,
    },
  );
});

const ready = {
  workspaceId: ids.workspace,
  setupId: ids.setup,
  stage: "ready",
  organisationId: ids.organisation,
};
const unlockDetails = {
  workspacePassphrase: "workspace-passphrase-long-enough",
  username: "admin@tammy.local",
  administratorPassword: "administrator-password-long-enough",
};

/** Clients for a completed workspace that another browser already has open. */
function openWorkspaceClients(calls) {
  let state = WorkspaceState.AUTHENTICATED;
  let signIns = 0;
  return {
    workspace: {
      getWorkspaceState: async () => ({ workspace: { id: ids.workspace, state } }),
      lockWorkspace: async () => {
        calls.push("lock");
        state = WorkspaceState.LOCKED;
        return {};
      },
      unlockWorkspace: async (request) => {
        if (
          new TextDecoder().decode(request.proof.proof.value.utf8) !==
          unlockDetails.workspacePassphrase
        ) {
          calls.push("unlock:rejected");
          throw fault("AUTHENTICATION_REQUIRED");
        }
        calls.push("unlock");
        state = WorkspaceState.UNAUTHENTICATED;
        return {};
      },
    },
    identity: {
      signIn: async (request) => {
        if (
          new TextDecoder().decode(request.password.utf8) !== unlockDetails.administratorPassword
        ) {
          calls.push("signIn:rejected");
          throw fault("AUTHENTICATION_REQUIRED");
        }
        signIns++;
        calls.push(`signIn:${signIns}`);
        state = WorkspaceState.AUTHENTICATED;
        return { user: { id: ids.user }, session: { id: `session-${signIns}` } };
      },
      signOut: async () => {
        calls.push("signOut");
        return {};
      },
    },
    organisation: {
      getOrganisation: async () => ({
        organisation: { id: ids.organisation, displayName: "Example" },
      }),
    },
  };
}

test("a takeover checks the credentials before it ends the signed-in session", async () => {
  const calls = [];
  await withFacade(
    openWorkspaceClients(calls),
    async (facade) => {
      // Missing details fail before anything reaches the core.
      await assert.rejects(facade.unlock({}), {
        status: 400,
        message: "Enter the workspace passphrase.",
      });
      await assert.rejects(facade.unlock({ ...unlockDetails, administratorPassword: "" }), {
        status: 400,
      });
      assert.deepEqual(calls, []);
      // Wrong credentials leave the open workspace and its session alone.
      await assert.rejects(
        facade.unlock({ ...unlockDetails, administratorPassword: "wrong-administrator-password" }),
        { status: 401, code: "unauthenticated" },
      );
      assert.deepEqual(calls.splice(0), ["signIn:rejected"]);
      // Right credentials sign in first, which ends the other session in the core. Locking and
      // unlocking then checks the passphrase, so a wrong one leaves the workspace locked.
      await assert.rejects(
        facade.unlock({ ...unlockDetails, workspacePassphrase: "wrong-workspace-passphrase" }),
        { status: 401, code: "unauthenticated" },
      );
      assert.deepEqual(calls.splice(0), ["signIn:1", "lock", "unlock:rejected"]);
      // A locked workspace is unlocked without a takeover.
      const result = await facade.unlock(unlockDetails);
      assert.equal(result.authentication.sessionId, "session-2");
      assert.deepEqual(calls.splice(0), ["unlock", "signIn:2"]);
      // An open one is taken over.
      assert.equal((await facade.unlock(unlockDetails)).authentication.sessionId, "session-4");
      assert.deepEqual(calls, ["signIn:3", "lock", "unlock", "signIn:4"]);
    },
    ready,
  );
});

test("takeover attempts stop before they could lock the administrator account", async () => {
  const calls = [];
  let now = deadline;
  await withFacade(
    { ...openWorkspaceClients(calls), now: () => now },
    async (facade) => {
      const wrong = { ...unlockDetails, administratorPassword: "wrong-administrator-password" };
      for (let attempt = 0; attempt < 3; attempt++)
        await assert.rejects(facade.unlock(wrong), { status: 401 });
      // The core locks the account, and every live session of it, at 5 failures in 15 minutes.
      await assert.rejects(facade.unlock(unlockDetails), {
        status: 429,
        code: "resource_exhausted",
        message: "Too many failed attempts. Wait 15 minutes before trying again.",
      });
      assert.deepEqual(calls.splice(0), Array(3).fill("signIn:rejected"));
      // The core forgets a failure after 15 minutes, and so does the takeover limit.
      now += 15 * 60 * 1000;
      await facade.unlock(unlockDetails);
      assert.deepEqual(calls, ["signIn:1", "lock", "unlock", "signIn:2"]);
    },
    ready,
  );
});

test("a logout waits for a takeover and then leaves the newer session open", async () => {
  const calls = [];
  const clients = openWorkspaceClients(calls);
  const unlockWorkspace = clients.workspace.unlockWorkspace;
  let gate;
  clients.workspace.unlockWorkspace = async (request) => {
    await gate?.promise;
    return unlockWorkspace(request);
  };
  await withFacade(
    clients,
    async (facade) => {
      const first = await facade.unlock(unlockDetails);
      calls.length = 0;
      gate = Promise.withResolvers();
      const second = facade.unlock(unlockDetails);
      const loggedOut = facade.logout({ authentication: first.authentication });
      await new Promise((resolve) => setImmediate(resolve));
      // The takeover waits at the passphrase check, and the logout has not run.
      assert.deepEqual(calls, ["signIn:3", "lock"]);
      gate.resolve();
      const { authentication } = await second;
      await loggedOut;
      // The earlier session's logout neither signs out nor locks the newer session.
      assert.deepEqual(calls.splice(0), ["signIn:3", "lock", "unlock", "signIn:4"]);
      await facade.logout({ authentication });
      assert.deepEqual(calls, ["signOut", "lock"]);
    },
    ready,
  );
});
