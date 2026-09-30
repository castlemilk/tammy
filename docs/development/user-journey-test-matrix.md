# User journey test matrix

This matrix keeps Tammy's development walkthrough tied to executable evidence. It is a product-journey checklist, not App Store approval, external accounting assurance, or a replacement for the separate security review.

## Coverage layers

- **Development Electron** proves the electron-vite main, preload, and app outputs launch together with the real supervised Core before packaging.
- **Packaged Electron** proves the ordinary electron-builder app authenticates its ASAR, Electron fuses, resources, and signature state; launches offline; exposes only the fixed `window.conveyor` bridge; and exits without an orphaned Core.
- **App journeys** exercise screens through the allowlisted Conveyor modules with typed protobuf data. These tests cover interaction, validation, state labels, and failure presentation without replacing Core integration.
- **Local-core integration** runs generated Connect clients through the real TLS server and SQLCipher workspace where a test is listed below.

## Current journeys

| Journey | App evidence | Local-core evidence | Desktop runtime evidence |
| --- | --- | --- | --- |
| Launch offline and review privacy | `app.test.tsx`, privacy tests | System transport tests | `electron-vite-development.spec.ts`, `foundation.spec.ts`, `app-store-privacy.spec.ts` |
| Create workspace, save recovery, sign in, create organisation | `setup-screen.test.tsx` | `TestLocalCompositionCreatesConfirmsAndAuthenticatesRealWorkspace` | First-run form and Conveyor smoke |
| Reopen workspace and sign in | `unlock-screen.test.tsx` | `TestLocalCompositionReopensAndAuthenticatesExistingWorkspaceAfterRestart` | Not destructive-tested in the package suite |
| Inspect and extend the AU chart | `chart-screen.test.tsx` | `TestLedgerModuleCreatesOrganisationAndInstallsAustralianChartThroughRealServer` | Covered after authentication, not in first-run smoke |
| Post a journal, open its detail, and inspect the trial balance | `journals-screen.test.tsx`, `trial-balance-screen.test.tsx` | `TestLedgerModuleCreatesOrganisationAndInstallsAustralianChartThroughRealServer` | Covered after authentication, not in first-run smoke |
| Retain and review a document | `documents-screen.test.tsx`, PDF text tests | `TestLedgerModuleCreatesOrganisationAndInstallsAustralianChartThroughRealServer` | Covered after authentication, not in first-run smoke |
| Import, match, then reconcile banking rows | `banking-screen.test.tsx` | Repository/module integration is the next backend journey gap | Covered after authentication, not in first-run smoke |
| Create and review a BAS workpaper | `bas-screen.test.tsx` | Reporting/module integration is the next backend journey gap | Covered after authentication, not in first-run smoke |
| Review retained local activity | `audit-screen.test.tsx` | The view is a product projection; audit-chain verification is tested separately | Covered after authentication, not in first-run smoke |

The journal list regression also proves retained rows are keyboard focusable and open with Enter, rather than being mouse-only.

## Commands

From the repository root:

```sh
rtk mise exec -- task test
rtk mise exec -- task test:desktop:dev
rtk mise exec -- task package:e2e
```

The complete desktop test command includes an integration suite that compiles the Go core with a fixed timeout. A timeout is a failed gate, not permission to mark the suite green. `test:desktop:dev` exercises `apps/desktop/out/main/index.js`, `apps/desktop/out/preload/index.cjs`, and `apps/desktop/out/renderer/index.html` through real Electron. Focused app results may still be reported separately while the build environment is repaired.

For the direct local-core journeys:

```sh
rtk go test -tags tammy_sqlcipher ./services/core/internal/app \
  -run '^TestLocalComposition(CreatesConfirmsAndAuthenticatesRealWorkspace|ReopensAndAuthenticatesExistingWorkspaceAfterRestart)$' \
  -count=1 -timeout=3m

rtk go test -tags tammy_sqlcipher ./services/core/internal/localproduct \
  -run '^TestLedgerModuleCreatesOrganisationAndInstallsAustralianChartThroughRealServer$' \
  -count=1 -timeout=3m
```

Keep these commands bounded and serial. Do not repeatedly launch duplicate SQLCipher builds when one is still active.

## Release rule

Do not describe a journey as packaged-E2E verified unless `task package:e2e` completes against the newly built Builder application at `.tmp/electron-builder/ordinary/mac-arm64/Tammy.app` or `.tmp/electron-builder/ordinary/win-unpacked/Tammy.exe`. App-test success, development Playwright discovery, or an older package is not equivalent evidence.
