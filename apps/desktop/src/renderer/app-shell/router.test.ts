import { describe, expect, it } from "vitest";

import { resolveAppLocation } from "./router";

const journalID = "018f2f2a-7c1d-7a62-8d11-216b8d6ea4cb";

describe("guarded walkthrough routes", () => {
  it("keeps the privacy route available at every workspace access level", () => {
    for (const access of ["unconfigured", "locked", "signed-out", "authenticated"] as const) {
      expect(resolveAppLocation("/privacy", access)).toEqual({ path: "/privacy" });
    }
  });

  it("sends a locked business route to unlock and retains the safe return route", () => {
    expect(resolveAppLocation("/accounting/trial-balance", "locked")).toEqual({
      path: "/unlock",
      returnTo: "/accounting/trial-balance",
    });
  });

  it("sends signed-out deep links to sign in and first-run links to setup", () => {
    expect(resolveAppLocation("/accounting/chart", "signed-out")).toEqual({
      path: "/sign-in",
      returnTo: "/accounting/chart",
    });
    expect(resolveAppLocation("/overview", "unconfigured")).toEqual({
      path: "/setup/workspace",
    });
  });

  it("returns authenticated unknown routes to Overview with a quiet notice", () => {
    expect(resolveAppLocation("/not-a-real-route", "authenticated")).toEqual({
      notice: "That page is not available.",
      path: "/overview",
    });
  });

  it("returns the unimplemented General Ledger route to Overview", () => {
    expect(resolveAppLocation("/accounting/general-ledger", "authenticated")).toEqual({
      notice: "That page is not available.",
      path: "/overview",
    });
  });

  it("accepts only the canonical journal selection query", () => {
    expect(
      resolveAppLocation(`/accounting/journals?journal=${journalID}`, "authenticated"),
    ).toEqual({ path: `/accounting/journals?journal=${journalID}` });

    for (const location of [
      "/accounting/journals?journal=bad-id",
      `/accounting/journals?journal=${journalID}&journal=${journalID}`,
      `/accounting/journals?journal=${journalID}&extra=true`,
    ]) {
      expect(resolveAppLocation(location, "authenticated")).toEqual({
        notice: "That journal link is not valid.",
        path: "/accounting/journals",
      });
    }
  });

  it("accepts only the exact SBR doctor query", () => {
    expect(resolveAppLocation("/settings/sbr", "authenticated")).toEqual({
      path: "/settings/sbr",
    });
    expect(resolveAppLocation("/settings/sbr?doctor=1", "authenticated")).toEqual({
      path: "/settings/sbr?doctor=1",
    });

    for (const location of [
      "/settings/sbr?doctor=0",
      "/settings/sbr?doctor=1&doctor=1",
      "/settings/sbr?doctor=1&extra=true",
      "/settings/organisation?doctor=1",
      "/settings/sbr?doctor=1#again",
      "/settings/sbr?doctor=1#",
      "https://example.invalid/settings/sbr?doctor=1",
    ]) {
      expect(resolveAppLocation(location, "authenticated")).toEqual({
        notice: "That page is not available.",
        path: "/overview",
      });
    }
  });

  it("preserves all complete nested routes without adding them to primary navigation", () => {
    for (const path of [
      "/workspace-trust",
      "/restore",
      "/restore/evidence",
      "/settings/security",
      "/settings/backup",
      "/settings/users",
      "/settings/organisation",
      "/settings/sbr",
      "/settings/ownership",
      "/accounting/opening-balances",
      "/accounting/periods",
      "/audit",
    ]) {
      expect(resolveAppLocation(path, "authenticated")).toEqual({ path });
    }
  });
});
