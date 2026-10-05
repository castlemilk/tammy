import type { GetInvoiceRequest } from "@tammy/connect-client/tammy/v1/invoices_pb.js";
import type { InvoiceDraftData } from "@tammy/invoices/model";
import { initConveyor } from "electron-conveyor/define";
import type { CoreClient } from "../main/core-client";
import type { InvoiceExport } from "../main/invoice-export";
import type { SbrFileIntake } from "../main/sbr-file-intake";
import type { ReportExports } from "../shared/report-export";

export interface TammyConveyorContext {
  readonly client: CoreClient;
  readonly fileIntake: SbrFileIntake;
  readonly reportExports?: ReportExports;
  readonly invoiceExport?: InvoiceExport<GetInvoiceRequest>;
  readonly invoiceProposal?: (request: GetInvoiceRequest) => Promise<InvoiceDraftData | undefined>;
}

export const { command, defineModule, query } = initConveyor<TammyConveyorContext>();
