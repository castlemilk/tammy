import { mkdir, mkdtemp, realpath, rename, rm, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  createRendererSecurityPolicy,
  createSecureWebPreferences,
  denyPermission,
  denyWindowOpen,
  detectAsarBoundary,
  installApplicationProtocol,
  installApplicationScheme,
  installContentSecurityPolicy,
  installSessionGuards,
  installWindowGuards,
  isAllowedApplicationDocumentURL,
  isAllowedApplicationURL,
  isAllowedNavigation,
  MAX_RENDERER_ASSET_BYTES,
  PRODUCTION_APP_URL,
  readRendererAsset,
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

  it.each([
    ["production route", "tammy://app/overview", PRODUCTION_APP_URL, true],
    [
      "production route query",
      "tammy://app/accounting/journals?journal=018f0000-0000-7000-8000-000000000001",
      PRODUCTION_APP_URL,
      true,
    ],
    ["development route", "http://localhost:5173/overview", "http://localhost:5173/", true],
    ["lookalike host", "tammy://app.evil/overview", PRODUCTION_APP_URL, false],
    ["foreign development port", "http://localhost:5174/overview", "http://localhost:5173/", false],
    ["fragment", "tammy://app/overview#unsafe", PRODUCTION_APP_URL, false],
  ])("classifies an application document URL for %s", (_name, candidate, allowed, expected) => {
    expect(isAllowedApplicationDocumentURL(candidate, allowed)).toBe(expected);
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
    "tammy://app/assets/app.js:secret",
    "tammy://app/assets/app.js%3Asecret",
    "tammy://app/assets/trailing.",
    "tammy://app/assets/trailing%20",
    "tammy://app/CON",
    "tammy://app/con.txt",
    "tammy://app/PRN.css",
    "tammy://app/AUX",
    "tammy://app/NUL.js",
    "tammy://app/COM1.js",
    "tammy://app/com9",
    "tammy://app/LPT1.css",
    "tammy://app/lpt9",
    "file:///index.html",
    "data:text/html,hello",
  ])("does not map confused or traversing URL %s", async (url) => {
    const { root } = await fixture();

    await expect(resolveRendererAssetPath(url, root)).resolves.toBeNull();
  });

  it("detects only a complete ASAR path component", () => {
    const base = resolve("/physical");

    expect(detectAsarBoundary(join(base, "app.asar", "renderer"))).toEqual({
      archivePath: join(base, "app.asar"),
      internalRootSegments: ["renderer"],
    });
    expect(detectAsarBoundary(join(base, "APP.ASAR", "renderer"))).toEqual({
      archivePath: join(base, "APP.ASAR"),
      internalRootSegments: ["renderer"],
    });
    expect(detectAsarBoundary(join(base, "app.asar.unpacked", "renderer"))).toBeNull();
    expect(detectAsarBoundary(join(base, "app.asar.txt", "renderer"))).toBeNull();
    expect(detectAsarBoundary(join(base, "not-asar", "renderer"))).toBeNull();
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

  it("rejects a renderer root that is a symlink or regular file", async () => {
    const { outside, root } = await fixture();
    const rootLink = join(root, "..", "renderer-link");
    await symlink(root, rootLink);

    await expect(resolveRendererAssetPath("tammy://app/", rootLink)).resolves.toBeNull();
    await expect(resolveRendererAssetPath("tammy://app/", outside)).resolves.toBeNull();
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

    const first = installApplicationScheme({ app, protocol });
    const second = installApplicationScheme({ app, protocol });

    expect(first).not.toBe(second);
    expect(calls).toEqual(["scheme", "sandbox"]);
    first();
    first();
    second();
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

  it("refuses a symlink or non-directory protocol root before registering a handler", async () => {
    const { outside, root } = await fixture();
    const rootLink = join(root, "..", "protocol-root-link");
    await symlink(root, rootLink);
    const protocol = {
      handle: vi.fn(),
      isProtocolHandled: vi.fn(() => false),
      unhandle: vi.fn(),
    };

    await expect(
      installApplicationProtocol({
        app: { isReady: () => true },
        protocol,
        rendererRoot: rootLink,
      }),
    ).rejects.toThrow("INVALID_RENDERER_ROOT");
    await expect(
      installApplicationProtocol({
        app: { isReady: () => true },
        protocol,
        rendererRoot: outside,
      }),
    ).rejects.toThrow("INVALID_RENDERER_ROOT");
    expect(protocol.handle).not.toHaveBeenCalled();
    expect(protocol.unhandle).not.toHaveBeenCalled();
  });

  it("serves exact file bytes with conservative response headers after ready", async () => {
    const { root } = await fixture();
    let handler: ((request: { method: string; url: string }) => Promise<Response>) | undefined;
    const app = { isReady: () => true };
    let handled = false;
    const protocol = {
      handle: vi.fn(
        (
          _scheme: string,
          registered: (request: { method: string; url: string }) => Promise<Response>,
        ) => {
          handled = true;
          handler = registered;
        },
      ),
      isProtocolHandled: vi.fn(() => handled),
      unhandle: vi.fn(() => {
        handled = false;
      }),
    };

    const release = await installApplicationProtocol({ app, protocol, rendererRoot: root });

    expect(protocol.handle).toHaveBeenCalledWith("tammy", expect.any(Function));
    const response = await handler?.({
      method: "GET",
      url: "tammy://app/assets/app.js",
    });
    expect(response?.status).toBe(200);
    await expect(response?.text()).resolves.toBe("asset");
    expect(response?.headers.get("Content-Type")).toBe("text/javascript; charset=utf-8");
    expect(response?.headers.get("X-Content-Type-Options")).toBe("nosniff");

    const notFound = await handler?.({
      method: "POST",
      url: "tammy://app/assets/app.js",
    });
    expect(notFound?.status).toBe(404);
    await expect(notFound?.text()).resolves.toBe("Not found.");
    expect(notFound?.headers.get("X-Content-Type-Options")).toBe("nosniff");

    release();
    expect(protocol.unhandle).not.toHaveBeenCalled();
  });

  it("refuses to install the file protocol before Electron is ready", async () => {
    const protocol = {
      handle: vi.fn(),
      isProtocolHandled: vi.fn(() => false),
      unhandle: vi.fn(),
    };

    await expect(
      installApplicationProtocol({
        app: { isReady: () => false },
        protocol,
        rendererRoot: "/renderer",
      }),
    ).rejects.toThrow("PROTOCOL_REQUIRES_READY");
    expect(protocol.handle).not.toHaveBeenCalled();
  });

  it("refuses to replace a tammy handler not owned by the registrar", async () => {
    const { root } = await fixture();
    const protocol = {
      handle: vi.fn(),
      isProtocolHandled: vi.fn(() => true),
      unhandle: vi.fn(),
    };

    await expect(
      installApplicationProtocol({
        app: { isReady: () => true },
        protocol,
        rendererRoot: root,
      }),
    ).rejects.toThrow("PROTOCOL_HANDLER_NOT_OWNED");
    expect(protocol.handle).not.toHaveBeenCalled();
    expect(protocol.unhandle).not.toHaveBeenCalled();
  });

  it("reference-counts distinct protocol leases without weakening the owned handler", async () => {
    const { root } = await fixture();
    const other = await fixture();
    let handled = false;
    const protocol = {
      handle: vi.fn(() => {
        handled = true;
      }),
      isProtocolHandled: vi.fn(() => handled),
      unhandle: vi.fn(() => {
        handled = false;
      }),
    };
    const options = {
      app: { isReady: () => true },
      protocol,
      rendererRoot: root,
    };

    const first = await installApplicationProtocol(options);
    const second = await installApplicationProtocol(options);

    expect(first).not.toBe(second);
    expect(protocol.handle).toHaveBeenCalledTimes(1);
    first();
    first();
    expect(handled).toBe(true);
    expect(protocol.unhandle).not.toHaveBeenCalled();

    await expect(
      installApplicationProtocol({ ...options, rendererRoot: other.root }),
    ).rejects.toThrow("PROTOCOL_ALREADY_CONFIGURED");
    expect(protocol.handle).toHaveBeenCalledTimes(1);
    expect(protocol.unhandle).not.toHaveBeenCalled();

    second();
    const successor = await installApplicationProtocol(options);
    expect(protocol.handle).toHaveBeenCalledTimes(1);
    successor();
    expect(handled).toBe(true);
    expect(protocol.unhandle).not.toHaveBeenCalled();
  });

  it("returns a sanitized 404 if a validated asset is swapped before the request", async () => {
    const { outside, root } = await fixture();
    let handler: ((request: { method: string; url: string }) => Promise<Response>) | undefined;
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
    await installApplicationProtocol({
      app: { isReady: () => true },
      protocol,
      rendererRoot: root,
    });
    await rename(join(root, "assets", "app.js"), join(root, "assets", "moved.js"));
    await symlink(outside, join(root, "assets", "app.js"));

    const response = await handler?.({
      method: "GET",
      url: "tammy://app/assets/app.js",
    });

    expect(response?.status).toBe(404);
    const body = await response?.text();
    expect(body).toBe("Not found.");
    expect(body).not.toContain("secret");
  });

  it("rejects oversized renderer assets without returning their bytes", async () => {
    const { root } = await fixture();
    await writeFile(join(root, "assets", "large.js"), new Uint8Array(MAX_RENDERER_ASSET_BYTES + 1));

    await expect(readRendererAsset("tammy://app/assets/large.js", root)).resolves.toBeNull();
  });

  it("closes the exact file handle when reading fails", async () => {
    const root = resolve("renderer");
    const close = vi.fn(async () => undefined);
    const fileStats = {
      dev: 1,
      ino: 2,
      size: 5,
      isDirectory: () => false,
      isFile: () => true,
      isSymbolicLink: () => false,
    };
    const rootStats = {
      ...fileStats,
      ino: 1,
      isDirectory: () => true,
      isFile: () => false,
    };
    const fileSystem = {
      lstat: vi.fn(async (path: string) => (path === root ? rootStats : fileStats)),
      open: vi.fn(async () => ({
        close,
        read: vi.fn(async () => {
          throw new Error("sensitive read failure");
        }),
        stat: vi.fn(async () => fileStats),
      })),
      realpath: vi.fn(async (path: string) => path),
    };

    await expect(readRendererAsset("tammy://app/index.html", root, fileSystem)).resolves.toBeNull();
    expect(close).toHaveBeenCalledTimes(1);
  });

  it("returns no asset when closing the opened file handle fails", async () => {
    const root = resolve("renderer");
    const close = vi.fn(async () => {
      throw new Error("sensitive close failure");
    });
    const fileStats = {
      dev: 1,
      ino: 2,
      size: 5,
      isDirectory: () => false,
      isFile: () => true,
      isSymbolicLink: () => false,
    };
    const rootStats = {
      ...fileStats,
      ino: 1,
      isDirectory: () => true,
      isFile: () => false,
    };
    const read = vi.fn(
      async (buffer: Uint8Array, _offset: number, _length: number, position: number) => {
        if (position > 0) {
          return { bytesRead: 0 };
        }
        buffer.set(new TextEncoder().encode("asset"));
        return { bytesRead: 5 };
      },
    );
    const fileSystem = {
      lstat: vi.fn(async (path: string) => (path === root ? rootStats : fileStats)),
      open: vi.fn(async () => ({
        close,
        read,
        stat: vi.fn(async () => fileStats),
      })),
      realpath: vi.fn(async (path: string) => path),
    };

    await expect(readRendererAsset("tammy://app/index.html", root, fileSystem)).resolves.toBeNull();
    expect(read).toHaveBeenCalled();
    expect(close).toHaveBeenCalledTimes(1);
  });

  it("bounds a file that grows after its opened size check", async () => {
    const root = resolve("renderer");
    const close = vi.fn(async () => undefined);
    const fileStats = {
      dev: 1,
      ino: 2,
      size: 5,
      isDirectory: () => false,
      isFile: () => true,
      isSymbolicLink: () => false,
    };
    const rootStats = {
      ...fileStats,
      ino: 1,
      isDirectory: () => true,
      isFile: () => false,
    };
    const read = vi.fn(
      async (buffer: Uint8Array, _offset: number, length: number, position: number) => {
        if (position > MAX_RENDERER_ASSET_BYTES) {
          return { bytesRead: 0 };
        }
        buffer.fill(1);
        return { bytesRead: length };
      },
    );
    const fileSystem = {
      lstat: vi.fn(async (path: string) => (path === root ? rootStats : fileStats)),
      open: vi.fn(async () => ({
        close,
        read,
        stat: vi.fn(async () => fileStats),
      })),
      realpath: vi.fn(async (path: string) => path),
    };

    await expect(readRendererAsset("tammy://app/index.html", root, fileSystem)).resolves.toBeNull();
    expect(read).toHaveBeenLastCalledWith(expect.any(Uint8Array), 0, 1, MAX_RENDERER_ASSET_BYTES);
    expect(close).toHaveBeenCalledTimes(1);
  });
});

describe("packaged ASAR asset security", () => {
  function stats(kind: "directory" | "file" | "symlink", dev: number, ino: number, size: number) {
    return {
      dev,
      ino,
      size,
      isDirectory: () => kind === "directory",
      isFile: () => kind === "file",
      isSymbolicLink: () => kind === "symlink",
    };
  }

  function asarHarness(
    options: {
      readonly entrySize?: number;
      readonly fetchFile?: (url: string) => Promise<Response>;
    } = {},
  ) {
    const archivePath = resolve("/physical/app.asar");
    const rendererRoot = join(archivePath, "renderer");
    const indexPath = join(rendererRoot, "index.html");
    let archiveIdentity = { dev: 11, ino: 12, size: 4096 };
    let archiveSymlink = false;
    let virtualIdentity = 100;
    let handled = false;
    let handler: ((request: { method: string; url: string }) => Promise<Response>) | undefined;
    const protocol = {
      handle: vi.fn(
        (
          _scheme: string,
          registered: (request: { method: string; url: string }) => Promise<Response>,
        ) => {
          handled = true;
          handler = registered;
        },
      ),
      isProtocolHandled: vi.fn(() => handled),
      unhandle: vi.fn(),
    };
    const fileSystem = {
      lstat: vi.fn(async (path: string) => {
        virtualIdentity += 1;
        if (path === rendererRoot) {
          return stats("directory", 0, virtualIdentity, 0);
        }
        if (path === indexPath) {
          return stats("file", 0, virtualIdentity, options.entrySize ?? 5);
        }
        throw new Error("missing virtual entry");
      }),
      open: vi.fn(async () => {
        throw new Error("ASAR entries must not use fs.open");
      }),
      realpath: vi.fn(async (path: string) => path),
    };
    const originalFileSystem = {
      lstat: vi.fn(async (path: string) => {
        if (path !== archivePath) {
          throw new Error("unexpected physical path");
        }
        return stats(
          archiveSymlink ? "symlink" : "file",
          archiveIdentity.dev,
          archiveIdentity.ino,
          archiveIdentity.size,
        );
      }),
      realpath: vi.fn(async (path: string) => path),
    };
    const fetchFile =
      options.fetchFile ??
      vi.fn(
        async () =>
          new Response("asset", {
            headers: {
              Authorization: "must-not-forward",
              "Content-Type": "application/hostile",
            },
          }),
      );

    return {
      archivePath,
      fetchFile,
      fileSystem,
      getHandler: () => handler,
      originalFileSystem,
      protocol,
      rendererRoot,
      setArchiveIdentity: (identity: typeof archiveIdentity) => {
        archiveIdentity = identity;
      },
      setArchiveSymlink: (symlink: boolean) => {
        archiveSymlink = symlink;
      },
    };
  }

  it("ignores unstable virtual inode values while reusing the physical archive owner", async () => {
    const harness = asarHarness();
    const options = {
      app: { isReady: () => true },
      fetchFile: harness.fetchFile,
      fileSystem: harness.fileSystem,
      originalFileSystem: harness.originalFileSystem,
      protocol: harness.protocol,
      rendererRoot: harness.rendererRoot,
    };

    const first = await installApplicationProtocol(options);
    const second = await installApplicationProtocol(options);

    expect(first).not.toBe(second);
    expect(harness.protocol.handle).toHaveBeenCalledTimes(1);
    const response = await harness.getHandler()?.({
      method: "GET",
      url: "tammy://app/",
    });
    expect(response?.status).toBe(200);
    await expect(response?.text()).resolves.toBe("asset");
    expect(response?.headers.get("Content-Type")).toBe("text/html; charset=utf-8");
    expect(response?.headers.get("X-Content-Type-Options")).toBe("nosniff");
    expect(response?.headers.has("Authorization")).toBe(false);
    expect(harness.fileSystem.open).not.toHaveBeenCalled();
    expect(harness.fetchFile).toHaveBeenCalledWith(
      pathToFileURL(join(harness.rendererRoot, "index.html")).href,
    );
    first();
    second();
  });

  it("rejects an invalid virtual root and invalid virtual file type", async () => {
    const invalidRoot = asarHarness();
    invalidRoot.fileSystem.lstat.mockImplementation(async () => stats("file", 0, 1, 5));

    await expect(
      installApplicationProtocol({
        app: { isReady: () => true },
        fetchFile: invalidRoot.fetchFile,
        fileSystem: invalidRoot.fileSystem,
        originalFileSystem: invalidRoot.originalFileSystem,
        protocol: invalidRoot.protocol,
        rendererRoot: invalidRoot.rendererRoot,
      }),
    ).rejects.toThrow("INVALID_RENDERER_ROOT");
    expect(invalidRoot.protocol.handle).not.toHaveBeenCalled();

    const invalidFile = asarHarness();
    invalidFile.fileSystem.lstat.mockImplementation(async (path: string) =>
      stats("directory", 0, path.endsWith("index.html") ? 2 : 1, 0),
    );
    await installApplicationProtocol({
      app: { isReady: () => true },
      fetchFile: invalidFile.fetchFile,
      fileSystem: invalidFile.fileSystem,
      originalFileSystem: invalidFile.originalFileSystem,
      protocol: invalidFile.protocol,
      rendererRoot: invalidFile.rendererRoot,
    });
    const response = await invalidFile.getHandler()?.({
      method: "GET",
      url: "tammy://app/",
    });
    expect(response?.status).toBe(404);
    expect(invalidFile.fetchFile).not.toHaveBeenCalled();
  });

  it("rejects a virtual symlink without fetching it", async () => {
    const harness = asarHarness();
    harness.fileSystem.lstat.mockImplementation(async (path: string) =>
      stats(path.endsWith("index.html") ? "symlink" : "directory", 0, 1, 5),
    );
    await installApplicationProtocol({
      app: { isReady: () => true },
      fetchFile: harness.fetchFile,
      fileSystem: harness.fileSystem,
      originalFileSystem: harness.originalFileSystem,
      protocol: harness.protocol,
      rendererRoot: harness.rendererRoot,
    });

    const response = await harness.getHandler()?.({
      method: "GET",
      url: "tammy://app/",
    });

    expect(response?.status).toBe(404);
    expect(harness.fetchFile).not.toHaveBeenCalled();
  });

  it.each([
    ["replacement", { dev: 11, ino: 99, size: 4096 }, false],
    ["size change", { dev: 11, ino: 12, size: 8192 }, false],
    ["symlink swap", { dev: 11, ino: 12, size: 4096 }, true],
  ])("returns 404 after a physical archive %s", async (_name, identity, symlink) => {
    const harness = asarHarness();
    await installApplicationProtocol({
      app: { isReady: () => true },
      fetchFile: harness.fetchFile,
      fileSystem: harness.fileSystem,
      originalFileSystem: harness.originalFileSystem,
      protocol: harness.protocol,
      rendererRoot: harness.rendererRoot,
    });
    harness.setArchiveIdentity(identity);
    harness.setArchiveSymlink(symlink);

    const response = await harness.getHandler()?.({
      method: "GET",
      url: "tammy://app/",
    });

    expect(response?.status).toBe(404);
    await expect(response?.text()).resolves.toBe("Not found.");
    expect(harness.fetchFile).not.toHaveBeenCalled();
  });

  it("returns 404 when the physical archive changes during the file fetch", async () => {
    const harness = asarHarness();
    const fetchFile = vi.fn(async () => {
      harness.setArchiveIdentity({ dev: 11, ino: 99, size: 4096 });
      return new Response("asset");
    });
    await installApplicationProtocol({
      app: { isReady: () => true },
      fetchFile,
      fileSystem: harness.fileSystem,
      originalFileSystem: harness.originalFileSystem,
      protocol: harness.protocol,
      rendererRoot: harness.rendererRoot,
    });

    const response = await harness.getHandler()?.({
      method: "GET",
      url: "tammy://app/",
    });

    expect(fetchFile).toHaveBeenCalledTimes(1);
    expect(response?.status).toBe(404);
    await expect(response?.text()).resolves.toBe("Not found.");
  });

  it("rejects an oversized virtual entry before fetching it", async () => {
    const harness = asarHarness({
      entrySize: MAX_RENDERER_ASSET_BYTES + 1,
    });
    await installApplicationProtocol({
      app: { isReady: () => true },
      fetchFile: harness.fetchFile,
      fileSystem: harness.fileSystem,
      originalFileSystem: harness.originalFileSystem,
      protocol: harness.protocol,
      rendererRoot: harness.rendererRoot,
    });

    const response = await harness.getHandler()?.({
      method: "GET",
      url: "tammy://app/",
    });

    expect(response?.status).toBe(404);
    expect(harness.fetchFile).not.toHaveBeenCalled();
  });

  it.each([
    ["rejection", async () => Promise.reject(new Error("sensitive fetch error"))],
    ["non-ok response", async () => new Response("denied", { status: 500 })],
    ["oversized response", async () => new Response(new Uint8Array(MAX_RENDERER_ASSET_BYTES + 1))],
  ])("sanitizes an ASAR fetch %s", async (_name, fetchFile) => {
    const harness = asarHarness({ fetchFile });
    await installApplicationProtocol({
      app: { isReady: () => true },
      fetchFile,
      fileSystem: harness.fileSystem,
      originalFileSystem: harness.originalFileSystem,
      protocol: harness.protocol,
      rendererRoot: harness.rendererRoot,
    });

    const response = await harness.getHandler()?.({
      method: "GET",
      url: "tammy://app/",
    });

    expect(response?.status).toBe(404);
    await expect(response?.text()).resolves.toBe("Not found.");
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

  it("reference-counts distinct CSP leases through one monotonic dispatcher", () => {
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

    expect(first).not.toBe(second);
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

    first();
    first();
    expect(session.webRequest.onHeadersReceived).toHaveBeenCalledTimes(1);
    const responseAfterStaleRelease = vi.fn();
    listeners[0]?.({ responseHeaders: {} }, responseAfterStaleRelease);
    expect(responseAfterStaleRelease).toHaveBeenCalledWith({
      responseHeaders: {
        "Content-Security-Policy": [policy.contentSecurityPolicy],
      },
    });

    expect(() =>
      installContentSecurityPolicy(
        session as never,
        createRendererSecurityPolicy("http://localhost:5173"),
      ),
    ).toThrow("CSP_ALREADY_CONFIGURED");
    expect(session.webRequest.onHeadersReceived).toHaveBeenCalledTimes(1);

    second();
    const successor = installContentSecurityPolicy(session as never, policy);
    expect(successor).not.toBe(first);
    expect(successor).not.toBe(second);
    successor();
    expect(session.webRequest.onHeadersReceived).toHaveBeenCalledTimes(1);
    expect(listeners).not.toContain(null);
  });

  it("keeps permissions and downloads denied across independent lease release order", () => {
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

    const first = installSessionGuards(session as never);
    const second = installSessionGuards(session as never);

    expect(first).not.toBe(second);
    expect(session.setPermissionCheckHandler).toHaveBeenCalledTimes(1);
    expect(session.setPermissionRequestHandler).toHaveBeenCalledTimes(1);
    expect(session.setDevicePermissionHandler).toHaveBeenCalledTimes(1);
    expect(session.on).toHaveBeenCalledTimes(1);

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

    first();
    first();
    expect(permissionCheck?.()).toBe(false);
    expect(devicePermission?.()).toBe(false);
    second();
    const successor = installSessionGuards(session as never);
    successor();
    expect(permissionCheck?.()).toBe(false);
    expect(session.setPermissionCheckHandler).toHaveBeenCalledTimes(1);
    expect(session.setPermissionRequestHandler).toHaveBeenCalledTimes(1);
    expect(session.setDevicePermissionHandler).toHaveBeenCalledTimes(1);
    expect(session.removeListener).not.toHaveBeenCalled();
  });

  it("keeps navigation and new windows denied across independent lease release order", () => {
    type NavigationHandler = (event: { preventDefault: () => void }, url: string) => void;
    const navigationHandlers = new Map<string, NavigationHandler>();
    let windowOpenHandler: ((details: { url: string }) => { action: "deny" }) | undefined;
    const openExternal = vi.fn(async () => undefined);
    const privacyUrl = "https://example.com/tammy/privacy";
    const webContents = {
      getURL: vi.fn(() => PRODUCTION_APP_URL),
      on: vi.fn((event: string, handler: NavigationHandler) => {
        navigationHandlers.set(event, handler);
      }),
      removeListener: vi.fn(),
      setWindowOpenHandler: vi.fn((handler: typeof windowOpenHandler) => {
        windowOpenHandler = handler;
      }),
    };

    const first = installWindowGuards(webContents as never, PRODUCTION_APP_URL, {
      allowedExternalUrls: [privacyUrl],
      openExternal,
    });
    const second = installWindowGuards(webContents as never, PRODUCTION_APP_URL, {
      allowedExternalUrls: [privacyUrl],
      openExternal,
    });

    expect(first).not.toBe(second);
    expect(webContents.on).toHaveBeenCalledTimes(2);
    expect(webContents.setWindowOpenHandler).toHaveBeenCalledTimes(1);

    const allowedEvent = { preventDefault: vi.fn() };
    navigationHandlers.get("will-navigate")?.(allowedEvent, PRODUCTION_APP_URL);
    expect(allowedEvent.preventDefault).not.toHaveBeenCalled();

    const deniedEvent = { preventDefault: vi.fn() };
    navigationHandlers.get("will-redirect")?.(deniedEvent, "https://example.com/");
    expect(deniedEvent.preventDefault).toHaveBeenCalledTimes(1);
    expect(windowOpenHandler?.({ url: "https://example.com/other" })).toEqual({ action: "deny" });
    expect(openExternal).not.toHaveBeenCalled();
    expect(windowOpenHandler?.({ url: privacyUrl })).toEqual({ action: "deny" });
    expect(openExternal).toHaveBeenCalledExactlyOnceWith(privacyUrl);

    first();
    first();
    const deniedAfterStaleRelease = { preventDefault: vi.fn() };
    navigationHandlers.get("will-navigate")?.(deniedAfterStaleRelease, "https://example.com/");
    expect(deniedAfterStaleRelease.preventDefault).toHaveBeenCalledTimes(1);

    expect(() => installWindowGuards(webContents as never, "http://localhost:5173/")).toThrow(
      "WINDOW_GUARDS_ALREADY_CONFIGURED",
    );
    second();
    const successor = installWindowGuards(webContents as never, PRODUCTION_APP_URL, {
      allowedExternalUrls: [privacyUrl],
      openExternal,
    });
    successor();
    expect(webContents.on).toHaveBeenCalledTimes(2);
    expect(webContents.setWindowOpenHandler).toHaveBeenCalledTimes(1);
    expect(webContents.removeListener).not.toHaveBeenCalled();
    expect(windowOpenHandler?.({ url: "https://example.com/other" })).toEqual({ action: "deny" });
  });
});
