import {
  create,
  type DescMessage,
  fromBinary,
  type Message,
  type MessageInitShape,
  type MessageShape,
  ScalarType,
  toBinary,
} from "@bufbuild/protobuf";
import { AnySchema } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { PurchaseService } from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { createCaller } from "electron-conveyor/main";
import { describe, expect, it, vi } from "vitest";
import { createProtoMethodCodec } from "../../shared/proto-ipc";

vi.mock("electron", () => ({ app: { isPackaged: true } }));

import { createTammyRouter } from "../router";

const maximumResponseBytes = 4 * 1024 * 1024;
const queries = new Set(["getPurchaseAccounting", "listPurchasePayments"]);

function fixture<Schema extends DescMessage>(schema: Schema): MessageShape<Schema> {
  const values: Record<string, unknown> = {};
  const oneofs = new Set<string>();
  for (const field of schema.fields) {
    if (field.oneof && oneofs.has(field.oneof.localName)) continue;
    if (field.oneof) oneofs.add(field.oneof.localName);
    const scalar = (kind: number): unknown =>
      kind === 9
        ? "reviewed"
        : kind === 12
          ? new Uint8Array(32).fill(42)
          : kind === 8
            ? true
            : [3, 4, 6, 16, 18].includes(kind)
              ? 9007199254740993n
              : 1;
    let value: unknown;
    if (field.fieldKind === "message") value = fixture(field.message);
    else if (field.fieldKind === "enum") value = field.enum.values[1]?.number ?? 0;
    else if (field.fieldKind === "scalar") value = scalar(field.scalar);
    else if (field.fieldKind === "list")
      value = [
        field.listKind === "message"
          ? fixture(field.message)
          : field.listKind === "enum"
            ? (field.enum.values[1]?.number ?? 0)
            : scalar(field.scalar),
      ];
    else throw new Error("Unexpected map in purchase schema");
    if (field.oneof) values[field.oneof.localName] = { case: field.localName, value };
    else values[field.localName] = value;
  }
  return create(schema, values as MessageInitShape<Schema>);
}

function frame(schema: DescMessage, value: Uint8Array): Uint8Array {
  return toBinary(
    AnySchema,
    create(AnySchema, { typeUrl: `type.googleapis.com/${schema.typeName}`, value }),
  );
}

type MessagePath = readonly (string | number)[];

function readPath(value: unknown, path: MessagePath): unknown {
  for (const key of path) value = (value as Record<string | number, unknown>)[key];
  return value;
}

function replacePath(message: Message, path: MessagePath, replacement: unknown): void {
  const last = path.at(-1);
  if (last === undefined) throw new Error("Missing fixture path");
  const owner = readPath(message, path.slice(0, -1)) as Record<string | number, unknown>;
  owner[last] = replacement;
}

function representationPaths(schema: DescMessage, value: Message, path: MessagePath = []) {
  const scalars = new Map<ScalarType, MessagePath>();
  const oneofs: MessagePath[] = [];
  function visit(currentSchema: DescMessage, current: unknown, currentPath: MessagePath): void {
    const message = current as Record<string, unknown>;
    for (const oneof of currentSchema.oneofs) oneofs.push([...currentPath, oneof.localName]);
    for (const field of currentSchema.fields) {
      let nextPath = [...currentPath, field.localName];
      let next = message[field.localName];
      if (field.oneof) {
        const selected = message[field.oneof.localName] as { case?: string; value?: unknown };
        if (selected.case !== field.localName) continue;
        nextPath = [...currentPath, field.oneof.localName, "value"];
        next = selected.value;
      }
      if (field.fieldKind === "scalar" && !scalars.has(field.scalar))
        scalars.set(field.scalar, nextPath);
      else if (field.fieldKind === "message") visit(field.message, next, nextPath);
      else if (field.fieldKind === "list") {
        if (field.listKind === "message")
          visit(field.message, (next as unknown[])[0], [...nextPath, 0]);
        else if (field.listKind === "scalar" && !scalars.has(field.scalar))
          scalars.set(field.scalar, [...nextPath, 0]);
      }
    }
  }
  visit(schema, value, path);
  return { scalars, oneofs };
}

