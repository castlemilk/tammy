# macOS App Store release runbook

This runbook turns Tammy's repository-owned release profile into a Mac App Store candidate. The repository is prepared for this workflow; Apple credentials, legal decisions, signed-build inspection, upload, and App Review remain operator-owned gates.

Tammy uses bundle identifier `com.tammy.desktop`, category `public.app-category.finance`, and an arm64 Mac App Store build. The ordinary development package remains an ad-hoc signed `darwin` build. Never use development output as App Store evidence.

## Repository readiness

From a clean checkout with the pinned toolchain installed:

```sh
rtk mise exec -- pnpm install --frozen-lockfile
rtk mise exec -- pnpm check:macos-store
rtk mise exec -- pnpm desktop:typecheck
rtk mise exec -- pnpm desktop:package
```

`check:macos-store` validates the repository-owned bundle identity, category, icon, privacy manifest, sandbox entitlements, packaging profile, and metadata template. `desktop:package` is the ordinary local package smoke test; it does not produce an App Store build.

## One-time Apple setup

Complete these steps in the Apple Developer portal and App Store Connect. Do not store certificates, private keys, provisioning profiles, credentials, or session tokens in this repository.

- [ ] Confirm that the legal entity responsible for Tammy owns the Apple Developer membership and will submit the financial app.
- [ ] Register the explicit Mac App ID `com.tammy.desktop` with App Sandbox enabled.
- [ ] Create Apple Development and Apple Distribution signing certificates and a Mac Installer Distribution certificate (older keychains may show the legacy 3rd Party Mac Developer Installer name).
- [ ] Create separate Mac App Store development and distribution provisioning profiles for the App ID.
- [ ] Create the App Store Connect app record, using the primary Finance category.
- [ ] Publish HTTPS privacy-policy and support pages, then replace every `OPERATOR_REQUIRED` value in [store-metadata.md](../../apps/desktop/release/macos/store-metadata.md).
- [ ] Obtain and record the export-compliance determination for SQLCipher and local TLS. Set the build input and App Store Connect answers to the same determination.
- [ ] Assign a monotonically increasing positive decimal `CFBundleVersion` for every upload. Marketing version comes from `apps/desktop/package.json`.

Apple's current signing, provisioning, upload, metadata, screenshot, privacy, and review requirements remain authoritative. Re-check them for every release.

## Build inputs

The release command accepts only explicit inputs and never prints their values. Certificate identity names are selected from the operator's keychain; the provisioning profile must be an absolute path outside the repository. Finalize and commit the metadata worksheet before invoking the signed-build command: repository mode accepts its visible template placeholders, while `--release` rejects them.

```sh
export TAMMY_MACOS_BUILD_NUMBER='1'
export TAMMY_MACOS_EXPORT_COMPLIANCE='exempt' # or non-exempt, from the legal determination
export TAMMY_MACOS_PROVISIONING_PROFILE='/absolute/path/Tammy_MAS_Development.provisionprofile'
export TAMMY_MACOS_PRIVACY_POLICY_URL='https://example.com/tammy/privacy'
export TAMMY_MACOS_SIGNING_IDENTITY='Apple Development: Example Person (TEAMID1234)'
export TAMMY_MACOS_SIGNING_MODE='development'
export TAMMY_MACOS_SUPPORT_URL='https://example.com/tammy/support'
export TAMMY_MACOS_TEAM_ID='TEAMID1234'

rtk mise exec -- pnpm desktop:make:mas
```

Development mode produces `apps/desktop/out/Tammy-mas-arm64/Tammy.app`. Use it for a local sandbox smoke test. It deliberately does not produce an installer package.

For an upload candidate, use the distribution profile and identities:

```sh
export TAMMY_MACOS_BUILD_NUMBER='2'
export TAMMY_MACOS_EXPORT_COMPLIANCE='exempt' # or non-exempt
export TAMMY_MACOS_PROVISIONING_PROFILE='/absolute/path/Tammy_MAS_Distribution.provisionprofile'
export TAMMY_MACOS_PRIVACY_POLICY_URL='https://example.com/tammy/privacy'
export TAMMY_MACOS_SIGNING_IDENTITY='Apple Distribution: Example Company Pty Ltd (TEAMID1234)'
export TAMMY_MACOS_INSTALLER_IDENTITY='3rd Party Mac Developer Installer: Example Company Pty Ltd (TEAMID1234)'
export TAMMY_MACOS_SIGNING_MODE='distribution'
export TAMMY_MACOS_SUPPORT_URL='https://example.com/tammy/support'
export TAMMY_MACOS_TEAM_ID='TEAMID1234'

rtk mise exec -- pnpm desktop:make:mas
```

