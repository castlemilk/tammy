# macOS App Store prime-time readiness design

**Date:** 30 August 2026  
**Status:** Approved; specification review iteration 1  
**Scope:** Public compliance pages, truthful App Store assets and metadata, signed macOS release evidence, and submission-readiness tooling

## Goal

Make Tammy's existing macOS App Store release path ready for a production review submission. The result must give Apple a complete, internally consistent product page; public privacy and support destinations; representative screenshots made from the real app; an inspectable signed package; and concise reviewer instructions that match the submitted build.

This work matures the release boundary around the current product. It does not turn deferred capabilities into production claims. Tammy remains a local-first Australian accounting application whose current store build supports encrypted workspaces, organisation setup, journals, source-document review, bank-statement reconciliation, GST/BAS drafts, and retained local activity. BAS and reporting output are preparation-only. Production ATO/SBR submission and company tax return lodgement are not available in this release.

## Product and legal identity

The release uses these canonical values:

- App Store name: **Tammy Accounting**
- Installed name: **Tammy**
- Bundle identifier: `com.tammy.desktop`
- Publisher and privacy-policy owner: **Gamma Systems Pty Ltd**
- Public support contact: `ben.ebsworth@gmail.com`
- Locale: English (Australia)
- Primary category: Finance
- Secondary category: Business
- Initial price and availability: Free in Australia
- Marketing version: `0.1.0`, read from `apps/desktop/package.json`
- Minimum system: macOS 14 or later on Apple silicon (`arm64`)
- Copyright: `© 2026 Gamma Systems Pty Ltd`

The release checker must keep the repository copy, packaged `Info.plist`, public links, App Store worksheet, privacy policy, screenshots, and release evidence consistent with these values. `LSMinimumSystemVersion`, architecture, marketing version, build number, and copyright are candidate evidence, not documentation-only claims.

Gamma Systems Pty Ltd ownership is a legal submission prerequisite, not a string-replacement exercise. Before the site represents Gamma Systems Pty Ltd as publisher, an authorised company controller records a redacted attestation that the company controls the policy and support address. Before upload, the operator must verify the Apple Developer membership type, App Store Connect seller name, Account Holder, active agreements, copyright authority, and any authorization Apple requires for the app's accounting/financial scope. The pre-upload check blocks if that attestation is absent or if the seller is still an individual without documented company authority. The repository reports the mismatch; it never edits Apple account identity or treats a matching display string as evidence.

## Non-goals

- Do not add TestFlight invitation, wait-list, lead-capture, analytics, advertising, tracking, or a marketing database.
- Do not build a broad promotional website, app preview video, or Remotion production in this pass.
- Do not claim ATO lodgement, production SBR, live bank feeds, cloud OCR, company tax return submission, or other deferred capability.
- Do not automate the final App Review declaration or release decision where Apple requires an accountable human answer.
- Do not store Apple credentials, signing private keys, provisioning profiles, session cookies, or App Store Connect tokens in the repository or release evidence.
- Do not change the product architecture or add cloud services merely to support the public pages.
- Do not call a repository check, local package, or successful upload an App Store approval.

## Deliverables

The implementation produces one coherent submission-readiness bundle:

1. A small public Sites project with a concise product home page and canonical `/privacy` and `/support` routes.
2. An updated in-repository privacy policy and in-app privacy/support wording owned by Gamma Systems Pty Ltd.
3. Final English (Australia) App Store metadata, privacy answers, export-compliance notes, age-rating guidance, review notes, and screenshot captions.
4. Five deterministic Mac screenshots at one Apple-accepted 16:10 size, created from the real packaged UI using fictional business data.
5. Release tasks that generate or validate screenshots, build the signed candidate, collect non-secret package evidence, and report submission blockers.
6. A versioned release-record template that links the source commit, build number, package hash, signing/profile names, privacy report, screenshots, App Store Connect build, and reviewer outcome.
7. Updated release documentation that gives an operator one ordered path from a clean checkout to App Store Connect submission.
8. A repository-owned build-number ledger and redacted operator-attestation format for the current release state.

## Public compliance site

### Structure

The site lives as a dedicated workspace application under `apps/site` and uses the Sites scaffold and hosting contract. It is static and has no authentication, database, uploads, cookies, tracking, analytics, or form submission.