const malformedScalars: Readonly<Partial<Record<ScalarType, readonly unknown[]>>> = {
  [ScalarType.BOOL]: ["false", {}, 0, 1, null, undefined],
  [ScalarType.STRING]: [42, {}, null, undefined, "\ud800"],
  [ScalarType.BYTES]: [[42], new ArrayBuffer(32), { byteLength: 32 }, null, undefined],
  [ScalarType.INT64]: [42, "42", null, undefined, -(1n << 63n) - 1n, 1n << 63n],
  [ScalarType.UINT64]: [42, "42", null, undefined, -1n, 1n << 64n],
  [ScalarType.INT32]: [
    "1",
    1.5,
    Number.NaN,
    Number.POSITIVE_INFINITY,
    undefined,
    -(2 ** 31) - 1,
    2 ** 31,
  ],
  [ScalarType.UINT32]: ["1", 1.5, Number.NaN, Number.POSITIVE_INFINITY, undefined, -1, 2 ** 32],
};

const validScalars: Readonly<Partial<Record<ScalarType, readonly unknown[]>>> = {
  [ScalarType.BOOL]: [false, true],
  [ScalarType.STRING]: ["", "Reviewed café 🧾"],
  [ScalarType.BYTES]: [new Uint8Array(), Uint8Array.of(0, 255)],
  [ScalarType.INT64]: [0n, -(1n << 63n), (1n << 63n) - 1n],
  [ScalarType.UINT64]: [0n, (1n << 64n) - 1n],
  [ScalarType.INT32]: [0, -(2 ** 31), 2 ** 31 - 1],
  [ScalarType.UINT32]: [0, 2 ** 32 - 1],
};

