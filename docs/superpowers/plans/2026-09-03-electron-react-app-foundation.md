# Electron React App Foundation Migration Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Tammy's Electron Forge and hand-written IPC foundation with electron-vite, electron-builder, and a security-hardened electron-conveyor while preserving the accounting, SBR, Taskfile, packaged-E2E, and Mac App Store contracts.

**Architecture:** Migrate in place and keep one runtime at completion. First pin and minimally patch the upstream foundation, then move the existing application onto electron-vite, migrate process calls into eight typed Conveyor modules, switch packaging and MAS staging to electron-builder, and finally remove every Forge and legacy IPC path.

**Tech Stack:** Node.js 24, pnpm 11, TypeScript, Electron 43, React 19, electron-vite 5, Vite 7, electron-builder 26, electron-conveyor 0.4, Zod 4, TanStack Query 5, Vitest, Playwright, Go Core, SQLCipher, macOS codesign/productbuild.

**Authoritative design:** `docs/superpowers/specs/2026-09-03-electron-react-app-foundation-design.md`

**Upstream references:** `guasam/electron-react-app@d68dfcd916239f7691e31cd25e88174652b967b7`; `electron-conveyor@88569fd45c214f4e5a384ac534b247a14364c3c6` (`0.4.0`).

---

## Chunk 1: Secure upstream foundation and electron-vite runtime

### File structure for this chunk

- Modify `apps/desktop/package.json` and `pnpm-lock.yaml` to pin the new foundation without removing Forge until its packaging replacement is green.
- Modify `pnpm-workspace.yaml` only through pnpm's generated `patchedDependencies` entry.
- Create `patches/electron-conveyor@0.4.0.patch` as the minimal ownership/authorization patch.
- Create `apps/desktop/src/conveyor/upstream-contract.test.ts` to lock the patched API and call order.
- Create `apps/desktop/electron.vite.config.ts` as the only target build configuration.
- Move `apps/desktop/index.html` to `apps/desktop/src/app/index.html` and `apps/desktop/src/renderer/**` to `apps/desktop/src/app/**` without redesigning it.
- Modify `apps/desktop/src/main/index-production.ts` and its tests for electron-vite paths and development URL discovery.
- Create `apps/desktop/playwright.electron-vite.config.ts` and `apps/desktop/tests/e2e/electron-vite-development.spec.ts` for a bounded real development-runtime smoke.
- Modify `apps/desktop/src/main/vite-env.d.ts` temporarily, then delete it once no Forge global remains.
- Modify `scripts/launch-local-scenario.mjs`, its tests, and the root development script sentinel.
- Modify `apps/desktop/tsconfig.json` and renderer tooling tests for the new aliases and paths.
- Delete the three old `apps/desktop/vite.*.config.ts` files after the electron-vite build is green.

### Task 1: Pin and harden the upstream dependencies

Use `@superpowers:test-driven-development` and `@security-best-practices`.

**Files:**
- Modify: `apps/desktop/package.json`
- Modify: `pnpm-lock.yaml`
- Modify: `pnpm-workspace.yaml`
- Create: `patches/electron-conveyor@0.4.0.patch`
- Create: `apps/desktop/src/conveyor/upstream-contract.test.ts`

- [ ] **Step 1: Install the exact upstream foundation versions**

Add runtime dependencies `electron-conveyor@0.4.0` and `@tanstack/react-query@5.101.4`. Add development dependencies `electron-vite@5.0.0` and `electron-builder@26.15.3`. Downgrade only the incompatible build peers to `vite@7.3.6` and `@vitejs/plugin-react@5.2.0`; keep Tammy's current Electron, React, TypeScript, test, and product dependencies.

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop add --save-exact electron-conveyor@0.4.0 @tanstack/react-query@5.101.4
rtk mise exec -- pnpm --dir apps/desktop add --save-dev --save-exact electron-vite@5.0.0 electron-builder@26.15.3 vite@7.3.6 @vitejs/plugin-react@5.2.0
```

Expected: the frozen lock records exact versions and pnpm reports no peer conflict between electron-vite and Vite.

- [ ] **Step 2: Write the failing upstream ownership test**

Create a fake `ipcMain` boundary that records `handle`, `removeHandler`, `on`, and `removeListener`. Define one query with a Zod schema whose validator records when it runs. Require this patched contract:

```ts
const calls: string[] = [];
const router = createRouter(
  { probe: defineModule({ read: query(probeSchema, () => "ok") }) },
  {
    ipcMain: fakeIpcMain,
    streams: false,
    authorize: (event) => {
      calls.push("authorize");
      if (event !== acceptedEvent) throw new ConveyorError("IPC_SENDER_REJECTED");
    },
  },
);

expect(typeof router.dispose).toBe("function");
await registeredProbeHandler(acceptedEvent, "read", validInput);
expect(calls).toEqual(["authorize", "validate", "handle"]);
```

Also assert that authorization runs before unknown-method and invalid-input responses, the manifest listener is authorized, `streams: false` registers no stream handlers, a second router cannot replace an owned channel, `dispose()` is idempotent, and disposal leaves a separately registered sentinel listener untouched.

- [ ] **Step 3: Run the contract test RED**

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- src/conveyor/upstream-contract.test.ts`

Expected: FAIL because stock `electron-conveyor@0.4.0` ignores injected `ipcMain`, installs stream handlers, and returns no disposer.

- [ ] **Step 4: Create the minimal pnpm patch**

Use `pnpm patch electron-conveyor@0.4.0` and change only the package's main router/dispatch implementation and corresponding declarations. The patched option/result contract is:

```ts
interface TammyRouterOptions {
  ipcMain?: Pick<Electron.IpcMain, "handle" | "removeHandler" | "on" | "removeListener">;
  authorize?: (event: Electron.IpcMainEvent | Electron.IpcMainInvokeEvent) => void;
  streams?: boolean;
}

interface OwnedRouter {
  dispose(): void;
}
```

Implementation rules:

- call the synchronous `authorize(event)` before context construction, method lookup, arity/schema validation, or manifest return; synchronous authorization is required because Conveyor obtains its manifest through `sendSync`;
- catch authorization failure and return only code/message `IPC_SENDER_REJECTED` for invoke calls, and an empty manifest for rejected synchronous manifest calls;
- require exactly `method` plus zero-or-one procedure input arguments and reject extras;
- default to upstream behavior when the new options are absent;
- when `streams: false`, register neither stream lifecycle handler;
- track each installed handler/listener with a unique ownership token in a `WeakMap` keyed by the injected boundary;
- refuse to overwrite a channel already owned by another router;
- remove a registration only when its ownership token still matches;
- make `dispose()` safe to call more than once.

Create and record the patch with the exact pnpm workflow:

```bash
rtk mise exec -- pnpm patch electron-conveyor@0.4.0 --edit-dir .tmp/electron-conveyor-patch
# Edit only the extracted router/dispatch runtime and declaration outputs with apply_patch.
rtk mise exec -- pnpm patch-commit .tmp/electron-conveyor-patch --patches-dir patches
```

Expected: pnpm creates `patches/electron-conveyor@0.4.0.patch` and records its hash/path in `pnpm-workspace.yaml` and `pnpm-lock.yaml`; do not vendor the extracted package.

- [ ] **Step 5: Run the patched contract GREEN**

Run:

```bash
rtk mise exec -- pnpm install --frozen-lockfile
rtk mise exec -- pnpm --dir apps/desktop test -- src/conveyor/upstream-contract.test.ts
rtk mise exec -- pnpm --dir apps/desktop typecheck
```

Expected: PASS; no stream channels are registered and rejected calls do not reach schemas or handlers.

- [ ] **Step 6: Commit the pinned foundation**

```bash
rtk git add apps/desktop/package.json pnpm-lock.yaml pnpm-workspace.yaml patches/electron-conveyor@0.4.0.patch apps/desktop/src/conveyor/upstream-contract.test.ts
rtk git commit -m "build: pin secure Electron React foundation"
```

### Task 2: Move the build to electron-vite

Use `@superpowers:test-driven-development`.

**Files:**
- Create: `apps/desktop/electron.vite.config.ts`
- Create: `apps/desktop/src/main/electron-vite-contract.test.ts`
- Move: `apps/desktop/index.html` → `apps/desktop/src/app/index.html`
- Move: `apps/desktop/src/renderer/**` → `apps/desktop/src/app/**`
- Modify: `apps/desktop/src/app/main.tsx`
- Modify: `apps/desktop/package.json`
- Modify: `apps/desktop/tsconfig.json`
- Modify: `apps/desktop/src/app/test/tooling.test.ts`
- Modify: `apps/desktop/vitest.config.ts`
- Modify: `apps/desktop/components.json`
- Modify: `taskfiles/sbr.yml`
- Modify: `scripts/check-taskfiles.test.mjs`

- [ ] **Step 1: Write the failing electron-vite contract test**

Import the config and its exported externalization predicate, then require exactly:

```ts
expect(config.main?.build?.outDir).toBe(resolve(desktopRoot, "out/main"));
expect(config.preload?.build?.outDir).toBe(resolve(desktopRoot, "out/preload"));
expect(config.renderer?.build?.outDir).toBe(resolve(desktopRoot, "out/renderer"));
expect(config.renderer?.root).toBe(resolve(desktopRoot, "src/app"));
expect(config.renderer?.build?.rollupOptions?.input).toBe(
  resolve(desktopRoot, "src/app/index.html"),
);
expect(config.main?.build?.lib?.fileName()).toBe("index.js");
expect(config.preload?.build?.lib?.fileName()).toBe("index.cjs");
expect(Object.keys(config.main?.resolve?.alias ?? {})).toEqual([
  "@/app",
  "@/conveyor",
  "@/main",
  "@/shared",
  "@/resources",
]);
expect(isElectronRuntimeExternal("electron")).toBe(true);
expect(isElectronRuntimeExternal("node:path")).toBe(true);
expect(isElectronRuntimeExternal("@tammy/connect-client")).toBe(false);
expect(isElectronRuntimeExternal("electron-conveyor")).toBe(false);
```