- `/` gives a restrained product summary, supported macOS scope, current accounting capabilities, local-first data posture, and links to privacy and support.
- `/privacy` publishes the canonical privacy policy for the current macOS release.
- `/support` identifies Tammy and Gamma Systems Pty Ltd, links to `mailto:ben.ebsworth@gmail.com`, gives safe diagnostic information to include, warns users not to send accounting data, credentials, recovery codes, machine credentials, or private keys, and links back to privacy.

The home page is a release-support surface, not a speculative sales site. It uses Tammy's established warm off-white, deep forest green, restrained borders, serif wordmark, and calm, precise copy. It may show one factual product screenshot after the screenshot set exists. No capability claim may exceed the store description or the submitted build.

### Canonical content and links

The repository privacy policy remains the editable source of legal content and is rendered into the site rather than copied into an independently maintained page. It separates three boundaries:

- the Tammy app does not transmit accounting records, credentials, analytics, advertising identifiers, or tracking data to Gamma Systems Pty Ltd or third parties in this release;
- the public site has no app-owned analytics, cookies, account, or form, while its hosting provider may process ordinary request/security logs under the provider's terms; and
- email support is user-initiated and its content is processed by the sender's and recipient's email providers, so users must not send accounting records, credentials, recovery codes, machine credentials, or private keys.

The policy states exactly what deletion does and does not do. Removing the app alone does not claim to delete workspaces or Keychain entries. `/support` provides the tested macOS 14 procedure for closing Tammy, removing selected workspace/application-container data, and removing only Tammy-owned Keychain items. The procedure identifies the relevant Tammy service names without exposing secret contents and warns that deletion is irreversible. A release test creates a disposable workspace and isolated Tammy Keychain items, follows the same deletion owner, and proves that workspace, catalogue, remembered-workspace, and Tammy SBR-vault items are removed without touching unrelated Keychain entries.

A deterministic consistency test prevents the rendered policy, site identity, in-app summary, and store metadata from drifting from the canonical source. Tests assert structured policy sections and identifiers; they do not rely on an undefined notion of “material” textual equivalence.

After public deployment, the resulting HTTPS `/privacy` and `/support` URLs replace the GitHub links in the App Store worksheet and become the exact `TAMMY_MACOS_PRIVACY_POLICY_URL` and `TAMMY_MACOS_SUPPORT_URL` inputs embedded in the candidate. Tammy continues to allow only those validated URLs to open externally from the store build.

The site is published publicly because Apple reviewers and customers must be able to reach both pages without authentication. The user has approved an initial public Sites URL; a custom domain is deferred.

## App Store metadata package

`apps/desktop/release/macos/store-metadata.md` remains the human-readable submission worksheet. It is updated to contain final product copy and explicit operator gates rather than stale GitHub URLs or misleading completed checkboxes.

The worksheet covers:

- name, subtitle, description, keywords, categories, locale, price, availability, copyright, privacy URL, support URL, and optional marketing URL;
- App Privacy recommendation: no data collected and no tracking for this offline build, subject to candidate-bound manifest, dependency, and runtime-egress evidence plus an accountable operator attestation;
- export-compliance determination aligned with `ITSAppUsesNonExemptEncryption` and the candidate input;
- age-rating answers based on the actual build, with no Kids Category selection;
- review contact and step-by-step offline setup instructions requiring no remote demo account;
- an honest explanation of the supervised loopback core, imported files, local encryption, clean quit, and preparation-only reporting scope;
- a declaration checklist for advertising, analytics, tracking, in-app purchases, licence keys, downloaded executable code, and background processes; and
- the exact five screenshot filenames and captions in upload order.

### Readiness states and attestations

Repository checks use five explicit states instead of one circular “ready” flag:

1. `REPOSITORY_READY` requires canonical identity/version/platform facts, final copy, public content, screenshot definitions, schemas, and tests.
2. `CANDIDATE_READY` additionally requires one exact clean source commit, reserved build number, signed app/package hashes, signing/profile inspection, public URL match, privacy evidence, runtime egress evidence, and candidate-linked screenshots.
3. `PRE_UPLOAD_READY` additionally requires the company-controller, seller/agreement, content-rights, export, pricing/availability, and privacy-answer attestations.
4. `UPLOADED` is recorded only after App Store Connect accepts the package and supplies a build identifier.
5. `PRE_SUBMIT_READY` additionally requires processed-build selection, uploaded screenshots/metadata, completed App Privacy/export/age-rating declarations, and a final App Store Connect omission/warning review. Submission and review outcomes are later release-record events, never prerequisites for `PRE_SUBMIT_READY`.

