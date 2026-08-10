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

- use the stable bundle identifier `com.tammy.desktop` unless an operator intentionally supplies a different registered identifier;
- target the Electron `mas` platform and retain the bundled Go core and SQLCipher resources;
- require Apple distribution identity, team identifier, and provisioning profile inputs instead of falling back to ad-hoc signing;
- enable the App Sandbox with the minimum entitlements required by the current architecture;
- keep outbound Internet access disabled by product design while permitting only the local client/server transport needed between Electron and the supervised core;
- place the privacy manifest and licensed resources in the application bundle;
- preserve hardened Electron fuses and refuse a dirty or internally inconsistent release input;
- produce an application bundle suitable for Apple's validation/upload tooling.

Development and store profiles must not silently select one another. A normal developer package must remain easy to build, and a store package must fail early when release inputs are missing.

## Sandbox and child process design

The Go core is an embedded executable, not a separately downloadable component. The store profile signs the application, Electron helpers, and the core consistently with the application's sandbox. The entitlements allow the Electron process to connect to the loopback core and the core to listen only for the application's authenticated local transport. User-selected import/export locations use Apple user-selection permissions; Tammy's internal encrypted workspace remains in its application container.

The existing exact-path core supervision, build-manifest authentication, SQLCipher-only build, and clean-shutdown checks remain release gates. The store profile must not weaken them.

## Privacy and store metadata

The repository will include a minimal privacy manifest that declares no tracking and no collected data only to the extent supported by the current offline implementation. Any future telemetry, third-party SDK, networking, or data collection change must update the manifest and App Store privacy answers together.

The repository will also provide a concise store-metadata template covering name, subtitle, description, keywords, category, privacy URL, support URL, copyright, review notes, and screenshots. Fields requiring legal or business decisions remain visibly incomplete and block the final submission checklist.

## Release-readiness checker

A small deterministic script will validate repository-owned prerequisites without contacting Apple. It will check at least:

- app name, semantic version, bundle identifier, and Mac App Store profile;
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
3. a normal unsigned package plus the existing packaged end-to-end flow where practical;
4. a local unsigned or ad-hoc MAS-layout inspection when Electron tooling permits it without Apple credentials;
5. operator-only signed archive validation, Transporter upload, App Store processing, and TestFlight/App Review smoke tests.

No documentation or local checker may report the app as submitted or approved. The final handoff will state exactly which repository checks pass and which Apple-controlled gates remain.

## External prerequisites

The following cannot be truthfully completed from the repository alone:

- active Apple Developer Program membership and App Store Connect access;
- registered bundle identifier matching the release configuration;
- Mac App Distribution and Mac Installer Distribution certificates;
- App Store provisioning profile and the associated Team ID;
- final legal entity, copyright, pricing, tax/banking, support URL, privacy URL, and privacy questionnaire answers;
- approved product icon/brand direction and final App Store screenshots;
- App Review submission, review notes, and release decision.

These appear as a short operator checklist rather than being disguised as code defaults.

## Source references

- Electron, *Mac App Store Submission Guide*: <https://www.electronjs.org/docs/latest/tutorial/mac-app-store-submission-guide/>
- Apple, *App Sandbox*: <https://developer.apple.com/documentation/security/app-sandbox>
- Apple, *Accessing files from the macOS App Sandbox*: <https://developer.apple.com/documentation/security/accessing-files-from-the-macos-app-sandbox>
- Apple, *Privacy manifest files*: <https://developer.apple.com/documentation/bundleresources/privacy-manifest-files>
- Apple, *Upload builds*: <https://developer.apple.com/help/app-store-connect/manage-builds/upload-builds/>