describe("purchase Conveyor module", () => {
  it("exposes exactly eight typed methods in the purchases module", () => {
    const router = createTammyRouter();
    expect(router.manifest.purchases).toEqual(
      Object.fromEntries(
        PurchaseService.methods.map((method) => [
          method.localName,
          queries.has(method.localName) ? "query" : "command",
        ]),
      ),
    );
    expect(
      Object.values(router.manifest)
        .flatMap((module) => Object.keys(module))
        .filter((method) => PurchaseService.methods.some(({ localName }) => localName === method)),
    ).toHaveLength(8);
    for (const forbidden of ["ipc", "files", "network"])
      expect(Object.keys(router.modules)).not.toContain(forbidden);
    for (const method of PurchaseService.methods)
      for (const forbidden of ["journal", "snapshot_bytes", "original_content", "path"])
        expect(method.input.fields.map((field) => field.name)).not.toContain(forbidden);
  });

  for (const method of PurchaseService.methods) {
    const maximumRequestBytes = queries.has(method.localName) ? 16_384 : 131_072;
    const codec = createProtoMethodCodec({
      input: method.input,
      output: method.output,
      maximumRequestBytes,
      maximumResponseBytes,
    });
    function harness(call: (request: Message) => Promise<Message>) {
      const caller = createCaller(createTammyRouter(), {
        ctx: { client: { [method.localName]: call } as never, fileIntake: {} as never },
      });
      const invoke = (
        caller as unknown as {
          purchases: Record<string, (frame: Uint8Array) => Promise<Uint8Array>>;
        }
      ).purchases[method.localName];
      if (!invoke) throw new Error(`Missing purchase method ${method.localName}`);
      return invoke;
    }

    const representations = representationPaths(method.output, fixture(method.output));
    for (const [kind, path] of representations.scalars) {
      const invalids = malformedScalars[kind];
      if (!invalids) throw new Error(`Missing scalar fixture kind ${kind}`);
      it(`${method.localName} checks scalar representation ${ScalarType[kind]} at ${path.join(".")}`, async () => {
        for (const invalid of invalids) {
          const response = fixture(method.output);
          replacePath(response, path, invalid);
          const result = await harness(async () => response)(
            codec.encodeRequest(fixture(method.input)),
          ).then(
            () => ({ accepted: true }),
            (error: { code?: string }) => ({ accepted: false, code: error.code }),
          );
          expect(result).toEqual({ accepted: false, code: "CORE_REQUEST_FAILED" });
        }
      });
      it(`${method.localName} preserves scalar boundary values ${ScalarType[kind]}`, async () => {
        for (const valid of validScalars[kind] ?? []) {
          const response = fixture(method.output);
          replacePath(response, path, valid);
          const encoded = await harness(async () => response)(
            codec.encodeRequest(fixture(method.input)),
          );
          expect(codec.decodeResponse(encoded)).toEqual(response);
        }
      });
    }

    for (const path of representations.oneofs) {
      it(`${method.localName} checks oneof consistency at ${path.join(".")}`, async () => {
        const selected = readPath(fixture(method.output), path) as { case: string; value: unknown };
        for (const inconsistent of [
          {},
          { case: undefined, value: selected.value },
          { case: undefined, value: null },
          { case: selected.case },
          { case: selected.case, value: undefined },
        ]) {
          const response = fixture(method.output);
          replacePath(response, path, inconsistent);
          const result = await harness(async () => response)(
            codec.encodeRequest(fixture(method.input)),
          ).then(
            () => ({ accepted: true }),
            (error: { code?: string }) => ({ accepted: false, code: error.code }),
          );
          expect(result).toEqual({ accepted: false, code: "CORE_REQUEST_FAILED" });
        }
      });
      it(`${method.localName} preserves a coherently unset oneof at ${path.join(".")}`, async () => {
        for (const unset of [{ case: undefined }, { case: undefined, value: undefined }]) {
          const response = fixture(method.output);
          replacePath(response, path, unset);
          const encoded = await harness(async () => response)(
            codec.encodeRequest(fixture(method.input)),
          );
          expect(readPath(codec.decodeResponse(encoded), path)).toEqual(unset);
        }
      });
    }

    it(`${method.localName} retains exact nested facts and calls Core once`, async () => {
      const request = fixture(method.input);
      const response = fixture(method.output);
      const call = vi.fn(async () => response);
      expect(codec.decodeResponse(await harness(call)(codec.encodeRequest(request)))).toEqual(
        response,
      );
      expect(call).toHaveBeenCalledExactlyOnceWith(request);
    });

    it(`${method.localName} rejects unknown fields, wrong schema and oversized input before Core`, async () => {
      const call = vi.fn(async () => fixture(method.output));
      const invoke = harness(call);
      const canonical = toBinary(method.input, fixture(method.input));
      const unknown = frame(method.input, Uint8Array.from([...canonical, 0xf8, 0x7f, 0x01]));
      for (const invalid of [
        unknown,
        frame(method.output, new Uint8Array()),
        Uint8Array.of(0xff),
      ]) {
        await expect(invoke(invalid)).rejects.toMatchObject({
          code: "INVALID_RPC_REQUEST",
          message: "INVALID_RPC_REQUEST",
        });
      }
      await expect(invoke(new Uint8Array(maximumRequestBytes + 1))).rejects.toMatchObject({
        code: "INVALID_INPUT",
      });
      expect(call).not.toHaveBeenCalled();
    });

    it(`${method.localName} rejects malformed Core output without exposing retained facts`, async () => {
      const known = toBinary(method.output, fixture(method.output));
      const unknownWireFact = fromBinary(
        method.output,
        Uint8Array.from([...known, 0xf8, 0x7f, 0x01]),
      );
      for (const response of [
        create(method.input),
        { ...fixture(method.output), originalContent: "private bytes" },
        unknownWireFact,
        undefined,
      ]) {
        const invoke = harness(async () => response as Message);
        await expect(invoke(codec.encodeRequest(fixture(method.input)))).rejects.toMatchObject({
          code: "CORE_REQUEST_FAILED",
          message: "CORE_REQUEST_FAILED",
        });
      }
    });

    it(`${method.localName} sanitizes canceled and failed requests without retrying`, async () => {
      for (const code of [Code.Canceled, Code.FailedPrecondition]) {
        const call = vi.fn(async () => {
          throw new ConnectError("/private/evidence secret", code);
        });
        await expect(
          harness(call)(codec.encodeRequest(fixture(method.input))),
        ).rejects.toMatchObject({ code: "CORE_REQUEST_FAILED", message: "CORE_REQUEST_FAILED" });
        expect(call).toHaveBeenCalledTimes(1);
      }
    });

    it(`${method.localName} rejects unknown nested response enums`, async () => {
      const response = fixture(method.output);
      function corrupt(schema: DescMessage, message: Message): boolean {
        const values = message as unknown as Record<string, unknown>;
        for (const field of schema.fields) {
          let value = values[field.localName];
          if (field.oneof) {
            const selected = values[field.oneof.localName] as { case?: string; value?: unknown };
            if (selected.case !== field.localName) continue;
            value = selected.value;
          }
          if (field.fieldKind === "enum") {
            values[field.localName] = 999;
            return true;
          }
          if (field.fieldKind === "message" && value && corrupt(field.message, value as Message))
            return true;
          if (
            field.fieldKind === "list" &&
            field.listKind === "message" &&
            Array.isArray(value) &&
            value[0] &&
            corrupt(field.message, value[0] as Message)
          )
            return true;
        }
        return false;
      }
      expect(corrupt(method.output, response)).toBe(true);
      await expect(
        harness(async () => response)(codec.encodeRequest(fixture(method.input))),
      ).rejects.toMatchObject({ code: "CORE_REQUEST_FAILED" });
    });
  }

  it("rejects a validly encoded unknown allocation enum before posting", async () => {
    const method = PurchaseService.method.postReviewedPurchase;
    const request = fixture(method.input) as unknown as { allocations: Array<{ use: number }> };
    const allocation = request.allocations[0];
    if (!allocation) throw new Error("Missing allocation fixture");
    allocation.use = 999;
    const call = vi.fn(async () => fixture(method.output));
    const caller = createCaller(createTammyRouter(), {
      ctx: { client: { postReviewedPurchase: call } as never, fileIntake: {} as never },
    });
    await expect(
      (
        caller as unknown as {
          purchases: { postReviewedPurchase(frame: Uint8Array): Promise<Uint8Array> };
        }
      ).purchases.postReviewedPurchase(
        frame(method.input, toBinary(method.input, request as never)),
      ),
    ).rejects.toMatchObject({ code: "INVALID_RPC_REQUEST" });
    expect(call).not.toHaveBeenCalled();
  });

  it("bounds the complete payment-page Any envelope at the actual 4 MiB boundary", async () => {
    const method = PurchaseService.method.listPurchasePayments;
    const roomy = createProtoMethodCodec({
      input: method.input,
      output: method.output,
      maximumRequestBytes: 16_384,
      maximumResponseBytes: maximumResponseBytes + 1024,
    });
    const response = fixture(method.output) as unknown as {
      payments: Array<{ reference: string }>;
    };
    const payment = response.payments[0];
    if (!payment) throw new Error("Missing payment fixture");
    payment.reference = "x".repeat(maximumResponseBytes - 4096);
    let encoded = roomy.encodeResponse(response as never);
    payment.reference += "x".repeat(maximumResponseBytes - encoded.byteLength);
    encoded = roomy.encodeResponse(response as never);
    expect(encoded.byteLength).toBe(maximumResponseBytes);
    const call = vi.fn(async () => response as unknown as Message);
    const caller = createCaller(createTammyRouter(), {
      ctx: { client: { listPurchasePayments: call } as never, fileIntake: {} as never },
    });
    const invoke = (
      caller as unknown as {
        purchases: { listPurchasePayments(frame: Uint8Array): Promise<Uint8Array> };
      }
    ).purchases.listPurchasePayments;
    const input = frame(method.input, toBinary(method.input, fixture(method.input)));
    await expect(invoke(input)).resolves.toHaveLength(maximumResponseBytes);
    payment.reference += "x";
    await expect(invoke(input)).rejects.toMatchObject({ code: "CORE_REQUEST_FAILED" });
  });
});
