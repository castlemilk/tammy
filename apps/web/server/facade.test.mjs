import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { create } from "@bufbuild/protobuf";
import {
  DocumentSchema,
  ListDocumentsResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { PurchaseReviewStatus } from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import {
  BasPurchaseReviewKind,
  BasPurchaseSourceFormat,
  BasWorkpaperSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { createFacade } from "./facade.mjs";

const ids = {
  workpaper: "018f0000-0000-7000-8000-000000000001",
  organisation: "018f0000-0000-7000-8000-000000000002",
  reviewed: "018f0000-0000-7000-8000-000000000003",
  changed: "018f0000-0000-7000-8000-000000000004",
  posted: "018f0000-0000-7000-8000-000000000005",
  workspace: "018f0000-0000-7000-8000-000000000006",
  setup: "018f0000-0000-7000-8000-000000000007",
};
const session = {
  authentication: { actorUserId: ids.setup, sessionId: ids.workspace },
  organisation: { id: ids.organisation, name: "Example" },
};
const aud = (minorUnits) => ({ currencyCode: "AUD", minorUnits });
const day = (month, dayOfMonth) => ({ year: 2024, month, day: dayOfMonth });

async function withFacade(client, run, metadata) {
  const dataRoot = await mkdtemp(path.join(os.tmpdir(), "tammy-web-facade-"));
  try {
    if (metadata)
      await writeFile(path.join(dataRoot, "web-workspace.json"), JSON.stringify(metadata));
    await run(await createFacade({ client, dataRoot }));
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

function legacyCandidate(documentId, reviewedVersion, unclaimed) {
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
    invoiceDate: day(5, 12),
    eligibleGst: aud(unclaimed),
    unclaimedGst: aud(unclaimed),
    reason: "Legacy source needs review of eligibility, settlement and prior GST claims.",
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
  await withFacade(client, async (facade) => {
    const { workpaper: result } = await facade.bas(session);
    assert.equal(documentReads, 1);
    assert.equal(result.gstCredits1bMinor, 500);
    assert.equal(result.candidateGstMinor, 3900);
    assert.equal(result.salesG1Minor, null);
    assert.equal(result.netGstPayableMinor, null);
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
    assert.match(result.limitations[1], /provisional GST credit candidates, not claimed credits/);
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
  await withFacade(client, async (facade) => {
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

test("workspace state conflicts carry the documented conflict code", async () => {
  const conflict = (message) => ({ status: 409, code: "conflict", message });
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
  let finish;
  const client = {
    createWorkspace: () =>
      new Promise((resolve) => {
        finish = () =>
          resolve({
            workspace: { id: ids.workspace },
            recoverySecret: { utf8: new TextEncoder().encode("ABCD-EFGH") },
          });
      }),
  };
  const details = {
    workspacePassphrase: "workspace-passphrase-long-enough",
    username: "admin@tammy.local",
    displayName: "Tammy Admin",
    administratorPassword: "administrator-password-long-enough",
  };
  await withFacade(client, async (facade) => {
    const first = facade.create(details);
    await assert.rejects(
      facade.create(details),
      conflict("A workspace operation is already in progress."),
    );
    finish();
    assert.equal((await first).recoveryCode, "ABCD-EFGH");
  });
});