Assert renderer `base` is `./`, React and Tailwind plugins are present, source maps are disabled, and no config sets `ELECTRON_DISABLE_SANDBOX`.

- [ ] **Step 2: Run the config test RED**

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- src/main/electron-vite-contract.test.ts`

Expected: FAIL because `electron.vite.config.ts` and `src/app` do not exist.

- [ ] **Step 3: Move the renderer without changing product behavior**

Move the existing renderer directory to `src/app`, move the root HTML entry to `src/app/index.html`, update its module entry, and remove `@vitejs/plugin-react/preamble` from `src/app/main.tsx`. Update `vitest.config.ts`, `components.json`, `src/app/app.test.tsx`, `taskfiles/sbr.yml`, and the exact Taskfile assertion in `scripts/check-taskfiles.test.mjs` from `src/renderer`/`@/renderer` to `src/app`/`@/app`. Do not import the upstream welcome page, custom title bar, menus, multi-window store, or styling.

- [ ] **Step 4: Add the single electron-vite configuration**

Create a config with explicit absolute inputs and outputs:

```ts
export const isElectronRuntimeExternal = (id: string): boolean =>
  id === "electron" || id.startsWith("node:") || builtinModules.includes(id);

const aliases = {
  "@/app": resolve(desktopRoot, "src/app"),
  "@/conveyor": resolve(desktopRoot, "src/conveyor"),
  "@/main": resolve(desktopRoot, "src/main"),
  "@/shared": resolve(desktopRoot, "src/shared"),
  "@/resources": resolve(desktopRoot, "resources"),
};

export default defineConfig({
  main: {
    build: {
      outDir: resolve(desktopRoot, "out/main"),
      lib: {
        entry: resolve(desktopRoot, "src/main/index.ts"),
        fileName: () => "index.js",
        formats: ["es"],
      },
      rollupOptions: { external: isElectronRuntimeExternal },
      sourcemap: false,
    },
    resolve: { alias: aliases },
  },
  preload: {
    build: {
      outDir: resolve(desktopRoot, "out/preload"),
      lib: {
        entry: resolve(desktopRoot, "src/preload/index.ts"),
        fileName: () => "index.cjs",
        formats: ["cjs"],
      },
      rollupOptions: { external: isElectronRuntimeExternal },
      sourcemap: false,
    },
    resolve: { alias: aliases },
  },
  renderer: {
    root: resolve(desktopRoot, "src/app"),
    base: "./",
    build: {
      outDir: resolve(desktopRoot, "out/renderer"),
      rollupOptions: { input: resolve(desktopRoot, "src/app/index.html") },
      sourcemap: false,
    },
    resolve: { alias: aliases },
    plugins: [tailwindcss(), react()],
  },
});
```

Main and preload externalize only Electron and Node built-ins; app dependencies, especially `@tammy/connect-client` and `electron-conveyor`, stay bundled so Builder does not have to resolve pnpm workspace links at runtime.

- [ ] **Step 5: Update package scripts and TypeScript scope**

Set `main` to `./out/main/index.js`; add `dev`, `build`, and `preview` scripts using electron-vite; make the existing `start` script call `electron-vite dev -w` after `prepare:runtime`. Replace the old Vite/Forge config entries in `tsconfig.json` with `electron.vite.config.ts`, and update the alias tooling test to the five explicit roots.

- [ ] **Step 6: Run the config, renderer, and type tests GREEN**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- src/main/electron-vite-contract.test.ts src/app/test/tooling.test.ts src/app/app.test.tsx
rtk mise exec -- pnpm --dir apps/desktop typecheck
rtk mise exec -- pnpm --dir apps/desktop build
rtk test -f apps/desktop/out/main/index.js
rtk test -f apps/desktop/out/preload/index.cjs
rtk test -f apps/desktop/out/renderer/index.html
```

Expected: PASS and the three exact output entries exist. The config test explicitly proves that `@tammy/connect-client` and `electron-conveyor` are not externalized; Chunk 2 verifies the built Conveyor preload after it exists.

- [ ] **Step 7: Commit the build migration**

```bash
rtk git add apps/desktop/electron.vite.config.ts apps/desktop/src/app apps/desktop/package.json apps/desktop/tsconfig.json apps/desktop/vitest.config.ts apps/desktop/components.json apps/desktop/src/main/electron-vite-contract.test.ts taskfiles/sbr.yml scripts/check-taskfiles.test.mjs
rtk git commit -m "build: migrate desktop compilation to electron-vite"
```

### Task 3: Rebind runtime paths and local launch

Use `@superpowers:test-driven-development` and `@security-best-practices`.

**Files:**
- Modify: `apps/desktop/src/main/index-production.ts`
- Modify: `apps/desktop/src/main/index-production.test.ts`
- Delete: `apps/desktop/src/main/vite-env.d.ts`
- Modify: `scripts/launch-local-scenario.mjs`
- Modify: `scripts/launch-local-scenario.test.mjs`
- Modify: `package.json`
- Create: `apps/desktop/playwright.electron-vite.config.ts`
- Create: `apps/desktop/tests/e2e/electron-vite-development.spec.ts`
- Delete: `apps/desktop/vite.main.config.ts`
- Delete: `apps/desktop/vite.preload.config.ts`
- Delete: `apps/desktop/vite.renderer.config.ts`

- [ ] **Step 1: Make runtime-path tests fail on the new contract**

Extend production dependency tests to assert:

- development reads only `process.env.ELECTRON_RENDERER_URL` and accepts one canonical loopback origin through `createRendererSecurityPolicy`;
- missing, credentialed, non-loopback, path-bearing, query-bearing, or fragment-bearing development URLs fail closed;
- preload resolves from `out/main` to `out/preload/index.cjs`;
- development resources resolve from `out/main` to `apps/desktop/resources`;
- packaged renderer root is exactly `path.join(app.getAppPath(), "out/renderer")`;
- production continues loading `tammy://app/`.

Update scenario-owner tests to require the private `--electron-vite` sentinel and reject `--electron-forge`.

- [ ] **Step 2: Run the focused tests RED**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- src/main/index-production.test.ts
rtk mise exec -- node --test scripts/launch-local-scenario.test.mjs
```

Expected: FAIL on Forge globals, `.vite` roots, and the old sentinel.

- [ ] **Step 3: Implement the electron-vite runtime paths**

Replace `MAIN_WINDOW_VITE_DEV_SERVER_URL` with `process.env.ELECTRON_RENDERER_URL`. Resolve the preload as `../preload/index.cjs` from the compiled main directory and the packaged renderer as `app.getAppPath()/out/renderer`. Retain the current scheme-before-ready, security-before-Core, Core-diagnostics-before-window, show-on-ready, and shutdown ordering unchanged.

- [ ] **Step 4: Update the local scenario owner**

Change only the private sentinel to `--electron-vite`. Keep native Core, SBR helper, manifest build, isolated user-data arguments, signal forwarding, retained-root reporting, and output bounds unchanged. Update root `desktop:start:scenario` to pass the new sentinel.

- [ ] **Step 5: Delete obsolete Vite/Forge build inputs**

Delete `vite.main.config.ts`, `vite.preload.config.ts`, `vite.renderer.config.ts`, and `src/main/vite-env.d.ts`. Keep `forge.config.ts` only until Chunk 3 replaces packaging.

- [ ] **Step 6: Add a bounded real Electron development smoke**

Create a dedicated one-worker Playwright config. Its single test must:

1. bind a static server to an OS-assigned `127.0.0.1` port and serve only `out/renderer`, rejecting traversal and non-GET requests;
2. launch the installed Electron binary through Playwright with `out/main/index.js`, isolated temporary user data, and `ELECTRON_RENDERER_URL` set to that canonical loopback origin;
3. wait for the first real window and call system diagnostics through the current public bridge;
4. assert `runtimeMode: "offline"`, the first BrowserWindow's authoritative `webContents.getLastWebPreferences().sandbox === true`, `typeof process === "undefined"` in the renderer (Node is intentionally not exposed), and the exact loopback page origin;
5. close Electron gracefully, verify Core exits, close the server, and remove the temporary user-data directory in `finally`.

The config has a 30-second timeout, one worker, no retries, no external URL, and no mocked Electron or Core boundary. Chunk 2 updates the one bridge call from the legacy API to Conveyor.

- [ ] **Step 7: Run local foundation verification GREEN**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- src/main/index-production.test.ts src/main/security.test.ts src/main/electron-lifecycle.test.ts
rtk mise exec -- node --test scripts/launch-local-scenario.test.mjs
rtk mise exec -- pnpm desktop:typecheck
rtk mise exec -- pnpm --dir apps/desktop build
rtk mise exec -- pnpm core:build
rtk mise exec -- pnpm build:manifest
rtk mise exec -- pnpm --dir apps/desktop exec playwright test --config playwright.electron-vite.config.ts
```

Expected: PASS; Playwright opens the real non-packaged Electron application, obtains offline diagnostics, shuts it down without an orphan Core, and no source references `MAIN_WINDOW_VITE_*` or the three old config filenames.

- [ ] **Step 8: Commit the runtime cutover**

```bash
rtk git add package.json apps/desktop scripts/launch-local-scenario.mjs scripts/launch-local-scenario.test.mjs
rtk git commit -m "refactor: run Tammy through electron-vite"
```

---

## Chunk 2: Typed Conveyor boundary and product cutover

### File structure for this chunk

