import { lstat, realpath, stat } from "node:fs/promises";
import { isAbsolute, join, relative, resolve, sep } from "node:path";
import { pathToFileURL } from "node:url";

import type {
  App,
  BrowserWindowConstructorOptions,
  DownloadItem,
  Event,
  OnHeadersReceivedListenerDetails,
  Protocol,
  Session,
  WebContents,
  WebRequestFilter,
} from "electron";

export const PRODUCTION_APP_URL = "tammy://app/";

const PRODUCTION_CSP =
  "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'";
const CSP_HEADER = "Content-Security-Policy";
const CUSTOM_SCHEME = "tammy";
const LOOPBACK_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"]);

export interface RendererSecurityPolicy {
  readonly applicationUrl: string;
  readonly contentSecurityPolicy: string;
  readonly responseFilter: WebRequestFilter;
}

type SchemeApp = Pick<App, "enableSandbox" | "isReady">;
type SchemeProtocol = Pick<Protocol, "registerSchemesAsPrivileged">;
type ApplicationProtocol = Pick<Protocol, "handle" | "isProtocolHandled" | "unhandle">;

interface ApplicationProtocolOptions {
  readonly app: Pick<App, "isReady">;
  readonly fetch: (url: string) => Promise<Response>;
  readonly protocol: ApplicationProtocol;
  readonly rendererRoot: string;
}

type ContentSecuritySession = Pick<Session, "webRequest">;
type GuardedSession = Pick<
  Session,
  | "on"
  | "removeListener"
  | "setDevicePermissionHandler"
  | "setPermissionCheckHandler"
  | "setPermissionRequestHandler"
>;
type GuardedWebContents = Pick<
  WebContents,
  "getURL" | "on" | "removeListener" | "setWindowOpenHandler"
>;

interface InstalledContentSecurityPolicy {
  readonly dispose: () => void;
  readonly policyKey: string;
}

interface InstalledSessionGuards {
  readonly dispose: () => void;
}

interface InstalledWindowGuards {
  readonly applicationUrl: string;
  readonly dispose: () => void;
}

const contentSecurityInstallations = new WeakMap<object, InstalledContentSecurityPolicy>();
const sessionGuardInstallations = new WeakMap<object, InstalledSessionGuards>();
const windowGuardInstallations = new WeakMap<object, InstalledWindowGuards>();

