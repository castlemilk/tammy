# macOS App Store Prime-Time Readiness Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a truthful, public, candidate-bound macOS App Store submission bundle for Tammy, with public privacy/support URLs, real-UI screenshots, signed-build evidence, and explicit Apple-controlled gates.

**Architecture:** Keep the existing Electron/Go/SQLCipher product and MAS packager intact. Add a small static Sites workspace fed from repository-owned release identity and privacy content; add deterministic release-state, screenshot, payload, and privacy evidence around the existing package path; and keep upload/submission as explicit operator events. Every result is tied to an immutable product-source tree and reports what is observed rather than implying Apple approval.

**Tech Stack:** TypeScript/Node.js 24, pnpm 11, Go Task, Electron Forge 7, Playwright 1.61, Vitest/Node test, macOS `codesign`/`security`/`pkgutil`/`spctl`/`productbuild`, OpenAI Sites/Vinext, React 19, Tailwind CSS 4, shadcn/ui.

**Authoritative design:** `docs/superpowers/specs/2026-08-30-macos-app-store-prime-time-readiness-design.md`

---

## Chunk 1: Canonical public content and compliance site

### File structure for this chunk

- Create `apps/desktop/release/macos/store-identity.json` as the machine-readable product/legal/platform identity.
- Create `docs/release/authority/publisher-controller.json` only after an explicit authorised-controller confirmation; a later per-release attestation references it.
- Modify `scripts/check-macos-store.mjs` to validate that identity against the desktop package and release configuration.
- Modify `scripts/check-macos-store.test.mjs` with identity and drift tests.
- Modify `PRIVACY.md` as the canonical app/site/support privacy policy.
- Create `scripts/generate-public-content.mjs` to turn the canonical Markdown and identity into safe generated site data.
- Create `scripts/generate-public-content.test.mjs` to test supported Markdown, rejected unsafe constructs, and deterministic output.
- Create `apps/site/` with the pinned Sites scaffold; do not hand-roll or replace the generated Sites architecture.
- Create `apps/site/content/public-content.generated.ts` as generated, committed site input.
- Create `apps/site/components/policy-document.tsx` as the renderer for pre-parsed safe policy blocks.
- Modify `apps/site/app/layout.tsx`, `apps/site/app/globals.css`, and `apps/site/app/page.tsx` for the Tammy shell/home route.
- Create `apps/site/app/privacy/page.tsx` and `apps/site/app/support/page.tsx`.
- Create `apps/desktop/release/macos/data-removal.json` as the single public/test inventory of Tammy-owned macOS data.
- Create `scripts/macos-data-removal.mjs` and `scripts/macos-data-removal.test.mjs` for isolated deletion-boundary verification.
- Create `scripts/macos-data-removal.integration.test.mjs` for a temporary macOS Keychain integration check.
- Create `apps/site/tests/public-routes.test.tsx` for actual route-component assertions.
- Create `scripts/check-public-site.mjs` and `scripts/check-public-site.test.mjs` for pre-publish and deployed-route checks.
- Create `taskfiles/site.yml`; modify `Taskfile.yml` and root `package.json` to expose the site workflows.
- Create `docs/release/public-site/current.json` and immutable files below `docs/release/public-site/deployments/` and `docs/release/public-site/events/` only after Sites deployment succeeds and the public routes pass.

### Task 0: Record the company-controller authority gate

Use `@app-store-review:app-store-review`. This is an accountable user gate; no agent or script may infer it.

**Files:**
- Create: `docs/release/authority/publisher-controller.json`
- Modify: `scripts/check-macos-store.test.mjs`
- Modify: `scripts/check-macos-store.mjs`

- [ ] **Step 1: Ask for the exact attestation**

Ask the user to confirm, in one sentence, that they are authorised by Gamma Systems Pty Ltd to publish Tammy's privacy policy and use `ben.ebsworth@gmail.com` as its public support contact. Do not preview or publish Gamma-owned content until they confirm.

- [ ] **Step 2: Write the failing authority test**

Require this exact redacted authority-record shape, with a real confirmation timestamp supplied after the answer:

```json
{
  "schemaVersion": 1,
  "kind": "publisher-controller-authority",
  "company": "Gamma Systems Pty Ltd",
  "accountablePerson": "Ben Ebsworth",
  "controlsPrivacyPolicy": true,
  "controlsSupportAddress": true,
  "supportEmail": "ben.ebsworth@gmail.com",
  "confirmedAt": "<UTC RFC3339 from the confirmation>",
  "evidenceReference": "user-confirmation-in-task"
}
```

This is a version-independent authority record, not the common per-release attestation. Chunk 2 creates the compliant `company-controller` release attestation with version, build, outcome, and a reference to this authority record. Test rejection of missing confirmation, false controls, wrong company/email, invalid time, extra keys, and any key matching `/secret|token|password|credential/i`.

- [ ] **Step 3: Verify the authority test is RED**

Run: `rtk mise exec -- node --test scripts/check-macos-store.test.mjs`

Expected: FAIL with `MACOS_STORE_COMPANY_AUTHORITY_MISSING`.

- [ ] **Step 4: Record only the confirmed attestation and validator**

Create the JSON only after Step 1 succeeds. Add `validateCompanyControllerAttestation` and include a named `company-controller-attestation` blocker in repository output when absent or invalid.

- [ ] **Step 5: Run the authority test GREEN and commit**

```bash
rtk mise exec -- node --test scripts/check-macos-store.test.mjs
rtk git add docs/release/authority/publisher-controller.json scripts/check-macos-store.mjs scripts/check-macos-store.test.mjs
rtk git commit -m "docs: attest Tammy publisher authority"
```

### Task 1: Canonicalize Tammy's release identity

Use `@app-store-review:app-store-review` and `@superpowers:test-driven-development`.

**Files:**
- Create: `apps/desktop/release/macos/store-identity.json`
- Modify: `scripts/check-macos-store.mjs`
- Modify: `scripts/check-macos-store.test.mjs`

- [ ] **Step 1: Add failing identity validation tests**

Add tests that require one strict object and reject drift:

```js
const validStoreIdentity = {
  schemaVersion: 1,
  appStoreName: "Tammy Accounting",
  installedName: "Tammy",
  bundleIdentifier: "com.tammy.desktop",
  publisher: "Gamma Systems Pty Ltd",
  supportEmail: "ben.ebsworth@gmail.com",
  locale: "en-AU",
  primaryCategory: "Finance",
  secondaryCategory: "Business",
  minimumMacOSVersion: "14.0",
  architectures: ["arm64"],
  copyright: "© 2026 Gamma Systems Pty Ltd",
  capabilityBoundary: {
    reporting: "preparation-only",
    atoLodgement: "not-lodged",
  },
};

test("accepts the canonical Gamma Systems release identity", () => {
  assert.deepEqual(validateMacOSStoreIdentity(validStoreIdentity), validStoreIdentity);
});

for (const [key, value] of [
  ["publisher", "Ben Ebsworth"],
  ["supportEmail", "support@example.com"],
  ["minimumMacOSVersion", "13.0"],
  ["architectures", ["x64"]],
]) {
test(`rejects release identity drift in ${key}`, () => {
  assert.throws(
    () => validateMacOSStoreIdentity({ ...validStoreIdentity, [key]: value }),
    /MACOS_STORE_IDENTITY_INVALID/,
  );
});
}
```

- [ ] **Step 2: Run the focused test and verify RED**

Run: `rtk mise exec -- node --test scripts/check-macos-store.test.mjs`

Expected: FAIL because `validateMacOSStoreIdentity` and `store-identity.json` do not exist.

- [ ] **Step 3: Add the strict identity file and validator**

Create the JSON with the exact values above. Export `validateMacOSStoreIdentity(value)` from `check-macos-store.mjs`; require exact keys, exact canonical values, a single `arm64` architecture, and no additional properties. Read it from `inspectMacOSStoreRepository()` and cross-check:

```js
if (
  identity.installedName !== desktopPackage.productName ||
  identity.bundleIdentifier !== APP_BUNDLE_ID ||
  identity.minimumMacOSVersion !== "14.0" ||
  identity.architectures.join(",") !== "arm64"
) {
  fail("MACOS_STORE_IDENTITY_MISMATCH");
}
```

Return the validated identity in repository-check JSON without operator secrets.

- [ ] **Step 4: Run the identity and repository tests GREEN**

Run: `rtk mise exec -- node --test scripts/check-macos-store.test.mjs`

Expected: PASS with the new identity and drift cases.

- [ ] **Step 5: Run the non-signing repository check**

Run: `rtk mise exec -- pnpm check:macos-store`

Expected: JSON reports `NOT_READY` with named outstanding metadata/site/screenshot/state blockers, while the identity sub-check is valid and includes Gamma Systems Pty Ltd, macOS 14.0, arm64, and the preparation-only/not-lodged boundary. It must not report `REPOSITORY_READY` or print environment values until every requirement for that state exists.

- [ ] **Step 6: Commit the identity boundary**

```bash
rtk git add apps/desktop/release/macos/store-identity.json scripts/check-macos-store.mjs scripts/check-macos-store.test.mjs
rtk git commit -m "feat: canonicalize App Store identity"
```

### Task 2: Make the privacy policy a safe generated site input

Use `@app-store-review:app-store-review` and `@superpowers:test-driven-development`.

**Files:**
- Modify: `PRIVACY.md`
- Create: `scripts/generate-public-content.mjs`
- Create: `scripts/generate-public-content.test.mjs`
- Create: `apps/site/content/public-content.generated.ts` after the scaffold exists; until then write the test output to a temporary fixture path.

- [ ] **Step 1: Write failing parser/generator tests**

Test a deliberately small Markdown contract: one H1, effective-date paragraph, H2 sections, paragraphs, unordered lists, emphasis, code spans, and HTTPS/mailto links. Reject raw HTML, images, relative links, scripts, duplicate required headings, and unsupported syntax. Assert deterministic output shaped as:

```ts
export interface PolicySection {
  readonly heading: string;
  readonly blocks: readonly (
    | { readonly kind: "paragraph"; readonly inlines: readonly PolicyInline[] }
    | { readonly kind: "list"; readonly items: readonly (readonly PolicyInline[])[] }
  )[];
}

export const publicContent = {
  identity: {
    appStoreName: "Tammy Accounting",
    installedName: "Tammy",
    publisher: "Gamma Systems Pty Ltd",
    supportEmail: "ben.ebsworth@gmail.com",
    minimumMacOSVersion: "14.0",
    architectures: ["arm64"],
    capabilityBoundary: { reporting: "preparation-only", atoLodgement: "not-lodged" },
  },
  marketingVersion: "0.1.0",
  policy: { effectiveDate: "30 August 2026", sections: [/* parsed safe blocks */] },
} as const;
```

- [ ] **Step 2: Verify the generator test is RED**

Run: `rtk mise exec -- node --test scripts/generate-public-content.test.mjs`

Expected: FAIL because the generator module is absent.

- [ ] **Step 3: Rewrite `PRIVACY.md` to the approved three-boundary policy**

The canonical policy must state:

- publisher: Gamma Systems Pty Ltd;
- app boundary: accounting records, credentials, analytics, advertising identifiers, and tracking data are not transmitted by this release;
- site boundary: no Gamma-owned analytics, cookies, account, or form; infrastructure request/security logs may be processed by the hosting provider;
- support boundary: email is user initiated and processed by email providers;
- local retention: app removal alone does not promise workspace or Keychain deletion;
- deletion: link to `/support` for the tested macOS cleanup steps;
- warnings not to email accounting data, recovery codes, passwords, machine credentials, or keys.

Do not state that hosted pages generate no logs or that Gmail is part of the Tammy app.

- [ ] **Step 4: Implement the deterministic generator**

Export pure `parsePolicyMarkdown(source)` and `generatePublicContent({ identity, privacy, desktopPackage })` functions. The CLI reads `PRIVACY.md`, `store-identity.json`, and `apps/desktop/package.json`; it validates the semantic package version, emits a temporary file, compares bytes, and atomically renames the result. Use `JSON.stringify` for all generated string literals; never splice raw Markdown into TypeScript source.

The generated site input must include every public identity/platform/capability claim from `store-identity.json`; route components may not hard-code app names, publisher, email, macOS minimum, architecture, or the preparation-only/not-lodged boundary.

- [ ] **Step 5: Run generator tests GREEN**

Run: `rtk mise exec -- node --test scripts/generate-public-content.test.mjs`

Expected: PASS for deterministic generation and every unsafe-input rejection.

- [ ] **Step 6: Commit the canonical policy and generator**

```bash
rtk git add PRIVACY.md scripts/generate-public-content.mjs scripts/generate-public-content.test.mjs
rtk git commit -m "feat: define public privacy content"
```

### Task 3: Scaffold and build the first meaningful Tammy Sites slice

Use `@sites:sites-building`. The root agent remains the only Sites owner. Do not delegate scaffolding, source edits, Sites tool calls, credentials, preview handoff, or deployment.

**Files:**
- Create: `apps/site/**` using the pinned Sites scaffold
- Modify: `apps/site/app/layout.tsx`
- Modify: `apps/site/app/globals.css`
- Modify: `apps/site/app/page.tsx`
- Create: `apps/site/content/public-content.generated.ts`

- [ ] **Step 1: Confirm `apps/site` is absent or empty**

Run: `rtk proxy /bin/test ! -e apps/site`

Expected: PASS. If it exists, inspect it and follow the existing-site path; never initialize over files.

- [ ] **Step 2: Scaffold with the pinned Sites release and shadcn add-on**

Run from the repository root:

```bash
rtk pnpm create @openai/sites@0.3.0 apps/site --yes --add-ons shadcn --install
```

Expected: a Vinext/Sites project under `apps/site` with `.openai/hosting.json`, shadcn primitives, and installed workspace dependencies. If the repository's minimum-release-age policy rejects the pinned release, stop and report that blocker rather than changing the pin or bypassing policy.

- [ ] **Step 3: Inspect only the generated project contract**

Run:

```bash
rtk sed -n '1,220p' apps/site/AGENTS.md
rtk sed -n '1,220p' apps/site/package.json
rtk sed -n '1,220p' apps/site/app/page.tsx
rtk sed -n '1,220p' apps/site/app/layout.tsx
rtk sed -n '1,260p' apps/site/app/globals.css
rtk sed -n '1,160p' apps/site/.openai/hosting.json
```

Expected: enough information to preserve the scaffold's scripts, Vinext build, and Sites hosting contract.

- [ ] **Step 4: Generate the committed site content**

Run: `rtk mise exec -- node scripts/generate-public-content.mjs --write`

Expected: `apps/site/content/public-content.generated.ts` contains only safe structured data from the canonical identity and policy.

- [ ] **Step 5: Add the site test owner and a failing home-route test**

Add the repository-pinned Vitest/jsdom/Testing Library versions to `apps/site` and define `"test": "vitest run"`. Create `apps/site/tests/public-routes.test.tsx` with a failing home assertion before changing the starter:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import Home from "../app/page";