Redacted operator attestations use a versioned JSON schema. Every entry contains a fixed attestation kind, release version/build, UTC timestamp, accountable person's name, non-secret evidence reference, and outcome. The schema rejects free-form credential material and known secret fields. Repository checks may recommend answers but never synthesize operator attestations.

## Screenshot production

### Source and data safety

Screenshots must come from Tammy's actual packaged renderer, not a recreated mockup. A dedicated Playwright release scenario creates a fresh temporary encrypted workspace and deterministic fictional Australian business named **Wattle & Co Supplies Pty Ltd**. All people, ABNs, bank references, invoices, documents, balances, dates, and identifiers are explicitly fictional and contain no developer or customer data.

The scenario uses production-like packaged application code with test-only orchestration outside the shipped bundle. Fixture provenance is committed as structured data. Business names are unmistakably fictional; displayed identifiers either come from an ATO-published test-only set with a cited source or are omitted. The fixture validator rejects developer/customer names, real support addresses in transaction data, production credential formats, unapproved ABNs, and common secret patterns before capture.

It must not enable a hidden reviewer mode, bypass product security in the release build, or leave seeded records in a developer's normal workspace. Candidate inspection proves that screenshot orchestration modules, fixture files, Playwright hooks, test environment switches, and screenshot-only labels are absent from the packaged application resources and executable strings.

### Output

The canonical set uses PNG without alpha at exactly 1440×900 pixels. The capture owner fixes Electron window content bounds, display scale, font readiness, reduced-motion setting, locale, timezone, and deterministic data clock before capture; it then verifies the encoded pixel dimensions. A validator rejects mixed dimensions, alpha, missing files, unexpected filenames, non-PNG output, and a count outside Apple's one-to-ten range.

The five images are:

1. `01-overview.png` — accounting overview with documents, banking, and reporting attention.
2. `02-document-review.png` — reviewed source document and extracted accounting details.
3. `03-journal-trial-balance.png` — balanced journal linked to the resulting trial balance.
4. `04-bank-reconciliation.png` — imported statement lines and a clear reconciliation state.
5. `05-bas-draft.png` — GST/BAS workpaper with an unmistakable **draft — not lodged** boundary.

Capture writes to a new temporary directory and replaces the canonical set only after all five images, the sensitive-data scan, and the manifest pass. Failure preserves the last validated set. The manifest contains locale, dimensions, fixture provenance/hash, source commit, app version, build number, development-signed app hash, capture timestamp, filename, image hash, and caption. After the distribution candidate exists, finalization links the manifest to its package SHA-256 while explicitly retaining `capture_artifact_kind: development-signed-app`; it never claims that the non-runnable distribution package produced the screenshots. Screenshot generation or candidate linking does not imply that Apple has accepted the assets.

## Signed candidate and evidence

The existing Mac App Store profile remains the sole candidate builder. It continues to require an arm64 Mac, a monotonically increasing `CFBundleVersion`, an explicit export-compliance value, a distribution provisioning profile outside the repository, matching Apple Distribution and Mac Installer Distribution identities, the Team ID, and the exact deployed privacy/support URLs.

Candidate production must:

- refuse a dirty or internally inconsistent source tree;
- rebuild the Go core and SQLCipher inputs from pinned source;
- authenticate the manifest-bound core before and after signing;
- preserve sandbox inheritance for Electron helpers and the bundled core;
- verify the main bundle, nested code, entitlements, embedded profile, application identifier, Team ID, privacy manifest, icon, and public URLs;
- create the signed installer package with `productbuild`;
- validate the package signature and record local Gatekeeper output as observational evidence only; and
- emit a SHA-256 digest and release-evidence record without exposing credential material.

The evidence collector first writes under a gitignored version/build staging directory. It records commands and outcomes in structured JSON plus a readable summary. It may record certificate/profile display names, expiry dates, identifiers, and hashes, but never private keys, certificate exports, passwords, tokens, profile contents, or environment values. It fails closed when required evidence is missing, stale, belongs to another commit/build, or contradicts the repository metadata. Complete non-secret evidence is promoted into the durable release record defined below.

### Candidate-bound privacy and network evidence

The App Privacy recommendation is based on the exact signed candidate rather than an unbound Xcode report. Candidate inspection creates `privacy-evidence.json` containing the package SHA-256, signed app hash, source commit, version/build, every bundled `PrivacyInfo.xcprivacy` path and hash, executable/framework/dylib inventory, native dependency inventory, and production JavaScript package inventory. Static policy checks reject known updater, crash-reporting, telemetry, advertising, tracking, remote-code, or undeclared network SDKs unless the privacy/store design is revised before release.

