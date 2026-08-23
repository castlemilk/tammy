import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { FreshFactorContextSchema } from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  AssertTOTPRequestSchema,
  AssertTOTPResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  ImportSbrProductIdRequestSchema,
  ImportSbrProductIdResponseSchema,
  ProductIdState,
  RemoveSbrProductIdRequestSchema,
  RemoveSbrProductIdResponseSchema,
} from "@tammy/connect-client/tammy/v1/sbr_pb.js";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { expect, it, vi } from "vitest";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { ProductIdForm } from "./product-id-form";

const workspace = {
  workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
  userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
  sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
};
const assertCodec = createProtoMethodCodec({
  input: AssertTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: AssertTOTPResponseSchema,
});
const importCodec = createProtoMethodCodec({
  input: ImportSbrProductIdRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: ImportSbrProductIdResponseSchema,
});
const removeCodec = createProtoMethodCodec({
  input: RemoveSbrProductIdRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: RemoveSbrProductIdResponseSchema,
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve;
  });
  return { promise, resolve };
}

it("sends a Product ID once for the exact authenticated scope and never renders the value", async () => {
  const assertTotp = vi.fn((frame: Uint8Array) => {
    const request = assertCodec.decodeRequest(frame);
    expect(request.purpose).toBe("sbr_product_id_import");
    return Promise.resolve(
      assertCodec.encodeResponse(
        create(AssertTOTPResponseSchema, {
          freshFactor: create(FreshFactorContextSchema, {
            assertionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
            purpose: request.purpose,
            assertedAt: create(TimestampSchema, { seconds: 1n }),
          }),
        }),
      ),
    );
  });
  const importSbrProductId = vi.fn(
    ({ command, productId }: { command: Uint8Array; productId: string }) => {
      const request = importCodec.decodeRequest(command);
      expect(productId).toBe("PRIVATE-PRODUCT-ID");
      expect(request.evteProductIdentifier).toBe("TAMMY.EVTE");
      expect(request.evteServiceIdentifier).toBe("BAS.LODGE");
      return Promise.resolve(
        importCodec.encodeResponse(
          create(ImportSbrProductIdResponseSchema, { productIdState: ProductIdState.PRESENT }),
        ),
      );
    },
  );
  const user = userEvent.setup();
  render(
    <StrictMode>
      <ProductIdForm
        api={{ assertTotp, importSbrProductId, removeSbrProductId: vi.fn() }}
        onChanged={vi.fn()}
        productIdentifier="TAMMY.EVTE"
        serviceIdentifier="BAS.LODGE"
        state={ProductIdState.MISSING}
        workspace={workspace}
      />
    </StrictMode>,
  );
  await user.click(screen.getByRole("button", { name: "Import Product ID" }));
  await user.type(screen.getByLabelText("Product ID value"), "PRIVATE-PRODUCT-ID");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.dblClick(screen.getByRole("button", { name: "Continue" }));
  expect(importSbrProductId).toHaveBeenCalledOnce();
  expect(document.body.textContent).not.toContain("PRIVATE-PRODUCT-ID");
});

