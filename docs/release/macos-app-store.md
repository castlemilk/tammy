# macOS App Store release runbook

This runbook turns Tammy's repository-owned release profile into a Mac App Store candidate. The repository is prepared for this workflow; Apple credentials, legal decisions, signed-build inspection, upload, and App Review remain operator-owned gates.

Tammy uses bundle identifier `com.tammy.desktop`, category `public.app-category.finance`, and an arm64 Mac App Store build. The ordinary development package remains an ad-hoc signed `darwin` build. Never use development output as App Store evidence.

## Repository readiness

From a clean checkout, install the pinned toolchain once and check the repository-owned profile:

```sh
mise install
mise exec -- task setup
mise exec -- task release:check
mise exec -- task release:state
```

`release:check` validates the repository-owned bundle identity, category, icon, privacy manifest, sandbox entitlements, packaging profile, and metadata template without signing credentials. `release:state` prints only the validated state, passed gates, and redacted blockers. Neither task signs, uploads, publishes, or submits. `mise exec -- task verify:release` adds the supported release-readiness verification but does not sign a candidate. `mise exec -- task package` remains an ordinary local package smoke test, not an App Store build.

## Apple-controlled setup and confirmations

The repository records the product identifiers and canonical copy, but it does not infer current Apple account state from those files. An accountable operator must observe and record each Apple-controlled fact for the exact build. Do not store certificates, private keys, provisioning profiles, credentials, session tokens, or receipt bodies in this repository.

The accountable Apple setup needs Apple Development and Apple Distribution certificate identities and separate Mac App Store development and distribution provisioning profiles for the explicit Mac App ID. Their Team ID, application identifier, app group, and keychain group must match the release profile; repository copy alone cannot prove that they exist or remain valid.

- **Seller eligibility and legal entity:** `OPERATOR_CONFIRMATION_REQUIRED`
- **Active agreements:** `OPERATOR_CONFIRMATION_REQUIRED`
- **Application and installer certificates:** `OPERATOR_CONFIRMATION_REQUIRED`
- **Distribution provisioning profile:** `OPERATOR_CONFIRMATION_REQUIRED`
- **Export compliance:** `OPERATOR_CONFIRMATION_REQUIRED`
- **Pricing and Australia availability:** `OPERATOR_CONFIRMATION_REQUIRED`
- **App privacy answer:** `OPERATOR_CONFIRMATION_REQUIRED`
- **Age rating:** `OPERATOR_CONFIRMATION_REQUIRED`
- **Processed build selection:** `OPERATOR_CONFIRMATION_REQUIRED`
- **Metadata and assets entered:** `OPERATOR_CONFIRMATION_REQUIRED`
- **App Store warning review:** `OPERATOR_CONFIRMATION_REQUIRED`

