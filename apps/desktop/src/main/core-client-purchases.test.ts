// @vitest-environment node
import { create, type Message } from "@bufbuild/protobuf";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import {
  ErrorCategory,
  RetryClassification,
  ValidationErrorDetailSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import { PurchaseService } from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { expect, it } from "vitest";
import { sanitizeHandlerError } from "../conveyor/error";
import { invokeCore } from "../conveyor/proto";
import { type CoreClient, createCoreClient } from "./core-client";

const readiness = {
  protocol: "tammy-core-ready-v1",
  port: 43219,
  caPem: "TEST",
  capability: "secret",
} as const;

it("authenticates every typed purchase query and command through its generated client", async () => {
  const calls: Array<{ method: string; request: Message; capability: string | null }> = [];
  const implementation = Object.fromEntries(
    PurchaseService.methods.map((method) => [
      method.localName,
      (request: Message, context: { requestHeader: Headers }) => {
        calls.push({
          method: method.localName,
          request,
          capability: context.requestHeader.get("X-Tammy-Capability"),
        });
        return create(method.output);
      },
    ]),
  );
  const client = createCoreClient(readiness, (options) =>
    createRouterTransport((router) => router.service(PurchaseService, implementation), {
      transport: { interceptors: options.interceptors ?? [] },
    }),
  );
  for (const method of PurchaseService.methods) {
    const request = create(method.input, {
      organisationId: "01900000-0000-7000-8000-000000000001",
    });
    const response = await (
      client[method.localName as keyof CoreClient] as (request: Message) => Promise<Message>
    )(request);
    expect(response.$typeName).toBe(method.output.typeName);
    expect(calls.at(-1)).toEqual({ method: method.localName, request, capability: "secret" });
  }
  expect(calls.map(({ method }) => method)).toEqual(
    PurchaseService.methods.map(({ localName }) => localName),
  );
  expect(Object.isFrozen(client)).toBe(true);
});

for (const method of PurchaseService.methods) {
  for (const code of [Code.Canceled, Code.PermissionDenied, Code.FailedPrecondition]) {
    it(`${method.localName} sanitizes ${Code[code]} while preserving its status`, async () => {
      let attempts = 0;
      const client = createCoreClient(readiness, (options) =>
        createRouterTransport(
          (router) =>
            router.service(PurchaseService, {
              [method.localName]: () => {
                attempts += 1;
                throw new ConnectError(
                  "/private/evidence secret=retained",
                  code,
                  new Headers({ Authorization: "secret" }),
                );
              },
            }),
          { transport: { interceptors: options.interceptors ?? [] } },
        ),
      );
      const error = await (
        client[method.localName as keyof CoreClient] as (request: Message) => Promise<Message>
      )(create(method.input)).catch((caught: unknown) => caught);
      expect(error).toBeInstanceOf(ConnectError);
      expect(error).toMatchObject({ code, rawMessage: "Core request failed." });
      expect((error as ConnectError).metadata.get("Authorization")).toBeNull();
      expect(JSON.stringify(error)).not.toMatch(/private|evidence|secret=retained/);
      expect(attempts).toBe(1);
    });
  }
}

it("retains a bounded purchase correction through CoreClient and Conveyor without private server text", async () => {
  const detail = create(ValidationErrorDetailSchema, {
    context: {
      code: "PURCHASE_PAYMENT_INVALID",
      category: ErrorCategory.VALIDATION,
      retry: RetryClassification.AFTER_USER_ACTION,
      correlationId: "01900000-0000-7000-8000-000000000001",
      safeSummary: "private SQL password=secret",
      remediation: "/private/evidence.pdf",
    },
  });
  const client = createCoreClient(readiness, (options) =>
    createRouterTransport(
      (router) =>
        router.service(PurchaseService, {
          recordPurchasePayment: () => {
            throw new ConnectError("private SQL password=secret", Code.InvalidArgument, undefined, [
              { desc: ValidationErrorDetailSchema, value: detail },
            ]);
          },
        }),
      { transport: { interceptors: options.interceptors ?? [] } },
    ),
  );
  const method = PurchaseService.method.recordPurchasePayment;
  const caught = await invokeCore(
    { decodeRequest: () => create(method.input), encodeResponse: () => new Uint8Array([1]) },
    new Uint8Array([1]),
    client.recordPurchasePayment,
  ).catch((error: unknown) => error);
  const sanitized = sanitizeHandlerError(caught);
  expect(sanitized).toMatchObject({
    code: "PURCHASE_PAYMENT_INVALID",
    message: "PURCHASE_PAYMENT_INVALID",
  });
  expect(JSON.stringify(sanitized)).not.toMatch(/private|password|secret|evidence/);
});