it("fails closed before mutation for permission or stale-factor failure", async () => {
  const importSbrProductId = vi.fn();
  const user = userEvent.setup();
  render(
    <ProductIdForm
      api={{
        assertTotp: vi.fn().mockRejectedValue(new Error("permission secret")),
        importSbrProductId,
        removeSbrProductId: vi.fn(),
      }}
      onChanged={vi.fn()}
      productIdentifier="TAMMY.EVTE"
      serviceIdentifier="BAS.LODGE"
      state={ProductIdState.MISSING}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Import Product ID" }));
  await user.type(screen.getByLabelText("Product ID value"), "PRIVATE");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(await screen.findByText(/no Product ID operation was started/i)).toBeTruthy();
  expect(importSbrProductId).not.toHaveBeenCalled();
  expect(document.body.textContent).not.toContain("permission secret");
});

it("removes the Product ID once with the exact authenticated scope", async () => {
  const assertTotp = vi.fn((frame: Uint8Array) => {
    const request = assertCodec.decodeRequest(frame);
    expect(request.purpose).toBe("sbr_product_id_remove");
    return Promise.resolve(
      assertCodec.encodeResponse(
        create(AssertTOTPResponseSchema, {
          freshFactor: create(FreshFactorContextSchema, {
            assertionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
            purpose: request.purpose,
            assertedAt: create(TimestampSchema, { seconds: 1n }),
          }),
        }),
      ),
    );
  });
  const removeSbrProductId = vi.fn((frame: Uint8Array) => {
    const request = removeCodec.decodeRequest(frame);
    expect(request.evteProductIdentifier).toBe("TAMMY.EVTE");
    expect(request.evteServiceIdentifier).toBe("BAS.LODGE");
    return Promise.resolve(
      removeCodec.encodeResponse(
        create(RemoveSbrProductIdResponseSchema, { productIdState: ProductIdState.MISSING }),
      ),
    );
  });
  const user = userEvent.setup();
  render(
    <ProductIdForm
      api={{ assertTotp, importSbrProductId: vi.fn(), removeSbrProductId }}
      onChanged={vi.fn()}
      productIdentifier="TAMMY.EVTE"
      serviceIdentifier="BAS.LODGE"
      state={ProductIdState.PRESENT}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Remove Product ID" }));
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.dblClick(screen.getByRole("button", { name: "Continue" }));
  expect(removeSbrProductId).toHaveBeenCalledOnce();
});

it("does not dispatch a Product ID mutation when unmounted during factor assertion", async () => {
  const factor = deferred<Uint8Array>();
  const importSbrProductId = vi.fn();
  const user = userEvent.setup();
  const view = render(
    <ProductIdForm
      api={{
        assertTotp: vi.fn(() => factor.promise),
        importSbrProductId,
        removeSbrProductId: vi.fn(),
      }}
      onChanged={vi.fn()}
      productIdentifier="TAMMY.EVTE"
      serviceIdentifier="BAS.LODGE"
      state={ProductIdState.MISSING}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Import Product ID" }));
  await user.type(screen.getByLabelText("Product ID value"), "PRIVATE");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  view.unmount();
  await act(async () => {
    factor.resolve(
      assertCodec.encodeResponse(
        create(AssertTOTPResponseSchema, {
          freshFactor: create(FreshFactorContextSchema, {
            assertionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
            purpose: "sbr_product_id_import",
            assertedAt: create(TimestampSchema, { seconds: 1n }),
          }),
        }),
      ),
    );
    await factor.promise;
  });
  expect(importSbrProductId).not.toHaveBeenCalled();
});

it("fails closed when Product ID import does not report PRESENT", async () => {
  const changed = vi.fn();
  const assertTotp = vi.fn((frame: Uint8Array) => {
    const request = assertCodec.decodeRequest(frame);
    return Promise.resolve(
      assertCodec.encodeResponse(
        create(AssertTOTPResponseSchema, {
          freshFactor: create(FreshFactorContextSchema, {
            assertionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073994",
            purpose: request.purpose,
            assertedAt: create(TimestampSchema, { seconds: 1n }),
          }),
        }),
      ),
    );
  });
  const importSbrProductId = vi.fn(() =>
    Promise.resolve(
      importCodec.encodeResponse(
        create(ImportSbrProductIdResponseSchema, { productIdState: ProductIdState.MISSING }),
      ),
    ),
  );
  const user = userEvent.setup();
  render(
    <ProductIdForm
      api={{ assertTotp, importSbrProductId, removeSbrProductId: vi.fn() }}
      onChanged={changed}
      productIdentifier="TAMMY.EVTE"
      serviceIdentifier="BAS.LODGE"
      state={ProductIdState.MISSING}
      workspace={workspace}
    />,
  );
  await user.click(screen.getByRole("button", { name: "Import Product ID" }));
  await user.type(screen.getByLabelText("Product ID value"), "PRIVATE");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(await screen.findByText(/outcome is unknown/i)).toBeTruthy();
  await user.click(screen.getByRole("button", { name: "Continue" }));
  expect(importSbrProductId).toHaveBeenCalledOnce();
  expect(screen.getByRole("button", { name: "Refresh status" })).toBeTruthy();
  expect(changed).not.toHaveBeenCalled();
});

it("keeps its live status region mounted across a known authorization failure", async () => {
  const user = userEvent.setup();
  render(
    <ProductIdForm
      api={{
        assertTotp: vi.fn().mockRejectedValue(new Error("denied")),
        importSbrProductId: vi.fn(),
        removeSbrProductId: vi.fn(),
      }}
      onChanged={vi.fn()}
      productIdentifier="TAMMY.EVTE"
      serviceIdentifier="BAS.LODGE"
      state={ProductIdState.MISSING}
      workspace={workspace}
    />,
  );
  const region = screen.getByRole("status");
  await user.click(screen.getByRole("button", { name: "Import Product ID" }));
  await user.type(screen.getByLabelText("Product ID value"), "PRIVATE");
  await user.type(screen.getByLabelText("Fresh six-digit code"), "123456");
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await screen.findByText(/no Product ID operation was started/i);
  expect(screen.getByRole("status")).toBe(region);
});
