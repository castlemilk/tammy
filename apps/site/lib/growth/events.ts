/**
 * Privacy-first event contract. This module never sends network requests.
 * Transport must be explicitly injected AFTER policy/consent approval.
 * Do not add autocapture, form values, monetary amounts, ABNs, URLs or PDFs.
 */
export const EVENT_NAMES = [
  "tool_started", "tool_completed", "tool_error",
  "export_started", "export_completed", "export_failed",
  "save_offer_shown", "save_offer_selected",
  "accounting_interest_opened",
] as const;
export const TOOL_IDS = [
  "invoice", "quote", "gst", "abn", "due_date", "margin",
  "break_even", "runway", "cash_forecast", "receipts",
] as const;
export const PAGE_KEYS = [
  "home", "tools_index", "tool_detail", "blog_post", "guide", "accounting_info",
] as const;
export const OUTCOMES = [
  "success", "invalid_input", "export_error", "unavailable", "unknown",
] as const;

export type EventName = typeof EVENT_NAMES[number];
export type ToolId = typeof TOOL_IDS[number];
export type PageKey = typeof PAGE_KEYS[number];
export type Outcome = typeof OUTCOMES[number];

export interface GrowthEvent {
  readonly version: 1;
  readonly event: EventName;
  readonly tool_id: ToolId;
  readonly page_key: PageKey;
  readonly outcome: Outcome;
  readonly consent_state: "allowed";
}
const keys = new Set(["version", "event", "tool_id", "page_key", "outcome", "consent_state"]);
const contains = (items: readonly string[], item: unknown): item is string =>
  typeof item === "string" && items.includes(item);

export function validateGrowthEvent(value: unknown): GrowthEvent {
  if (!value || typeof value !== "object" || Array.isArray(value) ||
    Object.getPrototypeOf(value) !== Object.prototype) {
    throw new Error("Analytics event must be a plain object");
  }
  const event = value as Record<string, unknown>;
  if (Object.keys(event).length !== keys.size ||
    Object.keys(event).some((key) => !keys.has(key))) {
    throw new Error("Analytics event contains forbidden or missing fields");
  }
  if (event.version !== 1 || event.consent_state !== "allowed" ||
    !contains(EVENT_NAMES, event.event) || !contains(TOOL_IDS, event.tool_id) ||
    !contains(PAGE_KEYS, event.page_key) || !contains(OUTCOMES, event.outcome)) {
    throw new Error("Analytics event contains unknown or unconsented values");
  }
  return Object.freeze({
    version: 1,
    event: event.event as EventName,
    tool_id: event.tool_id as ToolId,
    page_key: event.page_key as PageKey,
    outcome: event.outcome as Outcome,
    consent_state: "allowed" as const,
  });
}

export interface GrowthReporter {
  readonly begin: (tool: ToolId) => boolean;
  readonly complete: (tool: ToolId) => boolean;
  readonly exportStarted: (tool: ToolId) => boolean;
  readonly exportCompleted: (tool: ToolId) => boolean;
  readonly exportFailed: (tool: ToolId) => boolean;
  readonly resetAttempt: (tool: ToolId) => void;
}
type Consent = "allowed" | "denied" | "unknown";

/**
 * No default transport, persistent ID or fingerprint. Events can be tested with
 * injected collectors, but nothing is sent from the production website until
 * an approved consent gate and transport are explicitly wired.
 */
export function createGrowthReporter({
  getConsent,
  publish,
  pageKey = "tool_detail",
}: {
  readonly getConsent: () => Consent;
  readonly publish?: (event: GrowthEvent) => void;
  readonly pageKey?: PageKey;
}): GrowthReporter {
  const started = new Set<ToolId>();
  const completed = new Set<ToolId>();
  const exporting = new Set<ToolId>();

  function dispatch(name: EventName, tool: ToolId, outcome: Outcome) {
    if (getConsent() !== "allowed" || !publish) return false;
    const event = validateGrowthEvent({
      version: 1, event: name, tool_id: tool,
      page_key: pageKey, outcome, consent_state: "allowed",
    });
    publish(event);
    return true;
  }
  return {
    begin(tool) {
      if (started.has(tool)) return false;
      if (!dispatch("tool_started", tool, "success")) return false;
      started.add(tool);
      return true;
    },
    complete(tool) {
      if (!started.has(tool) || completed.has(tool)) return false;
      if (!dispatch("tool_completed", tool, "success")) return false;
      completed.add(tool);
      return true;
    },
    exportStarted(tool) {
      if (!started.has(tool) || exporting.has(tool)) return false;
      if (!dispatch("export_started", tool, "success")) return false;
      exporting.add(tool);
      return true;
    },
    exportCompleted(tool) {
      if (!exporting.has(tool)) return false;
      if (!dispatch("export_completed", tool, "success")) return false;
      exporting.delete(tool);
      return true;
    },
    exportFailed(tool) {
      if (!exporting.has(tool)) return false;
      if (!dispatch("export_failed", tool, "export_error")) return false;
      exporting.delete(tool);
      return true;
    },
    resetAttempt(tool) {
      started.delete(tool);
      completed.delete(tool);
      exporting.delete(tool);
    },
  };
}
