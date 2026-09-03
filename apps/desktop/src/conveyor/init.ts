import { initConveyor } from "electron-conveyor/define";

import type { CoreClient } from "../main/core-client";
import type { SbrFileIntake } from "../main/sbr-file-intake";

export interface TammyConveyorContext {
  readonly client: CoreClient;
  readonly fileIntake: SbrFileIntake;
}

export const { command, defineModule, query } = initConveyor<TammyConveyorContext>();
