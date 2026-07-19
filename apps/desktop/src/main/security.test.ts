import { mkdir, mkdtemp, realpath, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  createRendererSecurityPolicy,
  createSecureWebPreferences,
  denyPermission,
  denyWindowOpen,
  installApplicationProtocol,
  installApplicationScheme,
  installContentSecurityPolicy,
  installSessionGuards,
  installWindowGuards,
  isAllowedApplicationURL,
  isAllowedNavigation,
  PRODUCTION_APP_URL,
  resolveRendererAssetPath,
  withSingleContentSecurityPolicy,
} from "./security";

const temporaryDirectories: string[] = [];

afterEach(async () => {
  await Promise.all(
    temporaryDirectories
      .splice(0)
      .map((directory) => rm(directory, { force: true, recursive: true })),
  );
});

describe("renderer security policy", () => {
  it("uses the exact offline production URL and CSP", () => {
    const policy = createRendererSecurityPolicy();

    expect(policy).toEqual({
      applicationUrl: PRODUCTION_APP_URL,
      contentSecurityPolicy:
        "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'",
      responseFilter: { urls: ["tammy://app/*"] },
    });
    expect(policy.contentSecurityPolicy).not.toContain("unsafe-eval");
    expect(policy.contentSecurityPolicy).not.toMatch(/https?:\/\//);
  });

  it.each([
    ["http://localhost:5173", "http://localhost:5173/", "ws://localhost:5173"],
    ["http://127.0.0.1:4173/", "http://127.0.0.1:4173/", "ws://127.0.0.1:4173"],
    ["https://[::1]:5173", "https://[::1]:5173/", "wss://[::1]:5173"],
  ])(
    "builds a development CSP for only the configured Vite origin %s",
    (viteUrl, applicationUrl, websocketOrigin) => {
      const policy = createRendererSecurityPolicy(viteUrl);

      expect(policy.applicationUrl).toBe(applicationUrl);
      expect(policy.responseFilter).toEqual({ urls: [`${applicationUrl}*`] });
      expect(policy.contentSecurityPolicy).toContain("default-src 'self'");
      expect(policy.contentSecurityPolicy).toContain("script-src 'self'");
      expect(policy.contentSecurityPolicy).toContain(`connect-src 'self' ${websocketOrigin}`);
      expect(policy.contentSecurityPolicy).toContain("style-src 'self' 'unsafe-inline'");
      expect(policy.contentSecurityPolicy).not.toContain("unsafe-eval");
      expect(policy.contentSecurityPolicy.match(/unsafe-inline/g)).toHaveLength(1);
      expect(policy.contentSecurityPolicy).not.toContain("*");
    },
  );

  it.each([
    "http://example.com:5173",
    "https://192.0.2.1:5173",
    "ftp://localhost:5173",
    "http://user@localhost:5173",
    "http://localhost:80",
    "http://LOCALHOST:5173",
    "http://localhost:5173/path",
    "http://localhost:5173/?query",
    "http://localhost:5173/#fragment",
    "http://localhost:5173.evil",
  ])("rejects unsafe or noncanonical Vite URL %s", (viteUrl) => {
    expect(() => createRendererSecurityPolicy(viteUrl)).toThrow("INVALID_VITE_ORIGIN");
  });

  it.each([
    ["exact production URL", PRODUCTION_APP_URL, PRODUCTION_APP_URL, true],
    ["lookalike host", "tammy://app.evil/", PRODUCTION_APP_URL, false],
    ["credentials", "tammy://app@evil/", PRODUCTION_APP_URL, false],
    ["host case", "tammy://APP/", PRODUCTION_APP_URL, false],
    ["path", "tammy://app/index.html", PRODUCTION_APP_URL, false],
    ["file", "file:///index.html", PRODUCTION_APP_URL, false],
    ["data", "data:text/html,hello", PRODUCTION_APP_URL, false],
    ["javascript", "javascript:alert(1)", PRODUCTION_APP_URL, false],
    ["http", "http://localhost:5173/", PRODUCTION_APP_URL, false],
    ["exact development URL", "http://localhost:5173/", "http://localhost:5173/", true],
    ["development subpath", "http://localhost:5173/dashboard", "http://localhost:5173/", false],
    ["development default port trick", "http://localhost:80/", "http://localhost/", false],
  ])("%s application URL decision is %s", (_name, candidate, allowed, expected) => {
    expect(isAllowedApplicationURL(candidate, allowed)).toBe(expected);
  });
});

describe("default-deny policies", () => {
  it("denies every permission and window-open request", () => {
    expect(denyPermission()).toBe(false);
    expect(denyPermission("notifications")).toBe(false);
    expect(denyWindowOpen()).toEqual({ action: "deny" });
    expect(denyWindowOpen({ url: PRODUCTION_APP_URL })).toEqual({ action: "deny" });
  });

  it.each([
    ["same already loaded app URL", PRODUCTION_APP_URL, PRODUCTION_APP_URL, true],
    ["not loaded yet", "", PRODUCTION_APP_URL, false],
    ["target subpath", PRODUCTION_APP_URL, "tammy://app/settings", false],
    ["current lookalike", "tammy://app.evil/", PRODUCTION_APP_URL, false],
    ["external target", PRODUCTION_APP_URL, "https://example.com/", false],
  ])("%s navigation decision is %s", (_name, current, target, expected) => {
    expect(isAllowedNavigation(current, target, PRODUCTION_APP_URL)).toBe(expected);
  });
});

describe("custom renderer protocol", () => {
  async function fixture(): Promise<{
    readonly outside: string;
    readonly root: string;
  }> {
    const directory = await mkdtemp(join(tmpdir(), "tammy-security-"));
    temporaryDirectories.push(directory);
    const root = join(directory, "renderer");
    const outside = join(directory, "outside.txt");
    await mkdir(join(root, "assets"), { recursive: true });
    await writeFile(join(root, "index.html"), "index");
    await writeFile(join(root, "assets", "app.js"), "asset");
    await symlink(join(root, "assets", "app.js"), join(root, "assets", "alias.js"));
    await writeFile(outside, "secret");
    await symlink(outside, join(root, "assets", "escape.js"));
    return { outside, root };
  }

  it.each([
    ["tammy://app/", "index.html"],
    ["tammy://app/assets/app.js", join("assets", "app.js")],
  ])("maps %s within the compiled renderer root", async (url, expected) => {
    const { root } = await fixture();

    await expect(resolveRendererAssetPath(url, root)).resolves.toBe(
      await realpath(join(root, expected)),
    );
  });

  it.each([
    "tammy://app",
    "tammy://APP/",
    "TAMMY://app/",
    "tammy://app.evil/",
    "tammy://app@evil/",
    "tammy://app:443/",
    "tammy://app//assets/app.js",
    "tammy://app/../outside.txt",
    "tammy://app/%2e%2e/outside.txt",
    "tammy://app/%252e%252e/outside.txt",
    "tammy://app/assets%2fapp.js",
    "tammy://app/assets%5capp.js",
    "tammy://app/assets/app.js?download",
    "tammy://app/assets/app.js#fragment",
    "tammy://app/assets",
    "file:///index.html",
    "data:text/html,hello",
  ])("does not map confused or traversing URL %s", async (url) => {
    const { root } = await fixture();

    await expect(resolveRendererAssetPath(url, root)).resolves.toBeNull();
  });

  it("does not follow a renderer symlink outside the compiled root", async () => {
    const { root } = await fixture();

    await expect(
      resolveRendererAssetPath("tammy://app/assets/escape.js", root),
    ).resolves.toBeNull();
  });

  it("does not serve a symlink whose target remains inside the renderer root", async () => {
    const { root } = await fixture();

    await expect(resolveRendererAssetPath("tammy://app/assets/alias.js", root)).resolves.toBeNull();
  });

  it("registers the standard secure fetch-enabled scheme and sandbox before ready", () => {
    const calls: string[] = [];
    const app = {
      enableSandbox: vi.fn(() => calls.push("sandbox")),
      isReady: () => false,
    };
    const protocol = {
      registerSchemesAsPrivileged: vi.fn(() => calls.push("scheme")),
    };

    installApplicationScheme({ app, protocol });

    expect(calls).toEqual(["scheme", "sandbox"]);
    expect(protocol.registerSchemesAsPrivileged).toHaveBeenCalledWith([
      {
        scheme: "tammy",
        privileges: {
          secure: true,
          standard: true,
          supportFetchAPI: true,
        },
      },
    ]);
  });

  it("refuses to register the privileged scheme after Electron is ready", () => {
    const app = {
      enableSandbox: vi.fn(),
      isReady: () => true,
    };
    const protocol = {
      registerSchemesAsPrivileged: vi.fn(),
    };

    expect(() => installApplicationScheme({ app, protocol })).toThrow(
      "SCHEME_REGISTRATION_TOO_LATE",
    );
    expect(protocol.registerSchemesAsPrivileged).not.toHaveBeenCalled();
    expect(app.enableSandbox).not.toHaveBeenCalled();
  });

  it("serves only GET requests for contained renderer assets after ready", async () => {
    const { root } = await fixture();
    let handler: ((request: { method: string; url: string }) => Promise<Response>) | undefined;
    const app = { isReady: () => true };
    const protocol = {
      handle: vi.fn(
        (
          _scheme: string,
          registered: (request: { method: string; url: string }) => Promise<Response>,
        ) => {
          handler = registered;
        },
      ),
      isProtocolHandled: vi.fn(() => false),
      unhandle: vi.fn(),
    };
    const fetch = vi.fn(async () => new Response("asset"));

    installApplicationProtocol({ app, fetch, protocol, rendererRoot: root });

    expect(protocol.handle).toHaveBeenCalledWith("tammy", expect.any(Function));
    await expect(
      handler?.({ method: "GET", url: "tammy://app/assets/app.js" }),
    ).resolves.toMatchObject({ status: 200 });
    expect(fetch).toHaveBeenCalledWith(expect.stringMatching(/^file:.*assets\/app\.js$/));

    const notFound = await handler?.({
      method: "POST",
      url: "tammy://app/assets/app.js",
    });
    expect(notFound?.status).toBe(404);
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("refuses to install the file protocol before Electron is ready", () => {
    const protocol = {
      handle: vi.fn(),
      isProtocolHandled: vi.fn(() => false),
      unhandle: vi.fn(),
    };

    expect(() =>
      installApplicationProtocol({
        app: { isReady: () => false },
        fetch: vi.fn(),
        protocol,
        rendererRoot: "/renderer",
      }),
    ).toThrow("PROTOCOL_REQUIRES_READY");
    expect(protocol.handle).not.toHaveBeenCalled();
  });
});

describe("Electron boundary installation", () => {
  it("returns exactly the planned secure web preferences", () => {
    expect(createSecureWebPreferences("/absolute/preload.js")).toEqual({
      preload: "/absolute/preload.js",
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      webSecurity: true,
      allowRunningInsecureContent: false,
      spellcheck: false,
    });
  });

  it("replaces any existing CSP case-insensitively with exactly one header", () => {
    const headers = withSingleContentSecurityPolicy(
      {
        "content-security-policy": ["old"],
        "Content-Security-Policy": ["duplicate"],
        "X-Test": ["preserved"],
      },
      "default-src 'none'",
    );

    expect(headers).toEqual({
      "Content-Security-Policy": ["default-src 'none'"],
      "X-Test": ["preserved"],
    });
    expect(
      Object.keys(headers).filter((name) => name.toLowerCase() === "content-security-policy"),
    ).toHaveLength(1);
  });

  it("installs one idempotent CSP response hook", () => {
    const listeners: Array<
      | ((
          details: { readonly responseHeaders?: Record<string, string[]> },
          callback: (response: { readonly responseHeaders: Record<string, string[]> }) => void,
        ) => void)
      | null
    > = [];
    const session = {
      webRequest: {
        onHeadersReceived: vi.fn(
          (
            filterOrListener: unknown,
            listener?: (
              details: { readonly responseHeaders?: Record<string, string[]> },
              callback: (response: { readonly responseHeaders: Record<string, string[]> }) => void,
            ) => void,
          ) => {
            listeners.push(listener ?? (filterOrListener as null));
          },
        ),
      },
    };
    const policy = createRendererSecurityPolicy();

    const first = installContentSecurityPolicy(session as never, policy);
    const second = installContentSecurityPolicy(session as never, policy);

    expect(first).toBe(second);
    expect(session.webRequest.onHeadersReceived).toHaveBeenCalledTimes(1);
    expect(session.webRequest.onHeadersReceived).toHaveBeenCalledWith(
      policy.responseFilter,
      expect.any(Function),
    );

    const response = vi.fn();
    listeners[0]?.(
      {
        responseHeaders: {
          "content-security-policy": ["stale"],
          "X-Test": ["preserved"],
        },
      },
      response,
    );
    expect(response).toHaveBeenCalledWith({
      responseHeaders: {
        "Content-Security-Policy": [policy.contentSecurityPolicy],
        "X-Test": ["preserved"],
      },
    });
    expect(
      Object.keys(response.mock.calls[0]?.[0]?.responseHeaders ?? {}).filter(
        (name) => name.toLowerCase() === "content-security-policy",
      ),
    ).toHaveLength(1);
  });

  it("denies permission checks, permission requests, and downloads", () => {
    let permissionCheck: (() => boolean) | undefined;
    let permissionRequest:
      | ((_contents: unknown, _permission: unknown, callback: (allowed: boolean) => void) => void)
      | undefined;
    let downloadHandler:
      | ((event: { preventDefault: () => void }, item: { cancel: () => void }) => void)
      | undefined;
    let devicePermission: (() => boolean) | undefined;
    const session = {
      on: vi.fn((_event: string, handler: typeof downloadHandler) => {
        downloadHandler = handler;
      }),
      removeListener: vi.fn(),
      setPermissionCheckHandler: vi.fn((handler: typeof permissionCheck) => {
        permissionCheck = handler;
      }),
      setPermissionRequestHandler: vi.fn((handler: typeof permissionRequest) => {
        permissionRequest = handler;
      }),
      setDevicePermissionHandler: vi.fn((handler: typeof devicePermission) => {
        devicePermission = handler;
      }),
    };

    installSessionGuards(session as never);

    expect(permissionCheck?.()).toBe(false);
    expect(devicePermission?.()).toBe(false);
    const permissionResult = vi.fn();
    permissionRequest?.({}, "notifications", permissionResult);
    expect(permissionResult).toHaveBeenCalledWith(false);
    const event = { preventDefault: vi.fn() };
    const item = { cancel: vi.fn() };
    downloadHandler?.(event, item);
    expect(event.preventDefault).toHaveBeenCalledTimes(1);
    expect(item.cancel).toHaveBeenCalledTimes(1);
  });

  it("denies navigation except an exact reload and denies every new window", () => {
    let navigationHandler:
      | ((event: { preventDefault: () => void }, url: string) => void)
      | undefined;
    let windowOpenHandler: (() => { action: "deny" }) | undefined;
    const webContents = {
      getURL: vi.fn(() => PRODUCTION_APP_URL),
      on: vi.fn((_event: string, handler: typeof navigationHandler) => {
        navigationHandler = handler;
      }),
      removeListener: vi.fn(),
      setWindowOpenHandler: vi.fn((handler: typeof windowOpenHandler) => {
        windowOpenHandler = handler;
      }),
    };

    installWindowGuards(webContents as never, PRODUCTION_APP_URL);

    const allowedEvent = { preventDefault: vi.fn() };
    navigationHandler?.(allowedEvent, PRODUCTION_APP_URL);
    expect(allowedEvent.preventDefault).not.toHaveBeenCalled();

    const deniedEvent = { preventDefault: vi.fn() };
    navigationHandler?.(deniedEvent, "https://example.com/");
    expect(deniedEvent.preventDefault).toHaveBeenCalledTimes(1);
    expect(windowOpenHandler?.()).toEqual({ action: "deny" });
  });
});
