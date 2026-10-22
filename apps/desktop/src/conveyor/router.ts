import { buildManifest } from "electron-conveyor/define";

import { CONVEYOR_MANIFEST } from "../shared/conveyor-contract";
import { accounting } from "./modules/accounting";
import { banking } from "./modules/banking";
import { basPreparation } from "./modules/bas-preparation";
import { documents } from "./modules/documents";
import { organisationModule } from "./modules/organisation";
import { purchases } from "./modules/purchases";
import { reporting } from "./modules/reporting";
import { sbr } from "./modules/sbr";
import { system } from "./modules/system";
import { workspace } from "./modules/workspace";

const modules = Object.freeze({
  system,
  workspace,
  organisation: organisationModule,
  accounting,
  banking,
  basPreparation,
  documents,
  purchases,
  reporting,
  sbr,
});
const manifest = buildManifest(modules);

if (JSON.stringify(manifest) !== JSON.stringify(CONVEYOR_MANIFEST)) {
  throw new Error("CONVEYOR_MANIFEST_MISMATCH");
}

export function createTammyRouter() {
  return Object.freeze({ modules, manifest });
}

export type TammyRouter = ReturnType<typeof createTammyRouter>;