describe("Tammy public routes", () => {
  it("states the supported platform and preparation-only boundary", () => {
    render(<Home />);
    expect(screen.getByRole("heading", { name: "Local accounting for Australia" })).toBeTruthy();
    expect(screen.getByText(/macOS 14 or later.*Apple silicon/i)).toBeTruthy();
    expect(screen.getByText(/preparation-only.*not lodged/i)).toBeTruthy();
    expect(screen.getByText("Gamma Systems Pty Ltd")).toBeTruthy();
  });
});
```

Run: `rtk mise exec -- pnpm --dir apps/site test`

Expected: FAIL against the untouched starter page.

- [ ] **Step 6: Apply the Tammy theme and minimal home-page implementation**

Before styling components, replace generated theme tokens in `app/globals.css` with Tammy's warm off-white background, deep forest foreground/action, muted sage, restrained borders, modest radii, readable system sans body, and serif wordmark/display stack. Implement only:

- compact header with Tammy wordmark and Privacy/Support links;
- first viewport with “Local accounting for Australia”, accurate one-paragraph scope, macOS 14+/Apple silicon note, and visible Privacy/Support affordances;
- one “Your records stay on this Mac” trust strip;
- footer identity for Gamma Systems Pty Ltd.

All names/platform/boundary copy comes from `public-content.generated.ts`. Do not add gradients, glass, invented testimonials, wait-list forms, TestFlight copy, production SBR/ATO submission claims, or fake UI artwork.

- [ ] **Step 7: Run the home-route test GREEN**

Run: `rtk mise exec -- pnpm --dir apps/site test`

Expected: PASS for the real home route component.

- [ ] **Step 8: Start the retained development server**

Run: `rtk pnpm --dir apps/site dev`

Expected: retained session prints one Local URL and compiles the bounded home route.

- [ ] **Step 9: Satisfy the first meaningful preview gate**

Make one lightweight request to the exact Local URL and require HTTP 200. Then use `open_in_codex` once to open that URL and retain its returned browser-tab ID for the remainder of the Sites lifecycle. Do not inspect DOM, take screenshots, resize, click, or perform visual QA.

- [ ] **Step 10: Commit the scaffold and bounded slice**

```bash
rtk git add apps/site pnpm-lock.yaml pnpm-workspace.yaml
rtk git commit -m "feat: scaffold Tammy public site"
```

### Task 4: Define and test the macOS local-data removal boundary

Use `@app-store-review:app-store-review`, `@security-best-practices`, and `@superpowers:test-driven-development`.

**Files:**
- Create: `apps/desktop/release/macos/data-removal.json`
- Create: `scripts/macos-data-removal.mjs`
- Create: `scripts/macos-data-removal.test.mjs`
- Create: `scripts/macos-data-removal.integration.test.mjs`
- Modify: `scripts/generate-public-content.mjs`
- Modify: `scripts/generate-public-content.test.mjs`

- [ ] **Step 1: Write the failing isolated removal test**

Use a temporary fake home and injected in-memory Keychain adapter. Create:

- `Library/Containers/com.tammy.desktop/Data/Library/Application Support/Tammy`;
- `Library/Containers/com.example.sentinel`;
- `Library/Group Containers/TEAM123456.com.tammy.desktop`;
- `Library/Group Containers/TEAM123456.com.example.sentinel`;
- entries for Tammy services and an unrelated `com.example.sentinel` service.

The expected Tammy-owned service inventory is exactly:

```js
[
  "com.tammy.workspace",
  "com.tammy.attempt-journal-anchor.v1",
  "com.tammy.audit-mirror",
  "com.tammy.sbr.production",
]
```

Assert the removal owner deletes only the Tammy container, exact team-scoped Tammy group container, and entries in those services. Assert both filesystem and Keychain sentinels survive. Assert refusal of `/`, a real home path, non-absolute roots, invalid Team IDs, symlinked target containers, unknown services, and development simulator services.

- [ ] **Step 2: Verify the isolated test is RED**

Run: `rtk mise exec -- node --test scripts/macos-data-removal.test.mjs`

Expected: FAIL because the inventory/removal owner is absent.

- [ ] **Step 3: Implement the exact inventory and non-CLI removal owner**

`data-removal.json` owns the MAS container relative path, group-container suffix, and exact production Keychain services. `macos-data-removal.mjs` exports pure validation and an injected `removeTammyData({ isolatedHome, teamID, keychain })`; it has no executable CLI entry point, never expands `~`, never accepts a glob, and never invokes `rm` or `security` itself. Use `lstat`/real-path containment checks and `fs.rm` only after all exact targets validate inside the supplied isolated home.

- [ ] **Step 4: Generate public deletion guidance from the same inventory**

Extend `generate-public-content.mjs` so `/support` receives the exact container display path, group-container suffix, and service names as safe data. Tests prove production identifiers are present, development simulator identifiers are absent, and route code cannot invent additional paths/services.

- [ ] **Step 5: Write and run the real isolated macOS Keychain test RED**

On macOS 14+, create a temporary Keychain file below `mkdtemp` with `/usr/bin/security create-keychain`, unlock only that Keychain, and add generic-password entries for all four Tammy service names plus `com.example.sentinel`. Inject a Keychain adapter that passes the temporary Keychain path explicitly to every `security` invocation; never change the user's default Keychain/search list and never query or delete the login Keychain. Exercise the same inventory/removal owner, assert Tammy entries are absent, assert the sentinel remains readable, then delete only the temporary Keychain in `finally`.

Run: `rtk mise exec -- node --test scripts/macos-data-removal.integration.test.mjs`

Expected: FAIL until the temporary-Keychain adapter and inventory deletion are connected. Skip with the explicit reason `REQUIRES_MACOS_14_KEYCHAIN` only on non-Darwin or macOS below 14.

- [ ] **Step 6: Implement the temporary-Keychain adapter and run all removal tests GREEN**

Run:

```bash
rtk mise exec -- node --test scripts/macos-data-removal.test.mjs scripts/macos-data-removal.integration.test.mjs scripts/generate-public-content.test.mjs
```

Expected: PASS, including unrelated filesystem sentinels, in-memory sentinels, and the real temporary-Keychain sentinel.

- [ ] **Step 7: Commit the tested data-removal inventory**

```bash
rtk git add apps/desktop/release/macos/data-removal.json scripts/macos-data-removal.mjs scripts/macos-data-removal.test.mjs scripts/macos-data-removal.integration.test.mjs scripts/generate-public-content.mjs scripts/generate-public-content.test.mjs apps/site/content/public-content.generated.ts
rtk git commit -m "feat: define safe Tammy data removal"
```

### Task 5: Complete privacy/support routes and site validation

Use `@sites:sites-building`, `@superpowers:test-driven-development`, and the established browser tab from Task 3.

**Files:**
- Create: `apps/site/components/policy-document.tsx`
- Create: `apps/site/app/privacy/page.tsx`
- Create: `apps/site/app/support/page.tsx`
- Modify: `apps/site/app/page.tsx`
- Modify: `apps/site/app/layout.tsx`
- Modify: `apps/site/package.json`
- Create: `scripts/check-public-site.mjs`
- Create: `scripts/check-public-site.test.mjs`
- Create: `taskfiles/site.yml`
- Modify: `Taskfile.yml`
- Modify: `package.json`

- [ ] **Step 1: Extend the real route-component tests before implementation**

In `apps/site/tests/public-routes.test.tsx`, import the actual privacy and support page modules and assert:

```tsx
it("renders the canonical app/site/support privacy boundaries", () => {
  render(<PrivacyPage />);
  expect(screen.getByRole("heading", { name: "Privacy policy" })).toBeTruthy();
  expect(screen.getByText(/does not transmit your accounting records/i)).toBeTruthy();
  expect(screen.getByText(/hosting provider may process.*request.*security logs/i)).toBeTruthy();
  expect(screen.getByText(/email.*processed by.*email providers/i)).toBeTruthy();
});

it("renders only the canonical support and deletion inventory", () => {
  render(<SupportPage />);
  expect(
    screen.getByRole("link", { name: /ben\.ebsworth@gmail\.com/i }).getAttribute("href"),
  ).toBe("mailto:ben.ebsworth@gmail.com");
  expect(screen.getByText(/com\.tammy\.workspace/)).toBeTruthy();
  expect(screen.queryByText(/simulator-v2/)).toBeNull();
  expect(screen.getByText(/version 0\.1\.0/i)).toBeTruthy();
  expect(screen.getByText(/app deletion alone does not remove/i)).toBeTruthy();
});
```

Do not add jest-dom solely for attribute assertions.

- [ ] **Step 2: Run route tests and verify RED**

Run: `rtk mise exec -- pnpm --dir apps/site test`

Expected: FAIL because the privacy/support page modules do not exist.

- [ ] **Step 3: Implement the safe policy renderer and routes**

`PolicyDocument` renders only generated block/inline discriminated unions into React elements. It never uses `dangerouslySetInnerHTML`. `/privacy` renders every policy section with last-updated information and links to Support. `/support` includes:

- `mailto:ben.ebsworth@gmail.com`;
- app version, macOS version, observed error wording, and steps-to-reproduce guidance;
- a strong no-sensitive-data warning;
- exact manual deletion guidance for closing Tammy, deleting the chosen workspace/application data, and using Keychain Access to remove only Tammy-owned entries;
- a warning that deletion is irreversible and app deletion alone does not remove those records.

Keep deletion commands out of the page; give Finder/Keychain Access instructions to reduce destructive copy/paste risk.

- [ ] **Step 4: Run the actual route tests GREEN**

Run: `rtk mise exec -- pnpm --dir apps/site test`

Expected: PASS for `/`, `/privacy`, and `/support` components.

- [ ] **Step 5: Write failing public-site checker tests**

Inject `fetch` and test preview/deployed origins. Require exactly `/`, `/privacy`, and `/support`; HTTPS only for deployed mode; status 200 without an off-origin redirect; HTML content type; Tammy Accounting, Gamma Systems Pty Ltd, policy effective date, canonical navigation, `mailto:ben.ebsworth@gmail.com`, macOS 14/arm64, and preparation-only/not-lodged wording. Reject placeholders, TestFlight copy, and positive production-lodgement claims such as “lodges with the ATO” or “submit BAS to the ATO”; do not reject the required “not lodged” disclosure. Do not assert unspecified platform security headers.

- [ ] **Step 6: Verify checker tests are RED**

Run: `rtk mise exec -- node --test scripts/check-public-site.test.mjs`

Expected: FAIL because the checker is absent.

- [ ] **Step 7: Finish metadata, accessibility, checker, and Task scenarios**

Set site title/description, canonical route metadata, skip link, focus-visible treatment, AA contrast, 44px touch targets, semantic headings, and responsive widths. Implement `check-public-site.mjs` with injected `fetch`, bounded response bytes, same-origin redirect validation, explicit route expectations, and redacted JSON evidence.

Use the scaffold's real `preview` script for the built Vinext server. If the scaffold does not define one, add the framework-supported preview command discovered in Task 3; do not treat `dist/` as static HTML and do not invent a server command.

Expose:

Expose:

```yaml
tasks:
  dev:
    cmds: [mise exec -- pnpm --dir apps/site dev]
  test:
    cmds:
      - mise exec -- node --test scripts/generate-public-content.test.mjs scripts/check-public-site.test.mjs
      - mise exec -- pnpm --dir apps/site test
  build:
    cmds:
      - mise exec -- node scripts/generate-public-content.mjs --check
      - mise exec -- pnpm --dir apps/site build
  publish-check:
    cmds:
      - task: build
      - mise exec -- node scripts/check-public-site.mjs --built-preview apps/site
  post-deploy-check:
    requires:
      vars: [SITE_ORIGIN]
    cmds:
      - mise exec -- node scripts/check-public-site.mjs --origin {{.SITE_ORIGIN}} --write-evidence
  verify-deployed:
    requires:
      vars: [SITE_ORIGIN]
    cmds:
      - mise exec -- node scripts/check-public-site.mjs --origin {{.SITE_ORIGIN}} --read-only
