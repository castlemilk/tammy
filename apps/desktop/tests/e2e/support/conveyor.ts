import type { Page } from "@playwright/test";

import { CONVEYOR_MANIFEST } from "../../../src/shared/conveyor-contract";

const CHANNELS = {
  accounting: "conveyor:accounting",
  banking: "conveyor:banking",
  basPreparation: "conveyor:basPreparation",
  documents: "conveyor:documents",
  organisation: "conveyor:organisation",
  purchases: "conveyor:purchases",
  reporting: "conveyor:reporting",
  sbr: "conveyor:sbr",
  system: "conveyor:system",
  workspace: "conveyor:workspace",
} as const;

export type ConveyorModule = keyof typeof CHANNELS;
type ConveyorEnvelope =
  | { readonly data: unknown; readonly ok: true }
  | { readonly error?: { readonly code?: string }; readonly ok: false };

function assertMethod(module: ConveyorModule, method: string): void {
  const procedures = CONVEYOR_MANIFEST[module];
  if (!procedures || !Object.hasOwn(procedures, method)) {
    throw new Error("E2E_CONVEYOR_PROCEDURE_NOT_ALLOWLISTED");
  }
}

export async function invokeConveyorFrame(
  page: Page,
  module: ConveyorModule,
  method: string,
  request: Uint8Array,
): Promise<Uint8Array> {
  assertMethod(module, method);
  const bytes = await page.evaluate(
    async ({ channel, methodName, requestBytes }) => {
      const envelope = (await window.conveyor.invoke(
        channel,
        methodName,
        Uint8Array.from(requestBytes),
      )) as ConveyorEnvelope;
      if (!envelope.ok) throw new Error(envelope.error?.code ?? "CONVEYOR_REQUEST_FAILED");
      if (!(envelope.data instanceof Uint8Array)) throw new Error("CONVEYOR_FRAME_INVALID");
      return [...envelope.data];
    },
    { channel: CHANNELS[module], methodName: method, requestBytes: [...request] },
  );
  return Uint8Array.from(bytes);
}

export async function invokeConveyorValue(
  page: Page,
  module: ConveyorModule,
  method: string,
): Promise<unknown> {
  assertMethod(module, method);
  return page.evaluate(
    async ({ channel, methodName }) => {
      const envelope = (await window.conveyor.invoke(channel, methodName)) as ConveyorEnvelope;
      if (!envelope.ok) throw new Error(envelope.error?.code ?? "CONVEYOR_REQUEST_FAILED");
      return envelope.data;
    },
    { channel: CHANNELS[module], methodName: method },
  );
}

export async function invokeConveyorCommandWithFrame(
  page: Page,
  module: ConveyorModule,
  method: string,
  command: Uint8Array,
  fields: Readonly<Record<string, string>>,
): Promise<unknown> {
  assertMethod(module, method);
  return page.evaluate(
    async ({ channel, methodName, commandBytes, commandFields }) => {
      const envelope = (await window.conveyor.invoke(channel, methodName, {
        ...commandFields,
        command: Uint8Array.from(commandBytes),
      })) as ConveyorEnvelope;
      if (!envelope.ok) throw new Error(envelope.error?.code ?? "CONVEYOR_REQUEST_FAILED");
      return envelope.data;
    },
    {
      channel: CHANNELS[module],
      commandBytes: [...command],
      commandFields: fields,
      methodName: method,
    },
  );
}
