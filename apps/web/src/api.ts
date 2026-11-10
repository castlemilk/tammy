import type {
  Account,
  BankLine,
  BasWorkpaper,
  CapabilitySummary,
  DocumentRecord,
  Insight,
  Journal,
  ListPage,
  Overview,
  Session,
  TrialBalance,
} from "./types";

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

// The gateway's answer when it no longer holds this browser's session, for example after the
// session idled out or the server restarted.
const BROWSER_SESSION_REQUIRED = "Start a browser session first.";

async function request<T>(path: string, body?: unknown, renewed = false): Promise<T> {
  const response = await fetch(`/api${path}`, {
    credentials: "same-origin",
    ...(body === undefined
      ? {}
      : {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        }),
  });
  const value: unknown = await response.json().catch(() => undefined);
  if (!response.ok) {
    const message =
      typeof value === "object" &&
      value !== null &&
      "error" in value &&
      typeof value.error === "object" &&
      value.error !== null &&
      "message" in value.error &&
      typeof value.error.message === "string"
        ? value.error.message
        : "The local service could not complete that request.";
    // The setup and unlock forms can outlive their browser session. Start a new one and send the
    // request once more.
    if (
      response.status === 401 &&
      path.startsWith("/workspace/") &&
      message === BROWSER_SESSION_REQUIRED &&
      !renewed
    ) {
      await request<Session>("/session");
      return request<T>(path, body, true);
    }
    if (response.status === 401 && path !== "/session" && !path.startsWith("/workspace/")) {
      window.dispatchEvent(new Event("tammy:session-expired"));
    }
    throw new ApiError(message, response.status);
  }
  if (value === undefined)
    throw new ApiError("The local service returned an unreadable response.", 502);
  return value as T;
}

export interface CreateWorkspaceInput {
  displayName: string;
  username: string;
  workspacePassphrase: string;
  administratorPassword: string;
}

export interface ConfirmWorkspaceInput {
  setupId: string;
  recoveryCode: string;
  username: string;
  administratorPassword: string;
  businessLegalName: string;
  businessDisplayName: string;
  abn: string;
  workspacePassphrase?: string;
}

export interface JournalInput {
  idempotencyKey: string;
  date: string;
  description: string;
  lines: { accountId: string; description: string; debitMinor: number; creditMinor: number }[];
}

export const api = {
  session: () => request<Session>("/session"),
  createWorkspace: (input: CreateWorkspaceInput) =>
    request<{ recoveryCode: string; setupId: string; expiresAt: string }>(
      "/workspace/create",
      input,
    ),
  confirmWorkspace: (input: ConfirmWorkspaceInput) => request<Session>("/workspace/confirm", input),
  unlock: (
    input: Pick<CreateWorkspaceInput, "username" | "workspacePassphrase" | "administratorPassword">,
  ) => request<Session>("/workspace/unlock", input),
  logout: () => request<void>("/session/logout", {}),
  overview: () => request<Overview>("/overview"),
  accounts: () => request<{ accounts: Account[]; page?: ListPage }>("/accounts"),
  createAccount: (input: { idempotencyKey: string; code: string; name: string; type: string }) =>
    request<unknown>("/accounts", input),
  journals: () => request<{ journals: Journal[]; page?: ListPage }>("/journals"),
  postJournal: (input: JournalInput) => request<unknown>("/journals", input),
  trialBalance: (date: string) =>
    request<TrialBalance>(`/trial-balance?asOfDate=${encodeURIComponent(date)}`),
  documents: () => request<{ documents: DocumentRecord[]; page?: ListPage }>("/documents"),
  uploadDocument: (input: {
    idempotencyKey: string;
    sourceDisplayName: string;
    mimeType: string;
    originalBase64: string;
    extractedText: string;
    candidate: {
      supplierName: string;
      invoiceNumber: string;
      date: string;
      subtotalMinor: number;
      gstMinor: number;
      totalMinor: number;
    };
  }) => request<{ document: DocumentRecord }>("/documents/upload", input),
  reviewDocument: (
    id: string,
    input: {
      idempotencyKey: string;
      version: string;
      supplierName: string;
      invoiceNumber: string;
      date: string;
      subtotalMinor: number;
      gstMinor: number;
      totalMinor: number;
    },
  ) => request<unknown>(`/documents/${encodeURIComponent(id)}/review`, input),
  banking: () =>
    request<{
      lines: BankLine[];
      page?: ListPage;
      summary?: {
        importedLineCount: number;
        unmatchedLineCount: number;
        unreconciledLineCount: number;
        latestClosingBalanceMinor: import("./money").MinorUnits;
      };
    }>("/banking"),
  importBank: (input: {
    idempotencyKey: string;
    openingBalanceMinor: number;
    lines: { date: string; description: string; amountMinor: number }[];
  }) => request<unknown>("/banking/import", input),
  matchBank: (id: string, input: { idempotencyKey: string; version: string; reference: string }) =>
    request<unknown>(`/banking/${encodeURIComponent(id)}/match`, input),
  reconcileBank: (input: { idempotencyKey: string }) =>
    request<unknown>("/banking/reconcile", input),
  bas: () => request<{ workpaper: BasWorkpaper | null }>("/bas"),
  createBas: (input: { idempotencyKey: string; periodStart: string; periodEnd: string }) =>
    request<unknown>("/bas", input),
  insights: () => request<{ insights: Insight[] }>("/insights"),
  capabilities: () => request<CapabilitySummary>("/capabilities"),
};