A packaged runtime test runs the development-signed equivalent from the same commit/version/build with all non-loopback egress denied and audited. It exercises setup, sign-in, documents, banking, BAS draft, idle time, external privacy/support links, and clean quit. Only the authenticated loopback core channel and the two explicit user-initiated external link handoffs may occur; silent update, crash, telemetry, analytics, and background egress attempts fail the gate. The evidence records hashes and observed destinations but no accounting content or secrets.

If current Apple/Xcode tooling can generate a privacy report for the exact Electron `.app` or `.pkg`, the operator attaches it and its hash. It is supplemental, not mislabelled as an archive Tammy does not produce. The privacy attestation cites the candidate-bound inventory, runtime egress result, manifest inspection, and any Apple report before recommending **No data collected** and **No tracking**.

## Task scenarios

The top-level Taskfile exposes scenario-oriented commands while keeping individual checks composable:

- `task site:dev`, `task site:test`, and `task site:build` operate the public compliance site locally.
- `task release:check` validates repository-owned Mac App Store inputs without credentials.
- `task release:screenshots` creates the canonical fictional screenshot set.
- `task release:screenshots:check` validates screenshot count, format, size, manifest, and captions without recapturing.
- `task release:development` produces the locally runnable sandboxed development app when development signing inputs exist.
- `task release:candidate` produces and locally validates the signed distribution package but never uploads it.
- `task release:evidence` collects or refreshes non-secret evidence for an existing exact candidate.
- `task release:pre-upload-check` requires `REPOSITORY_READY`, `CANDIDATE_READY`, and the pre-upload attestations.
- `task release:pre-submit-check` runs only after an upload/build identifier has been recorded and requires the App Store Connect declarations/assets attestation; it never requires a submission or approval result.

Task summaries explicitly state whether they build, validate, capture, publish, upload, or submit. No task named `deploy` or `release` may silently perform an App Store Connect upload or public Sites deployment.

## App Store Connect handoff

Once the public URLs, screenshots, metadata, signed candidate, evidence, and `PRE_UPLOAD_READY` state pass, the operator follows one ordered checklist:

1. Verify the Apple Developer/App Store Connect seller and agreements for Gamma Systems Pty Ltd.
2. Upload the signed `.pkg` with Apple's supported uploader and wait for processing.
3. Select the processed build for version `0.1.0` using a unique build number.
4. Enter the canonical privacy/support URLs and metadata.
5. Upload the five screenshots in manifest order.
6. Complete App Privacy, export-compliance, content-rights, age-rating, price, availability, and release-method declarations from the worksheet and signed-build evidence.
7. Enter the offline reviewer setup instructions and support contact.
8. Record the accepted App Store Connect build identifier, complete the remaining declarations/assets, run `release:pre-submit-check`, and submit for review only after an accountable operator confirms every declaration.

TestFlight-specific copy, invitations, and testing workflows are intentionally omitted from this pass.

## Error handling and truthfulness

All readiness checks fail with a named blocker and a direct remediation hint. Missing Apple-controlled inputs are reported as external gates, not test failures or fabricated defaults. A failed Sites deployment leaves GitHub URLs unchanged until a public HTTPS deployment is verified. A failed screenshot capture preserves the last validated set and writes new output to a temporary location until the complete replacement passes. A failed candidate never overwrites the last release record.

The implementation must use these terms consistently:

- **draft** or **workpaper** for GST/BAS output;
- **not lodged** for the current reporting boundary;
- **candidate** for a locally validated distribution package;
- **uploaded** only after App Store Connect accepts the binary;
- **submitted** only after App Review submission; and
- **approved** only after Apple approval.

## Verification strategy

Verification is layered and proportional:

1. Unit tests for metadata parsing, identity consistency, public URL rules, screenshot manifests/dimensions, evidence freshness, and redaction.
2. Site typecheck/build tests plus non-browser HTTP checks for `/`, `/privacy`, and `/support`, including status, canonical metadata, internal navigation, and mail link.
3. Renderer tests proving the pre-sign-in privacy surface contains the exact release URLs and continues to deny other external URLs.
4. A deterministic packaged Playwright screenshot journey using a fresh fictional workspace, followed by screenshot validation.
5. Existing quick, full, release, typecheck, lint, package, and packaged end-to-end verification appropriate to touched code.
6. Apple Development-signed launch testing for setup, sign-in, organisation, journal, document, banking, BAS draft, privacy/support, and clean quit.
7. Distribution candidate inspection with `codesign`, `security`, `pkgutil`, and `spctl`, plus candidate-bound privacy/dependency evidence and any exact-artifact Apple privacy report the current toolchain can produce.
8. Manual App Store Connect review of seller identity, declarations, screenshots, processed build, and reviewer notes before submission.

