import { defineModule, query } from "../init";

export const system = defineModule({
  getSystemDiagnostics: query(({ ctx }) => ctx.client.getDiagnostics()),
});