The command checks the clean tree, finalized metadata, and release inputs; rebuilds the Go core and authenticates its SQLCipher runtime before signing; verifies the Apple signature and records the signed core hash without executing that inherited child outside its sandbox parent; packages and signs the outer MAS app without signing the core a second time; verifies the actual `Tammy-mas-arm64` core/manifest equality; and uses Apple's `/usr/bin/productbuild` to create `apps/desktop/out/make/pkg/arm64/Tammy-<version>-build.<number>.pkg`. A distribution-signed app is for App Store upload; use the Apple Development build for local execution.

## Inspect the signed build

Set `APP` to the development or distribution app path and inspect the actual output:

```sh
APP='apps/desktop/out/Tammy-mas-arm64/Tammy.app'

rtk codesign --verify --deep --strict --verbose=2 "$APP"
rtk codesign -d --entitlements :- "$APP"
rtk security cms -D -i "$APP/Contents/embedded.provisionprofile"
rtk find "$APP/Contents" -type f -perm -111 -print
rtk spctl --assess --type execute --verbose=4 "$APP"
```

Verify, rather than assume, all of the following:

- [ ] The main app has App Sandbox, network client/server, and user-selected read-only file access.
- [ ] Electron helpers inherit the sandbox.
- [ ] `Contents/Resources/core/darwin-arm64/tammy-core` is signed with exactly App Sandbox and inheritance; it receives network-server access from the parent sandbox for the authenticated local transport.
- [ ] `ElectronTeamID`, the provisioning application identifier, and application groups agree for the main app and every nested executable.
- [ ] `PrivacyInfo.xcprivacy`, the build manifest, SQLCipher libraries, the core, and the icon are present.
- [ ] Every nested framework, helper, library, and executable has a valid Apple signature.
- [ ] Xcode's privacy report for the signed archive agrees with the manifest and the App Store privacy answers.

For a development-signed build, launch the app and exercise create/reopen workspace, recovery-code confirmation, sign-in, organisation setup, journal posting, document intake, bank reconciliation, BAS draft, and clean quit. Confirm the supervised core exits with the app and no background process remains.

For a distribution package:

```sh
PKG='apps/desktop/out/make/pkg/arm64/Tammy-0.1.0-build.2.pkg'
rtk pkgutil --check-signature "$PKG"
rtk spctl --assess --type install --verbose=4 "$PKG"
```

Record the `spctl` output as local Gatekeeper evidence. A pre-submission Mac App Store candidate may not be accepted like a notarized Developer ID build before Apple processing, so do not treat that local result as a substitute for App Store Connect validation.

## Metadata and App Review

Use [store-metadata.md](../../apps/desktop/release/macos/store-metadata.md) as the submission worksheet.

- [ ] Replace every operator placeholder; publish the in-app privacy statement's matching public policy and support URL.
- [ ] Open `/privacy` before sign-in and confirm both exact HTTPS links open in the external browser while every other new-window URL remains denied.
- [ ] Capture one to ten factual screenshots using only fictional business data. Apple currently accepts Mac screenshots at 1280×800, 1440×900, 2560×1600, or 2880×1800, without alpha.
- [ ] Keep “BAS draft — not lodged” visible and do not claim ATO lodgement, bank feeds, cloud OCR, or other deferred capability.
- [ ] State that Tammy has no remote account or demo credentials. Reviewers create an offline encrypted workspace and local owner in the app.
- [ ] Confirm the build contains no advertising, analytics, tracking, in-app purchases, separate licence key, downloaded executable code, or orphan background process.
- [ ] Use final copy and assets only—no beta labels, placeholder values, test contact details, or development menus.

Upload the signed `.pkg` with Apple's Transporter app, wait for App Store Connect processing, attach the resulting build to the version, complete privacy/export/age-rating declarations, and submit only after every checklist item has observed evidence.

## Release record and rollback

Record the commit, marketing version, build number, signing/profile names, package SHA-256, validation results, screenshots, privacy report, App Store Connect build identifier, and reviewer notes. Do not record private key material or credentials.

App Store rollback means selecting a previously approved version where Apple permits it or shipping a higher build/version with the corrective change. Never reuse an uploaded build number. Preserve the release record and exact source commit for every submitted candidate.

## Primary references

- [Electron Mac App Store submission guide](https://www.electronjs.org/docs/latest/tutorial/mac-app-store-submission-guide/)
- [Electron Packager options](https://electron.github.io/packager/main/interfaces/Options.html)
- [Apple App Sandbox](https://developer.apple.com/documentation/security/app-sandbox)
- [Apple user-selected file access](https://developer.apple.com/documentation/security/accessing-files-from-the-macos-app-sandbox)
- [Apple privacy manifests](https://developer.apple.com/documentation/bundleresources/privacy-manifest-files)
- [Apple required-reason APIs](https://developer.apple.com/documentation/bundleresources/describing-use-of-required-reason-api)
- [Apple build upload guidance](https://developer.apple.com/help/app-store-connect/manage-builds/upload-builds/)
- [Apple Mac screenshot specifications](https://developer.apple.com/help/app-store-connect/reference/app-information/screenshot-specifications/)
- [Apple App Review Guidelines](https://developer.apple.com/app-store/review/guidelines/)
