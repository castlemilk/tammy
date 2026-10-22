export type WorkspaceAccess = "authenticated" | "locked" | "signed-out" | "unconfigured";

export interface ResolvedAppLocation {
  readonly notice?: string;
  readonly path: string;
  readonly returnTo?: string;
}

const JOURNAL_ID = /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
const BAS_PREPARATION_ID = JOURNAL_ID;
const SHA256_HEX = /^[0-9a-f]{64}$/;
const UINT64_MAX = 18446744073709551615n;
const ROUTER_ORIGIN = "https://tammy.invalid";

export interface BasPreparationRouteTarget {
  readonly id: string;
  readonly revision: bigint;
  readonly hash: string;
}

export function parseBasPreparationRouteTarget(
  path: string,
): BasPreparationRouteTarget | undefined {
  let url: URL;
  try {
    url = new URL(path, ROUTER_ORIGIN);
  } catch {
    return undefined;
  }
  if (url.origin !== ROUTER_ORIGIN || url.pathname !== "/gst-bas" || !url.search) return undefined;
  const keys = [...url.searchParams.keys()];
  const preparations = url.searchParams.getAll("preparation");
  const revisions = url.searchParams.getAll("revision");
  const hashes = url.searchParams.getAll("hash");
  const preparation = preparations[0];
  const revisionText = revisions[0];
  const hash = hashes[0];
  if (
    keys.length !== 3 ||
    keys[0] !== "preparation" ||
    keys[1] !== "revision" ||
    keys[2] !== "hash" ||
    preparations.length !== 1 ||
    revisions.length !== 1 ||
    hashes.length !== 1 ||
    preparation === undefined ||
    !BAS_PREPARATION_ID.test(preparation) ||
    revisionText === undefined ||
    !/^[1-9][0-9]{0,19}$/.test(revisionText) ||
    hash === undefined ||
    !SHA256_HEX.test(hash)
  )
    return undefined;
  const revision = BigInt(revisionText);
  if (revision > UINT64_MAX) return undefined;
  return { id: preparation, revision, hash };
}

export const COMPLETE_ROUTES = Object.freeze([
  "/overview",
  "/documents",
  "/invoices",
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
  "/settings/sbr",
  "/settings/ownership",
  "/accounting/chart",
  "/accounting/opening-balances",
  "/accounting/journals",
  "/accounting/trial-balance",
  "/accounting/periods",
  "/audit",
] as const);

const completeRoutes = new Set<string>(COMPLETE_ROUTES);

function authenticatedLocation(rawLocation: string): ResolvedAppLocation {
  if (
    !rawLocation.startsWith("/") ||
    rawLocation.startsWith("//") ||
    rawLocation.includes("\\") ||
    rawLocation.includes("#")
  ) {
    return { notice: "That page is not available.", path: "/overview" };
  }
  let url: URL;
  try {
    url = new URL(rawLocation, ROUTER_ORIGIN);
  } catch {
    return { notice: "That page is not available.", path: "/overview" };
  }

  if (url.origin !== ROUTER_ORIGIN || url.hash !== "") {
    return { notice: "That page is not available.", path: "/overview" };
  }
  if (`${url.pathname}${url.search}` !== rawLocation) {
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

  if (url.pathname === "/settings/sbr") {
    if (url.search === "" || url.search === "?doctor=1") {
      return { path: `${url.pathname}${url.search}` };
    }
    return { notice: "That page is not available.", path: "/overview" };
  }

  if (url.pathname === "/gst-bas") {
    if (url.search === "") return { path: url.pathname };
    if (
      `${url.pathname}${url.search}` === rawLocation &&
      parseBasPreparationRouteTarget(rawLocation)
    )
      return { path: rawLocation };
    return { notice: "That BAS preparation link is not valid.", path: "/overview" };
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
