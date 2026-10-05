import type { BrowserWindow, IpcMain } from "electron";
import { createRouter } from "electron-conveyor/main";

import type { CoreClient } from "../main/core-client";
import type { SbrFileIntake } from "../main/sbr-file-intake";
import { createConveyorAuthorizer } from "./guard";
import type { TammyConveyorContext } from "./init";
import { createTammyRouter } from "./router";

type IpcBoundary = Pick<IpcMain, "handle" | "removeHandler" | "on" | "removeListener">;

export interface TammyConveyorRegistrationOptions {
  readonly applicationUrl: string;
  readonly client: CoreClient;
  readonly fileIntake: SbrFileIntake;
  readonly getWindow: () => BrowserWindow | undefined;
  readonly ipcMain: IpcBoundary;
  readonly reportExports?: TammyConveyorContext["reportExports"];
  readonly invoiceExport?: TammyConveyorContext["invoiceExport"];
  readonly invoiceProposal?: TammyConveyorContext["invoiceProposal"];
}

export function registerTammyConveyor(options: TammyConveyorRegistrationOptions): () => void {
  const authorize = createConveyorAuthorizer({
    applicationUrl: options.applicationUrl,
    getWindow: options.getWindow,
  });
  const context = Object.freeze({
    client: options.client,
    fileIntake: options.fileIntake,
    ...(options.reportExports ? { reportExports: options.reportExports } : {}),
    ...(options.invoiceExport ? { invoiceExport: options.invoiceExport } : {}),
    ...(options.invoiceProposal ? { invoiceProposal: options.invoiceProposal } : {}),
  });
  const router = createRouter(createTammyRouter().modules, {
    authorize,
    createContext: () => context,
    ipcMain: options.ipcMain,
    streams: false,
  });
  let disposed = false;
  return () => {
    if (disposed) return;
    disposed = true;
    router.dispose();
  };
}
