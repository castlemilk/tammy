import { create } from "@bufbuild/protobuf";
import {
  GetInvoiceLogoRequestSchema,
  GetInvoiceLogoResponseSchema,
  GetInvoiceTemplateRequestSchema,
  GetInvoiceTemplateResponseSchema,
  SaveInvoiceTemplateRequestSchema,
  SaveInvoiceTemplateResponseSchema,
  UploadInvoiceLogoRequestSchema,
  UploadInvoiceLogoResponseSchema,
} from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import { createCaller } from "electron-conveyor/main";
import { expect, it, vi } from "vitest";
import { createProtoMethodCodec } from "../../shared/proto-ipc";
import { createTammyRouter } from "../router";

vi.mock("electron", () => ({ app: { isPackaged: true } }));

it("forwards the authenticated template and logo frames with independent bounded responses", async () => {
  const client = {
    getInvoiceTemplate: vi.fn(async () =>
      create(GetInvoiceTemplateResponseSchema, { template: { version: 1n } }),
    ),
    saveInvoiceTemplate: vi.fn(async () =>
      create(SaveInvoiceTemplateResponseSchema, { template: { version: 2n } }),
    ),
    uploadInvoiceLogo: vi.fn(async () =>
      create(UploadInvoiceLogoResponseSchema, {
        logo: { id: "01900000-0000-7000-8000-000000000001" },
      }),
    ),
    getInvoiceLogo: vi.fn(async () =>
      create(GetInvoiceLogoResponseSchema, { content: new Uint8Array([1, 2, 3]) }),
    ),
  };
  const caller = createCaller(createTammyRouter(), {
    ctx: { client: client as never, fileIntake: {} as never },
  });
  const get = createProtoMethodCodec({
    input: GetInvoiceTemplateRequestSchema,
    output: GetInvoiceTemplateResponseSchema,
    maximumRequestBytes: 8192,
    maximumResponseBytes: 32768,
  });
  const save = createProtoMethodCodec({
    input: SaveInvoiceTemplateRequestSchema,
    output: SaveInvoiceTemplateResponseSchema,
    maximumRequestBytes: 32768,
    maximumResponseBytes: 32768,
  });
  const upload = createProtoMethodCodec({
    input: UploadInvoiceLogoRequestSchema,
    output: UploadInvoiceLogoResponseSchema,
    maximumRequestBytes: 1179648,
    maximumResponseBytes: 8192,
  });
  const logo = createProtoMethodCodec({
    input: GetInvoiceLogoRequestSchema,
    output: GetInvoiceLogoResponseSchema,
    maximumRequestBytes: 8192,
    maximumResponseBytes: 1179648,
  });
  const context = {
    organisationId: "01900000-0000-7000-8000-000000000002",
    authentication: {
      actorUserId: "01900000-0000-7000-8000-000000000003",
      sessionId: "01900000-0000-7000-8000-000000000004",
    },
  };
  expect(
    get.decodeResponse(
      await caller.accounting.getInvoiceTemplate(
        get.encodeRequest(create(GetInvoiceTemplateRequestSchema, context)),
      ),
    ).template?.version,
  ).toBe(1n);
  expect(
    save.decodeResponse(
      await caller.accounting.saveInvoiceTemplate(
        save.encodeRequest(
          create(SaveInvoiceTemplateRequestSchema, {
            organisationId: context.organisationId,
            commandContext: {
              authentication: context.authentication,
              idempotencyKey: "01900000-0000-7000-8000-000000000005",
            },
            defaults: { paymentInstructions: "BSB 123-456", paymentTermsDays: 30 },
          }),
        ),
      ),
    ).template?.version,
  ).toBe(2n);
  const content = new Uint8Array([1, 2, 3]);
  expect(
    upload.decodeResponse(
      await caller.accounting.uploadInvoiceLogo(
        upload.encodeRequest(
          create(UploadInvoiceLogoRequestSchema, {
            organisationId: context.organisationId,
            commandContext: { authentication: context.authentication },
            mimeType: "image/png",
            content,
          }),
        ),
      ),
    ).logo?.id,
  ).toBeDefined();
  expect(
    logo.decodeResponse(
      await caller.accounting.getInvoiceLogo(
        logo.encodeRequest(
          create(GetInvoiceLogoRequestSchema, {
            ...context,
            logoAssetId: "01900000-0000-7000-8000-000000000001",
          }),
        ),
      ),
    ).content,
  ).toEqual(content);
  expect(client.getInvoiceTemplate).toHaveBeenCalledWith(
    create(GetInvoiceTemplateRequestSchema, context),
  );
  expect(client.uploadInvoiceLogo).toHaveBeenCalledWith(
    expect.objectContaining({ content, mimeType: "image/png" }),
  );
  await expect(caller.accounting.uploadInvoiceLogo(new Uint8Array(1179649))).rejects.toThrow();
  expect(client.uploadInvoiceLogo).toHaveBeenCalledTimes(1);
  client.getInvoiceLogo.mockResolvedValue(
    create(GetInvoiceLogoResponseSchema, { content: new Uint8Array(1179649) }),
  );
  await expect(
    caller.accounting.getInvoiceLogo(
      logo.encodeRequest(create(GetInvoiceLogoRequestSchema, context)),
    ),
  ).rejects.toMatchObject({ message: "CORE_REQUEST_FAILED" });
});
