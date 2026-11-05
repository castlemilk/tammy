import { describe, expect, it } from "vitest";
import {
  createGrowthReporter, validateGrowthEvent, type GrowthEvent,
} from "../lib/growth/events";

describe("Privacy-safe Tammy growth event contract", () => {
  const event = {
    version: 1, event: "tool_completed", tool_id: "invoice",
    page_key: "tool_detail", outcome: "success", consent_state: "allowed",
  } as const;

  it("enforces the entire field allowlist, not just known keys", () => {
    expect(validateGrowthEvent(event)).toEqual(event);
    expect(() => validateGrowthEvent({ ...event, abn: "51824753556" })).toThrow("forbidden");
    expect(() => validateGrowthEvent({ ...event, invoice_amount: 1200 })).toThrow("forbidden");
    expect(() => validateGrowthEvent({ ...event, name: "A customer" })).toThrow("forbidden");
    expect(() => validateGrowthEvent({ ...event, url: "/tools?customer=Jane" })).toThrow("forbidden");
    expect(() => validateGrowthEvent({ ...event, event: "PDF_200_ABC" })).toThrow("unknown");
    expect(() => validateGrowthEvent({ ...event, consent_state: "unknown" })).toThrow("unknown");
    expect(() => validateGrowthEvent({ ...event, tool_id: "anything" })).toThrow("unknown");
  });

  it("does not emit with denied or unknown consent", () => {
    const results: GrowthEvent[] = [];
    let consent: "unknown" | "allowed" | "denied" = "unknown";
    const r = createGrowthReporter({ getConsent: () => consent, publish: (e) => results.push(e) });
    expect(r.begin("invoice")).toBe(false);
    consent = "denied";
    expect(r.begin("invoice")).toBe(false);
    expect(results).toEqual([]);
    consent = "allowed";
    expect(r.begin("invoice")).toBe(true);
    expect(results).toHaveLength(1);
  });

  it("never emits without an explicitly injected publisher", () => {
    const r = createGrowthReporter({ getConsent: () => "allowed" });
    expect(r.begin("gst")).toBe(false);
    expect(r.complete("gst")).toBe(false);
  });

  it("deduplicates starts and completions within an explicit attempt", () => {
    const results: GrowthEvent[] = [];
    const r = createGrowthReporter({ getConsent: () => "allowed", publish: (e) => results.push(e) });
    expect(r.complete("invoice")).toBe(false);
    expect(r.begin("invoice")).toBe(true);
    expect(r.begin("invoice")).toBe(false);
    expect(r.complete("invoice")).toBe(true);
    expect(r.complete("invoice")).toBe(false);
    expect(results.map((e) => e.event)).toEqual(["tool_started", "tool_completed"]);
    r.resetAttempt("invoice");
    expect(r.begin("invoice")).toBe(true);
  });

  it("counts a PDF export only after a confirmed successful attempt", () => {
    const results: GrowthEvent[] = [];
    const r = createGrowthReporter({ getConsent: () => "allowed", publish: (e) => results.push(e) });
    expect(r.exportCompleted("invoice")).toBe(false);
    r.begin("invoice");
    expect(r.exportStarted("invoice")).toBe(true);
    expect(r.exportStarted("invoice")).toBe(false);
    expect(r.exportFailed("invoice")).toBe(true);
    expect(r.exportCompleted("invoice")).toBe(false);
    expect(r.exportStarted("invoice")).toBe(true);
    expect(r.exportCompleted("invoice")).toBe(true);
    expect(results.map((e) => e.event)).toEqual([
      "tool_started", "export_started", "export_failed", "export_started", "export_completed",
    ]);
    expect(JSON.stringify(results)).not.toContain("51824753556");
  });
});