The explicit Mac App ID is `com.tammy.desktop` (`DXP9QHD7JH`), and the App Store Connect record is `6800226692` for **Tammy Accounting** 0.1.0. These identifiers do not prove seller eligibility, agreement status, declaration answers, build processing, or submission readiness. The public [privacy policy](https://tammy-accounting.castlemilk.chatgpt.site/privacy) and [support page](https://tammy-accounting.castlemilk.chatgpt.site/support) are bound to the immutable deployment record checked by `release:check`.

Reserve each positive decimal `CFBundleVersion` only after candidate-affecting repository changes are committed:

```sh
mise exec -- task release:reserve-build VERSION=0.1.0 OPERATOR='Accountable operator' NUMBER=1
```

The task passes the three explicit values to the ledger owner and never guesses a number. Marketing version comes from `apps/desktop/package.json`; never reuse a number across versions.

Apple's current signing, provisioning, upload, metadata, screenshot, privacy, and review requirements remain authoritative. Re-check them for every release.

## Build inputs

The signed Task scenarios accept only explicit inputs and never print their values. Certificate identity names are selected from the operator's keychain; the provisioning profile must be an absolute path outside the repository. Finalize and commit the repository-owned metadata before invoking a signed-build scenario. The visible `OPERATOR_CONFIRMATION_REQUIRED` lines are deliberate gates and remain until immutable accountable attestations are recorded; they are not product-copy placeholders.

Record the accountable export-compliance determination for the exact release as either exempt or non-exempt before signing. The current repository profile emits `ITSAppUsesNonExemptEncryption: false` only for the recorded exempt branch.

```sh
export TAMMY_MACOS_BUILD_NUMBER='1'
export TAMMY_MACOS_EXPORT_COMPLIANCE='exempt' # or non-exempt, from the legal determination
export TAMMY_MACOS_PROVISIONING_PROFILE='/absolute/path/Tammy_MAS_Development.provisionprofile'
export TAMMY_MACOS_PRIVACY_POLICY_URL='https://tammy-accounting.castlemilk.chatgpt.site/privacy'
export TAMMY_MACOS_SIGNING_IDENTITY='Apple Development: Example Person (TEAMID1234)'
export TAMMY_MACOS_SUPPORT_URL='https://tammy-accounting.castlemilk.chatgpt.site/support'
export TAMMY_MACOS_TEAM_ID='TEAMID1234'

mise exec -- task release:development
```

`release:development` forces development signing and produces `apps/desktop/out/Tammy-mas-arm64/Tammy.app`. Use it for a local sandbox smoke test. It deliberately does not produce an installer package or upload.

For an upload candidate, use the distribution profile and identities:

```sh
export TAMMY_MACOS_BUILD_NUMBER='2'
export TAMMY_MACOS_EXPORT_COMPLIANCE='exempt' # or non-exempt
export TAMMY_MACOS_PROVISIONING_PROFILE='/absolute/path/Tammy_MAS_Distribution.provisionprofile'
export TAMMY_MACOS_PRIVACY_POLICY_URL='https://tammy-accounting.castlemilk.chatgpt.site/privacy'
export TAMMY_MACOS_SIGNING_IDENTITY='Apple Distribution: Example Company Pty Ltd (TEAMID1234)'
export TAMMY_MACOS_INSTALLER_IDENTITY='Mac Installer Distribution: Example Company Pty Ltd (TEAMID1234)'
export TAMMY_MACOS_SUPPORT_URL='https://tammy-accounting.castlemilk.chatgpt.site/support'
export TAMMY_MACOS_TEAM_ID='TEAMID1234'

mise exec -- task release:candidate
# Equivalent candidate alias; it never uploads:
mise exec -- task deploy:mas
```

`release:candidate` and `deploy:mas` force distribution signing. They check the clean tree, finalized metadata, and release inputs; rebuild the Go core and authenticate its SQLCipher runtime before signing; verify the Apple signature and record the signed core hash without executing that inherited child outside its sandbox parent; package and sign the outer MAS app without signing the core a second time; verify the actual `Tammy-mas-arm64` core/manifest equality; and use Apple's `/usr/bin/productbuild` to create `apps/desktop/out/make/pkg/arm64/Tammy-<version>-build.<number>.pkg`. They print local package evidence and never upload. A distribution-signed app is for App Store upload; use the Apple Development build for local execution.

Before an accountable operator uploads or submits, run the matching read-only state gate:

```sh
mise exec -- task release:pre-upload-check
mise exec -- task release:pre-submit-check
```

These tasks do not sign, upload, publish, or submit. They fail unless the exact release record has reached `PRE_UPLOAD_READY` or `PRE_SUBMIT_READY`; a failure names the missing repository, seller, candidate, or Apple-controlled evidence without printing sensitive inputs.

## Inspect the signed build

Set `APP` to the development or distribution app path and inspect the actual output:

```sh
APP='apps/desktop/out/Tammy-mas-arm64/Tammy.app'

codesign --verify --deep --strict --verbose=2 "$APP"
codesign -d --entitlements :- "$APP"
security cms -D -i "$APP/Contents/embedded.provisionprofile"
find "$APP/Contents" -type f -perm -111 -print
spctl --assess --type execute --verbose=4 "$APP"
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
pkgutil --check-signature "$PKG"
spctl --assess --type install --verbose=4 "$PKG"
```

Record the `spctl` output as observational local Gatekeeper evidence. A pre-submission Mac App Store candidate may not be accepted like a notarized Developer ID build before Apple processing, so do not treat that local classification as a substitute for App Store Connect validation.

## Metadata and App Review

Use [store-metadata.md](../../apps/desktop/release/macos/store-metadata.md) as the submission worksheet.

- [ ] Confirm the canonical privacy and support URLs still match the passing immutable public-site record; do not substitute mutable or repository-hosted URLs.
- [ ] Record accountable attestations for seller eligibility, export, pricing, privacy, age rating, processed build, entered metadata/assets, and warning review without storing credentials or session material.
- [ ] Open `/privacy` before sign-in and confirm both exact HTTPS links open in the external browser while every other new-window URL remains denied.
- [ ] Capture one to ten factual screenshots using only fictional business data. Apple currently accepts Mac screenshots at 1280×800, 1440×900, 2560×1600, or 2880×1800, without alpha.
- [ ] Keep “BAS draft — not lodged” visible and do not claim ATO lodgement, bank feeds, cloud OCR, or other deferred capability.
- [ ] State that Tammy has no remote account or demo credentials. Reviewers create an offline encrypted workspace and local owner in the app.
- [ ] Confirm the build contains no advertising, analytics, tracking, in-app purchases, separate licence key, downloaded executable code, or orphan background process.
- [ ] Use final copy and assets only—no beta labels, placeholder values, test contact details, or development menus.

Manually upload the signed `.pkg` with Apple's Transporter app, wait for App Store Connect processing, attach the resulting build to the version, complete privacy/export/age-rating declarations, and submit only after every checklist item has observed evidence. Task scenarios, including `deploy:mas`, never upload or call App Store Connect.

## Release record and rollback

Record the commit, marketing version, build number, signing/profile names, package SHA-256, validation results, screenshots, privacy report, App Store Connect build identifier, and reviewer notes. Do not record private key material or credentials.

The public privacy/support site has a separate immutable record under `docs/release/public-site`. A successful public deployment exclusively creates `deployments/<deployment-id>.json`; `current.json` is only an atomic pointer to the latest passing record. Credentials, source-write URLs, account-user IDs, tokens, and mutable deployment URLs never belong in those files.

To roll the public site back, first confirm that at least one distinct prior deployment file has a passing result for `/`, `/privacy`, and `/support`. Select that immutable prior Sites version intentionally, deploy it, and poll the returned deployment ID until it succeeds or fails. After success, run `mise exec -- task site:post-deploy-check SITE_ORIGIN=<exact-canonical-origin>`. Exclusively create one immutable rollback event under `docs/release/public-site/events` that identifies the new deployment ID/version/time, the version rolled back from, the prior version restored, the exact prior deployment-evidence path, and the new passing three-route result. Atomically update `current.json` only after the event and passing deployment record exist. Never delete or modify an earlier deployment or rollback file. If deployment or route verification fails, leave `current.json` and App Store metadata unchanged.

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
