export type WorkspaceAccess = "authenticated" | "locked" | "signed-out" | "unconfigured";

export interface ResolvedAppLocation {
  readonly notice?: string;
  readonly path: string;
  readonly returnTo?: string;
}

const JOURNAL_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export const COMPLETE_ROUTES = Object.freeze([
  "/overview",
  "/documents",
  "/banking",
  "/gst-bas",
  "/workspace-trust",
  "/restore",
  "/restore/evidence",
  "/settings",
  "/settings/security",
  "/settings/backup",
  "/settings/users",
  "/settings/organisation",
  "/settings/ownership",
  "/accounting/chart",
  "/accounting/opening-balances",
  "/accounting/journals",
  "/accounting/general-ledger",
  "/accounting/trial-balance",
  "/accounting/periods",
  "/audit",
] as const);

const completeRoutes = new Set<string>(COMPLETE_ROUTES);

function authenticatedLocation(rawLocation: string): ResolvedAppLocation {
  let url: URL;
  try {
    url = new URL(rawLocation, "https://tammy.invalid");
  } catch {
    return { notice: "That page is not available.", path: "/overview" };
  }

  if (url.pathname === "/accounting/journals") {
    if (url.search === "") return { path: url.pathname };
    const keys = [...url.searchParams.keys()];
    const journals = url.searchParams.getAll("journal");
    const journal = journals[0];
    if (
      keys.length === 1 &&
      keys[0] === "journal" &&
      journals.length === 1 &&
      journal !== undefined &&
      JOURNAL_ID.test(journal)
    ) {
      return { path: `${url.pathname}?journal=${journal}` };
    }
    return { notice: "That journal link is not valid.", path: "/accounting/journals" };
  }

  if (url.search !== "" || !completeRoutes.has(url.pathname)) {
    return { notice: "That page is not available.", path: "/overview" };
  }
  return { path: url.pathname };
}

export function resolveAppLocation(
  rawLocation: string,
  access: WorkspaceAccess,
): ResolvedAppLocation {
  if (rawLocation === "/privacy") return { path: "/privacy" };

  if (access === "unconfigured") {
    return { path: "/setup/workspace" };
  }

  if (access === "locked") {
    if (rawLocation === "/unlock") return { path: "/unlock" };
    const requested = authenticatedLocation(rawLocation);
    return { path: "/unlock", returnTo: requested.path };
  }

  if (access === "signed-out") {
    if (rawLocation === "/sign-in") return { path: "/sign-in" };
    const requested = authenticatedLocation(rawLocation);
    return { path: "/sign-in", returnTo: requested.path };
  }

  return authenticatedLocation(rawLocation);
}
