import { createCaller } from "electron-conveyor/main";
import { expect, it, vi } from "vitest";
import { createTammyRouter } from "../router";

vi.mock("electron", () => ({ app: { isPackaged: true } }));
const id = "018f0000-0000-7000-8000-000000000001";
const common = {
  authentication: { actorUserId: id, sessionId: id },
  organisationId: id,
  expectedOrganisationVersion: "2",
  format: "csv" as const,
  periodStart: "2026-10-01",
  periodEnd: "2026-12-31",
};
const income = { ...common, asOfDate: "2026-10-05", expectedFinancialRevision: "4" },
  bas = { ...common, expectedWorkpaperId: id, expectedWorkpaperVersion: "1" };
function fixture() {
  const reportExports = {
    exportIncome: vi.fn(async () => ({ saved: true })),
    exportBas: vi.fn(async () => ({ saved: false })),
  };
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: {} as never, fileIntake: {} as never, reportExports },
  });
  return { caller, reportExports };
}
it("forwards only typed source selection and returns saved/cancelled without local paths", async () => {
  const f = fixture();
  expect(await f.caller.reporting.exportIncome(income)).toEqual({ saved: true });
  expect(await f.caller.reporting.exportBas(bas)).toEqual({ saved: false });
  expect(f.reportExports.exportIncome).toHaveBeenCalledWith(income);
  expect(f.reportExports.exportBas).toHaveBeenCalledWith(bas);
});
it.each([
  { filePath: "/private/source.csv" },
  { bytes: new Uint8Array(20) },
  { template: "private document" },
  { format: "zip" },
  { expectedOrganisationVersion: "18446744073709551616" },
  { periodStart: "2026-02-31" },
  { authentication: { actorUserId: id, sessionId: id, token: "secret" } },
])("rejects unbounded or renderer-controlled request fields %s", async (changes) => {
  const f = fixture();
  await expect(
    f.caller.reporting.exportIncome({ ...income, ...changes } as never),
  ).rejects.toThrow();
  expect(f.reportExports.exportIncome).not.toHaveBeenCalled();
});
it("sanitises handler failures while retaining distinct supported refresh/render/save errors", async () => {
  const f = fixture();
  f.reportExports.exportIncome.mockRejectedValue(new Error("secret /private/document-body"));
  await expect(f.caller.reporting.exportIncome(income)).rejects.toMatchObject({
    message: "REPORT_SAVE_FAILED",
  });
  f.reportExports.exportBas.mockRejectedValue(new Error("REPORT_REFRESH_REQUIRED"));
  await expect(f.caller.reporting.exportBas(bas)).rejects.toMatchObject({
    message: "REPORT_REFRESH_REQUIRED",
  });
});

it("returns one bounded generic issue for maliciously large private property names", async () => {
  const f = fixture();
  const privateKey = "SYNTHETIC_PRIVATE_SOURCE_".repeat(5000);
  for (const request of [
    { ...income, [privateKey]: "private body" },
    { ...income, authentication: { ...income.authentication, [privateKey]: "private body" } },
  ]) {
    try {
      await f.caller.reporting.exportIncome(request as never);
      throw new Error("Expected rejection");
    } catch (error) {
      const encoded = JSON.stringify(error);
      expect(encoded.length).toBeLessThan(1024);
      expect(encoded).not.toContain("SYNTHETIC_PRIVATE_SOURCE_");
      expect(error).toMatchObject({ code: "INVALID_INPUT" });
    }
  }
  expect(f.reportExports.exportIncome).not.toHaveBeenCalled();
});