- Create `apps/desktop/src/shared/conveyor-manifest.json` and `apps/desktop/src/shared/conveyor-contract.ts` as the fixed public module/method contract.
- Create `apps/desktop/src/conveyor/init.ts`, `proto.ts`, `error.ts`, `guard.ts`, `registration.ts`, `router.ts`, and `client.ts`.
- Create eight files beneath `apps/desktop/src/conveyor/modules/`: `system.ts`, `workspace.ts`, `organisation.ts`, `accounting.ts`, `banking.ts`, `documents.ts`, `reporting.ts`, and `sbr.ts`.
- Create focused tests beside those Conveyor files.
- Create `apps/desktop/src/preload/conveyor-bridge.ts` and its test; modify the preload entry to expose only this bridge and the launch scenario.
- Modify main lifecycle registration and tests so one owned Conveyor router exists only while Core is ready.
- Modify `apps/desktop/src/app/**` and its tests to consume the domain clients and one shared React Query client.
- Modify current E2E helpers/specifications and the E2E coverage checker for the nested public API.
- Delete `apps/desktop/src/shared/desktop-api.ts`, `preload-methods.json`, `apps/desktop/src/main/ipc.ts`, `ipc.test.ts`, `rpc-router.ts`, and `rpc-router.test.ts` only after the replacement tests are green.
- Keep `apps/desktop/src/shared/proto-ipc.ts`; it remains the canonical protobuf envelope codec.

### Task 4: Establish the guarded Conveyor contract

Use `@superpowers:test-driven-development` and `@security-best-practices`.

**Files:**
- Create: `apps/desktop/src/shared/conveyor-manifest.json`
- Create: `apps/desktop/src/shared/conveyor-contract.ts`
- Create: `apps/desktop/src/conveyor/init.ts`
- Create: `apps/desktop/src/conveyor/proto.ts`
- Create: `apps/desktop/src/conveyor/proto.test.ts`
- Create: `apps/desktop/src/conveyor/error.ts`
- Create: `apps/desktop/src/conveyor/guard.ts`
- Create: `apps/desktop/src/conveyor/guard.test.ts`
- Modify: `apps/desktop/src/shared/desktop-api.ts`
- Modify: `apps/desktop/src/main/core-client.ts`
- Modify: `apps/desktop/src/main/core-client.test.ts`
- Modify: `apps/desktop/src/main/sbr-file-intake.ts`
- Modify: `apps/desktop/src/main/sbr-file-intake.test.ts`
- Modify: `apps/desktop/src/main/rpc-router.ts`

- [ ] **Step 1: Write the fixed eight-module manifest**

Create a JSON object whose keys, method names, and values are exact. Keep the current descriptive method names inside domain modules so renderer migrations stay mechanical rather than introducing a second vocabulary:

```json
{
  "system": { "getSystemDiagnostics": "query" },
  "workspace": {
    "createWorkspace": "command",
    "confirmRecovery": "command",
    "unlockWorkspace": "command",
    "signIn": "command"
  },
  "organisation": {
    "getCurrentUser": "query",
    "getOrganisation": "query",
    "createOrganisation": "command",
    "enrolTotp": "command",
    "confirmTotp": "command",
    "assertTotp": "command",
    "recordEntityVerification": "command"
  }
}
```

Write the complete eight-module object now using every method in the Task 5 matrix; Task 5 verifies rather than finishes it. `conveyor-contract.ts` deep-freezes a defensive copy, asserts exactly eight module keys, permits only `query` and `command`, proves all method names are unique for the existing E2E coverage index, and exports the exact procedure-channel allowlist `conveyor:<module>`. It declares only the schema-free `window.conveyor` bridge and the existing optional `window.tammyLaunchScenario`; it must not declare `window.tammy`.

Move `SystemDiagnostics`, `MachineCredentialFileSelection`, and `isBoundedUtf8String` into this pure shared contract. Make `desktop-api.ts` temporarily re-export them so the still-live legacy renderer remains green. Change `core-client.ts` to import `SystemDiagnostics` from the new contract. Move the `TrustedSbrMainBoundary` shape into `sbr-file-intake.ts` as its own `SbrFileIntake` methods, then have the temporary legacy router import that type instead of owning it. This removes all reverse dependencies on files Task 8 will delete.

- [ ] **Step 2: Write failing schema and error-boundary tests**

Cover these invariants in `proto.test.ts`:

- a procedure with input accepts exactly one non-empty `Uint8Array` no larger than its existing per-method limit;
- subclasses, arrays, `DataView`, detached/invalid views, empty frames, oversized frames, and extra procedure arguments fail validation;
- accepted frames are copied before decoding and encoded responses are copied before return;
- Zod shape/type/size rejection uses Conveyor's stable `INVALID_INPUT` code with generic issues that never echo the submitted value;
- canonical protobuf envelope/type failures surface as `INVALID_RPC_REQUEST`;
- any Core or response-encoding failure surfaces as `CORE_REQUEST_FAILED` without the original message;
- the handler-error sanitizer in `error.ts` never serializes a path, subprocess output, password, bookmark, file handle, stack, or unknown thrown value.

Use `apps/desktop/src/shared/proto-ipc.ts` for canonical encode/decode; do not reimplement protobuf framing.

- [ ] **Step 3: Write failing sender-authorization tests**

In `guard.test.ts`, construct minimal fake events/windows and prove the authorizer accepts only when all of these are the same live objects: `event.sender`, `event.senderFrame`, the registered BrowserWindow's `webContents`, and its main frame. Reject before any context/schema/domain callback when the window or contents is missing, destroyed, replaced, a child frame, a different sender, or has a non-canonical URL.

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- src/conveyor/proto.test.ts src/conveyor/guard.test.ts
```

Expected: FAIL because the Conveyor boundary files do not exist.

- [ ] **Step 4: Implement the shared primitives**

Initialize only the primitives needed by this application:

```ts
export interface TammyConveyorContext {
  readonly client: CoreClient;
  readonly fileIntake: SbrFileIntake;
}

export const { command, defineModule, query } =
  initConveyor<TammyConveyorContext>();
```

Do not export or initialize streams, events, stores, or middleware. In `proto.ts`, provide one bounded copied-frame Zod schema and one `invokeCore(codec, frame, call)` helper. Let pre-handler Zod rejection retain Conveyor's `INVALID_INPUT` code; use only generic schema messages and never interpolate values. Convert canonical request decoding failures to `new ConveyorError("INVALID_RPC_REQUEST")`; convert Core and response failures to `new ConveyorError("CORE_REQUEST_FAILED")`. Keep handler-originated public messages equal to their codes.

In `guard.ts`, create a synchronous authorizer around `getWindow()` and the already-validated `applicationUrl`. Throw only `new ConveyorError("IPC_SENDER_REJECTED")`. Do not parse a new environment URL inside this layer or trust `BrowserWindow.fromWebContents` as ownership evidence.

- [ ] **Step 5: Run the primitive tests GREEN**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- src/conveyor/proto.test.ts src/conveyor/guard.test.ts src/shared/proto-ipc.test.ts
rtk mise exec -- pnpm --dir apps/desktop typecheck
```

Expected: PASS; malformed calls never reach Core and all accepted binary values are defensive copies.

- [ ] **Step 6: Commit the boundary primitives**

```bash
rtk git add apps/desktop/src/shared/conveyor-manifest.json apps/desktop/src/shared/conveyor-contract.ts apps/desktop/src/shared/desktop-api.ts apps/desktop/src/conveyor apps/desktop/src/main/core-client.ts apps/desktop/src/main/core-client.test.ts apps/desktop/src/main/sbr-file-intake.ts apps/desktop/src/main/sbr-file-intake.test.ts apps/desktop/src/main/rpc-router.ts
rtk git commit -m "feat: define guarded Conveyor boundary"
```

### Task 5: Implement all eight domain modules

Use `@superpowers:test-driven-development`.

**Files:**
- Create: `apps/desktop/src/conveyor/modules/system.ts`
- Create: `apps/desktop/src/conveyor/modules/workspace.ts`
- Create: `apps/desktop/src/conveyor/modules/organisation.ts`
- Create: `apps/desktop/src/conveyor/modules/accounting.ts`
- Create: `apps/desktop/src/conveyor/modules/banking.ts`
- Create: `apps/desktop/src/conveyor/modules/documents.ts`
- Create: `apps/desktop/src/conveyor/modules/reporting.ts`
- Create: `apps/desktop/src/conveyor/modules/sbr.ts`
- Create: `apps/desktop/src/conveyor/modules/core-domains.test.ts`
- Create: `apps/desktop/src/conveyor/modules/sbr.test.ts`
- Create: `apps/desktop/src/conveyor/router.ts`
- Create: `apps/desktop/src/conveyor/router.test.ts`

- [ ] **Step 1: Lock the module-to-Core mapping with failing tests**

Require this complete public matrix. Every read is a `query`; every mutation is a `command`:

| Module | Queries | Commands |
|---|---|---|
| `system` | `getSystemDiagnostics` | — |
| `workspace` | — | `createWorkspace`, `confirmRecovery`, `unlockWorkspace`, `signIn` |
| `organisation` | `getCurrentUser`, `getOrganisation` | `createOrganisation`, `enrolTotp`, `confirmTotp`, `assertTotp`, `recordEntityVerification` |
| `accounting` | `listAccounts`, `listJournals`, `getJournal`, `getTrialBalance` | `createAccount`, `postManualJournal` |
| `banking` | `listBankStatementLines`, `getBankingSummary` | `importBankStatement`, `matchBankStatementLine`, `completeBankReconciliation` |
| `documents` | `listDocuments`, `getDocument` | `ingestDocument`, `saveDocumentReview` |
| `reporting` | `getCurrentBasDraft`, `getReportingCapability`, `getAttentionSummary` | `createBasDraft` |
| `sbr` | `getSbrReadiness`, `getMachineCredentialStatus` | `selectMachineCredentialFile`, `importMachineCredential`, `replaceMachineCredential`, `unlockMachineCredential`, `removeMachineCredential`, `importSbrProductId`, `removeSbrProductId`, `runSbrReadinessFixture` |

The tests compare the router's generated manifest to `conveyor-manifest.json`, call every procedure through Conveyor's pure caller, verify its exact Core method, and assert a wrong module/method cannot dispatch. Preserve every request and response limit currently encoded in `DESKTOP_PROTO_REQUEST_LIMITS` and the mediated SBR validators.

