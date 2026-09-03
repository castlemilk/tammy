import { create } from "@bufbuild/protobuf";
import {
  AuthenticationContextSchema,
  CommandContextSchema,
  type FreshFactorContext,
  TotpCodeInputSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  AssertTOTPRequestSchema,
  AssertTOTPResponseSchema,
} from "@tammy/connect-client/tammy/v1/identity_pb.js";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

export const SBR_PURPOSE = Object.freeze({
  importCredential: "sbr_machine_credential_import",
  importProduct: "sbr_product_id_import",
  removeCredential: "sbr_machine_credential_remove",
  removeProduct: "sbr_product_id_remove",
  replaceCredential: "sbr_machine_credential_replace",
  unlockCredential: "sbr_machine_credential_unlock",
  useCredential: "sbr_machine_credential_use",
});

const UUID_V7 = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const assertCodec = createProtoMethodCodec({
  input: AssertTOTPRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 8_192,
  output: AssertTOTPResponseSchema,
});

export function authentication(workspace: AuthenticatedWorkspace) {
  return create(AuthenticationContextSchema, {
    actorUserId: workspace.userId,
    sessionId: workspace.sessionId,
  });
}

export function uuidV7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  const now = BigInt(Date.now());
  for (let index = 0; index < 6; index += 1) {
    bytes[5 - index] = Number((now >> BigInt(index * 8)) & 0xffn);
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export function isUuidV7(value: string): boolean {
  return UUID_V7.test(value);
}

export function validTimestamp(
  value: { readonly nanos: number; readonly seconds: bigint } | undefined,
): boolean {
  return Boolean(
    value &&
      value.seconds >= -62_135_596_800n &&
      value.seconds <= 253_402_300_799n &&
      Number.isInteger(value.nanos) &&
      value.nanos >= 0 &&
      value.nanos <= 999_999_999,
  );
}

export async function assertFreshFactor(
  api: Pick<TammyDesktopAPI, "assertTotp">,
  workspace: AuthenticatedWorkspace,
  code: string,
  purpose: string,
): Promise<FreshFactorContext> {
  if (!/^\d{6}$/.test(code)) throw new Error("invalid factor input");
  const request = create(AssertTOTPRequestSchema, {
    authentication: authentication(workspace),
    code: create(TotpCodeInputSchema, { value: code }),
    purpose,
  });
  const frame = assertCodec.encodeRequest(request);
  const response = await (async () => {
    try {
      return assertCodec.decodeResponse(await api.assertTotp(frame));
    } finally {
      frame.fill(0);
    }
  })();
  const factor = response.freshFactor;
  if (
    !factor ||
    !isUuidV7(factor.assertionId) ||
    factor.purpose !== purpose ||
    !validTimestamp(factor.assertedAt)
  ) {
    throw new Error("invalid factor response");
  }
  return factor;
}

export function commandContext(workspace: AuthenticatedWorkspace, freshFactor: FreshFactorContext) {
  return create(CommandContextSchema, {
    authentication: authentication(workspace),
    freshFactor,
    idempotencyKey: uuidV7(),
  });
}

export const fieldClassName =
  "focus-ring h-9 w-full rounded-[5px] border border-border bg-background px-3 text-[11px] text-foreground placeholder:text-muted-foreground";

export const unknownOutcomeCopy =
  "The local operation outcome is unknown. Refresh status before trying again.";