No layer may substitute for a later Apple-controlled layer. The final handoff reports observed evidence and remaining external gates separately.

## Rollback and release records

The public site and candidate are versioned independently but linked in the release record. The store build embeds the exact public URLs that were verified before packaging. Site changes that alter privacy meaning require corresponding app/privacy/store review before deployment. Copy-only support corrections may be deployed independently if they do not contradict the submitted build.

### Durable records and build-number authority

The Git repository is the authority for non-secret release records and screenshots. `apps/desktop/release/macos/build-numbers.json` is the monotonic build-number ledger; a clean-tree candidate can use only a number reserved for its marketing version and source commit. Upload changes its ledger state to `uploaded`; rejected, expired, or superseded numbers remain permanently consumed. The checker validates the ledger against `CFBundleVersion`, package filename, `Info.plist`, evidence, and the App Store Connect build attestation.

Actual records live under `docs/release/records/macos/<version>/build-<number>/` and are committed and pushed to the trusted repository remote. They contain the redacted state/attestation JSON, metadata snapshot, screenshot manifest and image hashes, privacy/network evidence, package hash, public-site deployment URL, App Store Connect build identifier, and lifecycle events. Large signed packages remain in the operator's access-controlled release archive through review and are also retained by App Store Connect after upload; they are not committed. The release owner is Gamma Systems Pty Ltd's authorised App Store Account Holder or delegate, who verifies that record commits are backed up by the trusted remote.

Records use one immutable JSON file per lifecycle event after upload. Corrections add a new event and Git commit rather than rewriting prior events; validation rejects duplicate event IDs, reordered timestamps, changes to already committed event files in the release worktree, and version/build mismatches. Git history supplies the durable audit trail without adding a second signing system. Evidence is first written to a temporary directory and atomically promoted only when complete, so a failed collection cannot replace the last valid record.

Every uploaded build keeps its source commit, build number, package digest, screenshots, metadata snapshot, privacy evidence, public-site deployment URL, validation results, App Store Connect build identifier, and review outcome. Build numbers are never reused. Rollback means restoring a previously valid public site version where appropriate or shipping a higher corrective app version/build under Apple's rules.

## Acceptance criteria

The work is complete when:

- the public Sites URL serves unauthenticated product, privacy, and support pages over HTTPS;
- all public and in-app legal identity/support references say Gamma Systems Pty Ltd and `ben.ebsworth@gmail.com`;
- the company-controller and Apple seller/agreement prerequisites are recorded rather than inferred from display strings;
- `Info.plist`, store metadata, and release evidence agree on version `0.1.0`, the reserved build number, macOS 14+, and arm64;
- the candidate embeds the exact public privacy/support URLs and denies all other new-window URLs;
- the App Store worksheet contains final truthful copy and no unresolved repository-owned placeholders;
- five validated real-UI screenshots with fictional data exist in one accepted Mac size;
- repository, screenshot, privacy/network, and candidate evidence tasks pass for the exact source revision and build;
- the signed package, candidate-bound privacy/network evidence, and release record are ready for operator inspection;
- the release runbook can be followed from a clean checkout without undocumented repository steps; and
- remaining gates are limited to explicit Apple-account declarations, upload processing, and App Review.

## Primary references

- Apple, *App Review Guidelines*: <https://developer.apple.com/app-store/review/guidelines/>
- Apple, *Screenshot specifications*: <https://developer.apple.com/help/app-store-connect/reference/app-information/screenshot-specifications/>
- Apple, *Upload builds*: <https://developer.apple.com/help/app-store-connect/manage-builds/upload-builds/>
- Apple, *Manage app privacy*: <https://developer.apple.com/help/app-store-connect/manage-app-information/manage-app-privacy/>
- Apple, *App information*: <https://developer.apple.com/help/app-store-connect/reference/app-information/app-information/>
- Apple, *Privacy manifest files*: <https://developer.apple.com/documentation/bundleresources/privacy-manifest-files>
- Electron, *Mac App Store submission guide*: <https://www.electronjs.org/docs/latest/tutorial/mac-app-store-submission-guide/>
