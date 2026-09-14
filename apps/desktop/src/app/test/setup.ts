import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";
import { CONVEYOR_MANIFEST } from "../../shared/conveyor-contract";
import { activeConveyorFixtureProcedures, resetActiveConveyorFixture } from "./conveyor-fixtures";

if (typeof window !== "undefined")
  Object.defineProperty(window, "conveyor", {
    configurable: true,
    value: Object.freeze({
      invoke: async (channel: string, method: string, ...args: unknown[]) => {
        const module = channel.startsWith("conveyor:") ? channel.slice("conveyor:".length) : "";
        const procedures = activeConveyorFixtureProcedures();
        const moduleProcedures = procedures?.[module as keyof typeof procedures];
        const procedure = moduleProcedures?.[method as keyof typeof moduleProcedures] as
          | ((...values: unknown[]) => unknown)
          | undefined;
        if (!procedure) throw new Error("unavailable");
        return procedure(...args);
      },
      manifest: () => CONVEYOR_MANIFEST,
      subscribe: () => {
        throw new Error("IPC_SUBSCRIPTIONS_DISABLED");
      },
    }),
  });

afterEach(() => {
  cleanup();
  resetActiveConveyorFixture();
});
