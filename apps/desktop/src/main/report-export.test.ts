// @vitest-environment node
import { mkdtemp, readdir, readFile, rm, stat, symlink } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { create } from "@bufbuild/protobuf";
import { UserSchema, UserState } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import { BusinessIncomeSummarySchema } from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { OrganisationSchema } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { BasWorkpaperSchema } from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { afterEach, expect, it, vi } from "vitest";
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
  expect((await stat(f.filePath)).mode & 0o777).toBe(0o600);
  expect(await readFile(f.filePath, "utf8")).toContain('"minorUnits":"10001"');
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