function parseViteOrigin(viteUrl: string): URL {
  let parsed: URL;
  try {
    parsed = new URL(viteUrl);
  } catch {
    throw new Error("INVALID_VITE_ORIGIN");
  }

  const canonicalOrigin = parsed.origin;
  if (
    (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
    !LOOPBACK_HOSTS.has(parsed.hostname) ||
    parsed.username !== "" ||
    parsed.password !== "" ||
    parsed.pathname !== "/" ||
    parsed.search !== "" ||
    parsed.hash !== "" ||
    (viteUrl !== canonicalOrigin && viteUrl !== `${canonicalOrigin}/`)
  ) {
    throw new Error("INVALID_VITE_ORIGIN");
  }

  return parsed;
}

export function createRendererSecurityPolicy(viteUrl?: string): RendererSecurityPolicy {
  if (viteUrl === undefined) {
    return Object.freeze({
      applicationUrl: PRODUCTION_APP_URL,
      contentSecurityPolicy: PRODUCTION_CSP,
      responseFilter: { urls: ["tammy://app/*"] },
    });
  }

  const parsed = parseViteOrigin(viteUrl);
  const applicationUrl = `${parsed.origin}/`;
  const websocketProtocol = parsed.protocol === "https:" ? "wss:" : "ws:";
  const websocketOrigin = `${websocketProtocol}//${parsed.host}`;
  const contentSecurityPolicy = [
    "default-src 'self'",
    "script-src 'self'",
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data:",
    "font-src 'self'",
    `connect-src 'self' ${websocketOrigin}`,
    "object-src 'none'",
    "base-uri 'none'",
    "frame-ancestors 'none'",
    "form-action 'none'",
  ].join("; ");

  return Object.freeze({
    applicationUrl,
    contentSecurityPolicy,
    responseFilter: { urls: [`${applicationUrl}*`] },
  });
}

export function isTrustedApplicationURL(url: string): boolean {
  if (url === PRODUCTION_APP_URL) {
    return true;
  }

  try {
    return createRendererSecurityPolicy(url).applicationUrl === url;
  } catch {
    return false;
  }
}

export function isAllowedApplicationURL(candidate: string, allowed: string): boolean {
  return isTrustedApplicationURL(allowed) && candidate === allowed;
}

export function denyPermission(..._ignored: unknown[]): false {
  return false;
}

export function denyWindowOpen(..._ignored: unknown[]): { readonly action: "deny" } {
  return { action: "deny" };
}

export function isAllowedNavigation(
  currentUrl: string,
  targetUrl: string,
  applicationUrl: string,
): boolean {
  return (
    isAllowedApplicationURL(currentUrl, applicationUrl) &&
    isAllowedApplicationURL(targetUrl, applicationUrl)
  );
}

function rawRendererPath(url: string): string | null {
  const match = /^tammy:\/\/app(\/[^?#]*)$/.exec(url);
  if (!match) {
    return null;
  }

  const rawPath = match[1];
  if (
    rawPath === undefined ||
    rawPath.includes("\\") ||
    rawPath.includes("//") ||
    /%(?:2f|5c)/i.test(rawPath)
  ) {
    return null;
  }

  let decoded: string;
  try {
    decoded = decodeURIComponent(rawPath);
  } catch {
    return null;
  }

  if (
    decoded.includes("\0") ||
    decoded.includes("\\") ||
    /%(?:2e|2f|5c)/i.test(decoded) ||
    decoded.split("/").some((segment) => segment === "." || segment === "..")
  ) {
    return null;
  }

  return decoded === "/" ? "/index.html" : decoded;
}

function isContainedPath(root: string, candidate: string): boolean {
  const relativePath = relative(root, candidate);
  return (
    relativePath === "" ||
    (!relativePath.startsWith(`..${sep}`) && relativePath !== ".." && !isAbsolute(relativePath))
  );
}

export async function resolveRendererAssetPath(
  url: string,
  rendererRoot: string,
): Promise<string | null> {
  const rendererPath = rawRendererPath(url);
  if (rendererPath === null) {
    return null;
  }

  try {
    const canonicalRoot = await realpath(rendererRoot);
    const candidate = resolve(canonicalRoot, `.${rendererPath}`);
    if (!isContainedPath(canonicalRoot, candidate)) {
      return null;
    }

    const relativeCandidate = relative(canonicalRoot, candidate);
    let pathComponent = canonicalRoot;
    for (const component of relativeCandidate.split(sep)) {
      pathComponent = join(pathComponent, component);
      if ((await lstat(pathComponent)).isSymbolicLink()) {
        return null;
      }
    }

    const canonicalCandidate = await realpath(candidate);
    return isContainedPath(canonicalRoot, canonicalCandidate) &&
      (await stat(canonicalCandidate)).isFile()
      ? canonicalCandidate
      : null;
  } catch {
    return null;
  }
}

export function installApplicationScheme(options: {
  readonly app: SchemeApp;
  readonly protocol: SchemeProtocol;
}): void {
  if (options.app.isReady()) {
    throw new Error("SCHEME_REGISTRATION_TOO_LATE");
  }

  options.protocol.registerSchemesAsPrivileged([
    {
      scheme: CUSTOM_SCHEME,
      privileges: {
        secure: true,
        standard: true,
        supportFetchAPI: true,
      },
    },
  ]);
  options.app.enableSandbox();
}

function notFoundResponse(): Response {
  return new Response("Not found.", {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
    status: 404,
  });
}

export function installApplicationProtocol(options: ApplicationProtocolOptions): void {
  if (!options.app.isReady()) {
    throw new Error("PROTOCOL_REQUIRES_READY");
  }
  if (options.protocol.isProtocolHandled(CUSTOM_SCHEME)) {
    options.protocol.unhandle(CUSTOM_SCHEME);
  }

  options.protocol.handle(CUSTOM_SCHEME, async (request) => {
    if (request.method !== "GET") {
      return notFoundResponse();
    }

    const assetPath = await resolveRendererAssetPath(request.url, options.rendererRoot);
    if (assetPath === null) {
      return notFoundResponse();
    }

    try {
      return await options.fetch(pathToFileURL(assetPath).toString());
    } catch {
      return notFoundResponse();
    }
  });
}

export function createSecureWebPreferences(
  preloadPath: string,
): NonNullable<BrowserWindowConstructorOptions["webPreferences"]> {
  return Object.freeze({
    preload: preloadPath,
    sandbox: true,
    contextIsolation: true,
    nodeIntegration: false,
    webSecurity: true,
    allowRunningInsecureContent: false,
    spellcheck: false,
  });
}

export function withSingleContentSecurityPolicy(
  responseHeaders: Readonly<Record<string, readonly string[]>> | undefined,
  contentSecurityPolicy: string,
): Record<string, string[]> {
  const headers: Record<string, string[]> = {};
  for (const [name, values] of Object.entries(responseHeaders ?? {})) {
    if (name.toLowerCase() !== CSP_HEADER.toLowerCase()) {
      headers[name] = [...values];
    }
  }
  headers[CSP_HEADER] = [contentSecurityPolicy];
  return headers;
}

export function installContentSecurityPolicy(
  session: ContentSecuritySession,
  policy: RendererSecurityPolicy,
): () => void {
  const policyKey = JSON.stringify([
    policy.applicationUrl,
    policy.contentSecurityPolicy,
    policy.responseFilter.urls,
  ]);
  const existing = contentSecurityInstallations.get(session);
  if (existing) {
    if (existing.policyKey !== policyKey) {
      throw new Error("CSP_ALREADY_CONFIGURED");
    }
    return existing.dispose;
  }

  const listener = (
    details: OnHeadersReceivedListenerDetails,
    callback: (response: { readonly responseHeaders: Record<string, string[]> }) => void,
  ): void => {
    callback({
      responseHeaders: withSingleContentSecurityPolicy(
        details.responseHeaders,
        policy.contentSecurityPolicy,
      ),
    });
  };
  session.webRequest.onHeadersReceived(policy.responseFilter, listener);

  const dispose = (): void => {
    if (contentSecurityInstallations.get(session)?.dispose !== dispose) {
      return;
    }
    session.webRequest.onHeadersReceived(null);
    contentSecurityInstallations.delete(session);
  };
  contentSecurityInstallations.set(session, { dispose, policyKey });
  return dispose;
}

export function installSessionGuards(session: GuardedSession): () => void {
  const existing = sessionGuardInstallations.get(session);
  if (existing) {
    return existing.dispose;
  }

  const denyRequest: Parameters<Session["setPermissionRequestHandler"]>[0] = (
    _webContents,
    _permission,
    callback,
  ) => callback(false);
  const denyDownload = (event: Event, item: DownloadItem): void => {
    event.preventDefault();
    item.cancel();
  };

  session.setPermissionCheckHandler(denyPermission);
  session.setPermissionRequestHandler(denyRequest);
  session.setDevicePermissionHandler(denyPermission);
  session.on("will-download", denyDownload);

  const dispose = (): void => {
    if (sessionGuardInstallations.get(session)?.dispose !== dispose) {
      return;
    }
    session.setPermissionCheckHandler(null);
    session.setPermissionRequestHandler(null);
    session.setDevicePermissionHandler(null);
    session.removeListener("will-download", denyDownload);
    sessionGuardInstallations.delete(session);
  };
  sessionGuardInstallations.set(session, { dispose });
  return dispose;
}

export function installWindowGuards(
  webContents: GuardedWebContents,
  applicationUrl: string,
): () => void {
  if (!isTrustedApplicationURL(applicationUrl)) {
    throw new Error("INVALID_APPLICATION_URL");
  }

  const existing = windowGuardInstallations.get(webContents);
  if (existing) {
    if (existing.applicationUrl !== applicationUrl) {
      throw new Error("WINDOW_GUARDS_ALREADY_CONFIGURED");
    }
    return existing.dispose;
  }

  const denyNavigation = (event: Event, targetUrl: string): void => {
    let currentUrl = "";
    try {
      currentUrl = webContents.getURL();
    } catch {
      // A destroyed WebContents is never allowed to navigate.
    }
    if (!isAllowedNavigation(currentUrl, targetUrl, applicationUrl)) {
      event.preventDefault();
    }
  };

  webContents.on("will-navigate", denyNavigation);
  webContents.on("will-redirect", denyNavigation);
  webContents.setWindowOpenHandler(denyWindowOpen);

  const dispose = (): void => {
    if (windowGuardInstallations.get(webContents)?.dispose !== dispose) {
      return;
    }
    webContents.removeListener("will-navigate", denyNavigation);
    webContents.removeListener("will-redirect", denyNavigation);
    windowGuardInstallations.delete(webContents);
  };
  windowGuardInstallations.set(webContents, { applicationUrl, dispose });
  return dispose;
}