- [ ] **Step 2: Run the module tests RED**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- src/conveyor/modules/core-domains.test.ts src/conveyor/modules/sbr.test.ts src/conveyor/router.test.ts
```

Expected: FAIL because the modules and router do not exist.

- [ ] **Step 3: Implement system, workspace, and organisation**

Move the existing protobuf codec declarations from `main/rpc-router.ts` into the owning module. Keep the same generated request/response schemas and limits. `system.getSystemDiagnostics` is the only no-input query; all other non-mediated methods accept one bounded protobuf frame and return one bounded protobuf frame.

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- src/conveyor/modules/core-domains.test.ts`

Expected: the first three module groups pass while the deliberately incomplete router assertion remains red.

- [ ] **Step 4: Implement accounting, banking, documents, and reporting**

Move the remaining ordinary codecs without changing Core calls or renderer-facing protobuf frames. Do not add derived state or cross-domain services. Compose the seven completed modules in `router.ts` and keep the final SBR slot explicit.

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- src/conveyor/modules/core-domains.test.ts`

Expected: all ordinary-domain mappings, limits, canonical frames, and error sanitation pass.

- [ ] **Step 5: Implement the sensitive SBR module**

Keep `main/sbr-file-intake.ts` as an independent main-only capability boundary. The SBR tests must prove:

- file selection returns only `{ selected: false }` or `{ selected: true, handle: <UUIDv7> }`;
- no path or bookmark reaches the renderer;
- import/replace consumes the handle once and only main adds `selectedLocalPath`, bookmark, and password to the decoded Core request;
- the incoming protobuf command has empty trusted-file/password fields before enrichment;
- passwords are valid bounded UTF-8 and are never retained;
- the existing generation binding, five-minute expiry, MAS bookmark requirements, non-MAS bookmark rejection, byte wiping, replacement, and shutdown behavior remain intact.

Use exact-object Zod schemas for the mediated inputs. `selectMachineCredentialFile` has no input. Reuse the file-intake object; do not add renderer paths, new filesystem APIs, or a second credential store.

- [ ] **Step 6: Complete the router and run all module tests GREEN**

Export `type TammyRouter = ReturnType<typeof createTammyRouter>` without exporting the Core client or runtime context to renderer code. Compose exactly the eight modules once and assert the runtime manifest equals the fixed shared manifest.

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- src/conveyor/modules/core-domains.test.ts src/conveyor/modules/sbr.test.ts src/conveyor/router.test.ts src/main/sbr-file-intake.test.ts
rtk mise exec -- pnpm --dir apps/desktop typecheck
```

Expected: PASS; manifest and procedure counts match and the sensitive-flow tests observe no secret-bearing renderer output.

- [ ] **Step 7: Commit the domain router**

```bash
rtk git add apps/desktop/src/conveyor/modules apps/desktop/src/conveyor/router.ts apps/desktop/src/conveyor/router.test.ts
rtk git commit -m "feat: route Tammy domains through Conveyor"
```

### Task 6: Build the owned registrar and restricted preload bridge

Use `@superpowers:test-driven-development` and `@security-best-practices`.

**Files:**
- Create: `apps/desktop/src/conveyor/registration.ts`
- Create: `apps/desktop/src/conveyor/registration.test.ts`
- Create: `apps/desktop/src/preload/conveyor-bridge.ts`
- Create: `apps/desktop/src/preload/conveyor-bridge.test.ts`

- [ ] **Step 1: Write failing bridge and registrar tests**

The bridge test must capture the object passed to `contextBridge.exposeInMainWorld("conveyor", ...)` and assert its only keys are `invoke`, `manifest`, and `subscribe`. Require:

- `invoke` accepts only `conveyor:<one of eight modules>`, an exact listed method, and that method's exact zero/one input arity;
- `manifest` calls only synchronous `conveyor:manifest`, accepts only the exact fixed manifest, and returns a copied frozen value;
- `subscribe` throws `IPC_SUBSCRIPTIONS_DISABLED` before attaching an Electron listener;
- unknown channels/methods, extra arguments, malformed manifests, and prototype keys fail locally without IPC;
- rejected transport errors surface only a stable code.

The registrar test uses an injected fake `ipcMain` and BrowserWindow getter. It requires exactly eight procedure handlers plus the guarded manifest, `streams: false`, one immutable context containing the supplied Core client/file intake, pre-context authorization, and an ownership-safe idempotent disposer. It does not modify the live lifecycle yet.

- [ ] **Step 2: Run bridge and registrar tests RED**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- src/preload/conveyor-bridge.test.ts src/conveyor/registration.test.ts
```

Expected: FAIL because the restricted bridge and owned registrar do not exist.

- [ ] **Step 3: Implement the restricted bridge**

Implement the `ConveyorBridge` structural type directly rather than calling upstream `exposeConveyor()`. Use the fixed manifest to validate `channel`, `method`, and arity before calling `ipcRenderer.invoke(channel, method, ...args)`. The upstream typed renderer client still operates normally because this is the same three-member bridge shape, only narrower.

Keep `tammyLaunchScenario` as a separate frozen scalar exposed from the preload. Do not expose `ipcRenderer`, event objects, filesystem values, a generic channel, or the old flat facade.

- [ ] **Step 4: Implement the owned registration adapter**

`registration.ts` calls patched `createRouter` with injected dependencies:

```ts
createRouter(modules, {
  authorize: createConveyorAuthorizer({ applicationUrl, getWindow }),
  createContext: () => ({ client, fileIntake }),
  ipcMain,
  streams: false,
});
```

Return its idempotent disposer. The injected `getWindow` returns a native BrowserWindow only while the lifecycle-owned window is current; registration itself must not capture a stale window.

- [ ] **Step 5: Keep the new components dormant until the atomic cutover**

Do not import the new bridge from `preload/index.ts` or replace `registerIpc` in production yet. This keeps the currently runnable legacy app green between commits; Task 7 wires the main, preload, and renderer sides together in one change.

- [ ] **Step 6: Run component verification GREEN**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- src/preload/conveyor-bridge.test.ts src/conveyor/registration.test.ts
rtk mise exec -- pnpm --dir apps/desktop typecheck
rtk mise exec -- pnpm --dir apps/desktop build
```

Expected: PASS; the new files compile and their injected tests prove the intended bridge/registration behavior without creating a second live runtime.

- [ ] **Step 7: Commit the runtime boundary**

```bash
rtk git add apps/desktop/src/conveyor/registration.ts apps/desktop/src/conveyor/registration.test.ts apps/desktop/src/preload/conveyor-bridge.ts apps/desktop/src/preload/conveyor-bridge.test.ts
rtk git commit -m "feat: prepare secure Conveyor runtime"
```

### Task 7: Move the renderer onto the typed domain client

Use `@superpowers:test-driven-development`.

**Files:**
- Create: `apps/desktop/src/conveyor/client.ts`
- Create: `apps/desktop/src/app/test/conveyor-fixtures.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.test.ts`
- Modify: `apps/desktop/src/main/index-lifecycle.ts`
- Modify: `apps/desktop/src/main/electron-lifecycle.test.ts`
- Modify: `apps/desktop/src/main/index-production.ts`
- Modify: `apps/desktop/src/main/index-production.test.ts`
- Modify: `apps/desktop/src/app/main.tsx`
- Modify: `apps/desktop/src/app/app.tsx`
- Modify: `apps/desktop/src/app/app.test.tsx`
- Modify: `apps/desktop/src/app/app-shell/**`
- Modify: `apps/desktop/src/app/features/**`
- Modify: `apps/desktop/tests/e2e/electron-vite-development.spec.ts`

- [ ] **Step 1: Write the renderer, lifecycle, and cache contracts RED**

Create tests that require one process-local `QueryClient`, one `createConveyorReactClient<TammyRouter>({ queryClient })`, and one `QueryClientProvider` at the renderer root. The client file imports `TammyRouter` with `import type`; its built renderer graph must contain no Electron, Core, file-intake, main-router, path, or Node built-in import.

Use React Query only for the existing stable reads: diagnostics, current user/organisation, account/journal/trial-balance lists, banking lines/summary, document list/get, current BAS/capability/attention, and SBR readiness/status. Memoize their protobuf frames by actual filter/workspace inputs. Keep setup, recovery, sign-in, TOTP, credential/product-ID, and simulator commands imperative.

Extend lifecycle tests to require Core diagnostics before Conveyor registration, registration before window creation, router disposal before file-intake clearing/window/Core teardown, and idempotent cleanup after partial startup failure. Extend preload-entry tests to require only the restricted bridge plus launch scenario.

- [ ] **Step 2: Create one typed test fixture**

Replace repeated casts to `TammyDesktopAPI` with `createConveyorFixture()`. It returns independently mocked module objects shaped as `Pick<TammyConveyor, "system" | ...>` and a fresh QueryClient configured with retries disabled. Tests must fail if a screen asks for a procedure outside its declared module slice.

- [ ] **Step 3: Migrate the application shell and setup flows**

Use `tammyConveyor.system`, `.workspace`, and `.organisation` directly. Preserve the current startup/setup/unlock/authentication transitions and error copy. Convert only diagnostics/current-user/organisation reads to queries; keep secret-bearing actions as direct commands. Assert no passphrase, recovery material, or TOTP assertion enters a query key or cache.

- [ ] **Step 4: Migrate accounting, banking, documents, reporting, and SBR screens**

Retype each screen to the smallest owning module slice and pass that module from `app.tsx`. For the audit screen, pass the four named read modules rather than recreating a flat compatibility object. Use command `onSuccess` callbacks to invalidate only reads affected by the existing action; examples are journal → journals/trial balance/attention, statement change → banking lines/summary/attention, document review → documents/attention, BAS draft → current BAS/attention, and SBR mutations → readiness/status.

Do not introduce stores, streams, events, optimistic accounting writes, or a general repository layer.

- [ ] **Step 5: Wire all three sides in one cutover**

