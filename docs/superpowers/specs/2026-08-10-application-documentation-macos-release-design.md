# Application documentation and Mac App Store readiness design

**Date:** 10 August 2026  
**Status:** Approved for implementation  
**Scope:** Documentation, local release tooling, and repository-owned Mac App Store preparation

## Goal

Turn the repository into a clear handoff for the next developer and make the macOS application reproducibly ready for Apple signing and submission without replacing the existing Electron Forge, Go, SQLCipher, pnpm, or mise toolchain.

The work must describe the application as it exists today, not as an early foundation prototype. It must also distinguish repository-owned readiness from Apple-account actions that cannot be completed without Tammy's legal, commercial, and signing details.

## Non-goals

- Do not introduce another build system, release service, updater, telemetry stack, or cloud backend.
- Do not upload a build, create App Store Connect records, request certificates, or invent legal/support URLs.
- Do not call an unsigned development package production-ready.
- Do not broaden the deferred security-validation work or repeat long SQLCipher stress loops as part of this documentation task.
- Do not add Windows release work beyond retaining the current development packaging path.

## Documentation bundle

The repository will have five short, linked sources of truth:

1. `README.md` gives the product summary, current maturity, quick start, principal commands, and links to deeper guidance.
2. `docs/development/tech-state.md` records what is implemented and exercised, what is development-only, the major architectural boundaries, and known remaining work.
3. `docs/development/foundation.md` becomes the practical developer handbook: setup, repository map, local workspace behavior, contract generation, tests, troubleshooting, and safe change workflow.
4. `docs/development/local-accounting-walkthrough.md` describes the current setup-to-accounting journey and the expected observable outcomes.
5. `docs/release/macos-app-store.md` is the operator runbook for identity, signing, sandbox packaging, validation, upload, store metadata, and rollback.

The tech-state document is authoritative for current capability status. Historical design and implementation plans remain evidence of intent and decisions, but they are not treated as current-state documentation.

## Current application boundary

Tammy is a local-first Australian accounting desktop application. The current vertical slice includes an encrypted local workspace, setup and reopen, organisation and chart-of-accounts setup, manual journals, trial balance, document review, banking/reconciliation, BAS drafting, and local audit/activity surfaces. The Electron application supervises a packaged Go core and communicates over the existing authenticated local transport.

The release documentation will explicitly identify incomplete or external areas. In particular, a successful local package is not App Store approval; ATO/SBR production submission and other external assurance remain separate evidence boundaries; Apple account, legal, privacy, support, and commercial inputs remain operator-owned.

## Packaging profiles

The existing development profile remains available and simple:

- local Electron Forge start/package commands;
- ad-hoc signing for development;
- ZIP output for local macOS testing;
- existing packaged end-to-end workflow.

A separate Mac App Store profile will be selected explicitly by a release command. It will:

- use the pinned bundle identifier `com.tammy.desktop`; changing it requires a reviewed source change covering helpers, profiles, metadata, and App Store records;
- target the Electron `mas` platform and retain the bundled Go core and SQLCipher resources;
- require an Apple Development identity and development provisioning profile for a locally runnable sandbox test, or an Apple Distribution identity and distribution profile for upload, instead of falling back to ad-hoc signing;
- require a positive decimal `CFBundleVersion` build number distinct from the semantic release version and supplied for every upload;
- enable the App Sandbox with the minimum entitlements required by the current architecture;
- keep outbound Internet use absent from product code while enabling the coarse sandbox network entitlements required by the authenticated loopback transport;
- place the privacy manifest and licensed resources in the application bundle;
- preserve hardened Electron fuses and refuse a dirty or internally inconsistent release input;
- produce an application bundle suitable for Apple's validation/upload tooling.

Development and store profiles must not silently select one another. A normal developer package must remain easy to build, and a store package must fail early when release inputs are missing.

## Sandbox and child process design

The Go core is an embedded executable, not a separately downloadable component. The store command authenticates SQLCipher while the freshly built core can still run standalone, signs and verifies the core, re-hashes those signed bytes without executing the inherited child outside its sandbox parent, excludes the manifest-bound core from a second signing pass, and then signs the outer application, Electron helpers, frameworks, and dynamic libraries. The main app has the sandbox, network-client/server, and user-selected-read entitlements. Ordinary helpers and the core are signed with exactly sandbox inheritance, so the core inherits the parent's server permission. Signing supplies and later verifies the exact `ElectronTeamID`, embedded provisioning profile, helper identifiers, and application-group identity required by Electron.

