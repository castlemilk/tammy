export type PurchaseMutation =
  | "postReviewedPurchase"
  | "recordPurchasePayment"
  | "recordPurchaseTaxEvidence"
  | "attributePurchaseGst"
  | "deferPurchaseGstClaim"
  | "resolveLegacyPurchaseReview";

export function purchaseAttemptScope(
  workspaceId: string,
  organisationId: string,
  actorUserId: string,
  sourceKind: "document" | "legacy",
  sourceId: string,
): string {
  return `${workspaceId}:${organisationId}:${actorUserId}:${sourceKind}:${sourceId}`;
}

export interface PurchaseAttempt {
  readonly method: PurchaseMutation;
  readonly frame: Uint8Array;
  readonly status: "sending" | "uncertain" | "rejected";
}

/** One attempt owns its original frame, including its durable idempotency key. */
export class PurchaseCommandState {
  private attempt: PurchaseAttempt | undefined;
  private revision = 0;
  private readonly listeners = new Set<() => void>();

  constructor(private readonly retain?: (state: PurchaseCommandState) => void) {}
  readonly subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  readonly getSnapshot = (): number => this.revision;

  get pending(): PurchaseAttempt | undefined {
    return this.attempt && { ...this.attempt, frame: this.attempt.frame.slice() };
  }

  begin(method: PurchaseMutation, frame: Uint8Array): void {
    if (this.attempt) throw new Error("Resolve the existing purchase attempt first.");
    this.attempt = { method, frame: frame.slice(), status: "sending" };
    this.changed();
  }

  uncertain(): void {
    this.update("uncertain");
  }
  rejected(): void {
    this.update("rejected");
  }
  confirmed(): void {
    this.attempt = undefined;
    this.changed();
  }

  retry(): PurchaseAttempt {
    if (this.attempt?.status !== "uncertain")
      throw new Error("There is no uncertain request to retry.");
    this.update("sending");
    return this.pending!;
  }

  resolveRejected(): void {
    if (this.attempt?.status !== "rejected")
      throw new Error("Only a confirmed rejection permits a corrected request.");
    this.attempt = undefined;
    this.changed();
  }

  private update(status: PurchaseAttempt["status"]): void {
    if (!this.attempt) throw new Error("There is no pending purchase request.");
    this.attempt = { ...this.attempt, status };
    this.changed();
  }
  private changed(): void {
    this.revision += 1;
    this.retain?.(this);
    for (const listener of this.listeners) listener();
  }
}

// Facts and keys survive panel navigation in memory. Entries are never shared
// across workspace, organisation or actor; they contain no original file bytes.
const retainedAttempts = new Map<string, PurchaseCommandState>();
export function purchaseAttemptsFor(scope: string): PurchaseCommandState {
  const retained = retainedAttempts.get(scope);
  if (retained) return retained;
  return new PurchaseCommandState((state) => {
    if (state.pending) retainedAttempts.set(scope, state);
    else retainedAttempts.delete(scope);
  });
}

export function exactAud(value: string): bigint | undefined {
  const match = /^(\d+)(?:\.(\d{1,2}))?$/.exec(value.trim());
  if (!match) return undefined;
  const cents = BigInt(match[1]!) * 100n + BigInt((match[2] ?? "").padEnd(2, "0"));
  return cents <= 9223372036854775807n ? cents : undefined;
}

export function aud(value: bigint | undefined): string {
  const amount = value ?? 0n;
  const unsigned = amount < 0n ? -amount : amount;
  return `${amount < 0n ? "−" : ""}$${(unsigned / 100n).toLocaleString("en-AU")}.${String(unsigned % 100n).padStart(2, "0")}`;
}

export function decimalAud(value: bigint | undefined): string {
  const amount = value ?? 0n;
  return `${amount / 100n}.${String(amount % 100n).padStart(2, "0")}`;
}

export function uuidV7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  let milliseconds = Date.now();
  for (let index = 5; index >= 0; index -= 1) {
    bytes[index] = milliseconds & 0xff;
    milliseconds = Math.floor(milliseconds / 256);
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
