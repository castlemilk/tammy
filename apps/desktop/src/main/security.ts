import { constants } from "node:fs";
import { lstat, open, realpath } from "node:fs/promises";
import { extname, isAbsolute, join, relative, resolve, sep } from "node:path";

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
export const MAX_RENDERER_ASSET_BYTES = 16 * 1024 * 1024;

const PRODUCTION_CSP =
  "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'";
const CSP_HEADER = "Content-Security-Policy";
const CUSTOM_SCHEME = "tammy";
const LOOPBACK_HOSTS = new Set(["localhost", "127.0.0.1", "[::1]"]);
const RESERVED_WINDOWS_BASENAME = /^(?:con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i;
const OPEN_READ_ONLY_NO_FOLLOW = constants.O_RDONLY | (constants.O_NOFOLLOW ?? 0);

const CONTENT_TYPES: Readonly<Record<string, string>> = Object.freeze({
  ".css": "text/css; charset=utf-8",
  ".gif": "image/gif",
  ".html": "text/html; charset=utf-8",
  ".ico": "image/x-icon",
  ".jpeg": "image/jpeg",
  ".jpg": "image/jpeg",
  ".js": "text/javascript; charset=utf-8",
  ".json": "application/json; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".wasm": "application/wasm",
  ".webp": "image/webp",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
});

export interface RendererSecurityPolicy {
  readonly applicationUrl: string;
  readonly contentSecurityPolicy: string;
  readonly responseFilter: WebRequestFilter;
}

interface RendererFileStats {
  readonly dev: number;
  readonly ino: number;
  readonly size: number;
  isDirectory(): boolean;
  isFile(): boolean;
  isSymbolicLink(): boolean;
}

interface RendererFileHandle {
  close(): Promise<void>;
  readFile(): Promise<Uint8Array>;
  stat(): Promise<RendererFileStats>;
}

export interface RendererFileSystem {
  lstat(path: string): Promise<RendererFileStats>;
  open(path: string, flags: number): Promise<RendererFileHandle>;
  realpath(path: string): Promise<string>;
}

export interface RendererAsset {
  readonly bytes: Uint8Array;
  readonly contentType: string;
}

interface ValidatedRendererRoot {
  readonly dev: number;
  readonly ino: number;
  readonly path: string;
}

interface ValidatedRendererAsset {
  readonly components: readonly ComponentIdentity[];
  readonly path: string;
}

interface ComponentIdentity {
  readonly dev: number;
  readonly ino: number;
  readonly path: string;
}

interface LeaseRecord {
  readonly leases: Set<symbol>;
}

interface SchemeRecord extends LeaseRecord {}

interface ProtocolRecord extends LeaseRecord {
  readonly root: ValidatedRendererRoot;
}

interface ContentSecurityRecord extends LeaseRecord {
  readonly policy: RendererSecurityPolicy;
  readonly policyKey: string;
}

interface WindowGuardRecord extends LeaseRecord {
  readonly applicationUrl: string;
}

type SchemeApp = Pick<App, "enableSandbox" | "isReady">;
type SchemeProtocol = Pick<Protocol, "registerSchemesAsPrivileged">;
type ApplicationProtocol = Pick<Protocol, "handle" | "isProtocolHandled">;
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

interface ApplicationProtocolOptions {
  readonly app: Pick<App, "isReady">;
  readonly protocol: ApplicationProtocol;
  readonly rendererRoot: string;
}

// Electron patches these Node fs operations for files inside ASAR archives. Packaged
// archives are immutable; the identity and no-follow checks also protect unpacked and
// development renderer roots where filesystem entries can change between operations.
const nodeRendererFileSystem: RendererFileSystem = {
  lstat,
  open,
  realpath,
};

/**
 * Electron's singleton setter APIs have no corresponding getter, so ownership cannot
 * be re-verified before teardown. This registrar is the composition root's sole owner
 * of Tammy's scheme/protocol/CSP/permission/window handlers. Leases are reference
 * counted, but deny handlers remain installed monotonically rather than risk a stale
 * release clearing an active or externally replaced security handler.
 */
const electronSecurityRegistrar = {
  contentSecurity: new WeakMap<object, ContentSecurityRecord>(),
  protocols: new WeakMap<object, ProtocolRecord>(),
  schemes: new WeakMap<object, SchemeRecord>(),
  sessions: new WeakMap<object, LeaseRecord>(),
  windows: new WeakMap<object, WindowGuardRecord>(),
};

function createLease(record: LeaseRecord): () => void {
  const token = Symbol("electron-security-lease");
  record.leases.add(token);
  let released = false;

  return () => {
    if (released) {
      return;
    }
    released = true;
    record.leases.delete(token);
  };
}

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

function isWindowsAmbiguousSegment(segment: string): boolean {
  return (
    segment.includes(":") ||
    segment.endsWith(".") ||
    segment.endsWith(" ") ||
    RESERVED_WINDOWS_BASENAME.test(segment)
  );
}

function rawRendererSegments(url: string): readonly string[] | null {
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

  const normalizedPath = decoded === "/" ? "/index.html" : decoded;
  const segments = normalizedPath.slice(1).split("/");
  if (
    normalizedPath.includes("\0") ||
    normalizedPath.includes("\\") ||
    /%(?:2e|2f|5c)/i.test(normalizedPath) ||
    segments.some(
      (segment) =>
        segment === "" || segment === "." || segment === ".." || isWindowsAmbiguousSegment(segment),
    )
  ) {
    return null;
  }

  return segments;
}

function isContainedPath(root: string, candidate: string): boolean {
  const relativePath = relative(root, candidate);
  return (
    relativePath === "" ||
    (!relativePath.startsWith(`..${sep}`) && relativePath !== ".." && !isAbsolute(relativePath))
  );
}

function sameIdentity(
  left: Pick<RendererFileStats, "dev" | "ino">,
  right: Pick<RendererFileStats, "dev" | "ino">,
): boolean {
  return left.dev === right.dev && left.ino === right.ino;
}

async function validateRendererRoot(
  rendererRoot: string,
  fileSystem: RendererFileSystem,
): Promise<ValidatedRendererRoot | null> {
  try {
    const configuredRoot = resolve(rendererRoot);
    const configuredStats = await fileSystem.lstat(configuredRoot);
    if (configuredStats.isSymbolicLink() || !configuredStats.isDirectory()) {
      return null;
    }

    const canonicalRoot = await fileSystem.realpath(configuredRoot);
    const canonicalStats = await fileSystem.lstat(canonicalRoot);
    if (
      canonicalStats.isSymbolicLink() ||
      !canonicalStats.isDirectory() ||
      !sameIdentity(configuredStats, canonicalStats)
    ) {
      return null;
    }

    return {
      dev: canonicalStats.dev,
      ino: canonicalStats.ino,
      path: canonicalRoot,
    };
  } catch {
    return null;
  }
}

async function validateRendererAsset(
  segments: readonly string[],
  root: ValidatedRendererRoot,
  fileSystem: RendererFileSystem,
): Promise<ValidatedRendererAsset | null> {
  try {
    const currentRootStats = await fileSystem.lstat(root.path);
    if (
      currentRootStats.isSymbolicLink() ||
      !currentRootStats.isDirectory() ||
      !sameIdentity(root, currentRootStats)
    ) {
      return null;
    }

    const components: ComponentIdentity[] = [];
    let candidate = root.path;
    for (const [index, segment] of segments.entries()) {
      candidate = join(candidate, segment);
      const stats = await fileSystem.lstat(candidate);
      const finalComponent = index === segments.length - 1;
      if (stats.isSymbolicLink() || (finalComponent ? !stats.isFile() : !stats.isDirectory())) {
        return null;
      }
      components.push({ dev: stats.dev, ino: stats.ino, path: candidate });
    }

    const canonicalCandidate = await fileSystem.realpath(candidate);
    if (!isContainedPath(root.path, canonicalCandidate) || canonicalCandidate !== candidate) {
      return null;
    }

    return { components, path: candidate };
  } catch {
    return null;
  }
}

async function identitiesRemainStable(
  root: ValidatedRendererRoot,
  asset: ValidatedRendererAsset,
  fileSystem: RendererFileSystem,
): Promise<boolean> {
  try {
    const rootStats = await fileSystem.lstat(root.path);
    if (rootStats.isSymbolicLink() || !rootStats.isDirectory() || !sameIdentity(root, rootStats)) {
      return false;
    }

    for (const component of asset.components) {
      const stats = await fileSystem.lstat(component.path);
      if (stats.isSymbolicLink() || !sameIdentity(component, stats)) {
        return false;
      }
    }
    return true;
  } catch {
    return false;
  }
}

async function resolveRendererAsset(
  url: string,
  rendererRoot: string,
  fileSystem: RendererFileSystem,
): Promise<{
  readonly asset: ValidatedRendererAsset;
  readonly root: ValidatedRendererRoot;
} | null> {
  const segments = rawRendererSegments(url);
  if (segments === null) {
    return null;
  }

  const root = await validateRendererRoot(rendererRoot, fileSystem);
  if (root === null) {
    return null;
  }
  const asset = await validateRendererAsset(segments, root, fileSystem);
  return asset === null ? null : { asset, root };
}

export async function resolveRendererAssetPath(
  url: string,
  rendererRoot: string,
): Promise<string | null> {
  const validated = await resolveRendererAsset(url, rendererRoot, nodeRendererFileSystem);
  return validated?.asset.path ?? null;
}

async function readRendererAssetFromRoot(
  url: string,
  expectedRoot: ValidatedRendererRoot | undefined,
  rendererRoot: string,
  fileSystem: RendererFileSystem,
): Promise<RendererAsset | null> {
  const validated = await resolveRendererAsset(url, rendererRoot, fileSystem);
  if (
    validated === null ||
    (expectedRoot !== undefined && !sameIdentity(expectedRoot, validated.root))
  ) {
    return null;
  }

  let handle: RendererFileHandle | undefined;
  try {
    handle = await fileSystem.open(validated.asset.path, OPEN_READ_ONLY_NO_FOLLOW);
    const openedStats = await handle.stat();
    const finalIdentity = validated.asset.components.at(-1);
    if (
      finalIdentity === undefined ||
      !openedStats.isFile() ||
      !sameIdentity(finalIdentity, openedStats) ||
      openedStats.size < 0 ||
      openedStats.size > MAX_RENDERER_ASSET_BYTES ||
      !(await identitiesRemainStable(validated.root, validated.asset, fileSystem))
    ) {
      return null;
    }

    const bytes = await handle.readFile();
    if (bytes.byteLength > MAX_RENDERER_ASSET_BYTES) {
      return null;
    }

    return Object.freeze({
      bytes: new Uint8Array(bytes),
      contentType:
        CONTENT_TYPES[extname(validated.asset.path).toLowerCase()] ?? "application/octet-stream",
    });
  } catch {
    return null;
  } finally {
    if (handle !== undefined) {
      await handle.close().catch(() => undefined);
    }
  }
}

export async function readRendererAsset(
  url: string,
  rendererRoot: string,
  fileSystem: RendererFileSystem = nodeRendererFileSystem,
): Promise<RendererAsset | null> {
  return readRendererAssetFromRoot(url, undefined, rendererRoot, fileSystem);
}

export function installApplicationScheme(options: {
  readonly app: SchemeApp;
  readonly protocol: SchemeProtocol;
}): () => void {
  if (options.app.isReady()) {
    throw new Error("SCHEME_REGISTRATION_TOO_LATE");
  }

  const existing = electronSecurityRegistrar.schemes.get(options.protocol);
  if (existing) {
    return createLease(existing);
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

  const record: SchemeRecord = { leases: new Set() };
  electronSecurityRegistrar.schemes.set(options.protocol, record);
  return createLease(record);
}

function notFoundResponse(): Response {
  return new Response("Not found.", {
    headers: {
      "Content-Type": "text/plain; charset=utf-8",
      "X-Content-Type-Options": "nosniff",
    },
    status: 404,
  });
}

function assetResponse(asset: RendererAsset): Response {
  const body = new Uint8Array(asset.bytes.byteLength);
  body.set(asset.bytes);
  return new Response(body.buffer, {
    headers: {
      "Content-Length": asset.bytes.byteLength.toString(),
      "Content-Type": asset.contentType,
      "X-Content-Type-Options": "nosniff",
    },
    status: 200,
  });
}

export async function installApplicationProtocol(
  options: ApplicationProtocolOptions,
): Promise<() => void> {
  if (!options.app.isReady()) {
    throw new Error("PROTOCOL_REQUIRES_READY");
  }

  const root = await validateRendererRoot(options.rendererRoot, nodeRendererFileSystem);
  if (root === null) {
    throw new Error("INVALID_RENDERER_ROOT");
  }

  const existing = electronSecurityRegistrar.protocols.get(options.protocol);
  if (existing) {
    if (existing.root.path !== root.path || !sameIdentity(existing.root, root)) {
      throw new Error("PROTOCOL_ALREADY_CONFIGURED");
    }
    if (!options.protocol.isProtocolHandled(CUSTOM_SCHEME)) {
      throw new Error("PROTOCOL_OWNERSHIP_LOST");
    }
    return createLease(existing);
  }

  if (options.protocol.isProtocolHandled(CUSTOM_SCHEME)) {
    throw new Error("PROTOCOL_HANDLER_NOT_OWNED");
  }

  options.protocol.handle(CUSTOM_SCHEME, async (request) => {
    if (request.method !== "GET") {
      return notFoundResponse();
    }

    const asset = await readRendererAssetFromRoot(
      request.url,
      root,
      root.path,
      nodeRendererFileSystem,
    );
    return asset === null ? notFoundResponse() : assetResponse(asset);
  });

  const record: ProtocolRecord = { leases: new Set(), root };
  electronSecurityRegistrar.protocols.set(options.protocol, record);
  return createLease(record);
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
  const existing = electronSecurityRegistrar.contentSecurity.get(session);
  if (existing) {
    if (existing.policyKey !== policyKey) {
      throw new Error("CSP_ALREADY_CONFIGURED");
    }
    return createLease(existing);
  }

  const record: ContentSecurityRecord = {
    leases: new Set(),
    policy,
    policyKey,
  };
  const listener = (
    details: OnHeadersReceivedListenerDetails,
    callback: (response: { readonly responseHeaders: Record<string, string[]> }) => void,
  ): void => {
    callback({
      responseHeaders: withSingleContentSecurityPolicy(
        details.responseHeaders,
        record.policy.contentSecurityPolicy,
      ),
    });
  };
  session.webRequest.onHeadersReceived(policy.responseFilter, listener);
  electronSecurityRegistrar.contentSecurity.set(session, record);
  return createLease(record);
}

export function installSessionGuards(session: GuardedSession): () => void {
  const existing = electronSecurityRegistrar.sessions.get(session);
  if (existing) {
    return createLease(existing);
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

  const record: LeaseRecord = { leases: new Set() };
  electronSecurityRegistrar.sessions.set(session, record);
  return createLease(record);
}

export function installWindowGuards(
  webContents: GuardedWebContents,
  applicationUrl: string,
): () => void {
  if (!isTrustedApplicationURL(applicationUrl)) {
    throw new Error("INVALID_APPLICATION_URL");
  }

  const existing = electronSecurityRegistrar.windows.get(webContents);
  if (existing) {
    if (existing.applicationUrl !== applicationUrl) {
      throw new Error("WINDOW_GUARDS_ALREADY_CONFIGURED");
    }
    return createLease(existing);
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

  const record: WindowGuardRecord = {
    applicationUrl,
    leases: new Set(),
  };
  electronSecurityRegistrar.windows.set(webContents, record);
  return createLease(record);
}