Apple's network entitlements are not loopback-scoped, so they enable client access for Electron and server access for the core while Tammy's existing transport validation continues to constrain product behavior to the authenticated loopback channel. User-selected import locations use Apple's user-selection permission; Tammy's internal encrypted workspace remains in its application container. The current shipped UI does not persist an external export destination. Any future restart-resumable export feature must use application-scoped security-scoped bookmarks with stale-bookmark refresh and restart tests before it can be enabled in the store build.

The existing exact-path core supervision, build-manifest authentication, SQLCipher-only build, and clean-shutdown checks remain release gates. The store profile must not weaken them.

## Privacy and store metadata

The repository will include a minimal privacy manifest that declares no tracking and no collected data only to the extent supported by the current offline implementation. It will also declare the approved reasons for required-reason APIs Tammy directly uses to manage files in its container or files the user selected. An Xcode privacy report remains a final signed-build gate because bundled Electron and native code must be assessed as shipped. Any future telemetry, third-party SDK, networking, or data collection change must update the manifest and App Store privacy answers together. The app exposes its privacy statement on a route available before sign-in. Store builds require exact HTTPS privacy and support URLs, render them in-app, and allow only those two URLs to open in the external browser.

The repository will also provide a concise store-metadata template covering name, subtitle, description, keywords, category, privacy URL, support URL, copyright, review notes, and screenshots. Fields requiring legal or business decisions remain visibly incomplete and block the final submission checklist.

Because Tammy bundles SQLCipher and TLS, release metadata will include an explicit operator decision for export compliance and the matching `ITSAppUsesNonExemptEncryption` and App Store Connect answers. The repository will not guess the legal conclusion.

## Release-readiness checker

A small deterministic script will validate repository-owned prerequisites without contacting Apple. It will check at least:

- app name, semantic version, positive decimal build number, pinned bundle identifier, and Mac App Store profile;
- required entitlements and privacy manifest structure;
- application icon source/output presence and expected dimensions;
- required release documentation and operator placeholders;
- clean generated/build metadata expectations;
- presence, but never contents, of externally supplied signing inputs when store packaging is requested.

The checker will expose a read-only `check:macos-store` command. A separate store packaging command may require signing variables and should fail with direct guidance when they are absent.

## Verification

Implementation validation is proportionate and layered:

1. unit tests for release-profile selection and the readiness checker;
2. desktop typecheck and relevant Electron tests;
3. a normal ad-hoc signed macOS package plus the existing packaged end-to-end flow where practical;
4. a static MAS-layout inspection without claiming sandbox runtime evidence;
5. an Apple Development-signed MAS launch test on a provisioned Mac;
6. an Apple Distribution-signed `mas` app, followed by a Mac Installer Distribution-signed `.pkg` produced with `/usr/bin/productbuild`;
7. operator-only `codesign`/`pkgutil`/`spctl` validation, Transporter upload, App Store processing, and TestFlight/App Review smoke tests.

No documentation or local checker may report the app as submitted or approved. The final handoff will state exactly which repository checks pass and which Apple-controlled gates remain.

## External prerequisites

The following cannot be truthfully completed from the repository alone:

- active Apple Developer Program membership and App Store Connect access;
- registered bundle identifier matching the release configuration;
- Apple Development and Apple Distribution certificates, plus Mac Installer Distribution when producing the upload `.pkg` (older keychains may use the legacy “3rd Party Mac Developer Installer” name);
- App Store provisioning profile and the associated Team ID;
- a unique positive decimal build number greater than the latest App Store Connect upload;
- final legal entity, copyright, pricing, tax/banking, support URL, privacy URL, and privacy questionnaire answers;
- approved product icon/brand direction and final App Store screenshots;
- a signed-build Xcode privacy report confirming every shipped required-reason API declaration;
- an export-compliance determination for the bundled SQLCipher/TLS cryptography and matching App Store Connect answers;
- App Review submission, review notes, and release decision.

These appear as a short operator checklist rather than being disguised as code defaults.

## Source references

- Electron, *Mac App Store Submission Guide*: <https://www.electronjs.org/docs/latest/tutorial/mac-app-store-submission-guide/>
- Apple, *App Sandbox*: <https://developer.apple.com/documentation/security/app-sandbox>
- Apple, *Accessing files from the macOS App Sandbox*: <https://developer.apple.com/documentation/security/accessing-files-from-the-macos-app-sandbox>
- Apple, *Privacy manifest files*: <https://developer.apple.com/documentation/bundleresources/privacy-manifest-files>
- Apple, *Upload builds*: <https://developer.apple.com/help/app-store-connect/manage-builds/upload-builds/>
