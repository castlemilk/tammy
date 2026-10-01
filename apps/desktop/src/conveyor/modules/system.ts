import { ConveyorError, type StandardSchemaV1 } from "electron-conveyor/define";
import { z } from "zod";
import { defineModule, query } from "../init";

const zeroInput = z.undefined() as unknown as StandardSchemaV1<void, void>;

export const system = defineModule({
  getSystemDiagnostics: query(zeroInput, async ({ ctx }) => {
    try {
      return await ctx.client.getDiagnostics();
    } catch {
      throw new ConveyorError("CORE_REQUEST_FAILED", "CORE_REQUEST_FAILED");
    }
  }),
});
