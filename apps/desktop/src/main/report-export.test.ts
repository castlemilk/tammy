// @vitest-environment node
import { mkdtemp, readdir, readFile, rm, stat, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { create } from "@bufbuild/protobuf";
import { MoneySchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import { UserSchema, UserState } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { BusinessIncomeSummarySchema } from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { OrganisationSchema } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import {
  LegacyPurchaseOpeningPayableRefSchema,
  LegacyPurchaseSourceRefSchema,
  PurchaseClaimPeriodSchema,
} from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import type { BasWorkpaper } from "@tammy/connect-client/tammy/v1/tax_pb.js";
import {
  BasPurchaseLegacyReviewSchema,
  BasPurchaseLegacySupportSchema,
  BasPurchaseReviewSchema,
  BasPurchaseSourceLineSchema,
  BasWorkpaperSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { afterEach, expect, it, vi } from "vitest";
import { assertPurchasePdfRows } from "../../tests/e2e/support/purchase-export-assertions.js";
import { createReportExports } from "./report-export";

const directories: string[] = [];
afterEach(async () => {
  await Promise.all(
    directories.splice(0).map((directory) => rm(directory, { recursive: true, force: true })),
  );
});
const orgId = "018f0000-0000-7000-8000-000000000004",
  basId = "018f0000-0000-7000-8000-000000000005";
const start = { year: 2026, month: 10, day: 1 },
  end = { ...start, day: 31 };
const money = (minorUnits = 0n) => ({ currencyCode: "AUD", minorUnits });
const hash = (byte: number) => new Uint8Array(32).fill(byte);
const id = (index: number) => `018f0000-0000-7000-8000-${String(index).padStart(12, "0")}`;
async function fixture() {
  const directory = await mkdtemp(path.join(tmpdir(), "tammy-report-export-"));
  directories.push(directory);
  const organisation = create(OrganisationSchema, {
    id: orgId,
    version: 2n,
    legalName: "Retained Name",
    displayName: "Retained Display",
    abn: "51824753556",
  });
  const summary = create(BusinessIncomeSummarySchema, {
    organisationId: orgId,
    periodStart: start,
    periodEnd: end,
    asOfDate: start,
    businessIncome: money(10001n),
    invoiceIncome: money(10001n),
    paymentsReceived: money(),
    outstanding: money(11001n),
    invoiceGst: money(1000n),
    unpostedIncome: money(),
    financialRevision: 4n,
    gstBasis: 1,
  });
  const workpaper = create(BasWorkpaperSchema, {
    id: basId,
    organisationId: orgId,
    version: 1n,
    periodStart: start,
    periodEnd: end,
    status: 1,
    salesG1: money(),
    gstOnSales1a: money(),
    gstCredits1b: money(),
    netGstPayable: money(),
    gstBasis: 1,
    createdAt: { seconds: 1791165784n, nanos: 0 },
    sourceFingerprint: new Uint8Array(32),
  });
  const client = {
    getCurrentUser: vi.fn(async () => ({
      user: create(UserSchema, {
        id: "018f0000-0000-7000-8000-000000000002",
        state: UserState.ACTIVE,
      }),
    })),
    getOrganisation: vi.fn(async () => ({ organisation })),
    getBusinessIncomeSummary: vi.fn(async () => ({ summary })),
    getCurrentBasDraft: vi.fn(async () => ({ workpaper })),
  };
  let authorised = true,
    guardCalls = 0,
    failAfter = Infinity;
  const capture = vi.fn(() => () => {
    guardCalls++;
    if (!authorised || guardCalls >= failAfter) throw new Error("LOCAL_SESSION_REQUIRED");
  });
  const filePath = path.join(directory, "report.csv");
  const select = vi.fn(async () => ({ canceled: false, filePath }));
  const renderIncome = vi.fn(async (snapshot: unknown) =>
    new TextEncoder().encode(
      JSON.stringify(snapshot, (_, value) => (typeof value === "bigint" ? String(value) : value)),
    ),
  );
  const renderBas = vi.fn(async (_snapshot: unknown) =>
    new TextEncoder().encode("kind,value\r\nmetadata,fixture\r\n"),
  );
  const exporter = createReportExports({
    client,
    capture,
    select,
    renderIncome,
    renderBas,
    now: () => new Date("2026-10-05T02:03:04.000Z"),
  });
  const common = {
    authentication: {
      actorUserId: "018f0000-0000-7000-8000-000000000002",
      sessionId: "018f0000-0000-7000-8000-000000000003",
    },
    organisationId: orgId,
    expectedOrganisationVersion: "2",
    format: "csv" as const,
    periodStart: "2026-10-01",
    periodEnd: "2026-10-31",
  };
  const income = { ...common, expectedFinancialRevision: "4", asOfDate: "2026-10-01" };
  const bas = { ...common, expectedWorkpaperId: basId, expectedWorkpaperVersion: "1" };
  return {
    directory,
    filePath,
    client,
    summary,
    workpaper,
    organisation,
    select,
    renderIncome,
    renderBas,
    exporter,
    income,
    bas,
    revoke: () => {
      authorised = false;
    },
    failAfter: (count: number) => {
      failAfter = count;
    },
  };
}
it("acquires organisation/report/organisation and writes private exact retained data", async () => {
  const f = await fixture();
  expect(await f.exporter.exportIncome(f.income)).toEqual({ saved: true });
  expect(f.client.getOrganisation).toHaveBeenCalledTimes(2);
  const fileStats = await stat(f.filePath);
  expect(fileStats.isFile()).toBe(true);
  if (process.platform !== "win32") expect(fileStats.mode & 0o777).toBe(0o600);
  const exported = await readFile(f.filePath, "utf8");
  expect(exported).toContain('"minorUnits":"10001"');
  expect(exported).toContain('"legalName":"Retained Name"');
  expect(await readdir(f.directory)).toEqual(["report.csv"]);
});

it.each(["dialog", "render"])(
  "rejects a session expired in core during %s without activating a file",
  async (boundary) => {
    const f = await fixture();
    const expire = () =>
      f.client.getCurrentUser.mockRejectedValue(
        new Error("AUTHENTICATION_REQUIRED private detail"),
      );
    if (boundary === "dialog")
      f.select.mockImplementation(async () => {
        expire();
        return { canceled: false, filePath: f.filePath };
      });
    else
      f.renderIncome.mockImplementation(async () => {
        expire();
        return new TextEncoder().encode("valid retained report");
      });
    await expect(f.exporter.exportIncome(f.income)).rejects.toThrow("REPORT_SESSION_CHANGED");
    expect(await readdir(f.directory)).toEqual([]);
    if (boundary === "dialog") expect(f.renderIncome).not.toHaveBeenCalled();
  },
);

it.each(["wrong-user", "inactive-user", "changed-session"])(
  "requires current core user and captured session before activation: %s",
  async (cause) => {
    const f = await fixture();
    const active = create(UserSchema, {
      id: f.income.authentication.actorUserId,
      state: UserState.ACTIVE,
    });
    f.client.getCurrentUser
      .mockResolvedValueOnce({ user: active })
      .mockImplementationOnce(async () => {
        if (cause === "changed-session") f.revoke();
        return {
          user: {
            ...active,
            id: cause === "wrong-user" ? basId : active.id,
            state: cause === "inactive-user" ? UserState.AUTHENTICATION_LOCKED : active.state,
          },
        };
      });
    await expect(f.exporter.exportBas(f.bas)).rejects.toThrow("REPORT_SESSION_CHANGED");
    expect(await readdir(f.directory)).toEqual([]);
  },
);

it("revalidates core authorisation after native selection and before activation without rereading report or profile", async () => {
  const f = await fixture();
  await f.exporter.exportIncome(f.income);
  expect(f.client.getCurrentUser).toHaveBeenCalledTimes(2);
  expect(f.client.getCurrentUser).toHaveBeenLastCalledWith(
    expect.objectContaining({ authentication: expect.objectContaining(f.income.authentication) }),
  );
  expect(f.client.getOrganisation).toHaveBeenCalledTimes(2);
  expect(f.client.getBusinessIncomeSummary).toHaveBeenCalledTimes(1);
});
it.each(["before", "between"])(
  "requires refresh for profile version change %s acquisition",
  async (timing) => {
    const f = await fixture();
    if (timing === "before") f.organisation.version = 3n;
    else
      f.client.getBusinessIncomeSummary.mockImplementation(async () => {
        f.organisation.version = 3n;
        return { summary: f.summary };
      });
    await expect(f.exporter.exportIncome(f.income)).rejects.toThrow("REPORT_REFRESH_REQUIRED");
    expect(f.select).not.toHaveBeenCalled();
  },
);
it("requires requested organisation ID on both profile reads", async () => {
  const f = await fixture();
  f.client.getOrganisation
    .mockResolvedValueOnce({ organisation: f.organisation })
    .mockResolvedValueOnce({ organisation: { ...f.organisation, id: basId } });
  await expect(f.exporter.exportIncome(f.income)).rejects.toThrow("REPORT_REFRESH_REQUIRED");
});
it("requires displayed revision, dates and saved BAS identity without regenerating", async () => {
  const f = await fixture();
  await expect(
    f.exporter.exportIncome({ ...f.income, expectedFinancialRevision: "5" }),
  ).rejects.toThrow("REPORT_REFRESH_REQUIRED");
  await expect(f.exporter.exportIncome({ ...f.income, asOfDate: "2026-10-02" })).rejects.toThrow(
    "REPORT_REFRESH_REQUIRED",
  );
  await expect(f.exporter.exportBas({ ...f.bas, expectedWorkpaperVersion: "2" })).rejects.toThrow(
    "REPORT_REFRESH_REQUIRED",
  );
  expect(f.select).not.toHaveBeenCalled();
});
it("later profile and ledger changes cannot rewrite the retained snapshot", async () => {
  const f = await fixture();
  f.select.mockImplementation(async () => {
    f.organisation.legalName = "Later Name";
    f.organisation.version = 3n;
    if (!f.summary.businessIncome) throw new Error("Missing income");
    f.summary.businessIncome.minorUnits = 999n;
    return { canceled: false, filePath: f.filePath };
  });
  await f.exporter.exportIncome(f.income);
  const bytes = await readFile(f.filePath, "utf8");
  expect(bytes).toContain("Retained Name");
  expect(bytes).not.toContain("Later Name");
  expect(bytes).toContain('"minorUnits":"10001"');
});
it("cancellation creates no file and does not render", async () => {
  const f = await fixture();
  f.select.mockResolvedValue({ canceled: true, filePath: f.filePath });
  expect(await f.exporter.exportIncome(f.income)).toEqual({ saved: false });
  expect(f.renderIncome).not.toHaveBeenCalled();
  expect(await readdir(f.directory)).toEqual([]);
});
it.each(["profile-before", "report", "profile-after", "dialog", "render"])(
  "session changes at %s boundary cancel with no file",
  async (boundary) => {
    const f = await fixture();
    if (boundary === "profile-before")
      f.client.getOrganisation.mockImplementationOnce(async () => {
        f.revoke();
        return { organisation: f.organisation };
      });
    if (boundary === "report")
      f.client.getBusinessIncomeSummary.mockImplementation(async () => {
        f.revoke();
        return { summary: f.summary };
      });
    if (boundary === "profile-after")
      f.client.getOrganisation
        .mockResolvedValueOnce({ organisation: f.organisation })
        .mockImplementationOnce(async () => {
          f.revoke();
          return { organisation: f.organisation };
        });
    if (boundary === "dialog")
      f.select.mockImplementation(async () => {
        f.revoke();
        return { canceled: false, filePath: f.filePath };
      });
    if (boundary === "render")
      f.renderIncome.mockImplementation(async () => {
        f.revoke();
        return new Uint8Array([65]);
      });
    await expect(f.exporter.exportIncome(f.income)).rejects.toThrow("REPORT_SESSION_CHANGED");
    expect(await readdir(f.directory)).toEqual([]);
  },
);
it("rejects symlink/directory/relative/wrong extension and leaves no temporary files", async () => {
  const f = await fixture();
  await symlink(path.join(f.directory, "elsewhere"), f.filePath);
  await expect(f.exporter.exportIncome(f.income)).rejects.toThrow("REPORT_SAVE_FAILED");
  await rm(f.filePath);
  for (const filePath of [f.directory, "relative.csv", path.join(f.directory, "wrong.pdf")]) {
    f.select.mockResolvedValue({ canceled: false, filePath });
    await expect(f.exporter.exportIncome(f.income)).rejects.toThrow("REPORT_SAVE_FAILED");
  }
  expect(await readdir(f.directory)).toEqual([]);
});
it("bounds render size and errors without private source text", async () => {
  const f = await fixture();
  f.renderIncome.mockResolvedValue(new Uint8Array(16 * 1024 * 1024 + 1));
  await expect(f.exporter.exportIncome(f.income)).rejects.toThrow("REPORT_SIZE_LIMIT");
  f.renderIncome.mockRejectedValue(new Error("private document secret /private/path"));
  await expect(f.exporter.exportIncome(f.income)).rejects.toThrow(/^REPORT_RENDER_FAILED$/);
  expect(await readdir(f.directory)).toEqual([]);
});

it.each([11, 12, 13, 14, 15, 16, 17, 18])(
  "session revocation at the guarded write/activation boundary %s cleans the temporary file before activation",
  async (count) => {
    const f = await fixture();
    f.failAfter(count);
    await expect(f.exporter.exportIncome(f.income)).rejects.toThrow("REPORT_SESSION_CHANGED");
    expect(await readdir(f.directory)).toEqual([]);
  },
);
it("copies caller request before any asynchronous work", async () => {
  const f = await fixture();
  f.client.getOrganisation.mockImplementationOnce(async () => {
    f.income.expectedFinancialRevision = "99";
    f.income.asOfDate = "2026-10-02";
    return { organisation: f.organisation };
  });
  await expect(f.exporter.exportIncome(f.income)).resolves.toEqual({ saved: true });
});
it("exports the stale saved BAS and retained warnings without replacing it", async () => {
  const f = await fixture();
  f.workpaper.sourcesChanged = true;
  f.workpaper.reviewWarnings = ["retained review warning"];
  await expect(f.exporter.exportBas(f.bas)).resolves.toEqual({ saved: true });
  expect(f.renderBas.mock.calls[0]?.[0]).toMatchObject({
    workpaper: { id: basId, sourcesChanged: true, reviewWarnings: ["retained review warning"] },
  });
});
it("captures and retains the exact typed purchase workpaper before native selection", async () => {
  const f = await fixture();
  const period = create(PurchaseClaimPeriodSchema, { periodStart: start, periodEnd: end });
  const source = create(BasPurchaseSourceLineSchema, {
    postingId: id(20),
    postingHash: hash(1),
    documentId: id(21),
    originalContentHash: hash(2),
    reviewedSnapshotHash: hash(3),
    paymentId: id(22),
    paymentHash: hash(4),
    allocationSequence: 1,
    accountId: id(23),
    accountVersion: 5n,
    supplierName: "Retained Supplier",
    invoiceNumber: "PUR-20",
    use: 1,
    basClassification: 2,
    cashFlowClassification: 1,
    invoiceDate: start,
    actualPaymentDate: { ...start, day: 2 },
    normalTaxEventDate: { ...start, day: 2 },
    releasePostingDate: { ...start, day: 3 },
    claimPeriod: period,
    allocatedGross: money(11000n),
    supplierGst: money(1000n),
    eligibleGst: money(1000n),
    claimedGst: money(),
    evidence: { sourceId: id(24), sourceVersion: 2n, sourceHash: hash(5) },
    sourceRevision: 3n,
    sourceHash: hash(6),
  });
  const legacySource = create(LegacyPurchaseSourceRefSchema, {
    source: {
      case: "openingPayable",
      value: create(LegacyPurchaseOpeningPayableRefSchema, {
        openingPayableId: id(25),
        sourceVersion: 4n,
        sourceHash: hash(7),
      }),
    },
  });
  const legacyReview = create(BasPurchaseLegacyReviewSchema, {
    id: id(26),
    organisationId: orgId,
    source: legacySource,
    affectedPeriod: period,
    reportingBalanceDate: end,
    disposition: 1,
    reason: "Retained opening source decision.",
    sourceRevision: 8n,
    sourceHash: hash(8),
    actorUserId: id(27),
    recordedAt: { seconds: 1791165784n, nanos: 123456789 },
  });
  const review = create(BasPurchaseReviewSchema, {
    legacySource,
    status: 4,
    affectedPeriod: period,
    eligibleGst: money(700n),
    unclaimedGst: money(700n),
    legacyReview,
    sourceRevision: 9n,
    sourceHash: hash(9),
    reason: "Retained legacy review reason.",
    kind: 11,
  });
  const support = create(BasPurchaseLegacySupportSchema, {
    reviewId: id(26),
    kind: 1,
    sequence: 1,
    source: { sourceId: id(28), sourceVersion: 10n, sourceHash: hash(10) },
  });
  f.workpaper.purchaseSourceFormat = 1;
  f.workpaper.legacyCandidateGst = create(MoneySchema, {
    currencyCode: "AUD",
    minorUnits: 123n,
  });
  f.workpaper.purchaseSources.push(source);
  f.workpaper.purchaseReviews.push(review);
  f.workpaper.legacyPurchaseSupport.push(support);
  f.select.mockImplementation(async () => {
    source.supplierName = "Later Supplier";
    source.postingHash.fill(99);
    review.reason = "Later review reason.";
    legacyReview.reason = "Later legacy decision.";
    support.source?.sourceHash.fill(98);
    if (f.workpaper.legacyCandidateGst) f.workpaper.legacyCandidateGst.minorUnits = 999n;
    return { canceled: false, filePath: f.filePath };
  });
  await expect(f.exporter.exportBas(f.bas)).resolves.toEqual({ saved: true });
  const snapshot = f.renderBas.mock.calls[0]?.[0] as {
    workpaper: {
      id: string;
      sourceFingerprint: Uint8Array;
      purchaseSources: { supplierName: string; postingHash: Uint8Array; accountId: string }[];
      purchaseReviews: { reason: string; legacyReview?: { reason: string } }[];
      legacyPurchaseSupport: { source?: { sourceHash: Uint8Array } }[];
      legacyCandidateGst?: { minorUnits: bigint };
    };
  };
  expect(snapshot.workpaper.id).toBe(basId);
  expect(snapshot.workpaper.sourceFingerprint).toEqual(new Uint8Array(32));
  expect(snapshot.workpaper.purchaseSources[0]?.supplierName).toBe("Retained Supplier");
  expect(snapshot.workpaper.purchaseSources[0]?.accountId).toBe(id(23));
  expect(snapshot.workpaper.purchaseSources[0]?.postingHash).toEqual(hash(1));
  expect(snapshot.workpaper.purchaseSources[0]?.postingHash).not.toBe(source.postingHash);
  expect(snapshot.workpaper.purchaseReviews[0]?.reason).toBe("Retained legacy review reason.");
  expect(snapshot.workpaper.purchaseReviews[0]?.legacyReview?.reason).toBe(
    "Retained opening source decision.",
  );
  expect(snapshot.workpaper.legacyPurchaseSupport[0]?.source?.sourceHash).toEqual(hash(10));
  expect(snapshot.workpaper.legacyCandidateGst?.minorUnits).toBe(123n);
  expect(Object.isFrozen(snapshot.workpaper.purchaseSources)).toBe(true);
  expect(Object.isFrozen(snapshot.workpaper.purchaseSources[0])).toBe(true);
  expect(f.client.getCurrentBasDraft).toHaveBeenCalledTimes(1);
});

it("rejects missing, duplicated, or amount-swapped purchase PDF rows", () => {
  const source = (index: number, claimed: bigint) => ({
    postingId: id(index),
    postingHash: hash(index),
    documentId: id(index + 1),
    originalContentHash: hash(index + 1),
    reviewedSnapshotHash: hash(index + 2),
    paymentId: id(index + 3),
    paymentHash: hash(index + 3),
    attributionId: id(index + 4),
    attributionHash: hash(index + 4),
    allocationSequence: index,
    accountId: id(index + 5),
    accountVersion: 6n,
    supplierName: `Supplier ${index}`,
    invoiceNumber: `PUR-${index}`,
    use: 1,
    basClassification: 1,
    cashFlowClassification: 1,
    invoiceDate: start,
    actualPaymentDate: { ...start, day: 2 },
    normalTaxEventDate: { ...start, day: 2 },
    releasePostingDate: { ...start, day: 3 },
    claimPeriod: { periodStart: start, periodEnd: end },
    allocatedGross: money(10000n),
    supplierGst: money(1000n),
    eligibleGst: money(1000n),
    claimedGst: money(claimed),
    evidence: { sourceId: id(index + 6), sourceVersion: 7n, sourceHash: hash(index + 6) },
    sourceRevision: 8n,
    sourceHash: hash(index + 7),
  });
  const first = source(101, 50n);
  const second = source(201, 200n);
  const legacy = {
    documentId: id(401),
    supplierName: "Legacy supplier",
    invoiceNumber: "LEG-1",
    documentDate: start,
    gross: money(1100n),
    gstCredit: money(100n),
  };
  const bas = {
    purchaseSourceFormat: 1,
    sources: [legacy],
    purchaseSources: [first, second],
    purchaseReviews: [],
    legacyPurchaseSupport: [],
    gstCredits1b: money(350n),
  } as unknown as BasWorkpaper;
  const hex = (value: Uint8Array) => Buffer.from(value).toString("hex");
  const typedRow = (value: typeof first) =>
    [
      `Source allocation ${value.postingId} sequence ${value.allocationSequence} · document ${value.documentId}`,
      `Supplier ${value.supplierName} · Invoice ${value.invoiceNumber}`,
      `Posting ${value.postingId} · posting hash ${hex(value.postingHash)}`,
      `Original content hash ${hex(value.originalContentHash)} · reviewed snapshot hash ${hex(value.reviewedSnapshotHash)}`,
      `Payment ${value.paymentId} · payment hash ${hex(value.paymentHash)}`,
      `Attribution ${value.attributionId} · attribution hash ${hex(value.attributionHash)}`,
      `Allocation ${value.allocationSequence} · account ${value.accountId} · account version ${value.accountVersion}`,
      "Use business · BAS class capital · cash flow operating",
      "Invoice date 2026-10-01 · actual payment date 2026-10-02 · normal tax event 2026-10-02",
      "Selected claim period 2026-10-01 to 2026-10-31 · release posting date 2026-10-03",
      `Allocation amounts · gross allocated $100.00 · supplier GST $10.00 · eligible GST $10.00 · claimed GST $${(value.claimedGst.minorUnits / 100n).toString()}.${String(value.claimedGst.minorUnits % 100n).padStart(2, "0")}`,
      `Evidence ${value.evidence.sourceId} · version ${value.evidence.sourceVersion} · hash ${hex(value.evidence.sourceHash)}`,
      `Source revision ${value.sourceRevision} · source hash ${hex(value.sourceHash)}`,
    ].join(" ");
  const legacyRow = `Legacy purchase row ${legacy.documentId} · supplier ${legacy.supplierName} · invoice ${legacy.invoiceNumber} · source date 2026-10-01 · gross $11.00 · GST credit $1.00`;
  const firstRow = typedRow(first);
  const secondRow = typedRow(second);
  const pages = [legacyRow, firstRow, secondRow, "GST credits 1B $3.50"];

  expect(() => assertPurchasePdfRows(pages, bas)).not.toThrow();
  expect(() => assertPurchasePdfRows([legacyRow, firstRow, "GST credits 1B $3.50"], bas)).toThrow(
    /exactly once/u,
  );
  expect(() =>
    assertPurchasePdfRows([legacyRow, firstRow, firstRow, secondRow, "GST credits 1B $3.50"], bas),
  ).toThrow(/exactly once/u);
  const swappedFirst = firstRow.replace("claimed GST $0.50", "claimed GST $2.00");
  const swappedSecond = secondRow.replace("claimed GST $2.00", "claimed GST $0.50");
  expect(() =>
    assertPurchasePdfRows([legacyRow, swappedFirst, swappedSecond, "GST credits 1B $3.50"], bas),
  ).toThrow(/is missing claimed GST/u);
});
