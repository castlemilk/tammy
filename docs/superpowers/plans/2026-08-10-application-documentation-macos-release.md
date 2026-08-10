# Application Documentation and macOS Release Readiness Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bundle accurate application/developer documentation and add a small, fail-closed Mac App Store packaging profile with auditable readiness checks.

**Architecture:** Keep the existing Electron Forge, Go, SQLCipher, pnpm, and mise stack. Add a separately selected MAS profile and repository-owned release assets/checks while leaving certificates, provisioning, App Store Connect metadata, and upload under explicit operator control.

**Tech Stack:** Electron Forge 7, Electron, TypeScript, Node.js, pnpm, Go/SQLCipher, Apple plist/entitlements, macOS signing tools

---

## Chunk 1: Documentation and release inputs

### Task 1: Replace stale foundation guidance with the current app handbook

**Files:**
- Create: `README.md`
- Create: `docs/development/tech-state.md`
- Create: `docs/development/local-accounting-walkthrough.md`
- Modify: `docs/development/foundation.md`

- [ ] Write the root product summary, maturity statement, quick start, primary checks, and links.
- [ ] Record implemented, development-only, deferred, and external-evidence capabilities in one status table.
- [ ] Replace stale claims that accounting and BAS are absent with the actual Electron/Go/SQLCipher architecture and workflow.
- [ ] Document the setup → recovery → sign-in → organisation → chart → journals → documents → banking → BAS → audit walkthrough.
- [ ] Verify every command exists with `rtk jq -r '.scripts | keys[]' package.json` and every linked path with `rtk test -f <path>`.
- [ ] Run `rtk git diff --check` and commit the documentation bundle.

### Task 2: Add repository-owned macOS release resources

**Files:**
- Create: `apps/desktop/release/macos/entitlements.mas.plist`
- Create: `apps/desktop/release/macos/entitlements.mas.child.plist`
- Create: `apps/desktop/release/macos/entitlements.mas.core.plist`
- Create: `apps/desktop/release/macos/PrivacyInfo.xcprivacy`
- Create: `apps/desktop/release/macos/store-metadata.md`
- Create: `apps/desktop/assets/icon-source.png`
- Create: `apps/desktop/assets/icon.icns`

- [ ] Create a brand-consistent 1024×1024 source icon and deterministic `.icns` output.
- [ ] Give the app sandbox/client/user-selected-read access; give ordinary helpers sandbox inheritance; give the core inherited sandbox/server access.
- [ ] Declare no tracking/collection and the approved container/user-selected file metadata reasons in a valid privacy manifest.
- [ ] Add factual suggested store copy plus visible operator-owned placeholders for privacy/support URLs, legal identity, pricing, screenshots, and review contact.
- [ ] Validate plist syntax with `rtk plutil -lint apps/desktop/release/macos/*.plist apps/desktop/release/macos/PrivacyInfo.xcprivacy` and image dimensions with `rtk sips -g pixelWidth -g pixelHeight apps/desktop/assets/icon-source.png`.

## Chunk 2: Packaging and evidence

### Task 3: Add a separately selected MAS profile

**Files:**
- Create: `apps/desktop/release/macos/profile.ts`
- Create: `apps/desktop/release/macos/profile.test.ts`
- Modify: `apps/desktop/forge.config.ts`
- Modify: `apps/desktop/package.json`
- Modify: `pnpm-lock.yaml`

- [ ] Write tests proving development remains ad-hoc/darwin and MAS fails without exact identity/team/profile/installer inputs.
- [ ] Write tests proving MAS uses `com.tammy.desktop`, category `public.app-category.finance`, icon/privacy resources, MAS entitlements, and the packaged core-specific entitlements.
- [ ] Run `rtk mise exec -- pnpm --dir apps/desktop test release/macos/profile.test.ts` and observe the intended RED.
- [ ] Implement a pure environment parser and Forge profile builder; never log secret/profile contents.
- [ ] Add the pinned `@electron-forge/maker-pkg` version matching Forge 7.11.2 and select it only for MAS.
- [ ] Run the focused tests and desktop typecheck, then commit.

### Task 4: Add deterministic release readiness and packaging commands

**Files:**
- Create: `scripts/check-macos-store.mjs`
- Create: `scripts/check-macos-store.test.mjs`
- Modify: `package.json`

- [ ] Test that repository mode validates bundle ID, version, resources, plist contents, icon dimensions, docs, and metadata placeholders.
- [ ] Test that release mode rejects missing or relative signing/provisioning inputs and reports all operator prerequisites without exposing values.
- [ ] Run `rtk node --test scripts/check-macos-store.test.mjs` and observe the intended RED.
- [ ] Implement `check:macos-store` and `desktop:make:mas` commands using the existing build-manifest/core build before Forge `make --platform=mas --arch=arm64`.
- [ ] Run focused tests plus `rtk mise exec -- pnpm check:macos-store` and commit.

### Task 5: Write the operator runbook and App Review assessment

**Files:**
- Create: `docs/release/macos-app-store.md`
- Modify: `docs/development/tech-state.md`
- Modify: `README.md`

- [ ] Document development-signing versus distribution-signing behavior, App ID/profile/certificate creation, MAS sandbox testing, `codesign`, `spctl`, `productbuild`, Transporter, App Store processing, and rollback.
- [ ] Record App Review risks from @app-store-review: final/non-placeholder app, accurate screenshots, privacy/support links in metadata and app, no separate license gate, no orphan process, no downloaded code, no tracking/IAP today, fictional screenshot data, and financial-app legal-entity ownership.
- [ ] Explain that the local workspace sign-in is not a remote account and that reviewers create their own offline workspace; provide review-note text without embedding credentials.
- [ ] Link only Apple/Electron primary references and mark every external gate as unchecked until observed.
- [ ] Run the documentation/readiness checker and commit.

### Task 6: Proportionate validation and handoff

**Files:**
- Modify only if validation reveals an in-scope defect.

- [ ] Run `rtk mise exec -- pnpm check:macos-store`.
- [ ] Run `rtk mise exec -- pnpm desktop:typecheck` and focused desktop/release tests.
- [ ] Run `rtk mise exec -- pnpm desktop:package` to prove development packaging is unchanged.
- [ ] Inspect the development app's Info.plist, signature, icon, privacy resource, packaged core path, and build manifest.
- [ ] If credentials are available, run the development-certificate MAS build and signed validation; otherwise record the exact skipped gate without claiming readiness.
- [ ] Run `rtk git diff --check`, check status, and provide the exact remaining Apple-account/operator checklist.
