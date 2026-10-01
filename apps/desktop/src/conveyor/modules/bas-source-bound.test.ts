import { create } from "@bufbuild/protobuf";
import {
  BasWorkpaperSchema,
  CreateBasDraftRequestSchema,
  CreateBasDraftResponseSchema,
  GetCurrentBasDraftRequestSchema,
  GetCurrentBasDraftResponseSchema,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { createCaller } from "electron-conveyor/main";
import { expect, it, vi } from "vitest";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { createTammyRouter } from "../router";

vi.mock("electron", () => ({ app: { isPackaged: true } }));

it("returns all permitted sales and purchase sources with maximum Unicode identities", async () => {
  const id = "01900000-0000-7000-8000-000000000001";
  const date = { year: 2026, month: 10, day: 2 };
  const aud = { currencyCode: "AUD", minorUnits: 100n };
  const workpaper = create(BasWorkpaperSchema, {
    id,
    organisationId: id,
    version: 1n,
    periodStart: date,
    periodEnd: date,
    salesSources: Array.from({ length: 1000 }, () => ({
      invoiceId: id,
      invoiceNumber: "🧾".repeat(40),
      customerName: "🧾".repeat(200),
      taxDate: date,
      gross: aud,
      gst: aud,
      sourceRevision: 1n,
      sourceHash: new Uint8Array(32),
    })),
    sources: Array.from({ length: 1000 }, () => ({
      documentId: id,
      supplierName: "🧾".repeat(256),
      invoiceNumber: "🧾".repeat(128),
      documentDate: date,
      gross: aud,
      gstCredit: aud,
    })),
  });
  const get = createProtoMethodCodec({
    input: GetCurrentBasDraftRequestSchema,
    output: GetCurrentBasDraftResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 4_194_304,
  });
  const save = createProtoMethodCodec({
    input: CreateBasDraftRequestSchema,
    output: CreateBasDraftResponseSchema,
    maximumRequestBytes: 16_384,
    maximumResponseBytes: 4_194_304,
  });
  const caller = createCaller(createTammyRouter(), {
    ctx: {
      client: {
        getCurrentBasDraft: async () => create(GetCurrentBasDraftResponseSchema, { workpaper }),
        createBasDraft: async () => create(CreateBasDraftResponseSchema, { workpaper }),
      } as never,
      fileIntake: {} as never,
    },
  });
  const frame = await caller.reporting.getCurrentBasDraft(
    get.encodeRequest(create(GetCurrentBasDraftRequestSchema)),
  );
  expect(frame.byteLength).toBeGreaterThan(1_048_576);
  expect(get.decodeResponse(frame).workpaper?.salesSources).toHaveLength(1000);
  expect(get.decodeResponse(frame).workpaper?.sources).toHaveLength(1000);
  const saved = save.decodeResponse(
    await caller.reporting.createBasDraft(save.encodeRequest(create(CreateBasDraftRequestSchema))),
  );
  expect(saved.workpaper?.salesSources).toHaveLength(1000);
  expect(saved.workpaper?.sources).toHaveLength(1000);
});