Rename the lifecycle dependency `registerIpc` to `registerConveyor`. In `index-production.ts`, call the new registrar only after Core diagnostics; adapt its BrowserWindow getter to return `nativeWindow` only while the lifecycle's current window exists. Import `exposeTammyConveyor` from the preload entry and remove `createTammyDesktopAPI` exposure there. The old source files remain present but dead until Task 8 deletes them.

Update the real development smoke's one diagnostics call to the typed Conveyor client exercised by the application. Also assert `window.tammy` is absent, manifest keys equal the fixed eight modules, and `subscribe`/an arbitrary channel are refused without reaching main.

- [ ] **Step 6: Run renderer and real-runtime behavior GREEN**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- src/app
rtk mise exec -- pnpm --dir apps/desktop test -- src/preload/index.test.ts src/preload/conveyor-bridge.test.ts src/conveyor/registration.test.ts src/main/electron-lifecycle.test.ts src/main/index-production.test.ts
rtk mise exec -- pnpm --dir apps/desktop typecheck
rtk mise exec -- pnpm --dir apps/desktop build
rtk mise exec -- pnpm core:build
rtk mise exec -- pnpm build:manifest
rtk mise exec -- pnpm --dir apps/desktop exec playwright test --config playwright.electron-vite.config.ts
```

Expected: PASS; current product behavior remains visible through one real sandboxed Conveyor runtime, the authenticated Core exits cleanly, and no renderer source imports `desktop-api` or reads `window.tammy`.

- [ ] **Step 7: Commit the atomic application cutover**

```bash
rtk git add apps/desktop/src/conveyor/client.ts apps/desktop/src/app apps/desktop/src/preload/index.ts apps/desktop/src/preload/index.test.ts apps/desktop/src/main/index-lifecycle.ts apps/desktop/src/main/electron-lifecycle.test.ts apps/desktop/src/main/index-production.ts apps/desktop/src/main/index-production.test.ts apps/desktop/tests/e2e/electron-vite-development.spec.ts
rtk git commit -m "refactor: cut desktop over to Conveyor"
```

### Task 8: Remove the legacy IPC surface and update acceptance coverage

Use `@superpowers:test-driven-development` and `@superpowers:verification-before-completion`.

**Files:**
- Modify: `apps/desktop/tests/e2e/fixtures.ts`
- Modify: `apps/desktop/tests/e2e/foundation.spec.ts`
- Modify: `apps/desktop/tests/e2e/current-workflows.spec.ts`
- Modify: `apps/desktop/tests/e2e/sbr-readiness.spec.ts`
- Modify: `apps/desktop/tests/e2e/support/current-accounting-workflow.ts`
- Modify: `scripts/check-e2e-coverage.mjs`
- Modify: `scripts/check-e2e-coverage.test.mjs`
- Modify: `scripts/check-slice-one-coverage-policy.test.mjs`
- Modify: `taskfiles/sbr.yml`
- Modify: `scripts/check-taskfiles.test.mjs`
- Delete: `apps/desktop/src/shared/desktop-api.ts`
- Delete: `apps/desktop/src/shared/preload-methods.json`
- Delete: `apps/desktop/src/main/ipc.ts`
- Delete: `apps/desktop/src/main/ipc.test.ts`
- Delete: `apps/desktop/src/main/rpc-router.ts`
- Delete: `apps/desktop/src/main/rpc-router.test.ts`
- Create: `apps/desktop/src/conveyor/no-legacy-ipc.test.ts`

- [ ] **Step 1: Make policy and E2E source tests expect Conveyor**

Teach the coverage checker to load `conveyor-manifest.json`, flatten its unique method names, and compare that set to each existing production `preload` entry. Keep the coverage schema and declared-future rules unchanged. Update its isolated filesystem fixtures and tests to reject unknown kinds, duplicate method names, unlisted modules, and future RPCs accidentally added to the public manifest.

Update direct E2E setup/assertion helpers to use an allowlisted Conveyor channel/method through the restricted bridge only where UI setup cannot do the job; keep user journeys UI-driven. `foundation.spec.ts` must assert `window.tammy` is absent, the public bridge has exactly three members, the manifest has exactly eight modules, unknown calls/subscriptions fail, and diagnostics work.

- [ ] **Step 2: Add the legacy-source test RED**

Scan executable desktop source/config/E2E files, excluding only the negative assertion text in this test, and fail on:

```text
window.tammy
TammyDesktopAPI
DesktopRpcRouter
tammy:system-diagnostics
tammy:workspace-create
preload-methods.json
registerDesktopIpc
```

Run: `rtk mise exec -- pnpm --dir apps/desktop test -- src/conveyor/no-legacy-ipc.test.ts`

Expected: FAIL while the old files still exist.

- [ ] **Step 3: Delete the old API in one cutover**

Delete only the six listed implementation/test files, remove their imports, and remove obsolete Taskfile test paths. Do not leave adapters, aliases, deprecated globals, duplicate channel handlers, or a compatibility mode. Historical design/plan documents may retain their record of the old API and are outside the executable-source assertion.

- [ ] **Step 4: Run Chunk 2 verification GREEN**

Run:

```bash
rtk mise exec -- node --test scripts/check-e2e-coverage.test.mjs scripts/check-slice-one-coverage-policy.test.mjs scripts/check-taskfiles.test.mjs
rtk mise exec -- pnpm --dir apps/desktop test
rtk mise exec -- pnpm --dir apps/desktop typecheck
rtk mise exec -- pnpm --dir apps/desktop build
rtk mise exec -- pnpm core:build
rtk mise exec -- pnpm build:manifest
rtk mise exec -- pnpm --dir apps/desktop exec playwright test --config playwright.electron-vite.config.ts
rtk rg -n 'window\.tammy|TammyDesktopAPI|DesktopRpcRouter|tammy:[a-z-]+|preload-methods\.json|registerDesktopIpc' apps/desktop/src apps/desktop/tests taskfiles scripts
```

Expected: all checks pass; the final search returns only explicit negative assertions, and the real development smoke exercises the one remaining process boundary.

- [ ] **Step 5: Commit the single-runtime cutover**

```bash
rtk git add apps/desktop scripts/check-e2e-coverage.mjs scripts/check-e2e-coverage.test.mjs scripts/check-slice-one-coverage-policy.test.mjs scripts/check-taskfiles.test.mjs taskfiles/sbr.yml
rtk git commit -m "refactor: remove legacy desktop IPC"
```

---

## Chunk 3: electron-builder packaging and MAS staging

### File structure for this chunk

- Create `apps/desktop/electron-builder.yml` as the sole packaging configuration.
- Create `apps/desktop/scripts/electron-builder-after-pack.cjs` and focused configuration/hook tests.
- Modify desktop package scripts and the root package owner to build electron-vite output before invoking Builder.
- Modify the one packaged-layout locator and every consumer to use Builder's deterministic `.tmp` roots.
- Extend package verification to read Electron fuses and prove ASAR integrity at runtime.
- Modify the existing macOS two-copy release owner to assemble only its unsigned MAS app with Builder; keep Tammy's manual resource signing, app signing, package creation, and evidence gates.
- Modify App Store repository checks and privacy inspection to inspect Builder configuration/output directly.
- Keep `apps/desktop/forge.config.ts` and Forge dependencies only until all Builder paths pass; Chunk 4 removes them.

### Task 9: Define and prove the Builder package contract

Use `@superpowers:test-driven-development` and `@security-best-practices`.

**Files:**
- Create: `apps/desktop/electron-builder.yml`
- Create: `apps/desktop/scripts/electron-builder-after-pack.cjs`
- Create: `apps/desktop/scripts/electron-builder-after-pack.test.mjs`
- Create: `apps/desktop/scripts/electron-builder-config.test.mjs`
- Modify: `apps/desktop/package.json`
- Modify: `pnpm-lock.yaml`
- Modify: `scripts/write-build-manifest.mjs`
- Modify: `scripts/write-build-manifest.test.mjs`

- [ ] **Step 1: Write the failing static configuration test**

Parse `electron-builder.yml` and require:

- `appId: com.tammy.desktop`, `productName: Tammy`, finance category, minimum macOS 14, Tammy icon, Builder's supported base helper bundle ID, and no updater/publish URL; the desktop `package.json` remains the single marketing-version source, with no duplicate version in YAML;
- `directories.output: ${env.TAMMY_ELECTRON_BUILDER_OUTPUT}` so an orchestration owner chooses one validated absolute root beneath repository `.tmp`;
- an ASAR containing only `out/**` plus the minimal package metadata, with source/tests/configuration excluded;
- `npmRebuild: false` because all JavaScript dependencies are bundled and native Core/helper binaries are explicit resources;
- ordinary mac resources `build`, `core`, `sqlcipher`, `sbr`, `sbr-helper`; Windows resources `build`, `core`, `sqlcipher`; MAS repeats the mac set and adds `release/macos/PrivacyInfo.xcprivacy` as `PrivacyInfo.xcprivacy`;
- an `afterPack` hook and no unknown platform/architecture fallback;
- `mac.identity: "-"`, `hardenedRuntime: false`, and sign-ignore expressions matching only the authenticated Core and SBR-helper executable suffixes;
- `mas.identity: null`, so Builder assembles but never signs or creates an installer in Tammy's two-copy flow.
- MAS `bundleVersion`, `ElectronTeamID`, privacy URL, and support URL read only their named `TAMMY_MACOS_*` environment macros; minimum macOS 14 and copyright are fixed configuration values. The hook converts the validated export-compliance value to the required Boolean `ITSAppUsesNonExemptEncryption` rather than storing an environment string in the plist.

Assert the exact fuse block:

```yaml
electronFuses:
  runAsNode: false
  enableCookieEncryption: true
  enableNodeOptionsEnvironmentVariable: false
  enableNodeCliInspectArguments: true
  enableEmbeddedAsarIntegrityValidation: true
  onlyLoadAppFromAsar: true
```

Do not add auto-update, DMG, generic notarization, multi-architecture, Linux, or publishing configuration.

- [ ] **Step 2: Write failing after-pack hook tests**

Call the CommonJS hook with a temporary fake Builder context and injected filesystem/process functions. Require it to:

- accept only `darwin/arm64`, `mas/arm64`, and `win32/x64`;
- verify the app/output roots are real, non-symlinked descendants of the supplied output root;
- remove the same six unused sensitive Info.plist keys on macOS/MAS;
- set the exact `.helper`, `.helper.GPU`, `.helper.Plugin`, and `.helper.Renderer` bundle IDs;
- normalize Core and SBR-helper executable permissions without following symlinks;
- for `mas/arm64`, require the unsigned-staging release environment and write/verify the reserved `CFBundleVersion`, `ElectronTeamID`, Boolean export-compliance value, minimum OS, copyright, and exact privacy/support URLs in the assembled app's `Info.plist`;
- set and verify the exact `.helper`, `.helper.GPU`, `.helper.Plugin`, and `.helper.Renderer` IDs in their four helper plists;
- perform no macOS command on Windows;
- reject missing, duplicate, escaping, symlinked, or wrong-target app/resource paths with one stable error.

Run:

```bash
rtk mise exec -- node --test apps/desktop/scripts/electron-builder-config.test.mjs apps/desktop/scripts/electron-builder-after-pack.test.mjs
```

Expected: FAIL because the Builder configuration and hook do not exist.

- [ ] **Step 3: Implement the minimal Builder configuration and hook**

Use Builder's platform-specific `extraResources` arrays rather than copying a superset. Set the supported base `helperBundleId` in configuration; the after-pack hook is the single owner that writes and verifies all four exact helper IDs. For MAS, use Builder environment macros only for string-valued release-profile fields and have the hook validate the full unsigned-staging environment before writing the Boolean export field. The hook receives Builder's `appOutDir`, `electronPlatformName`, and numeric architecture and operates only before signing. It may invoke only `/usr/bin/plutil` and permission normalization; it must not sign, download, publish, or mutate source resources.

Keep ASAR integrity enabled (do not set `disableAsarIntegrity` or an equivalent). Keep the six approved fuse values exactly as before. Builder flips them after `afterPack` and before signing, so no post-sign hook may modify the application executable or ASAR.

- [ ] **Step 4: Record the adopted package tooling in the build manifest**

Add exact `electron_vite`, `electron_builder`, and `electron_conveyor` fields beside existing Electron/Vite toolchain evidence. Add direct `@electron/asar@3.4.1` to desktop dev dependencies so privacy/package inspection no longer reaches it through Forge. Update manifest schema/tests and keep the generated manifest deterministic.

- [ ] **Step 5: Run static configuration GREEN**

Run:

```bash
rtk mise exec -- pnpm install --frozen-lockfile
rtk mise exec -- node --test apps/desktop/scripts/electron-builder-config.test.mjs apps/desktop/scripts/electron-builder-after-pack.test.mjs scripts/write-build-manifest.test.mjs
rtk mise exec -- pnpm --dir apps/desktop typecheck
```

Expected: PASS with no signing identity, secret, URL, or updater in the Builder file.

- [ ] **Step 6: Commit the Builder contract**

```bash
rtk git add apps/desktop/electron-builder.yml apps/desktop/scripts/electron-builder-after-pack.cjs apps/desktop/scripts/electron-builder-after-pack.test.mjs apps/desktop/scripts/electron-builder-config.test.mjs apps/desktop/package.json pnpm-lock.yaml scripts/write-build-manifest.mjs scripts/write-build-manifest.test.mjs
rtk git commit -m "build: define electron-builder package contract"
```

### Task 10: Migrate ordinary packaging and packaged E2E

Use `@superpowers:test-driven-development` and `@superpowers:verification-before-completion`.

**Files:**
- Modify: `scripts/package-desktop.mjs`
- Modify: `scripts/package-desktop.test.mjs`
- Modify: `apps/desktop/scripts/find-packaged-app.mjs`
- Modify: `apps/desktop/scripts/find-packaged-app.test.mjs`
- Modify: `apps/desktop/tests/e2e/package-signature.test.mjs`
- Modify: `apps/desktop/tests/e2e/fixtures.ts`
- Modify: `apps/desktop/tests/e2e/electron-lifecycle.ts`
- Modify: `apps/desktop/tests/e2e/foundation.spec.ts`
- Modify: `apps/desktop/tests/e2e/current-workflows.spec.ts`
- Modify: `apps/desktop/tests/e2e/sbr-readiness.spec.ts`
- Modify: `apps/desktop/playwright.config.ts`
- Modify: `scripts/collect-macos-privacy-evidence.mjs`
- Modify: `scripts/collect-macos-privacy-evidence.test.mjs`

- [ ] **Step 1: Make layout and package-plan tests expect Builder**

Set the ordinary output root to `<repo>/.tmp/electron-builder/ordinary`. Require the platform locator to resolve only:

```text
darwin/arm64  .tmp/electron-builder/ordinary/mac-arm64/Tammy.app
win32/x64     .tmp/electron-builder/ordinary/win-unpacked/Tammy.exe
```

The locator validates every ancestor, rejects symlinks and additional candidates, authenticates the packaged build manifest/native resources, and prints only the canonical executable path. Add a bounded Builder contract smoke in the package-plan test so an upstream directory-name change fails at the packaging owner instead of silently broadening discovery.

Change the desktop package script to `pnpm build && electron-builder --config electron-builder.yml --dir --publish never`. Change `createDesktopPackagePlan()` to set the exact absolute `TAMMY_ELECTRON_BUILDER_OUTPUT` while holding the existing build lock, then retain Core build → SBR-helper build → build manifest → desktop package → signature/resource verification → locator verification.

- [ ] **Step 2: Run the package-owner tests RED**

Run:

```bash
rtk mise exec -- node --test scripts/package-desktop.test.mjs apps/desktop/scripts/find-packaged-app.test.mjs
```

Expected: FAIL on Forge commands and `apps/desktop/out/Tammy-*` paths.

- [ ] **Step 3: Implement the ordinary Builder owner and locators**

Before invoking Builder, safely replace only the exact ordinary output root: its repository `.tmp` parent must be real/non-symlinked, the target must be absent or a real contained directory, and the build lock must still be owned. Never accept an environment-supplied output path for ordinary packaging.

Update package signature/resource tests and Playwright fixtures to consume `find-packaged-app.mjs` or the same exact layout function; do not duplicate path guessing. Update privacy inspection to import the direct `@electron/asar` dependency and scan `out/main`, `out/preload`, and `out/renderer` rather than `.vite` paths.

- [ ] **Step 4: Add fuse and integrity artifact assertions**

Use `getCurrentFuseWire()` from the pinned `@electron/fuses` to assert the exact six fuse states on the packaged executable. Verify `app.asar` contains only the expected built application graph and that no fallback `app/` directory exists.

For a negative integrity test, copy the packaged app/executable plus resources into a bounded temporary directory, change one byte in the copied `app.asar`, then ad-hoc re-sign only that isolated macOS copy before launch so code-signature rejection cannot satisfy the test. Launch with isolated user data and require termination before a Tammy window becomes ready. Always reap the copied Electron/Core processes and delete the temporary directory. The untouched artifact must then launch through the normal packaged E2E suite, which proves its embedded ASAR metadata is valid. On Windows, perform the equivalent copied-ASAR tamper without signing.

- [ ] **Step 5: Build and inspect one real ordinary package**

Run on a supported native target:

```bash
rtk mise exec -- pnpm desktop:package
rtk mise exec -- node --test apps/desktop/tests/e2e/package-signature.test.mjs
rtk mise exec -- node apps/desktop/scripts/find-packaged-app.mjs --verify --source-manifest apps/desktop/resources/build/build-manifest.json
```

Expected on macOS arm64: exactly one ad-hoc-signed `mac-arm64/Tammy.app`, authenticated Core/helper/resources, valid ASAR integrity, correct helper IDs, and no sensitive usage strings. Expected on Windows x64: exactly one `win-unpacked/Tammy.exe`, no SBR resources, and explicit SBR unavailability. Other targets fail before packaging.

- [ ] **Step 6: Run packaged product E2E GREEN**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop e2e
```

Expected: PASS against the real Builder package with isolated user data and no mocks: foundation/security, workspace login, accounts/journals/trial balance, statement reconciliation, document upload/review, BAS/reporting, and (on macOS arm64) SBR registration/simulator flows. Teardown proves both Electron and Core exit.

- [ ] **Step 7: Commit ordinary packaging**

```bash
rtk git add scripts/package-desktop.mjs scripts/package-desktop.test.mjs apps/desktop/scripts apps/desktop/tests/e2e apps/desktop/playwright.config.ts scripts/collect-macos-privacy-evidence.mjs scripts/collect-macos-privacy-evidence.test.mjs apps/desktop/package.json
rtk git commit -m "build: package Tammy with electron-builder"
```

### Task 11: Port authenticated MAS assembly to Builder

Use `@superpowers:test-driven-development`, `@security-best-practices`, and `@superpowers:verification-before-completion`.

**Files:**
- Modify: `scripts/package-macos-store.mjs`
- Modify: `scripts/package-macos-store.test.mjs`
- Modify: `scripts/build-sbr-helper.test.mjs`
- Modify: `scripts/check-macos-store.mjs`
- Modify: `scripts/check-macos-store.test.mjs`
- Modify: `scripts/macos-unsigned-content.mjs`
- Modify: `scripts/macos-unsigned-content.test.mjs`
- Modify: `scripts/capture-app-store-screenshots.mjs`
- Modify: `scripts/capture-app-store-screenshots.test.mjs`
- Modify: `scripts/collect-macos-privacy-evidence.mjs`
- Modify: `scripts/collect-macos-privacy-evidence.test.mjs`
- Modify: `scripts/inspect-macos-release-package.mjs`
- Modify: `scripts/inspect-macos-release-package.test.mjs`
- Modify: `apps/desktop/tests/e2e/app-store-privacy.spec.ts`
- Modify: `apps/desktop/tests/e2e/app-store-screenshots.spec.ts`
- Modify: `apps/desktop/tests/e2e/package-signature.test.mjs`

- [ ] **Step 1: Make release-plan tests expect an unsigned Builder app**

Rename all live `forgeApp`/`forgeOutputRoot` fields and fixtures to `builderApp`/`builderOutputRoot`. Require exactly:

```text
.tmp/macos-release/<version>/build-<number>/.builder-unsigned/mas-arm64/Tammy.app
```

The Builder output environment contains `TAMMY_ELECTRON_BUILDER_OUTPUT=<that .builder-unsigned root>`, `TAMMY_MACOS_ARTIFACT_PHASE=unsigned-staging`, `TAMMY_MACOS_TARGET=mas/arm64`, `TAMMY_RELEASE_PROFILE=mas`, the reserved build number, export decision, validated Team ID, public URLs, and no signing, provisioning, or installer inputs. Replace the old `TAMMY_MACOS_UNSIGNED_OUTPUT_ROOT` key everywhere in the owner/tests; never accept both names. Reject an existing/symlinked/escaping Builder root and reject extra apps, archives, or packages.

Replace the Forge stage command with `pnpm --dir apps/desktop package:mas:unsigned`, implemented as `pnpm build && electron-builder --config electron-builder.yml --mac mas --arm64 --publish never`. The `mas.identity: null` contract makes Builder skip signing; because no sign result exists, Builder emits only `mas-arm64/Tammy.app` and no installer.

- [ ] **Step 2: Run release-owner tests RED**

Run:

```bash
rtk mise exec -- node --test scripts/package-macos-store.test.mjs scripts/macos-unsigned-content.test.mjs scripts/check-macos-store.test.mjs scripts/build-sbr-helper.test.mjs
```

Expected: FAIL on `.forge-unsigned`, Forge command arguments, and Forge configuration inspection.

- [ ] **Step 3: Preserve the existing two-copy signing order**

Change only the assembly tool and field names. Retain this exact order:

1. validate release inputs and build the raw helper/unsigned simulator profile;
2. build Core and the initial authenticated manifest;
3. let Builder assemble the unsigned MAS app, apply the after-pack policy, ASAR integrity, and fuses;
4. authenticate and atomically publish the immutable unsigned content manifest;
5. clone it into a fresh mode-specific signing root;
6. sign/verify the SBR helper, rebuild/sign its profile, sign/verify Core, then rebuild the authenticated build manifest;
7. replace only `build`, `core`, `sbr`, and `sbr-helper` in the signing copy;
8. sign the Electron app with parent/child entitlements and provisioning profile while excluding Core/helper;
9. verify signatures, resources, fuses, ASAR integrity, and signed-copy equivalence;
10. in distribution mode, create and verify the installer with `/usr/bin/productbuild`, then atomically publish the mode root.

Do not let Builder sign the MAS assembly, rebuild native dependencies, make the installer, or modify resources after Tammy records their authenticated hashes.

- [ ] **Step 4: Update App Store and evidence inspectors**

Make `check-macos-store.mjs` parse `electron-builder.yml` and the after-pack hook instead of `forge.config.ts`. Require the Builder dependency/version, exact IDs/category/resources/fuses, unsigned identity, native sign-ignore contract, and absence of publishing. Update privacy, screenshot, package-inspection, and unsigned-content tests to use Builder roots/names while leaving durable release-record schemas and already-recorded historical evidence untouched.

Assert both the unsigned and final signed app carry the six fuse states. Validate the macOS `ElectronAsarIntegrity` entry against `Contents/Resources/app.asar`, and require the signed app to launch in its isolated test home before screenshot/evidence promotion.

- [ ] **Step 5: Exercise a real unsigned MAS assembly without credentials**

On macOS arm64, build the native resources and run the unsigned package primitive into a disposable validated smoke root:

```bash
rtk mise exec -- pnpm core:build
rtk mise exec -- pnpm sbr-helper:build
rtk mise exec -- pnpm build:manifest
rtk mise exec -- env TAMMY_ELECTRON_BUILDER_OUTPUT="$PWD/.tmp/electron-builder/mas-smoke" TAMMY_MACOS_ARTIFACT_PHASE=unsigned-staging TAMMY_MACOS_BUILD_NUMBER=1 TAMMY_MACOS_EXPORT_COMPLIANCE=exempt TAMMY_MACOS_PRIVACY_POLICY_URL=https://tammy-accounting.castlemilk.chatgpt.site/privacy TAMMY_MACOS_SUPPORT_URL=https://tammy-accounting.castlemilk.chatgpt.site/support TAMMY_MACOS_TARGET=mas/arm64 TAMMY_MACOS_TEAM_ID=TEAMID1234 TAMMY_RELEASE_PROFILE=mas VITE_TAMMY_PRIVACY_POLICY_URL=https://tammy-accounting.castlemilk.chatgpt.site/privacy VITE_TAMMY_SUPPORT_URL=https://tammy-accounting.castlemilk.chatgpt.site/support pnpm --dir apps/desktop package:mas:unsigned
rtk test -f .tmp/electron-builder/mas-smoke/mas-arm64/Tammy.app/Contents/Resources/app.asar
rtk mise exec -- node -e 'const fs=require("node:fs"); if(fs.existsSync(".tmp/electron-builder/mas-smoke/mas-arm64/Tammy.pkg")) process.exit(1)'
```

Expected: one unsigned MAS app with the complete MAS resource matrix, privacy manifest, exact bundle/helper IDs, valid ASAR integrity, and expected fuses; no signing prompt, installer, network publication, or source mutation.

- [ ] **Step 6: Run repository-owned MAS checks GREEN**

Run:

```bash
rtk mise exec -- node --test scripts/package-macos-store.test.mjs scripts/macos-unsigned-content.test.mjs scripts/check-macos-store.test.mjs scripts/build-sbr-helper.test.mjs scripts/capture-app-store-screenshots.test.mjs scripts/collect-macos-privacy-evidence.test.mjs scripts/inspect-macos-release-package.test.mjs
rtk mise exec -- pnpm check:macos-store
```

Expected: PASS without consuming signing credentials or changing release state. A separately authorized `task deploy:mas` remains the owner for a real signed candidate.

- [ ] **Step 7: Commit MAS Builder staging**

```bash
rtk git add apps/desktop/package.json apps/desktop/tests/e2e scripts/package-macos-store.mjs scripts/package-macos-store.test.mjs scripts/build-sbr-helper.test.mjs scripts/check-macos-store.mjs scripts/check-macos-store.test.mjs scripts/macos-unsigned-content.mjs scripts/macos-unsigned-content.test.mjs scripts/capture-app-store-screenshots.mjs scripts/capture-app-store-screenshots.test.mjs scripts/collect-macos-privacy-evidence.mjs scripts/collect-macos-privacy-evidence.test.mjs scripts/inspect-macos-release-package.mjs scripts/inspect-macos-release-package.test.mjs
rtk git commit -m "build: assemble MAS releases with electron-builder"
```

---

## Chunk 4: Remove Forge and preserve the operator contract

### File structure for this chunk

- Delete `apps/desktop/forge.config.ts` after both ordinary and MAS Builder paths are green.
- Remove only Forge/Squirrel dependencies and workspace exceptions; retain direct `@electron/fuses` and `@electron/asar` inspection dependencies.
- Keep Tammy's public Task scenario names stable while replacing their implementation and output documentation.
- Update only current documentation and compliance evidence; historical designs, plans, and immutable release records remain untouched.

### Task 12: Remove the obsolete Forge toolchain

Use `@superpowers:test-driven-development`.

**Files:**
- Delete: `apps/desktop/forge.config.ts`
- Modify: `apps/desktop/package.json`
- Modify: `package.json`
- Modify: `pnpm-workspace.yaml`
- Modify: `pnpm-lock.yaml`
- Modify: `apps/desktop/scripts/electron-builder-config.test.mjs`

- [ ] **Step 1: Extend the Builder contract test with removal assertions**

Require the desktop package to contain the exact electron-vite, electron-builder, electron-conveyor, React Query, `@electron/fuses`, and `@electron/asar` pins established earlier. Require no `@electron-forge/*` package, maker script, `electron-winstaller`, Forge config, Forge public hoist, or Forge-only build allowlist/override. Also require root `test` to run `apps/desktop/scripts/*.test.mjs`, so Builder configuration, hook, and locator tests remain part of normal verification.

- [ ] **Step 2: Run the removal contract RED**

Run:

```bash
rtk mise exec -- node --test apps/desktop/scripts/electron-builder-config.test.mjs
```

Expected: FAIL while Forge dependencies/configuration and workspace exceptions remain.

- [ ] **Step 3: Remove Forge without adding a compatibility layer**

Delete `forge.config.ts`; remove the six `@electron-forge/*` development dependencies, Forge/maker scripts, `electron-winstaller` override/build permission, and Forge public hoist. Preserve the new `dev`, `build`, `package`, `package:mas:unsigned`, test, typecheck, and E2E scripts. Add the desktop Node-script tests to the existing root test composition rather than creating a second test framework.

Regenerate only the pnpm lockfile through the pinned package manager:

```bash
rtk mise exec -- pnpm install
rtk mise exec -- pnpm install --frozen-lockfile
```

- [ ] **Step 4: Run the one-runtime contract GREEN**

Run:

```bash
rtk mise exec -- node --test apps/desktop/scripts/electron-builder-config.test.mjs scripts/launch-local-scenario.test.mjs scripts/check-taskfiles.test.mjs
rtk mise exec -- pnpm test
rtk mise exec -- pnpm typecheck
rtk rg -n -i 'electron-forge|@electron-forge|electron-winstaller|forge\.config|\.forge-unsigned|--electron-forge|\.vite/' apps/desktop/src apps/desktop/scripts scripts taskfiles package.json pnpm-workspace.yaml
```

Expected: tests pass and the final search has no matches except explicit negative-test fixtures/assertions. Do not edit historical documents merely to erase old terminology.

- [ ] **Step 5: Commit removal**

```bash
rtk git add apps/desktop/package.json package.json pnpm-workspace.yaml pnpm-lock.yaml apps/desktop/scripts/electron-builder-config.test.mjs apps/desktop/forge.config.ts
rtk git commit -m "chore: remove Electron Forge foundation"
```

### Task 13: Keep Task scenarios and CI on the real new runtime

Use `@superpowers:test-driven-development` and `@superpowers:verification-before-completion`.

**Files:**
- Modify: `package.json`
- Modify: `Taskfile.yml`
- Modify: `taskfiles/build.yml`
- Modify: `taskfiles/package.yml`
- Modify: `taskfiles/dev.yml`
- Modify: `taskfiles/test.yml`
- Modify: `taskfiles/ci.yml`
- Modify: `taskfiles/release.yml`
- Modify: `taskfiles/sbr.yml`
- Modify: `scripts/check-taskfiles.test.mjs`

- [ ] **Step 1: Make Taskfile tests express the final scenario contract**

Keep these public entry points and assert their exact ownership:

| Scenario | Contract |
|---|---|
| `task dev` | Build Core, SBR-helper simulator inputs, and the manifest, then launch electron-vite development with persistent local accounting data. |
| `task dev:accounting:fresh` | Launch one isolated retained accounting workspace through the electron-vite scenario owner. |
| `task dev:sbr:simulator` | Launch the authenticated, network-disabled macOS simulator; never accept a live credential on the command line. |
| `task dev:sbr:evte` | Validate externally installed signed inputs and remain fail-closed until the EVTE owner exists. |
| `task test`, `verify:quick`, `verify:full` | Unit/contract/lint tiers retain their current native-target boundaries. |
| `task build` | Produce the ordinary Builder directory application through the single authenticated package owner. |
| `task package` | Build and verify the ordinary Builder application. |
| `task package:e2e` | Launch that real package with isolated user data and prove Electron/Core cleanup. |
| `task release:development` | Create the development-signed app in `.tmp/macos-release/<version>/build-<number>/development/Tammy.app`; no installer/upload. |
| `task release:candidate`, `task deploy:mas` | Create the distribution app/package beneath that build's `distribution/` root; never upload or submit. |

Add one focused root primitive `desktop:e2e:dev` and internal `test:desktop:dev` scenario that build the native resources/manifest and `out/**`, then run `playwright.electron-vite.config.ts`. Require `verify:release` and `ci:macos` to run this real non-packaged smoke before the existing Builder packaged E2E. Keep packaged evidence on macOS arm64 and Windows 11 x64; do not move it into Linux or `verify:full`.

- [ ] **Step 2: Run Taskfile tests RED**

Run:

```bash
rtk mise exec -- node --test scripts/check-taskfiles.test.mjs
```

Expected: FAIL on stale Forge/output wording and the missing durable electron-vite development-smoke owner.

- [ ] **Step 3: Update implementations and summaries only**

Replace stale Forge paths/names and deleted Vitest paths with the electron-vite, Conveyor, and Builder equivalents. Keep all existing platform preconditions, secret-free command-line rules, SBR truth labels, build lock, manual-signing boundary, and no-upload/no-submit summaries. Do not introduce a second task runner or rename the public scenarios.

- [ ] **Step 4: Exercise the scenario graph GREEN**

Run:

```bash
rtk mise exec -- node --test scripts/check-taskfiles.test.mjs scripts/launch-local-scenario.test.mjs
rtk mise exec -- task --list
rtk mise exec -- task verify:quick
rtk mise exec -- task test:desktop:dev
rtk mise exec -- task package:e2e
```

Expected on macOS arm64: all commands pass, both the development smoke and packaged acceptance launch real Electron/Core processes, and teardown leaves no orphan. On Windows x64, run the same development/package scenarios with SBR explicitly unavailable; hosted Windows 11 evidence remains separately observed. Unsupported targets fail at their existing preconditions.

- [ ] **Step 5: Commit Task and CI ownership**

```bash
rtk git add package.json Taskfile.yml taskfiles/build.yml taskfiles/package.yml taskfiles/dev.yml taskfiles/test.yml taskfiles/ci.yml taskfiles/release.yml taskfiles/sbr.yml scripts/check-taskfiles.test.mjs
rtk git commit -m "test: verify Electron foundation scenarios"
```

### Task 14: Update current documentation and compliance evidence

Use `@superpowers:test-driven-development` and `@superpowers:verification-before-completion`.

**Files:**
- Modify: `README.md`
- Modify: `docs/development/foundation.md`
- Modify: `docs/development/tech-state.md`
- Modify: `docs/development/local-accounting-walkthrough.md`
- Modify: `docs/development/sbr-local-readiness.md`
- Modify: `docs/development/user-journey-test-matrix.md`
- Modify: `docs/development/self-reporting-backlog.md`
- Modify: `docs/release/macos-app-store.md`
- Modify: `compliance/build/toolchain.lock.json`
- Modify: `compliance/traceability/foundation.csv`
- Modify: `compliance/threat-model/local-api-foundation.md`
- Modify: `scripts/check-foundation-evidence.test.mjs`

- [ ] **Step 1: Add failing current-evidence assertions**

Extend the foundation-evidence test to require:

- pinned electron-vite, electron-builder, and electron-conveyor entries, the final Vite/React-plugin versions, and no Forge/electron-winstaller entry in `toolchain.lock.json`;
- traceability references to `electron.vite.config.ts`, `electron-builder.yml`, `src/app`, the guarded Conveyor modules/bridge, and current tests rather than deleted Forge/renderer/IPC files;
- the current threat model to name Conveyor sender authorization, fixed method allowlisting, no streams/events, opaque machine-credential handles, ASAR integrity, and fuse verification;
- no stale `.vite`, `src/renderer`, `forge.config.ts`, `window.tammy`, or old `apps/desktop/out/Tammy-mas-arm64` path in current operator docs/evidence.

Historical `docs/superpowers/specs`, `docs/superpowers/plans`, and immutable `docs/release/records` are excluded because they are records, not current instructions.

- [ ] **Step 2: Run evidence tests RED**

Run:

```bash
rtk mise exec -- node --test scripts/check-foundation-evidence.test.mjs
```

Expected: FAIL on stale toolchain, path, transport, and package references.

- [ ] **Step 3: Record only observed platform truth**

Update current docs with the three `out/main`, `out/preload`, and `out/renderer` build outputs; the fixed eight-module `window.conveyor` surface; ordinary Builder paths; deterministic MAS release roots; and the exact Task scenarios above. Update current compliance records to the new implementation/tests and pinned tools.

Do not mark a self-reporting feature complete because of this migration. Add a short backlog status note that the desktop foundation now supports typed/reporting-safe transport and real package verification while company EOFY preparation, company tax return, ATO/SBR production lodgement, DSP registration, conformance, and external Apple approvals remain open.

- [ ] **Step 4: Run documentation and evidence GREEN**

Run:

```bash
rtk mise exec -- node --test scripts/check-foundation-evidence.test.mjs scripts/check-taskfiles.test.mjs scripts/check-macos-store.test.mjs
rtk mise exec -- pnpm compliance:foundation
rtk mise exec -- pnpm check:macos-store
rtk git diff --check
```

Expected: PASS without signing, uploading, publishing, or recording unobserved hosted evidence.

- [ ] **Step 5: Commit current truth**

```bash
rtk git add README.md docs/development/foundation.md docs/development/tech-state.md docs/development/local-accounting-walkthrough.md docs/development/sbr-local-readiness.md docs/development/user-journey-test-matrix.md docs/development/self-reporting-backlog.md docs/release/macos-app-store.md compliance/build/toolchain.lock.json compliance/traceability/foundation.csv compliance/threat-model/local-api-foundation.md scripts/check-foundation-evidence.test.mjs
rtk git commit -m "docs: record Electron foundation migration"
```

### Final acceptance

From a clean macOS arm64 checkout, run:

```bash
rtk mise exec -- pnpm install --frozen-lockfile
rtk mise exec -- task verify:full
rtk mise exec -- task test:desktop:dev
rtk mise exec -- task package:e2e
rtk mise exec -- pnpm core:build
rtk mise exec -- pnpm sbr-helper:build
rtk mise exec -- pnpm build:manifest
rtk mise exec -- env TAMMY_ELECTRON_BUILDER_OUTPUT="$PWD/.tmp/electron-builder/mas-final" TAMMY_MACOS_ARTIFACT_PHASE=unsigned-staging TAMMY_MACOS_BUILD_NUMBER=1 TAMMY_MACOS_EXPORT_COMPLIANCE=exempt TAMMY_MACOS_PRIVACY_POLICY_URL=https://tammy-accounting.castlemilk.chatgpt.site/privacy TAMMY_MACOS_SUPPORT_URL=https://tammy-accounting.castlemilk.chatgpt.site/support TAMMY_MACOS_TARGET=mas/arm64 TAMMY_MACOS_TEAM_ID=TEAMID1234 TAMMY_RELEASE_PROFILE=mas VITE_TAMMY_PRIVACY_POLICY_URL=https://tammy-accounting.castlemilk.chatgpt.site/privacy VITE_TAMMY_SUPPORT_URL=https://tammy-accounting.castlemilk.chatgpt.site/support pnpm --dir apps/desktop package:mas:unsigned
rtk mise exec -- pnpm check:macos-store
rtk mise exec -- pnpm compliance:foundation
rtk git diff --check
rtk mise exec -- node scripts/check-clean-tree.mjs
```

Acceptance requires the real development window, ordinary packaged workflows, and unsigned MAS artifact checks to pass; the final active source/dependency tree has no Forge or legacy IPC compatibility layer; and the worktree is clean. Do not claim hosted Windows 11/macOS CI, Apple signing/upload, or production ATO/SBR evidence until those external runs are actually observed.
