import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { type RenderOptions, type RenderResult, render } from "@testing-library/react";
import { createConveyorReactClient } from "electron-conveyor/react";
import { createElement, type ReactElement, type ReactNode } from "react";
import { type Mock, vi } from "vitest";

import type { TammyConveyor } from "../../conveyor/client";
import type { TammyRouter } from "../../conveyor/router";
import { CONVEYOR_MANIFEST } from "../../shared/conveyor-contract";

type ModuleName = keyof TammyConveyor;
type Procedure<T> = T extends (...args: infer Args) => infer Result
  ? (...args: Args) => Result
  : never;
export type ConveyorFixtureProcedures = {
  [Module in ModuleName]: {
    [Method in keyof TammyConveyor[Module]]: Mock<Procedure<TammyConveyor[Module][Method]>>;
  };
};

const ACTIVE_FIXTURE = "__tammyConveyorFixtureProcedures";
const ACTIVE_QUERY_CLIENT = "__tammyConveyorFixtureQueryClient";

function unavailable<T extends (...args: never[]) => unknown>(): Mock<T> {
  return vi.fn<T>().mockRejectedValue(new Error("unavailable"));
}

function createProcedures(): ConveyorFixtureProcedures {
  return {
    system: { getSystemDiagnostics: unavailable() },
    workspace: {
      createWorkspace: unavailable(),
      confirmRecovery: unavailable(),
      unlockWorkspace: unavailable(),
      signIn: unavailable(),
    },
    organisation: {
      getCurrentUser: unavailable(),
      getOrganisation: unavailable(),
      createOrganisation: unavailable(),
      enrolTotp: unavailable(),
      confirmTotp: unavailable(),
      assertTotp: unavailable(),
      recordEntityVerification: unavailable(),
    },
    accounting: {
      getInvoiceAccounting: unavailable(),
      postInvoiceIncome: unavailable(),
      recordInvoicePayment: unavailable(),
      getBusinessIncomeSummary: unavailable(),

      listAccounts: unavailable(),
      listJournals: unavailable(),
      getJournal: unavailable(),
      getTrialBalance: unavailable(),
      createAccount: unavailable(),
      postManualJournal: unavailable(),
      listInvoices: unavailable(),
      getInvoice: unavailable(),
      saveInvoiceDraft: unavailable(),
      issueInvoice: unavailable(),
      getInvoiceTemplate: unavailable(),
      saveInvoiceTemplate: unavailable(),
      uploadInvoiceLogo: unavailable(),
      getInvoiceLogo: unavailable(),
      exportInvoicePdf: unavailable(),
      getInvoiceProposal: unavailable(),
    },
    banking: {
      listBankStatementLines: unavailable(),
      getBankingSummary: unavailable(),
      importBankStatement: unavailable(),
      matchBankStatementLine: unavailable(),
      completeBankReconciliation: unavailable(),
    },
    documents: {
      listDocuments: unavailable(),
      getDocument: unavailable(),
      ingestDocument: unavailable(),
      saveDocumentReview: unavailable(),
    },
    reporting: {
      getCurrentBasDraft: unavailable(),
      getReportingCapability: unavailable(),
      getAttentionSummary: unavailable(),
      createBasDraft: unavailable(),
    },
    sbr: {
      getSbrReadiness: unavailable(),
      getMachineCredentialStatus: unavailable(),
      selectMachineCredentialFile: unavailable(),
      importMachineCredential: unavailable(),
      replaceMachineCredential: unavailable(),
      unlockMachineCredential: unavailable(),
      removeMachineCredential: unavailable(),
      importSbrProductId: unavailable(),
      removeSbrProductId: unavailable(),
      runSbrReadinessFixture: unavailable(),
    },
  };
}

function fixtureBridge(procedures: ConveyorFixtureProcedures): Window["conveyor"] {
  return Object.freeze({
    invoke: async (channel: string, method: string, ...args: unknown[]) => {
      const module = channel.startsWith("conveyor:") ? channel.slice("conveyor:".length) : "";
      const moduleProcedures = procedures[module as ModuleName];
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
  });
}

export function activeConveyorFixtureProcedures(): ConveyorFixtureProcedures | undefined {
  return (
    globalThis as typeof globalThis & {
      [ACTIVE_FIXTURE]?: ConveyorFixtureProcedures;
    }
  )[ACTIVE_FIXTURE];
}

export function resetActiveConveyorFixture(): void {
  const scope = globalThis as typeof globalThis & {
    [ACTIVE_FIXTURE]?: ConveyorFixtureProcedures;
    [ACTIVE_QUERY_CLIENT]?: QueryClient;
  };
  scope[ACTIVE_QUERY_CLIENT]?.clear();
  delete scope[ACTIVE_FIXTURE];
  delete scope[ACTIVE_QUERY_CLIENT];
}

export function renderWithConveyor(
  ui: ReactElement,
  options?: Omit<RenderOptions, "wrapper">,
): RenderResult {
  const queryClient = (globalThis as typeof globalThis & { [ACTIVE_QUERY_CLIENT]?: QueryClient })[
    ACTIVE_QUERY_CLIENT
  ];
  if (!queryClient) throw new Error("createConveyorFixture() must run before renderWithConveyor()");
  return render(ui, {
    ...options,
    wrapper: ({ children }: { readonly children: ReactNode }) =>
      createElement(QueryClientProvider, { client: queryClient }, children),
  });
}

/** Fresh, independently-owned module mocks keep each screen inside its declared IPC slice. */
export function createConveyorFixture(): {
  readonly conveyor: TammyConveyor;
  readonly procedures: ConveyorFixtureProcedures;
  readonly queryClient: QueryClient;
} {
  const procedures = createProcedures();
  (
    globalThis as typeof globalThis & {
      [ACTIVE_FIXTURE]?: ConveyorFixtureProcedures;
    }
  )[ACTIVE_FIXTURE] = procedures;
  const queryClient = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  (globalThis as typeof globalThis & { [ACTIVE_QUERY_CLIENT]?: QueryClient })[ACTIVE_QUERY_CLIENT] =
    queryClient;
  const previousBridge = window.conveyor;
  Object.defineProperty(window, "conveyor", {
    configurable: true,
    value: fixtureBridge(procedures),
  });
  const conveyor = createConveyorReactClient<TammyRouter>({ queryClient });
  Object.defineProperty(window, "conveyor", { configurable: true, value: previousBridge });
  return {
    conveyor,
    procedures,
    queryClient,
  };
}