```

`--built-preview apps/site` must spawn the scaffold's real preview script with `shell: false`, parse its single loopback Local URL with a bounded startup timeout, check all three routes, then terminate and reap the preview process on success or failure. `verify-deployed` repeats the three-route verification without writing or replacing evidence. Include this Taskfile as `site:` in the root `Taskfile.yml`. Add root package scripts only where Task delegates to an existing package owner; do not duplicate build logic.

- [ ] **Step 8: Add the Sites-required social preview after the first preview**

Because the new site has no existing social card, follow the Sites skill once: spawn exactly one image-only subagent with `fork_turns="none"`; require one image-generation request, output outside the Site checkout, and no Sites tools/skills/source edits/site initialization/delegation. The card must say exactly “Tammy Accounting” and “Local accounting for Australia”, use the approved warm off-white/forest palette, and contain no screenshot, credential, customer data, ATO logo, or unsupported feature claim. The root Site owner inspects the returned image and copies it to `apps/site/public/og.png`. Do not wire an invented absolute origin yet; Task 6 wires and revalidates Open Graph/X image metadata only after `create_site` returns the trusted canonical origin. Retry once only if text is incorrect.

- [ ] **Step 9: Run focused site tests and build GREEN**

Run:

```bash
rtk mise exec -- task site:test
rtk mise exec -- task site:build
rtk mise exec -- task site:publish-check
```

Expected: generated content is current; route and checker tests pass; deployment build emits `dist/server/index.js`, static assets, and staged hosting metadata; pre-publish structure check passes.

- [ ] **Step 10: Confirm HMR in the existing Sites tab**

Reuse the browser-tab ID established in Task 3. Do not open a second tab and do not perform browser visual QA.

- [ ] **Step 11: Commit the completed public site**

```bash
rtk git add apps/site scripts/generate-public-content.mjs scripts/generate-public-content.test.mjs scripts/check-public-site.mjs scripts/check-public-site.test.mjs taskfiles/site.yml Taskfile.yml package.json pnpm-lock.yaml PRIVACY.md
rtk git commit -m "feat: add public privacy and support site"
```

### Task 6: Publish, verify, and record the public Sites version

Use `@sites:sites-hosting`. The root agent remains the only Site owner. Do not delegate Sites tool calls, credentials, source staging, deployment, polling, browser handoff, or rollback.

**Files:**
- Modify: `apps/site/.openai/hosting.json` with the returned Sites `project_id` only
- Create: `docs/release/public-site/current.json`
- Create: `docs/release/public-site/deployments/<deployment-id>.json`
- Create on rollback only: `docs/release/public-site/events/<UTC>-rollback-to-<version-id>.json`
- Modify: `scripts/check-public-site.mjs`
- Modify: `scripts/check-public-site.test.mjs`
- Modify: `docs/release/macos-app-store.md`

- [ ] **Step 1: Write failing deployment-record and rollback-procedure tests**

Add strict validation for a non-secret record:

```json
{
  "schemaVersion": 1,
  "provider": "OpenAI Sites",
  "access": "public",
  "projectId": "<connector project id>",
  "versionId": "<immutable Sites version>",
  "deploymentId": "<successful deployment id>",
  "origin": "https://<public Sites host>",
  "deployedAt": "<UTC RFC3339>",
  "sourceCommit": "<40 lowercase hex>",
  "policySha256": "<64 lowercase hex>",
  "routes": [
    { "path": "/", "status": 200, "contentType": "text/html", "check": "passed" },
    { "path": "/privacy", "status": 200, "contentType": "text/html", "check": "passed" },
    { "path": "/support", "status": 200, "contentType": "text/html", "check": "passed" }
  ]
}
```

Reject credentials, tokens, source-write URLs, non-HTTPS origins, private/shared access, mismatched project IDs, mutable query/hash URLs, missing/duplicate routes, and any route evidence that failed. Test that the first successful deployment creates no rollback event. Test a rollback event factory only when a distinct prior passing deployment file exists; its immutable file contains the new deployment ID/version/time, `kind: "rollback"`, `fromVersionId`, `toVersionId`, exact prior deployment evidence path, and a new passing three-route result. It never deletes or mutates earlier files.

- [ ] **Step 2: Run deployment-record tests RED**

Run: `rtk mise exec -- node --test scripts/check-public-site.test.mjs`

Expected: FAIL because deployment-record validation is absent.

- [ ] **Step 3: Implement record validation and document rollback**

Export `validatePublicSiteDeployment`, `writeCurrentPublicSitePointer`, and `createRollbackEvent` from `check-public-site.mjs`. Deployment evidence and rollback events use exclusive creation (`flag: "wx"`); `current.json` is an atomic pointer to the latest passing deployment file. Add the exact operator procedure to `docs/release/macos-app-store.md`: when at least one distinct prior passing version exists, select it, deploy intentionally, poll to success, rerun `site:post-deploy-check`, add one immutable rollback event, atomically update `current.json`, and leave earlier files intact.

- [ ] **Step 4: Run the exact pre-publish checks**

Run:

```bash
rtk mise exec -- task site:test
rtk mise exec -- task site:publish-check
```

Expected: the final source/build is valid, the dev server remains alive, and no deployment has occurred yet.

- [ ] **Step 5: Create the public Site and persist only its project ID**

Call the Sites connector's `create_site` once. The user explicitly approved a public Sites URL in this task; preserve that approval in the deployment action. Add only the returned `project_id` to `.openai/hosting.json`. Keep the source credential in memory and out of Git, logs, shell history, URLs, and configuration.

- [ ] **Step 6: Wire the trusted origin and rebuild the final source**

Use the canonical public origin returned for the created Site to set absolute Open Graph/X image metadata for `public/og.png`. Because `.openai/hosting.json` and metadata changed after the earlier build, rerun the complete final gate:

```bash
rtk mise exec -- task site:test
rtk mise exec -- task site:publish-check
```

Expected: the exact project-linked source and rebuilt `dist/` pass route, metadata, generated-content, and hosting checks. Do not package or deploy the pre-`create_site` build.

- [ ] **Step 7: Save the exact final validated source as one Sites version**

Prepare a temporary Git repository containing only the final `apps/site` source snapshot required by Sites; do not push the Tammy parent repository or create a nested `.git` inside `apps/site`. Commit the exact validated snapshot, push with the connector credential as a one-command authorization header, package the rebuilt output with the plugin's `scripts/package-site.sh`, and save one version using that source commit SHA and archive. Remove the temporary source repository after hosting completes.

- [ ] **Step 8: Deploy publicly and wait for a terminal result**

Use `deploy_site_version` for the approved public access. Poll `get_deployment_status` directly until `succeeded` or `failed`; do not rediscover the site or create duplicate versions while polling. On failure, leave store metadata/candidate URLs unchanged and report the user-visible blocker.

- [ ] **Step 9: Verify the deployed routes and record immutable evidence**

Run: `rtk mise exec -- task site:post-deploy-check SITE_ORIGIN=<exact returned origin>`

Expected: all three HTTPS routes pass the exact content/identity/boundary checks. Exclusively create `docs/release/public-site/deployments/<deployment-id>.json` from connector identifiers plus the three route results, then atomically create/update `docs/release/public-site/current.json` to reference it. Include no credential or account-user ID. Do not create a rollback event for this first deployment.

- [ ] **Step 10: Reuse the established browser tab for the deployed URL**

Use `open_in_codex` with the Task 3 browser-tab ID and exact deployed origin. Do not open a second Site tab or perform visual QA.

- [ ] **Step 11: Run tests GREEN and commit the public deployment record**

```bash
rtk mise exec -- node --test scripts/check-public-site.test.mjs
rtk mise exec -- task site:post-deploy-check SITE_ORIGIN=<exact returned origin>
rtk git add apps/site/.openai/hosting.json apps/site/app/layout.tsx docs/release/public-site docs/release/macos-app-store.md scripts/check-public-site.mjs scripts/check-public-site.test.mjs
rtk git commit -m "docs: record public Tammy site"
```

Expected: a public origin and immutable Sites version are recorded; rollback logic is tested without pretending a prior first-deployment version exists.

## Chunk 2: Release states, seller eligibility, metadata, and build provenance

### File structure for this chunk

- Create `apps/desktop/release/macos/release-state.schema.json` as documentation for the fixed state/attestation contract.
- Create `scripts/macos-release-state.mjs` and `scripts/macos-release-state.test.mjs` as the executable state-machine validator.
- Create `apps/desktop/release/macos/build-numbers.json` as the monotonic two-phase build ledger.
- Create `scripts/reserve-macos-build.mjs` and `scripts/reserve-macos-build.test.mjs` as the only ledger writer.
- Create `scripts/macos-release-provenance.mjs` and `scripts/macos-release-provenance.test.mjs` for product-source/tree binding.
- Create `docs/release/records/macos/0.1.0/build-<N>/attestations/*.example.json` as non-passing templates; create real attestations only after accountable confirmation.
- Modify `apps/desktop/release/macos/store-metadata.md` with Gamma/public-Sites/final truthful copy.
- Modify `scripts/check-macos-store.mjs` and `scripts/check-macos-store.test.mjs` to consume identity, public-site, ledger, state, and metadata owners.
- Modify `apps/desktop/release/macos/profile.ts`, `apps/desktop/src/main/release-profile.test.ts`, and `apps/desktop/forge.config.ts` for canonical platform/copyright/package facts.
- Modify `taskfiles/release.yml`, `Taskfile.yml`, `scripts/check-taskfiles.test.mjs`, and `docs/release/macos-app-store.md` for scenario-oriented state commands.

### Task 7: Implement the redacted release-state and attestation contract

Use `@app-store-review:app-store-review`, `@security-best-practices`, and `@superpowers:test-driven-development`.

**Files:**
- Create: `apps/desktop/release/macos/release-state.schema.json`
- Create: `scripts/macos-release-state.mjs`
- Create: `scripts/macos-release-state.test.mjs`
- Create: `docs/release/records/macos/0.1.0/README.md`
- Create: `docs/release/records/macos/0.1.0/attestation-templates/*.example.json`

- [ ] **Step 1: Write failing state-transition tests**

Define the only readiness states and monotonic transitions:

```js
const RELEASE_STATES = [
  "NOT_READY",
  "REPOSITORY_READY",
  "CANDIDATE_READY",
  "PRE_UPLOAD_READY",
  "UPLOADED",
  "PRE_SUBMIT_READY",
];
```

Test:

- `REPOSITORY_READY` requires final public deployment, metadata, platform identity, policy, schemas, and tests but no signing credential;
- `CANDIDATE_READY` requires exact candidate/build/privacy/screenshot evidence;
- `PRE_UPLOAD_READY` requires all pre-upload attestations but no App Store build ID;
- `UPLOADED` requires a non-secret App Store build ID;
- `PRE_SUBMIT_READY` requires processed-build selection and declaration/assets attestations, never submission/approval;
- `uploaded`, `expired`, `submitted`, `approved`, `rejected`, and `superseded` are immutable lifecycle events, not readiness states;
- `UPLOADED` is derived only from a valid `uploaded` event carrying the exact package hash and App Store Connect build ID;
- `approved`/`rejected` events require an earlier `submitted` event;
- `expired` and `superseded` may consume a reserved/candidate/uploaded build before submission;
- no transition skips prerequisites or moves backward.

- [ ] **Step 2: Write failing common attestation tests**

Require exact keys:

```json
{
  "schemaVersion": 1,
  "kind": "company-controller",
  "releaseVersion": "0.1.0",
  "buildNumber": "<reserved decimal>",
  "accountablePerson": "Ben Ebsworth",
  "confirmedAt": "<UTC RFC3339>",
  "evidenceReference": "../../../../../authority/publisher-controller.json",
  "outcome": "confirmed"
}
```

Allow only fixed attestation `kind` values: `company-controller`, `seller-eligibility`, `content-rights`, `export-compliance`, `pricing-availability`, `privacy-answer`, `age-rating`, `processed-build`, `metadata-assets-entered`, and `app-store-warning-review`. Require version/build on every real attestation. Each kind has an exact allowed outcome: `confirmed`, `eligible`, `owned`, `exempt|non-exempt`, `confirmed`, `no-data-collected-no-tracking`, `completed`, `selected`, `entered`, and `clear|resolved`, respectively. Reject a generic outcome on the wrong kind, unknown/extra keys, free-form blobs, absolute paths, URLs with credentials, and any key matching `/secret|token|password|credential|privateKey/i`.

Lifecycle events are separate exact schemas stored with exclusive creation under `docs/release/records/macos/<version>/build-<N>/events/<UTC>-<kind>.json`:

- `uploaded`: version/build, operator, UTC time, product-source commit/tree, package SHA-256, and App Store Connect build ID;
- `expired`: version/build, operator, UTC time, package/source reference when one exists, and reason enum `certificate-expired|profile-expired|candidate-timeout`;
- `superseded`: version/build, operator, UTC time, and replacement version/build when known;
- `submitted`: version/build, operator, UTC time, and App Store submission reference;
- `approved|rejected`: version/build, operator, UTC time, review reference, and required earlier submitted-event path.

All events reject extra/secret-bearing fields. Ledger consumption is derived from reservations plus `uploaded`, `expired`, `superseded`, and later review events; no event mutates the reservation entry.

`seller-eligibility` is a strict tagged union with two branches. Both bind `teamId`, `sellerName`, `accountHolder`, `activeAgreements: true`, `appId: "com.tammy.desktop"`, `appleDeveloperIdentifierId`, `appStoreConnectId`, `applicationGroup`, exact helper identifiers, required certificate classes, and `profilesReissued: true` to the release. The `company-organization` branch additionally requires Apple's verified seller name `Gamma Systems Pty Ltd`; it does not infer membership type from a historical Team ID because Apple may preserve identifiers when updating an existing membership. The `written-apple-exception` branch requires the current individual seller/team plus a non-secret reference to Apple's written approval for this exact app and accounting scope. A general company authorization cannot satisfy either branch.

- [ ] **Step 3: Run state tests RED**

Run: `rtk mise exec -- node --test scripts/macos-release-state.test.mjs`

Expected: FAIL because the state owner does not exist.

- [ ] **Step 4: Implement pure validators and blocker reporting**

Export `validateReleaseState`, `validateReleaseAttestation`, and `evaluateReleaseState(inputs)`. `evaluateReleaseState` returns:

```js
{
  state: "NOT_READY",
  passed: ["store-identity", "publisher-authority"],
  blockers: [
    { code: "PUBLIC_SITE_NOT_RECORDED", owner: "repository", remediation: "Publish and verify the Sites version." },
  ],
}
```

Sort blocker codes, never include environment values, and fail validation if the same requirement appears in both `passed` and `blockers`.

- [ ] **Step 5: Add non-passing templates and release-record guidance**

Templates use `OPERATOR_REQUIRED` and `.example.json`, are never loaded as attestations, and explain which Apple screen/document supplies each non-secret evidence reference. The README distinguishes templates from accountable facts and states that agents cannot self-attest.

- [ ] **Step 6: Run focused tests GREEN**

Run: `rtk mise exec -- node --test scripts/macos-release-state.test.mjs`

Expected: PASS for allowed transitions, circularity rejection, redaction, and templates excluded from evaluation.

- [ ] **Step 7: Commit the release-state contract**

```bash
rtk git add apps/desktop/release/macos/release-state.schema.json scripts/macos-release-state.mjs scripts/macos-release-state.test.mjs docs/release/records/macos/0.1.0
rtk git commit -m "feat: define App Store release states"
```

### Task 8: Add a non-self-referential build-number ledger

Use `@superpowers:test-driven-development`.

**Files:**
- Create: `apps/desktop/release/macos/build-numbers.json`
- Create: `scripts/reserve-macos-build.mjs`
- Create: `scripts/reserve-macos-build.test.mjs`
- Create: `scripts/macos-release-provenance.mjs`
- Create: `scripts/macos-release-provenance.test.mjs`
- Modify: `package.json`

- [ ] **Step 1: Write failing ledger tests**

Use this strict structure:

```json
{
  "schemaVersion": 1,
  "entries": [
    {
      "buildNumber": "1",
      "marketingVersion": "0.1.0",
      "reservedAt": "2026-08-30T00:00:00.000Z",
      "reservedBy": "Ben Ebsworth",
      "state": "reserved"
    }
  ]
}
```

The reservation contains no commit SHA and its `state` remains permanently `reserved`. Test positive decimal strings, strict monotonic order, no reuse across versions, fixed entry keys, and valid UTC times. Uploaded/rejected/superseded consumption is derived from immutable release event files and never mutates the ledger entry. Test an exclusive lock plus temporary-write/fsync/rename update; parallel reservation attempts must yield distinct numbers or one explicit conflict, never duplicate or corrupt JSON.

- [ ] **Step 2: Write failing provenance tests**

`readProductSource(root)` must obtain the clean `HEAD` commit and tree with `git rev-parse HEAD` and `git rev-parse HEAD^{tree}`. Test that the reserved ledger entry is already committed, the tree is clean, the build number/version match, and the produced phase-two event contains:

```json
{
  "kind": "candidate-built",
  "buildNumber": "1",
  "marketingVersion": "0.1.0",
  "productSourceCommit": "<40 hex>",
  "productSourceTree": "<40 hex>",
  "unsignedContentManifestSha256": "<64 hex>",
  "appSha256": "<64 hex>",
  "packageSha256": "<64 hex>"
}
```

No phase-two fact is written into the phase-one reservation or product-source commit.

- [ ] **Step 3: Run ledger/provenance tests RED**

Run:

```bash
rtk mise exec -- node --test scripts/reserve-macos-build.test.mjs scripts/macos-release-provenance.test.mjs
```

Expected: FAIL because both owners are absent.

- [ ] **Step 4: Implement validation and atomic reservation**

The CLI accepts only `--version <semver> --operator <non-empty name> --number <positive decimal>`. It does not contact Apple, auto-guess the remote latest number, or accept a secret. It refuses a number not greater than every consumed/reserved entry. `--check` is read-only. The product-source reader rejects dirty trees and verifies `apps/desktop/package.json` version.

- [ ] **Step 5: Commit the ledger/provenance owners without reserving a build**

```bash
rtk mise exec -- node --test scripts/reserve-macos-build.test.mjs scripts/macos-release-provenance.test.mjs
rtk git add apps/desktop/release/macos/build-numbers.json scripts/reserve-macos-build.mjs scripts/reserve-macos-build.test.mjs scripts/macos-release-provenance.mjs scripts/macos-release-provenance.test.mjs package.json
rtk git commit -m "feat: add App Store build provenance"
```

Expected: the ledger may be empty and valid. Do not reserve `<N>` until Task 16, after every candidate-affecting metadata/profile/tooling change is committed.

### Task 9: Finalize truthful metadata and repository readiness

Use `@app-store-review:app-store-review` and `@superpowers:test-driven-development`.

**Files:**
- Modify: `apps/desktop/release/macos/store-metadata.md`
- Modify: `PRIVACY.md`
- Modify: `scripts/check-macos-store.mjs`
- Modify: `scripts/check-macos-store.test.mjs`
- Modify: `docs/release/macos-app-store.md`

- [ ] **Step 1: Write failing canonical metadata tests**

Assert the worksheet uses:

- the exact `/privacy` and `/support` URLs from `docs/release/public-site/current.json`;
- Gamma Systems Pty Ltd and `© 2026 Gamma Systems Pty Ltd`;
- `ben.ebsworth@gmail.com` as review support;
- `0.1.0`, macOS 14+, Apple silicon, Finance/Business, Free/Australia, en-AU;
- encrypted local workspace, organisation/chart, journals/trial balance, document review, bank-statement reconciliation, GST/BAS drafts, and local activity;
- explicit no cloud account, ads, analytics, tracking, IAP, or ATO lodgement;
- `BAS draft — not lodged` reviewer/screenshot wording;
- no TestFlight invitation, production SBR, company-return submission, or completed Apple declaration claim.

Reject GitHub privacy/support URLs, stale individual-publisher copy, `OPERATOR_REQUIRED` in repository-owned fields, and unchecked/completed Apple-owned fields represented as facts.

- [ ] **Step 2: Run metadata tests RED**

Run: `rtk mise exec -- node --test scripts/check-macos-store.test.mjs`

Expected: FAIL against the current Ben/GitHub worksheet.

- [ ] **Step 3: Rewrite the worksheet and runbook from canonical owners**

Keep `store-metadata.md` human-readable but make `check-macos-store.mjs` cross-check it against `store-identity.json`, `public-site/current.json`, desktop package version, policy hash, and state blockers. Mark Apple seller/agreement, export, privacy, age-rating, pricing, processed-build, and warning review as `OPERATOR_CONFIRMATION_REQUIRED`, not completed checkboxes.

- [ ] **Step 4: Run metadata/repository tests GREEN without overstating readiness**

Run:

```bash
rtk mise exec -- node --test scripts/check-macos-store.test.mjs scripts/macos-release-state.test.mjs
rtk mise exec -- pnpm check:macos-store
```

Expected: metadata/public policy/identity checks pass. State is `REPOSITORY_READY` only if every repository requirement now exists; otherwise `NOT_READY` contains only named later-chunk or Apple-owned blockers.

- [ ] **Step 5: Commit candidate-affecting metadata**

```bash
rtk git add apps/desktop/release/macos/store-metadata.md PRIVACY.md scripts/check-macos-store.mjs scripts/check-macos-store.test.mjs docs/release/macos-app-store.md
rtk git commit -m "docs: finalize Tammy App Store metadata"
```

### Task 10: Bind platform facts and legal seller eligibility into MAS packaging

Use `@app-store-review:app-store-review`, `@build-macos-swift-apps` only for macOS signing/platform review (Tammy remains Electron), and `@superpowers:test-driven-development`.

**Files:**
- Modify: `apps/desktop/release/macos/profile.ts`
- Modify: `apps/desktop/src/main/release-profile.test.ts`
- Modify: `apps/desktop/forge.config.ts`
- Modify: `scripts/check-macos-store.mjs`
- Modify: `scripts/check-macos-store.test.mjs`
- Modify: `taskfiles/release.yml`
- Modify: `Taskfile.yml`
- Modify: `scripts/check-taskfiles.test.mjs`
- Modify: `docs/release/macos-app-store.md`

- [ ] **Step 1: Write failing release-profile tests**

Require the MAS `extendInfo` to include:

```ts
{
  CFBundleDisplayName: "Tammy",
  ITSAppUsesNonExemptEncryption: false, // only for the currently recorded exempt branch
  LSMinimumSystemVersion: "14.0",
  NSHumanReadableCopyright: "© 2026 Gamma Systems Pty Ltd",
}
```

Cross-check identity JSON. Reject a package version other than `0.1.0`, non-arm64 MAS target, public URLs not equal to the deployed Sites record, and missing build reservation. For distribution only, reject a Team ID not covered by a passing tagged-union `seller-eligibility` attestation using either the Gamma organization or written-Apple-exception branch.

- [ ] **Step 2: Add failing Taskfile contract tests**

Require public scenarios:

- `release:state` — read-only redacted state/blockers;
- `release:reserve-build` — explicit version/operator/number, no guessing;
- `release:check` — repository-only;
- `release:development` — development signing, no upload;
- `release:candidate` — distribution candidate, no upload;
- `release:pre-upload-check` and `release:pre-submit-check` — state gates only.

Reject secret values/arguments, automatic upload, implicit Sites deployment, re-used build numbers, and aliases that claim submission.

- [ ] **Step 3: Run focused tests RED**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- release-profile.test.ts
rtk mise exec -- node --test scripts/check-macos-store.test.mjs scripts/check-taskfiles.test.mjs
```

Expected: FAIL because platform/legal state is not embedded or required.

- [ ] **Step 4: Implement the minimal profile and gates**

Read canonical identity/deployment/ledger/state in the repository checker; keep Forge consuming a validated release-profile object rather than parsing docs. `validateMacOSReleaseEnvironment` still validates identities/profiles and the exact build reservation. Seller eligibility is required only when `TAMMY_MACOS_SIGNING_MODE=distribution` and for pre-upload/pre-submit gates. Development signing may build local sandbox evidence while seller migration is pending, but development evidence alone cannot reach `CANDIDATE_READY`; that state requires a passing distribution app/package and candidate evidence.

- [ ] **Step 5: Implement Task scenarios with direct delegation**

Add no new build implementation to YAML. Delegate to the Node owners with exact inputs, preserve the current macOS arm64 preconditions, and state in every summary whether it signs, uploads, publishes, or submits.

- [ ] **Step 6: Run profile, Taskfile, and repository tests GREEN**

Run:

```bash
rtk mise exec -- pnpm --dir apps/desktop test -- release-profile.test.ts
rtk mise exec -- node --test scripts/check-macos-store.test.mjs scripts/check-taskfiles.test.mjs scripts/macos-release-state.test.mjs scripts/reserve-macos-build.test.mjs scripts/macos-release-provenance.test.mjs
rtk mise exec -- task release:check
rtk mise exec -- task release:state
```

Expected: tests pass; repository state is truthful; Apple seller/profile blockers remain named if the company organization migration/exception is not evidenced.

- [ ] **Step 7: Commit platform and release-state wiring**

```bash
rtk git add apps/desktop/release/macos/profile.ts apps/desktop/src/main/release-profile.test.ts apps/desktop/forge.config.ts scripts/check-macos-store.mjs scripts/check-macos-store.test.mjs taskfiles/release.yml Taskfile.yml scripts/check-taskfiles.test.mjs docs/release/macos-app-store.md
rtk git commit -m "feat: gate App Store candidate readiness"
```

---

## Chunk 3: Real screenshots and candidate-bound release evidence

### File structure for this chunk

- Create `scripts/macos-unsigned-content.mjs` and `scripts/macos-unsigned-content.test.mjs` for the authenticated, mode-independent staging manifest and signed-copy equivalence.
- Modify `scripts/package-macos-store.mjs` and `scripts/package-macos-store.test.mjs` so development and distribution artifacts originate from separate copies of one frozen unsigned staging result.
- Modify `apps/desktop/forge.config.ts`, `apps/desktop/release/macos/profile.ts`, the release checker, and the SBR profile owner as needed to expose an unsigned staging mode without weakening ordinary or MAS signing. Pin direct plist/code-signing dependencies at the workspace root.
- Create `apps/desktop/release/macos/screenshots/fixture.json` as the canonical fictional screenshot fixture and provenance owner.
- Create `scripts/check-app-store-screenshots.mjs` and `scripts/check-app-store-screenshots.test.mjs` for fixture, PNG, manifest, sensitive-data, and packaged-content checks.
- Create `apps/desktop/tests/e2e/app-store-screenshots.spec.ts` and `apps/desktop/playwright.app-store-screenshots.config.ts` for the five real packaged-renderer captures.
- Modify `apps/desktop/tests/e2e/support/current-accounting-workflow.ts` only to extract reusable deterministic setup helpers; do not change ordinary workflow behaviour.
- Create `apps/desktop/release/macos/screenshots/en-AU/manifest.json` and the five canonical PNGs only from a passing capture.
- Create `scripts/macos-runtime-egress.mjs` and `scripts/macos-runtime-egress.test.mjs` for authenticated process/socket observation and explicit external-handoff evidence.
- Create `apps/desktop/tests/e2e/app-store-privacy.spec.ts` and `apps/desktop/playwright.app-store-privacy.config.ts` for the packaged development-equivalent privacy journey.
- Create `scripts/collect-macos-privacy-evidence.mjs` and `scripts/collect-macos-privacy-evidence.test.mjs` for exact-artifact manifests and dependency/native/privacy inventories.
- Create `scripts/promote-macos-release-evidence.mjs` and `scripts/promote-macos-release-evidence.test.mjs` for atomic, redacted promotion into the durable release record.
- Modify `scripts/macos-release-state.mjs`, `scripts/macos-release-state.test.mjs`, `scripts/check-macos-store.mjs`, and `scripts/check-macos-store.test.mjs` to consume the exact evidence set.
- Modify `taskfiles/release.yml`, `scripts/check-taskfiles.test.mjs`, and `docs/release/macos-app-store.md` with screenshot, evidence, and candidate scenarios.
- Create candidate-specific files below `.tmp/macos-release/0.1.0/build-<N>/` first; promote only passing non-secret evidence below `docs/release/records/macos/0.1.0/build-<N>/`.

### Task 11: Implement one authenticated unsigned staging result for both signing modes

Use `@superpowers:test-driven-development` and `@app-store-review:app-store-review`.

**Files:**
- Create: `scripts/macos-unsigned-content.mjs`
- Create: `scripts/macos-unsigned-content.test.mjs`
- Modify: `scripts/package-macos-store.mjs`
- Modify: `scripts/package-macos-store.test.mjs`
- Modify: `apps/desktop/forge.config.ts`
- Modify: `apps/desktop/release/macos/profile.ts`
- Modify: `apps/desktop/src/main/release-profile.test.ts`
- Modify: `scripts/build-sbr-helper.mjs`
- Modify: `scripts/build-sbr-helper.test.mjs`
- Modify: `scripts/check-macos-store.mjs`
- Modify: `scripts/check-macos-store.test.mjs`
- Modify: `package.json`
- Modify: `pnpm-lock.yaml`
- Modify: `.gitignore`
- Modify: `docs/superpowers/plans/2026-08-30-macos-app-store-prime-time-readiness.md`

- [x] **Step 1: Write failing canonical-manifest tests**

Define a strict manifest with schema version, marketing/build version, product-source commit/tree, public URLs, bundle identifiers, staging directory hash, and sorted entries containing relative POSIX path, file kind, byte size, executable bit, and SHA-256. Preserve safe relative symlinks used inside Electron frameworks and record their link targets; reject absolute, escaping, broken, cyclic, or changed symlinks, traversal paths, devices, sockets, duplicate or unsorted paths, timestamps, ownership fields, unknown keys, files outside the staging root, and any source identity other than the reserved clean product source.

The manifest must include every runtime-relevant file before signing, including `app.asar`, native modules, core/helper unsigned inputs, privacy manifest, icons, generated build/profile inputs, and `Info.plist`. Hashing is stable against filesystem timestamps and user/group ownership.

- [x] **Step 2: Write failing equivalence tests**

Test `compareSignedCopies({ developmentApp, distributionApp, unsignedManifest })` using synthetic bundles. It must compare:

- every unsigned-manifest input and the corresponding packaged resource;
- application JavaScript/ASAR bytes and entry inventory;
- native modules and core/helper **unsigned input** hashes;
- bundle/helper identifiers, marketing/build versions, public URLs, privacy manifest, and entitlement intent;
- generated signed core/helper manifest semantics after normalising only their mode-specific code-signature digest fields.

Permit only `_CodeSignature/**`, code-directory bytes reported by `codesign`, the embedded provisioning profile, certificate/signature metadata, and the enumerated development-versus-distribution entitlement values. Reject any added/removed resource, changed application code, native/core/helper input, URL, identifier, version, privacy file, entitlement key, or unlisted manifest difference. Do not use a broad path or byte-pattern exclusion.

- [x] **Step 3: Verify the staging/equivalence tests are RED**

Run:

```bash
rtk mise exec -- node --test scripts/macos-unsigned-content.test.mjs scripts/package-macos-store.test.mjs
```

Expected: FAIL because staging and equivalence ownership do not exist.

- [x] **Step 4: Add an explicit unsigned Forge staging mode**

Add one internal `TAMMY_MACOS_ARTIFACT_PHASE=unsigned-staging` branch used only by `package-macos-store.mjs`. It packages the MAS layout with signing disabled, writes only below `.tmp/macos-release/<version>/build-<N>/unsigned/`, and, when executed after the freeze, is rejected unless the source commit/tree and reserved build match Task 16. It must never become a public Taskfile scenario or a distributable artifact.

Keep the current ordinary-package and signed MAS branches unchanged. The unsigned branch still applies the release profile's bundle IDs, versions, public URLs, privacy manifest, resource permission normalization, fuses, and arm64 target. It must not accept a provisioning profile, signing identity, or installer identity.

- [x] **Step 5: Refactor the packaging owner into stage, clone, sign, and verify phases**

The first invocation builds core, helper, SQLCipher inputs, renderer/main/preload output, and one unsigned MAS staging app; records its canonical manifest; then makes separate copies for `development` and `distribution` that preserve and revalidate only safe in-root framework symlinks. Each copy receives only its required core/helper/app signing, generated authenticated signed-byte manifests, entitlements, and provisioning profile. Distribution additionally runs `productbuild`.

Authenticate the unsigned manifest before and after every clone. Refuse to reuse a staging directory whose source commit/tree, version/build, public URLs, or manifest hash differ. Use an exclusive build lock, temporary directories, fsync/rename for the final manifest, and preserve the last passing artifacts when a later phase fails.

- [x] **Step 6: Verify unit-level staging and command ordering GREEN**

Run:

```bash
rtk mise exec -- node --test scripts/macos-unsigned-content.test.mjs scripts/package-macos-store.test.mjs
rtk mise exec -- pnpm --dir apps/desktop exec vitest run src/main/release-profile.test.ts --maxWorkers=1
```

Expected: PASS; tests prove one stage feeds both copies, distribution cannot reuse development bytes, and signing never changes a mode-independent input.

- [x] **Step 7: Commit the shared-payload pipeline**

```bash
rtk git add .gitignore package.json pnpm-lock.yaml apps/desktop/forge.config.ts apps/desktop/release/macos/profile.ts apps/desktop/src/main/release-profile.test.ts scripts/build-sbr-helper.mjs scripts/build-sbr-helper.test.mjs scripts/check-macos-store.mjs scripts/check-macos-store.test.mjs scripts/macos-unsigned-content.mjs scripts/macos-unsigned-content.test.mjs scripts/package-macos-store.mjs scripts/package-macos-store.test.mjs docs/superpowers/plans/2026-08-30-macos-app-store-prime-time-readiness.md
rtk git commit -m "feat: bind macOS signing modes to one payload"
```

This is a candidate-affecting tooling commit and must land before Task 16 freezes the product source. Do not build a release artifact yet; the staging script will later target the Task 16 commit/tree and fail if candidate-affecting source differs.

### Task 12: Define the fictional screenshot fixture and crash-recoverable image validator

Use `@superpowers:test-driven-development` and `@app-store-review:app-store-review`.

**Files:**
- Create: `apps/desktop/release/macos/screenshots/fixture.json`
- Create: `scripts/check-app-store-screenshots.mjs`
- Create: `scripts/check-app-store-screenshots.d.mts`
- Create: `scripts/check-app-store-screenshots.test.mjs`
- Modify: `apps/desktop/tests/e2e/support/current-accounting-workflow.ts`
- Modify: `apps/desktop/release/macos/store-metadata.md`

- [x] **Step 1: Write failing strict fixture tests**

Require a versioned fixture named `Wattle & Co Supplies Pty Ltd` with an explicit `fictional: true` marker, fixed 2024 Q4 Australian dates, deterministic UUIDs, accounts, journal lines, source-document fields, bank lines, reconciliation state, BAS draft, and expected captions. Remove the current `Wattle & Co Test Pty Ltd` duplication by making the reusable workflow consume this file.

Identifiers displayed in screenshots must either cite a committed official ATO test-only source URL and retrieval date or be omitted from the capture. The validator must reject:

- the publisher/controller/support names or email inside accounting records;
- fixture values matching repository developer/customer allow-deny lists;
- real credential, TFN, production machine-credential, private-key, recovery-code, access-token, or common secret formats;
- unapproved ABNs, BSB/account numbers, invoice references, or person names;
- mutable current dates, random IDs, or fixture fields not consumed by the scenario.

- [x] **Step 2: Write failing PNG and manifest tests**

Require exactly these files and order, plus one complete visible-text/accessibility snapshot per image:

```text
01-overview.png
02-document-review.png
03-journal-trial-balance.png
04-bank-reconciliation.png
05-bas-draft.png
```

Each must be a valid non-interlaced PNG at exactly 1440×900, RGB without alpha, and have a unique SHA-256. The strict manifest contains locale `en-AU`, dimensions, fixture path/hash/provenance, immutable product-source commit/tree, version/build, unsigned-content-manifest hash, development-signed app hash, `capture_artifact_kind: "development-signed-app"`, UTC capture time, ordered filename/hash/caption/accessibility-snapshot hashes, and nullable distribution package hash before finalization. Reject extra images, hidden files, stale hashes, caption drift from store metadata, unknown keys, package linkage without a candidate event, and any claim of Apple acceptance. Apply the same strict allowlist and secret/PII scan to the post-render accessibility text; fixture-input validation alone is insufficient.

Also specify failing promotion/recovery tests for a crash before and after each backup/staging rename and parent fsync. Require deterministic recovery to the last complete validated set. Reject an unvalidated existing canonical directory or backup, including unexpected files or symlinks, before either may be renamed, removed, or replaced; recovery must never delete unknown user-owned content.

- [x] **Step 3: Verify fixture/image tests are RED**

Run: `rtk mise exec -- node --test scripts/check-app-store-screenshots.test.mjs`

Expected: FAIL because fixture and validator do not exist.

- [x] **Step 4: Implement the fixture and pure validators**

Export `validateScreenshotFixture`, `inspectPng`, `validateScreenshotManifest`, and `scanScreenshotInputs`. Parse PNG chunks directly with bounded reads; do not add an image-processing dependency for validation. Validate every textual fixture value before the app launches and every resulting image/manifest before promotion.

Extract only reusable current-workflow setup primitives. Ordinary E2E remains on its existing test fixture unless its assertions are deliberately updated in the same commit.

- [x] **Step 5: Implement crash-recoverable canonical-set promotion**

The CLI validates a caller-supplied temporary capture directory, copies it into a new sibling staging directory, fsyncs files, and revalidates. Under an exclusive lock it renames an existing canonical directory to a unique backup, fsyncs the parent, renames staging to `en-AU`, fsyncs the parent again, revalidates canonical output, and only then removes the backup and fsyncs the parent. A recovery test covers a crash at every boundary: the next invocation restores the backup when canonical is absent, retains canonical when it is complete, and refuses to guess when both are invalid. Before the first rename, any error deletes only new staging and preserves the old set. Reject destination paths outside `apps/desktop/release/macos/screenshots/` and never follow symlinks.

- [x] **Step 6: Run validator and ordinary accounting workflow tests GREEN**

Run:

```bash
rtk mise exec -- node --test scripts/check-app-store-screenshots.test.mjs
rtk mise exec -- pnpm --dir apps/desktop test
rtk mise exec -- pnpm --dir apps/desktop exec playwright test tests/e2e/current-workflows.spec.ts --project=darwin-arm64 --workers=1
```

Expected: unit tests pass and the existing current accounting workflow remains functional. If the platform-specific packaged test cannot run on the current host, record the named host blocker rather than fabricating a pass.

Observed on 2026-08-30: screenshot validator `13/13`, desktop typecheck, and the packaged `darwin-arm64` current-accounting-workflow E2E `1/1` pass. A packaged UI probe bound all five complete accessibility-text contracts to the current shell and screen language plus fixture-controlled values. The validator includes real child-process death at lock, reclaim-marker, release-claim, and promotion boundaries; a barrier-controlled two-process stale-lock reclamation race; and exact zlib input-consumption checks. The broad desktop suite passes `668/669`; its unrelated `security.asar.integration.test.ts` Electron harness reaches `TAMMY_ASAR_READY_WAIT` but the host never delivers `app.whenReady()` before the pinned 20-second timeout. The real packaged accounting app launches and completes the changed workflow successfully on the same host.

- [x] **Step 7: Commit the fixture and validator**

```bash
rtk git add apps/desktop/release/macos/screenshots/fixture.json scripts/check-app-store-screenshots.d.mts scripts/check-app-store-screenshots.mjs scripts/check-app-store-screenshots.test.mjs apps/desktop/tests/e2e/support/current-accounting-workflow.ts apps/desktop/release/macos/store-metadata.md docs/superpowers/plans/2026-08-30-macos-app-store-prime-time-readiness.md
rtk git commit -m "feat: define App Store screenshot evidence"
```

### Task 13: Implement deterministic real-UI screenshot capture

Use `@superpowers:test-driven-development` and `@app-store-review:app-store-review`.

**Files:**
- Create: `apps/desktop/tests/e2e/app-store-screenshots.spec.ts`
- Create: `apps/desktop/playwright.app-store-screenshots.config.ts`
- Create: `scripts/capture-app-store-screenshots.mjs`
- Create: `scripts/capture-app-store-screenshots.d.mts`
- Create: `scripts/capture-app-store-screenshots.test.mjs`
- Modify: `apps/desktop/tests/e2e/fixtures.ts`
- Modify: `apps/desktop/tests/e2e/current-workflows.spec.ts`
- Modify: `apps/desktop/tests/e2e/support/current-accounting-workflow.ts`
- Modify: `apps/desktop/release/macos/screenshots/fixture.json`
- Modify: `apps/desktop/tests/e2e/electron-lifecycle.ts` only if a general fixed-window helper is required.
- Modify: `scripts/check-app-store-screenshots.mjs`
- Modify: `scripts/check-app-store-screenshots.d.mts`
- Modify: `scripts/check-app-store-screenshots.test.mjs`

- [x] **Step 1: Write the release screenshot E2E RED**

When executed in Task 17, launch only the development-signed MAS copy from the immutable Task 16 product source. Use a fresh temporary user-data root and encrypted workspace. Fix viewport/content bounds at 1440×900, device scale 1, locale `en-AU`, timezone `Australia/Melbourne`, UTC fixture clock, reduced motion, colour scheme, and font readiness. Fail if the host display cannot produce the exact pixels.

Exercise actual setup, sign-in, document ingestion/review, journal posting/trial balance, statement import/reconciliation, and BAS draft. Navigate through the UI, not renderer-only mocks or direct route injection after setup. Before each screenshot, assert the expected headings, seeded values, no loading/error state, and the visible `Draft — not lodged` boundary for BAS.

- [x] **Step 2: Add a test proving screenshot orchestration is external to the app**

Inspect `app.asar`, unpacked resources, executable strings, and packaged file inventory. Reject the fixture JSON, screenshot spec/config, Playwright package, capture filenames, a screenshot-only launch switch, a hidden reviewer mode, or fixture-only labels in the signed app. The harness may call the authenticated local API from Playwright, but no test bypass or seed endpoint may exist in the packaged application.

- [x] **Step 3: Write and run a genuinely failing capture-orchestrator test**

Run:

```bash
rtk mise exec -- node --test scripts/capture-app-store-screenshots.test.mjs scripts/check-app-store-screenshots.test.mjs
```

Expected: FAIL because `createScreenshotCapturePlan` and its strict artifact/fixture/temporary-output checks do not exist. The unit test must execute the owner; Playwright `--list` is only an additional discovery check and is not accepted as RED evidence.

- [x] **Step 4: Implement bounded capture and manifest creation**

Write images and a candidate manifest to a new `.tmp/macos-release/0.1.0/build-<N>/screenshots/<run-id>/` directory. Hash the development app, unsigned-content manifest, fixture, and images from stable file handles. Wait for two identical layout snapshots and loaded fonts before capture. Do not alter the packaged renderer or store persistent data outside the temporary workspace.

- [x] **Step 5: Validate the capture implementation without promoting images**

Use synthetic image fixtures plus the existing ordinary packaged E2E only; the signed release capture cannot run until Task 16 freezes the exact product source.

```bash
rtk mise exec -- node --test scripts/capture-app-store-screenshots.test.mjs scripts/check-app-store-screenshots.test.mjs
rtk mise exec -- pnpm --dir apps/desktop exec playwright test --config playwright.app-store-screenshots.config.ts --list
```

Expected: unit tests pass and Playwright discovers exactly one serial five-image journey. No canonical image is created or promoted.

Observed on 2026-08-31: the combined capture/evidence suite passes `23/23`, desktop TypeScript checking passes, and the screenshot config discovers exactly one test in one project. The packaged `darwin-arm64` accounting workflow passes `1/1` while navigating and exact-comparing the normalized real ARIA tree for all five target screens. The capture journey enforces `en-AU`, `Australia/Melbourne`, one CPU-rendering mode, device scale `1`, light colour scheme, reduced motion, exact 1440×900 content bounds, a pre-workflow UTC fixture clock that survives restart, font readiness, two identical layout snapshots, exact complete accessibility evidence, and empty console/page-error sets. The owner reuses Task 11 signed-copy equivalence, hashes the complete development app bundle before and after capture, scans the full packaged inventory/content for forbidden orchestration, rederives and validates every plan path, creates output through non-symlink realpath-checked ancestors, invokes the absolute Playwright CLI with a secret-free environment allowlist and bounded process-tree timeout, reaps stubborn descendants after abnormal leader exit, and rejects hostile `PATH`/`NODE_OPTIONS` input. The exact development-signed capture remains deferred until Task 17 as required; no canonical image was created or promoted.

- [x] **Step 6: Commit capture automation before the freeze**

```bash
rtk git add apps/desktop/release/macos/screenshots/fixture.json apps/desktop/tests/e2e/app-store-screenshots.spec.ts apps/desktop/tests/e2e/current-workflows.spec.ts apps/desktop/tests/e2e/fixtures.ts apps/desktop/tests/e2e/support/current-accounting-workflow.ts apps/desktop/playwright.app-store-screenshots.config.ts scripts/capture-app-store-screenshots.d.mts scripts/capture-app-store-screenshots.mjs scripts/capture-app-store-screenshots.test.mjs scripts/check-app-store-screenshots.d.mts scripts/check-app-store-screenshots.mjs scripts/check-app-store-screenshots.test.mjs docs/superpowers/plans/2026-08-30-macos-app-store-prime-time-readiness.md
rtk git commit -m "feat: automate real App Store screenshots"
```

This capture code and any normal-UI adjustment are candidate-affecting and must be committed before Task 16. After Task 16, a visual defect requires superseding `<N>` and reserving a higher build; do not patch the frozen build.

### Task 14: Implement candidate-bound privacy and runtime-egress evidence

Use `@security-best-practices`, `@app-store-review:app-store-review`, and `@superpowers:test-driven-development`.

**Files:**
- Create: `scripts/collect-macos-privacy-evidence.mjs`
- Create: `scripts/collect-macos-privacy-evidence.test.mjs`
- Create: `scripts/macos-runtime-egress.mjs`
- Create: `scripts/macos-runtime-egress.test.mjs`
- Create: `apps/desktop/tests/e2e/app-store-privacy.spec.ts`
- Create: `apps/desktop/playwright.app-store-privacy.config.ts`
- Modify: `apps/desktop/tests/e2e/fixtures.ts`
- Modify: `apps/desktop/tests/e2e/process-check.ts`
- Modify: `apps/desktop/src/main/security.test.ts`

- [x] **Step 1: Write failing static privacy-evidence tests**

Require evidence bound to source commit/tree, version/build, unsigned-content-manifest hash, development app hash, distribution app hash, and package hash. Inventory, with path/hash/type:

- every bundled `PrivacyInfo.xcprivacy`;
- executables, frameworks, dylibs, native modules, and universal/Mach-O architectures;
- production JavaScript packages actually represented in the ASAR/native payload, with lockfile version and license metadata;
- app/helper/core entitlements and declared accessed-API reasons;
- embedded public URLs and all string-observable hostnames.

Reject known or policy-defined updater, crash-reporting, analytics, telemetry, advertising, tracking, fingerprinting, dynamic remote-code, undeclared networking, or web-content SDKs unless the canonical privacy/store design is changed and re-approved before a new build reservation. Reject missing hashes, stale artifacts, extra secret-like keys, raw environment dumps, profile contents, certificate bytes, accounting data, and unbounded command output.

- [x] **Step 2: Write failing active-containment and runtime-egress tests**

Create `detectMacOSEgressEnforcer()` and a general process-tree observer. The supported local implementation must actively deny non-loopback networking for the exact development app process tree while allowing only the authenticated loopback core channel, and it must supply a reliable audit of denied connection and DNS attempts. Pin exact signed app/core/helper paths and hashes. Use `/usr/bin/sandbox-exec` only when a preflight positive/negative probe proves that this macOS release supports nested process-scoped denial, loopback allowance, child inheritance, and bounded sandbox-violation audit collection; sampled `/usr/sbin/lsof` is supplemental and cannot establish zero attempts.

Fail on unavailable/unproven containment with `MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE`, process replacement, unpinned child executables, listeners other than the authenticated loopback core, any non-loopback connection or DNS attempt, zero audit/observation samples, or observer failure. Do not silently downgrade to socket sampling. A dedicated disposable CI runner may provide a separately tested process/container-scoped enforcer, but a host-wide firewall or disabling the user's network is forbidden.

Model the two approved external-link actions separately as explicit Electron `shell.openExternal` handoff events. Validate exact equality with the recorded HTTPS privacy/support URLs, user gesture, timestamp within the journey, and no app-owned socket to those hosts. Do not whitelist arbitrary browser traffic or turn off the user's network globally.

- [x] **Step 3: Run privacy/egress tests RED**

Run:

```bash
rtk mise exec -- node --test scripts/collect-macos-privacy-evidence.test.mjs scripts/macos-runtime-egress.test.mjs
rtk mise exec -- pnpm --dir apps/desktop test -- security.test.ts
```

Expected: FAIL because the exact-artifact collectors and handoff seam are absent.

- [x] **Step 4: Implement bounded static inspection**

Use stable file descriptors and explicit size/count/depth limits. Read the production lockfile and ASAR inventory; inspect Mach-O headers and platform tools with argument arrays, never a shell. Store only redacted structured facts. If the installed Xcode version exposes a privacy-report command for this exact `.app` or `.pkg`, attach the generated report and SHA-256 as `supplemental`; otherwise record `not-supported-by-detected-toolchain` with the checked tool/version. Never call the Electron package an Xcode archive.

- [x] **Step 5: Implement the contained packaged privacy journey**

Plan the same development-signed equivalent and immutable fixture through setup, sign-in, document review, banking, BAS draft, a bounded idle interval, clicks on privacy/support, and clean quit. Start active containment and audit before Electron launch and stop only after all pinned processes exit. Record counts and destinations but no request payloads, page content, workspace paths, credentials, or accounting fields.

- [x] **Step 6: Run unit tests and journey discovery GREEN before freeze**

Run:

```bash
rtk mise exec -- node --test scripts/collect-macos-privacy-evidence.test.mjs scripts/macos-runtime-egress.test.mjs
rtk mise exec -- pnpm --dir apps/desktop exec playwright test --config playwright.app-store-privacy.config.ts --list
```

Expected: unit tests pass against positive/negative enforcer probes and Playwright discovers exactly one serial privacy journey. The exact signed journey runs only after Task 16 freezes the product source.

- [x] **Step 7: Commit collectors and tests, not transient evidence**

```bash
rtk git add scripts/collect-macos-privacy-evidence.mjs scripts/collect-macos-privacy-evidence.test.mjs scripts/macos-runtime-egress.mjs scripts/macos-runtime-egress.test.mjs apps/desktop/tests/e2e/app-store-privacy.spec.ts apps/desktop/playwright.app-store-privacy.config.ts apps/desktop/tests/e2e/fixtures.ts apps/desktop/tests/e2e/process-check.ts apps/desktop/src/main/security.test.ts
rtk git commit -m "feat: verify App Store privacy boundary"
```

This containment and collection code is candidate-affecting tooling and must be committed before Task 16.

### Task 15: Implement crash-consistent candidate evidence promotion and Task scenarios

Use `@app-store-review:app-store-review`, `@security-best-practices`, `@superpowers:test-driven-development`, and `@superpowers:verification-before-completion`.

**Files:**
- Create: `scripts/promote-macos-release-evidence.mjs`
- Create: `scripts/promote-macos-release-evidence.test.mjs`
- Create: `scripts/record-macos-app-store-fact.mjs`
- Create: `scripts/record-macos-app-store-fact.test.mjs`
- Create: `scripts/inspect-macos-release-package.mjs`
- Create: `scripts/inspect-macos-release-package.test.mjs`
- Modify: `scripts/macos-release-provenance.mjs`
- Modify: `scripts/macos-release-provenance.test.mjs`
- Modify: `scripts/macos-release-state.mjs`
- Modify: `scripts/macos-release-state.test.mjs`
- Modify: `scripts/check-macos-store.mjs`
- Modify: `scripts/check-macos-store.test.mjs`
- Modify: `scripts/check-app-store-screenshots.mjs`
- Modify: `taskfiles/release.yml`
- Modify: `scripts/check-taskfiles.test.mjs`
- Modify: `docs/release/macos-app-store.md`

- [x] **Step 1: Write failing promotion, accountable-fact, and candidate-state tests**

Test an exclusive-create-only promotion from `.tmp/macos-release/0.1.0/build-<N>/evidence/<event-id>/` into one previously absent durable candidate record. Require exact agreement among:

- reserved version/build and immutable product-source commit/tree;
- development/distribution app hashes and shared unsigned-content-manifest hash;
- distribution `.pkg` hash/filename, `Info.plist`, app/helper IDs, arm64 architecture, macOS 14 minimum, public URLs, entitlements, profile Team/application identifiers, signing certificate classes/expiry, installer signature, and local Gatekeeper observation;
- five screenshot image hashes/captions and explicit development-artifact provenance;
- static privacy inventory and runtime-egress result;
- public-site deployment ID/version/origin and canonical metadata/policy hashes.

Reject overwrite, mixed runs, missing commands/outcomes, stale timestamps, an expired certificate/profile at candidate time, development identity in the distribution package, known individual Team `WFTX6CN23F` without the exact written-exception branch, secrets/accounting data, symlinks, files outside the candidate root, and a screenshot manifest that claims the distribution package captured the images.

Test a separate `record-macos-app-store-fact.mjs --input <absolute temporary JSON path>` owner that accepts only the strict attestation/lifecycle schemas from Task 7, checks version/build/product/package/order prerequisites, copies with exclusive creation into the exact release-record location, fsyncs file/parent, and never prompts for or prints credential material. Require `--check --input ...` as a read-only mode that performs the same validation, prints a redacted destination/outcome, and creates no file. Reject self-attestation templates, duplicate facts, submitted-before-uploaded, approved/rejected-before-submitted, mismatched App Store build/package/source, paths inside the repository as mutable input, unknown keys, and secret-like values. This owner records operator-supplied facts; it never invents outcomes.

Add kind-specific consistency tests: `export-compliance: non-exempt` cannot match a frozen candidate with `ITSAppUsesNonExemptEncryption: false`; pricing/availability must equal the exact frozen metadata snapshot; and `privacy-answer: no-data-collected-no-tracking` must cite and agree with the exact passing candidate privacy/runtime evidence and SDK inventory. A schema-valid contradiction returns a supersession blocker, never readiness.

Write failing tests for `inspect-macos-release-package.mjs --package <absolute archive path> --record <absolute build-record path>`. It must use stable file reads and bounded platform-tool output to verify package SHA-256, signature, filename, app version/build, identifiers, architecture, and candidate/upload-event linkage without rebuilding, resigning, unpacking outside a new contained temporary directory, or mutating the archive/repository. Reject symlinks, traversal, mutable read races, wrong package/event, and secret-bearing output.

Extend `macos-release-provenance.mjs` with read-only `--verify-frozen --version <semver> --build <N>`. Test a clean current tree whose changes from the recorded Task 16 product-source commit are limited to the validated canonical screenshot directory and exact build-record directory. Reject any changed dependency, product/UI/build/profile/metadata/site/tool/test/runbook path, unexpected release-record sibling, or unvalidated screenshot/record file. This lets later verification prove that bookkeeping commits did not mutate the frozen candidate source.

- [x] **Step 2: Write failing `CANDIDATE_READY` evaluation tests**

Prove the state remains `REPOSITORY_READY` when any candidate artifact/evidence item is absent or mismatched. It reaches `CANDIDATE_READY` only for the complete promoted record, passing seller/controller attestations, a clean tracking commit, and proof that the commit is reachable from the configured fetched trusted remote. Apply the same durability rule to every attestation/event consumed for `PRE_UPLOAD_READY`, `UPLOADED`, and `PRE_SUBMIT_READY`; the fact recorder also rejects a new fact whose required prerequisite event/attestation is not already durable. Test untracked, committed-but-unpushed, stale remote ref, wrong remote, and passing remote ancestry at every readiness level. Candidate readiness does not imply content-rights, export, price/availability, App Privacy answers, upload, processing, or submission.

- [x] **Step 3: Verify promotion/state tests are RED**

Run:

```bash
rtk mise exec -- node --test scripts/promote-macos-release-evidence.test.mjs scripts/record-macos-app-store-fact.test.mjs scripts/inspect-macos-release-package.test.mjs scripts/macos-release-provenance.test.mjs scripts/macos-release-state.test.mjs scripts/check-macos-store.test.mjs scripts/check-app-store-screenshots.test.mjs scripts/check-taskfiles.test.mjs
```

Expected: FAIL because durable candidate evidence cannot yet be produced.

- [x] **Step 4: Implement redacted evidence staging and crash-consistent promotion**

The package owner writes raw bounded command output only under the gitignored run directory, then derives the complete strict non-secret JSON set. Promotion uses stable file reads, rehashes the app/package/screenshots, validates every schema against the staging set, and writes all evidence below a unique sibling staging directory. It fsyncs and atomically renames that directory to `evidence/candidate/<event-id>/`, then fsyncs the renamed evidence directory and its parent. It creates `events/<UTC>-candidate-built.json` last with exclusive semantics as the commit marker, fsyncs the marker file and `events/` directory, and only then reports promotion success. Validators ignore and report orphan evidence directories without a commit marker; a recovery command may remove only a validated orphan. An event never points to a partial set. Existing durable events/evidence are immutable; a correction needs a new event or a new build number. The README links hashes and explains which facts remain Apple/operator controlled.

`evaluateReleaseState` also verifies that every candidate event/evidence file and every later attestation/event it consumes is tracked by a clean Git commit reachable from the configured fetched trusted remote (`origin` unless the runbook records another exact remote). Before the relevant commit is pushed and fetched, it reports the state-specific `*_RECORD_NOT_DURABLE` blocker and cannot advance to `CANDIDATE_READY`, `PRE_UPLOAD_READY`, `UPLOADED`, or `PRE_SUBMIT_READY`.

- [x] **Step 5: Add the scenario-oriented Taskfile surface**

Implement direct delegation only:

- `release:screenshots` captures from an existing exact development artifact;
- `release:screenshots:validate CAPTURE_DIR=...` validates a temporary run without promotion;
- `release:screenshots:promote CAPTURE_DIR=...` performs the explicit reviewed-run promotion;
- `release:screenshots:check` is read-only;
- `release:evidence MODE=collect|check|promote` explicitly stages, validates, or promotes evidence for an existing exact candidate and prints the staged directory in collect mode;
- `release:inspect-package ARCHIVE_PKG=... RECORD_DIR=...` passes both exact absolute paths to the read-only stable archived-package/record comparison and rejects omitted or ambiguous build records;
- `release:candidate` builds/validates locally but does not upload;
- `release:state` reports redacted state/blockers;
- `release:pre-upload-check` remains a state/attestation gate.

Every summary says whether it builds, signs, captures, validates, publishes, uploads, or submits. No task may deploy Sites or contact App Store Connect implicitly.

- [x] **Step 6: Run promotion/state/Taskfile tests GREEN before freeze**

Run:

```bash
rtk mise exec -- node --test scripts/promote-macos-release-evidence.test.mjs scripts/record-macos-app-store-fact.test.mjs scripts/inspect-macos-release-package.test.mjs scripts/macos-release-provenance.test.mjs scripts/macos-release-state.test.mjs scripts/check-macos-store.test.mjs scripts/check-app-store-screenshots.test.mjs scripts/check-taskfiles.test.mjs
rtk mise exec -- task release:check
rtk mise exec -- task release:state
```

Expected: tests pass against synthetic complete/partial/crashed records. Repository state remains no higher than `REPOSITORY_READY` and names exact candidate/signing blockers.

- [x] **Step 7: Commit every evidence owner before the freeze**

```bash
rtk git add scripts/promote-macos-release-evidence.mjs scripts/promote-macos-release-evidence.test.mjs scripts/record-macos-app-store-fact.mjs scripts/record-macos-app-store-fact.test.mjs scripts/inspect-macos-release-package.mjs scripts/inspect-macos-release-package.test.mjs scripts/macos-release-provenance.mjs scripts/macos-release-provenance.test.mjs scripts/macos-release-state.mjs scripts/macos-release-state.test.mjs scripts/check-macos-store.mjs scripts/check-macos-store.test.mjs scripts/check-app-store-screenshots.mjs taskfiles/release.yml scripts/check-taskfiles.test.mjs docs/release/macos-app-store.md
rtk git commit -m "feat: promote immutable App Store evidence"
```

Expected: all tooling that can affect the built payload, capture, privacy evidence, or readiness decision is now committed. No signed candidate or durable candidate event exists yet.

### Task 16: Resolve seller authority, reserve the real build, and freeze product source

Use `@app-store-review:app-store-review`. This is the final candidate-affecting task in the plan. Tasks 11–15 must be committed first.

**Files:**
- Create after verified Apple evidence: `docs/release/authority/apple-seller.json`
- Modify if transfer/recreation changes them: `apps/desktop/release/macos/store-identity.json`
- Modify if transfer/recreation changes them: `apps/desktop/release/macos/store-metadata.md`
- Modify: `apps/desktop/release/macos/build-numbers.json`
- Create: `docs/release/records/macos/0.1.0/build-<N>/attestations/company-controller.json`
- Create: `docs/release/records/macos/0.1.0/build-<N>/attestations/seller-eligibility.json`

- [x] **Step 1: Inspect the signed-in Apple seller/account facts without guessing**

As the root agent, inspect Apple Developer and App Store Connect for the seller name, membership type, Team ID, Account Holder, active agreements, `com.tammy.desktop` App ID, Apple identifier ID, App Store Connect record ID, application group, certificates, profiles, and transfer eligibility. Record only non-secret identifiers and outcomes. Do not expose sessions, cookies, certificate bytes, profile bytes, or personal account data not required by the schema.

Observed read-only on 2026-08-31: the seller and Apple Developer team remain the
individual `Ben Ebsworth` account (`WFTX6CN23F`), with Ben Ebsworth as Account
Holder. The Free Apps Agreement is active and the Paid Apps Agreement is pending
user information. Tammy is App Store Connect record `6800226692`; its explicit App
ID is `DXP9QHD7JH` / `com.tammy.desktop` with prefix `WFTX6CN23F`. Tammy uses the
macOS Team-ID App Group `WFTX6CN23F.com.tammy.desktop`; Apple documents that this
form does not need App Group registration in Certificates, Identifiers & Profiles.
The downloaded `Tammy Mac App Store 20260813` profile is active and correctly
authorizes the explicit App ID and wildcard keychain group; Tammy's signed app still
requires its exact App Group and SBR keychain subgroup. Mac App Distribution and Mac
Installer Distribution certificate classes exist. App Store Connect has build `1`
for version `0.1.0`, Ready to Submit. No written Apple exception was present. The
membership page provides Apple's individual-to-organization update workflow, but it
requires the company's D-U-N-S/business details and declarations. Outcome:
`APPLE_SELLER_ELIGIBILITY_BLOCKED`; do not reserve build `2`.

App Store Connect preparation advanced on 2026-08-31 without selecting or submitting
a build: Tammy's canonical 0.1.0 description, keywords, public support/marketing URLs,
copyright, manual-release setting, offline reviewer workflow, and review contact were
saved. The product age rating is 4+ with every feature/content answer set from the
implemented local accounting boundary. The canonical privacy-policy URL and the
repository-verified `Data Not Collected` response were published. Pricing is free with
Australia as the base region, public distribution, and Australia as the only available
country on release. Content Rights remains unset because its legal declaration needs an
accountable decision for user-selected business documents. Screenshots remain empty,
and no current-source build has been selected. These app-level observations do not
replace build-bound attestations after build `2` is reserved and processed.

- [ ] **Step 2: Resolve exactly one eligible seller branch**

- Company branch: Apple Developer organization/App Store seller is Gamma Systems Pty Ltd after Apple's membership update (or Apple-directed transfer/recreation), and all IDs, certificates, and freshly issued profiles bind the resulting organization-controlled team. The Team ID may be retained or replaced by Apple.
- Exception branch: retain the current individual seller/team only when explicit written Apple approval names this exact app and accounting scope.

If neither branch is evidenced, stop before reservation with `APPLE_SELLER_ELIGIBILITY_BLOCKED`. Do not create a seller attestation or distribution candidate. Follow Apple's supported transfer/recreation flow; if IDs change, update identity/metadata/profile expectations, rerun all Chunk 2–3 tests, and commit those candidate-affecting changes before continuing.

- [ ] **Step 3: Record and validate version-independent Apple authority**

Create `apple-seller.json` with the selected branch, exact Team/seller/record/App ID/group/helper identifiers, certificate classes, profile reissuance status, agreements status, and non-secret evidence references. Validate with the same tagged-union owner. Commit it and any transfer-driven metadata changes:

```bash
rtk git add docs/release/authority/apple-seller.json apps/desktop/release/macos/store-identity.json apps/desktop/release/macos/store-metadata.md apps/desktop/release/macos/profile.ts scripts/check-macos-store.mjs scripts/check-macos-store.test.mjs
rtk git commit -m "docs: bind Tammy Apple seller authority"
```

Expected: this commit contains any final candidate-affecting seller migration change; no build is reserved yet.

- [ ] **Step 4: Determine the real next build number**

Inspect the signed-in App Store Connect record for version `0.1.0` and record only the greatest existing build number or “no uploaded builds”. If the record cannot be read, stop with `APP_STORE_BUILD_HISTORY_UNVERIFIED`. Choose `<N>` greater than every Apple build and every local ledger reservation/event.

- [ ] **Step 5: Reserve `<N>` and create the two per-release attestations before committing**

Run:

```bash
rtk mise exec -- node scripts/reserve-macos-build.mjs --version 0.1.0 --operator "Ben Ebsworth" --number <N>
rtk mise exec -- node scripts/reserve-macos-build.mjs --check
```

Create `company-controller.json` with outcome `confirmed` and evidence reference `../../../../../authority/publisher-controller.json`. Create `seller-eligibility.json` with outcome `eligible`, the selected exact branch fields, and evidence reference `../../../../../authority/apple-seller.json`. Both include version `0.1.0` and build `<N>` and pass the common/kind-specific validator.

- [ ] **Step 6: Run every candidate-affecting check before the freeze commit**

Run:

```bash
rtk mise exec -- task site:verify-deployed SITE_ORIGIN=<recorded origin>
rtk mise exec -- task release:check
rtk mise exec -- task release:state
rtk mise exec -- pnpm typecheck
rtk mise exec -- pnpm lint
rtk mise exec -- pnpm test
```

Expected: repository/tooling checks pass or list only candidate/evidence/declaration blockers; no source mutation remains unstaged.

- [ ] **Step 7: Commit the reservation as the immutable product-source commit**

```bash
rtk git add apps/desktop/release/macos/build-numbers.json docs/release/records/macos/0.1.0/build-<N>/attestations/company-controller.json docs/release/records/macos/0.1.0/build-<N>/attestations/seller-eligibility.json
rtk git commit -m "chore: reserve Tammy build <N>"
```

Expected: this clean commit contains every candidate-affecting source/tool, the immutable reservation, and required seller/controller attestations. From this point, any UI, build, dependency, metadata, profile, fixture, capture, privacy, or validation-code correction supersedes `<N>` and requires a new higher reservation. Later commits for `<N>` may add only generated screenshots, redacted evidence, attestations, and lifecycle records.

- [ ] **Step 8: Freeze and record the product-source identity read-only**

Run: `rtk mise exec -- node scripts/macos-release-provenance.mjs --source --build <N> --version 0.1.0`

Expected: redacted JSON with the exact clean `HEAD` commit/tree and reserved build; it mutates no file.

### Task 17: Capture, scan, review, and explicitly promote the real screenshots

Use `@app-store-review:app-store-review` and `@superpowers:verification-before-completion`.

**Files:**
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/manifest.json`
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/01-overview.png`
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/02-document-review.png`
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/03-journal-trial-balance.png`
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/04-bank-reconciliation.png`
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/05-bas-draft.png`
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/accessibility/01-overview.json`
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/accessibility/02-document-review.json`
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/accessibility/03-journal-trial-balance.json`
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/accessibility/04-bank-reconciliation.json`
- Create from the passing run: `apps/desktop/release/macos/screenshots/en-AU/accessibility/05-bas-draft.json`

- [ ] **Step 1: Build the exact development-signed equivalent**

From the clean Task 16 checkout, use the recorded build number, deployed public URLs, explicit export-compliance input, and the selected seller-eligibility branch's exact development certificate/profile, Team ID, bundle/helper identifiers, and application groups:

```bash
rtk mise exec -- task release:development
```

Expected: the development app is signed from one verified copy of the frozen unsigned staging result, locally runnable, and produces no installer/upload/submission.

- [ ] **Step 2: Capture only into a new temporary run**

Run: `rtk mise exec -- task release:screenshots`

Expected: the task prints one contained `.tmp/macos-release/0.1.0/build-<N>/screenshots/<run-id>/` path. It writes five PNGs, five complete accessibility snapshots, and a candidate manifest there; it does not alter the canonical screenshot directory.

- [ ] **Step 3: Run automated post-render validation before manual review**

Run:

```bash
rtk mise exec -- task release:screenshots:validate CAPTURE_DIR=<temporary run directory>
```

It must scan the actual accessibility text tied to every image, enforce the fixture allowlist and secret/PII denies, and validate image/manifest hashes and orchestration absence. A missing or unscannable snapshot fails closed.

- [ ] **Step 4: Manually inspect all five temporary PNGs**

Use image viewing on each file. Confirm legible 1440×900 output, no clipped controls, no transient/loading state, no personal data, balanced visual hierarchy, and unambiguous draft/not-lodged wording. If any image fails, supersede build `<N>`, fix the normal UI before a new freeze, and recapture under a new build; do not image-edit screenshots or promote a partial set.

- [ ] **Step 5: Explicitly promote the reviewed run and revalidate canonical output**

Run:

```bash
rtk mise exec -- task release:screenshots:promote CAPTURE_DIR=<validated run directory>
rtk mise exec -- task release:screenshots:check
```

Expected: promotion uses the tested crash-recoverable backup/rename protocol only after revalidation. An ordinary failure preserves the previous canonical set; an interrupted promotion is deterministically recovered on the next invocation.

- [ ] **Step 6: Commit the evidence-only screenshot set**

```bash
rtk git add apps/desktop/release/macos/screenshots/en-AU
rtk git commit -m "release: add Tammy App Store screenshots for build <N>"
```

Expected: image/accessibility hashes remain bound to the frozen source and development app; no product source changes are included.

### Task 18: Build the distribution candidate and promote its complete evidence record

Use `@app-store-review:app-store-review`, `@security-best-practices`, and `@superpowers:verification-before-completion`.

**Files:**
- Create after a passing candidate: `docs/release/records/macos/0.1.0/build-<N>/evidence/candidate/<event-id>/candidate.json`
- Create after a passing candidate: `docs/release/records/macos/0.1.0/build-<N>/evidence/candidate/<event-id>/privacy-evidence.json`
- Create after a passing candidate: `docs/release/records/macos/0.1.0/build-<N>/evidence/candidate/<event-id>/runtime-egress.json`
- Create after a passing candidate: `docs/release/records/macos/0.1.0/build-<N>/evidence/candidate/<event-id>/screenshots.json`
- Create after a passing candidate: `docs/release/records/macos/0.1.0/build-<N>/evidence/candidate/<event-id>/metadata-snapshot.json`
- Create after a passing candidate: `docs/release/records/macos/0.1.0/build-<N>/evidence/candidate/<event-id>/summary.md`
- Create last as commit marker: `docs/release/records/macos/0.1.0/build-<N>/events/<UTC>-candidate-built.json`

- [ ] **Step 1: Build and locally inspect the exact distribution package**

With the Task 16 reserved `<N>` and the selected seller-eligibility branch's exact distribution/installer certificates, profiles, Team ID, bundle/helper identifiers, and application groups:

```bash
rtk mise exec -- task release:candidate
```

Expected: the package owner authenticates the frozen unsigned staging manifest, signs the independent distribution copy, proves allowed-only equivalence with the screenshot development app, runs `codesign`, `security`, `pkgutil`, `spctl`, profile, entitlement, identifier, architecture, minimum-OS, public-URL, and installer checks, then prints package path/SHA-256. It never uploads.

- [ ] **Step 2: Run the actively contained development-equivalent privacy journey**

Run: `rtk mise exec -- pnpm --dir apps/desktop exec playwright test --config playwright.app-store-privacy.config.ts --workers=1`

Expected: the preflight-proven process-scoped enforcer actively denies and audits all non-loopback connection/DNS attempts; the signed app exercises setup, sign-in, documents, banking, BAS draft, idle, the two explicit external handoffs, and clean quit. Only authenticated loopback core traffic and the two separately recorded user gestures are permitted. If containment/audit is unsupported, stop with `MACOS_RUNTIME_EGRESS_CONTAINMENT_UNAVAILABLE`; `lsof` sampling cannot substitute.

- [ ] **Step 3: Stage the complete candidate evidence without touching the durable record**

Run:

```bash
rtk mise exec -- task release:evidence MODE=collect
```

The task runs the static privacy/dependency collector, exact-artifact hashes, candidate inspection, metadata/site snapshot, screenshot finalizer, and runtime-egress collector into one new `.tmp/macos-release/0.1.0/build-<N>/evidence/<event-id>/` directory and prints that exact path as redacted JSON. Finalize the screenshot evidence with the distribution package SHA-256 while retaining `capture_artifact_kind: development-signed-app`.

- [ ] **Step 4: Validate the entire staged record before promotion**

Run:

```bash
rtk mise exec -- node --test scripts/promote-macos-release-evidence.test.mjs scripts/macos-release-provenance.test.mjs scripts/macos-release-state.test.mjs scripts/check-macos-store.test.mjs scripts/check-app-store-screenshots.test.mjs scripts/check-taskfiles.test.mjs
rtk mise exec -- task release:evidence EVIDENCE_DIR=<staged event directory> MODE=check
```

Expected: all schemas, hashes, product-source facts, seller/profile facts, payload equivalence, screenshots, privacy inventory, active-egress evidence, metadata, and public-site linkage pass against staging. Durable state remains unchanged.

- [ ] **Step 5: Promote with the candidate event as the final commit marker**

Run: `rtk mise exec -- task release:evidence EVIDENCE_DIR=<staged event directory> MODE=promote`

Expected: the complete evidence directory lands first, then the exclusive `candidate-built` event commits it. A crash before the marker leaves an ignored/recoverable orphan; no partial candidate becomes ready.

- [ ] **Step 6: Verify the promoted record remains provisional until durable**

Run:

```bash
rtk mise exec -- task release:screenshots:check
rtk mise exec -- task release:check
rtk mise exec -- task release:state
```

Expected: all local candidate checks pass, but state remains `REPOSITORY_READY` with `CANDIDATE_RECORD_NOT_DURABLE` until the event/evidence commit is pushed and fetched from the trusted remote. It never reports uploaded/submitted. If any signing/evidence gate is unavailable, state remains `REPOSITORY_READY` with that exact blocker and prior durable records untouched.

- [ ] **Step 7: Commit and push only the complete non-secret record**

```bash
rtk git add docs/release/records/macos/0.1.0/build-<N>
rtk git commit -m "release: record Tammy macOS candidate <N>"
rtk git push
rtk git fetch origin
rtk mise exec -- task release:state
```

Expected: the trusted remote contains immutable evidence and screenshot hashes and state now reports `CANDIDATE_READY` for the exact package. The signed `.pkg`, private keys, profiles, sessions, passwords, tokens, raw command output, and temporary workspaces remain outside Git.

---

## Chunk 4: App Store Connect upload and prime-time handoff

### File structure for this chunk

All product, build, capture, validation, metadata, and runbook code is frozen in Task 16. This chunk creates only accountable attestations and immutable release events under `docs/release/records/macos/0.1.0/build-<N>/`; any required source/copy/tooling correction supersedes `<N>` and returns to Task 16 with a higher build number.

- Create after accountable confirmation: `docs/release/records/macos/0.1.0/build-<N>/attestations/content-rights.json`.
- Create after accountable confirmation: `docs/release/records/macos/0.1.0/build-<N>/attestations/export-compliance.json`.
- Create after accountable confirmation: `docs/release/records/macos/0.1.0/build-<N>/attestations/pricing-availability.json`.
- Create after accountable confirmation: `docs/release/records/macos/0.1.0/build-<N>/attestations/privacy-answer.json`.
- Create only after App Store Connect accepts the package: `docs/release/records/macos/0.1.0/build-<N>/events/<UTC>-uploaded.json`.
- Create after the processed build and form work are observed: `docs/release/records/macos/0.1.0/build-<N>/attestations/processed-build.json`.
- Create after the questionnaire is completed: `docs/release/records/macos/0.1.0/build-<N>/attestations/age-rating.json`.
- Create after exact metadata/screenshots are entered: `docs/release/records/macos/0.1.0/build-<N>/attestations/metadata-assets-entered.json`.
- Create after the final App Store Connect warning/omission pass: `docs/release/records/macos/0.1.0/build-<N>/attestations/app-store-warning-review.json`.
- Create as a release-record summary only: `docs/release/records/macos/0.1.0/build-<N>/prime-time-handoff.md`.

### Mandatory supersession procedure

When Tasks 19–21 discover a contradiction or Apple product/package/processing rejection that requires any frozen source, metadata, declaration basis, signing, or distribution-scope change, do not merely say `<N>` is superseded. Create a strict temporary `superseded` event (replacement build may be null until reserved), run `record-macos-app-store-fact.mjs --check --input <path>`, record it with `--input`, commit, push, fetch the trusted remote, and verify `release:state` reports lifecycle `superseded` and blocks further upload/submission actions for `<N>`. Only then return to Task 16 and reserve a higher build. A retryable uploader authentication, Apple service, or network failure that leaves the exact package unchanged creates no lifecycle event; retry the same already-reserved build after the external fault clears.

### Task 19: Record accountable pre-upload declarations and reach `PRE_UPLOAD_READY`

Use `@app-store-review:app-store-review`. These are accountable legal/product facts; no agent or script may infer or self-attest them.

**Files:**
- Create: `docs/release/records/macos/0.1.0/build-<N>/attestations/content-rights.json`
- Create: `docs/release/records/macos/0.1.0/build-<N>/attestations/export-compliance.json`
- Create: `docs/release/records/macos/0.1.0/build-<N>/attestations/pricing-availability.json`
- Create: `docs/release/records/macos/0.1.0/build-<N>/attestations/privacy-answer.json`

- [ ] **Step 1: Reconfirm the durable candidate and current Apple requirements**

Run:

```bash
rtk git fetch origin
rtk mise exec -- task release:state
rtk mise exec -- task release:pre-upload-check
```

Expected: state is `CANDIDATE_READY`; the pre-upload check names only missing accountable attestations. Open Apple's current official App Review, App Privacy, encryption/export, content-rights, pricing/availability, and financial-app guidance plus the signed-in App Store Connect forms. Use official Apple sources for current requirements and treat any inference as provisional.

- [ ] **Step 2: Present the evidence-backed recommended answers without recording them**

Show the accountable operator a concise table derived from the exact candidate:

- content rights: Gamma Systems Pty Ltd owns or is authorised to use all app code, icon, screenshots, and marketing content;
- export: recommended `exempt` only if the operator confirms the recorded Australia-only standard-encryption determination and `ITSAppUsesNonExemptEncryption: false` remain correct;
- price/availability: Free, Australia, manual release;
- App Privacy: recommended **No data collected** and **No tracking**, citing the exact candidate inventory, active egress-denial result, no ads/analytics/tracking SDK finding, and explicit browser handoffs.

Do not create a passing attestation until Ben Ebsworth explicitly confirms each answer and that they are authorised to do so for Gamma Systems Pty Ltd. A disagreement that changes the app, metadata, privacy meaning, export plist, or distribution scope follows the mandatory supersession procedure. In particular, `non-exempt` cannot be recorded against this frozen build while its candidate `Info.plist` says `ITSAppUsesNonExemptEncryption: false`; `privacy-answer` and `pricing-availability` must match the exact candidate evidence/metadata.

- [ ] **Step 3: Create and validate temporary strict attestation inputs**

For each confirmed kind, create one temporary JSON outside the repository with exact Task 7 keys, version `0.1.0`, build `<N>`, `accountablePerson: "Ben Ebsworth"`, the real UTC confirmation time, a non-secret evidence reference, and outcome:

- `content-rights`: `owned`;
- `export-compliance`: `exempt` or `non-exempt` exactly as confirmed;
- `pricing-availability`: `confirmed`;
- `privacy-answer`: `no-data-collected-no-tracking` only if confirmed.

Run the validator in check-only mode first for every file:

```bash
rtk mise exec -- node scripts/record-macos-app-store-fact.mjs --check --input <absolute temporary attestation path>
```

Never copy cookies, sessions, correspondence bodies, certificate material, secrets, or free-form legal notes into Git.

- [ ] **Step 4: Record each accountable fact with exclusive creation**

Run for each confirmed temporary file:

```bash
rtk mise exec -- node scripts/record-macos-app-store-fact.mjs --input <absolute temporary attestation path>
```

Expected: each strict attestation is created once in the exact build record, fsynced, redacted on stdout, and tied to the existing candidate. A missing confirmation leaves its blocker in place.

- [ ] **Step 5: Commit, push, fetch, and verify `PRE_UPLOAD_READY`**

```bash
rtk git add docs/release/records/macos/0.1.0/build-<N>/attestations
rtk git commit -m "release: attest Tammy build <N> for upload"
rtk git push
rtk git fetch origin
rtk mise exec -- task release:pre-upload-check
rtk mise exec -- task release:state
```

Expected: `PRE_UPLOAD_READY`; this state proves the exact package is ready to upload but does not claim it has been uploaded, processed, or submitted.

### Task 20: Upload the exact package and record App Store Connect acceptance

Use `@app-store-review:app-store-review` and `@superpowers:verification-before-completion`. Upload is an explicit external action and must never be hidden inside a build/check task.

**Files:**
- Create after acceptance: `docs/release/records/macos/0.1.0/build-<N>/events/<UTC>-uploaded.json`

- [ ] **Step 1: Verify the archived package immediately before upload**

Locate the access-controlled `.pkg` retained from Task 18 and run:

```bash
rtk mise exec -- task release:inspect-package ARCHIVE_PKG=<absolute retained package path> RECORD_DIR=<absolute docs/release/records/macos/0.1.0/build-N path>
```

Expected: the read-only owner performs stable SHA-256, `pkgutil --check-signature`, local `spctl`, package filename/version/build/identifier/architecture, and durable release-record comparison. Refuse any path/hash/signature mismatch. Do not rebuild or resign the package.

- [ ] **Step 2: Obtain explicit upload confirmation for the exact artifact**

Show the operator app name, version/build, package SHA-256, selected seller branch/seller, App Store Connect record ID, and that the build number has already been permanently reserved and upload will not submit for review. Proceed only after explicit confirmation to upload that exact package.

- [ ] **Step 3: Upload using Apple's currently supported authenticated uploader**

Check Apple's official upload guidance at execution time and use the supported Transporter/Xcode command or UI for this Mac and account. Keep credentials in Keychain/Apple's authenticated tool; never pass them through Taskfile arguments, logs, environment dumps, or Git. Upload only the Task 18 package to the exact Tammy App Store Connect record.

Expected: Apple's uploader reports accepted delivery or a precise failure. Retryable authentication/service/network failure creates no event and leaves state `PRE_UPLOAD_READY`. A product/package rejection creates no `uploaded` event and must follow the mandatory supersession procedure before a replacement is reserved.

- [ ] **Step 4: Observe the accepted App Store build identity**

In signed-in App Store Connect, verify seller, app record, marketing version `0.1.0`, build `<N>`, upload time, package/build identity, and Apple's non-secret build identifier. Wait for the upload to appear; do not interpret local uploader exit alone as App Store acceptance.

- [ ] **Step 5: Record the immutable `uploaded` event**

Create a temporary strict event with exact package/source hashes, App Store Connect build ID, operator, and observed UTC time, then run:

```bash
rtk mise exec -- node scripts/record-macos-app-store-fact.mjs --input <absolute temporary uploaded-event path>
```

Expected: the event is exclusive, ordered after `candidate-built`, and contains no session, receipt body, credential, or mutable free-form notes.

- [ ] **Step 6: Commit, push, fetch, and verify `UPLOADED`**

```bash
rtk git add docs/release/records/macos/0.1.0/build-<N>/events
rtk git commit -m "release: record Tammy build <N> upload"
rtk git push
rtk git fetch origin
rtk mise exec -- task release:state
```

Expected: state is `UPLOADED`. It does not claim processing, build selection, declaration completion, submission, or approval.

### Task 21: Complete the App Store product page and reach `PRE_SUBMIT_READY`

Use `@app-store-review:app-store-review`. This task enters already-frozen truthful content and records accountable observations; it does not change repository-owned copy or submit for review.

**Files:**
- Create: `docs/release/records/macos/0.1.0/build-<N>/attestations/processed-build.json`
- Create: `docs/release/records/macos/0.1.0/build-<N>/attestations/age-rating.json`
- Create: `docs/release/records/macos/0.1.0/build-<N>/attestations/metadata-assets-entered.json`
- Create: `docs/release/records/macos/0.1.0/build-<N>/attestations/app-store-warning-review.json`

- [ ] **Step 1: Wait for processing and select only the exact build**

Observe App Store Connect until version `0.1.0` build `<N>` finishes processing. Inspect any Apple warnings. Select only the build whose non-secret identifier matches the `uploaded` event. A processing rejection or binary warning that requires a product change follows the mandatory supersession procedure.

- [ ] **Step 2: Enter the frozen metadata and public destinations exactly**

Copy from the candidate's metadata snapshot, not memory:

- Tammy Accounting / installed Tammy;
- Gamma Systems Pty Ltd and `© 2026 Gamma Systems Pty Ltd`;
- Finance / Business, en-AU, macOS 14+, arm64;
- exact verified public Sites privacy/support URLs;
- final description, keywords, review contact `ben.ebsworth@gmail.com`, and offline workspace/reviewer instructions;
- Free, Australia, manual release.

Do not add TestFlight copy, ATO/SBR lodgement, company-tax submission, live bank feeds, cloud OCR, analytics, tracking, or other deferred claims.

- [ ] **Step 3: Upload the five canonical screenshots in manifest order**

Use the exact committed PNGs and captions, ordered `01` through `05`. Before upload, rerun `release:screenshots:check` and compare every file hash with candidate evidence. After upload, visually compare App Store Connect previews with the local set and verify `05-bas-draft.png` visibly says draft/not lodged.

- [ ] **Step 4: Complete declarations from the accountable answers**

Enter App Privacy, export compliance, content rights, age rating, price/availability, and release method from the recorded pre-upload attestations and current official Apple form definitions. Do not infer an answer from a disabled/default control. Review financial-app and encryption warnings explicitly.

- [ ] **Step 5: Perform the final omission/warning review before any attestation**

Review every App Store Connect section: selected processed build, app information, version copy, public URLs, support/review contact, screenshots, App Privacy, export, content rights, age rating, pricing/availability, release method, agreements/tax/banking status, and unresolved warnings. Confirm that the page offers a review-submission action but do not invoke it.

- [ ] **Step 6: Record the four observed attestations**

After Ben Ebsworth explicitly confirms the observations, create strict temporary inputs and record:

- `processed-build` → `selected`, referencing the exact App Store build ID;
- `age-rating` → `completed`, referencing the completed questionnaire/result;
- `metadata-assets-entered` → `entered`, referencing the frozen metadata snapshot and screenshot manifest hashes;
- `app-store-warning-review` → `clear` or `resolved`, referencing the final warning review.

Use `record-macos-app-store-fact.mjs --input` for each. The recorder validates upload ancestry and never presses Submit for Review.

- [ ] **Step 7: Commit, push, fetch, and verify `PRE_SUBMIT_READY`**

```bash
rtk git add docs/release/records/macos/0.1.0/build-<N>/attestations
rtk git commit -m "release: complete Tammy App Store handoff for build <N>"
rtk git push
rtk git fetch origin
rtk mise exec -- task release:pre-submit-check
rtk mise exec -- task release:state
```

Expected: `PRE_SUBMIT_READY`. Tammy is uploaded, processed, selected, populated, and ready for an accountable final submission decision. It is not `submitted` or `approved`.

### Task 22: Run the full prime-time verification and hand off the submission decision

Use `@app-store-review:app-store-review` and `@superpowers:verification-before-completion`.

**Files:**
- Create: `docs/release/records/macos/0.1.0/build-<N>/prime-time-handoff.md`

- [ ] **Step 1: Run the repository and release verification matrix from a clean tree**

```bash
rtk mise install
rtk mise exec -- task setup
rtk mise exec -- task verify:quick
rtk mise exec -- task verify:full
rtk mise exec -- task verify:release
rtk mise exec -- pnpm typecheck
rtk mise exec -- pnpm lint
rtk mise exec -- pnpm test
rtk mise exec -- task release:check
rtk mise exec -- task release:screenshots:check
rtk mise exec -- task release:pre-submit-check
rtk mise exec -- node scripts/check-clean-tree.mjs
rtk mise exec -- node scripts/macos-release-provenance.mjs --verify-frozen --version 0.1.0 --build <N>
```

Expected: every repository-owned test passes, the worktree remains clean after verification, and no candidate-affecting path differs from the Task 16 product source. Platform/credential-dependent tests either pass on the arm64 Mac with the exact artifacts or produce an already-documented external blocker; no skip may be relabelled a pass.

- [ ] **Step 2: Revalidate the live public and App Store surfaces**

Run:

```bash
rtk mise exec -- task site:verify-deployed SITE_ORIGIN=<recorded origin>
```

Then open `/`, `/privacy`, and `/support` and verify HTTPS, identity, policy effective date, email, navigation, and candidate-embedded URL equality. Reopen App Store Connect and compare the selected build, metadata, screenshots, declarations, and warnings against the durable record.

- [ ] **Step 3: Rehash the retained exact package and evidence**

From the access-controlled release archive, run:

```bash
rtk mise exec -- task release:inspect-package ARCHIVE_PKG=<absolute retained package path> RECORD_DIR=<absolute docs/release/records/macos/0.1.0/build-N path>
```

Compare its result with candidate/upload events, privacy/runtime evidence, screenshot manifest, metadata snapshot, public-site deployment, Apple build ID, and trusted-remote ancestry. Do not attempt a byte-for-byte rebuild of timestamped Apple signatures and do not substitute a newly signed artifact.

- [ ] **Step 4: Write a concise evidence-only handoff**

Record observed pass/fail outcomes, exact public privacy/support URLs, version/build, package SHA-256, App Store Connect build ID, seller branch, candidate/source commit, screenshot manifest hash, privacy/runtime evidence hashes, current readiness state, and any remaining external gate. Include no credentials or accounting data and no new product/marketing claim.

- [ ] **Step 5: Commit, push, and rerun final state**

```bash
rtk git add docs/release/records/macos/0.1.0/build-<N>/prime-time-handoff.md
rtk git commit -m "docs: hand off Tammy build <N> for App Review"
rtk git push
rtk git fetch origin
rtk mise exec -- task release:state
```

Expected: `PRE_SUBMIT_READY` with the handoff backed up on the trusted remote. Report the public URLs and exact App Store Connect state to the user. Stop before pressing **Submit for Review** unless the user explicitly authorises that separate external action after seeing this evidence.
