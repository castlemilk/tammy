# macOS App Store prime-time readiness design

**Date:** 30 August 2026  
**Status:** Approved for specification review  
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

The release checker must keep the repository copy, packaged public links, App Store worksheet, privacy policy, screenshots, and release evidence consistent with these values. It must report, but cannot resolve, any mismatch between Gamma Systems Pty Ltd and the seller name or legal entity shown by the active Apple Developer and App Store Connect agreements. The final submission remains blocked until the operator verifies that identity in App Store Connect.

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

## Public compliance site

### Structure

The site lives as a dedicated workspace application under `apps/site` and uses the Sites scaffold and hosting contract. It is static and has no authentication, database, uploads, cookies, tracking, analytics, or form submission.

- `/` gives a restrained product summary, supported macOS scope, current accounting capabilities, local-first data posture, and links to privacy and support.
- `/privacy` publishes the canonical privacy policy for the current macOS release.
- `/support` identifies Tammy and Gamma Systems Pty Ltd, links to `mailto:ben.ebsworth@gmail.com`, gives safe diagnostic information to include, warns users not to send accounting data, credentials, recovery codes, machine credentials, or private keys, and links back to privacy.

The home page is a release-support surface, not a speculative sales site. It uses Tammy's established warm off-white, deep forest green, restrained borders, serif wordmark, and calm, precise copy. It may show one factual product screenshot after the screenshot set exists. No capability claim may exceed the store description or the submitted build.

### Canonical content and links

The repository privacy policy remains the editable source of legal content. The site renders matching content, including the effective date, publisher, data categories, local storage, retention/deletion, third parties, support, and change notice. A deterministic consistency test prevents material data-handling statements or the publisher/support identity from drifting between the repository policy, site, in-app statement, and store metadata.

After public deployment, the resulting HTTPS `/privacy` and `/support` URLs replace the GitHub links in the App Store worksheet and become the exact `TAMMY_MACOS_PRIVACY_POLICY_URL` and `TAMMY_MACOS_SUPPORT_URL` inputs embedded in the candidate. Tammy continues to allow only those validated URLs to open externally from the store build.

The site is published publicly because Apple reviewers and customers must be able to reach both pages without authentication. The user has approved an initial public Sites URL; a custom domain is deferred.

## App Store metadata package

`apps/desktop/release/macos/store-metadata.md` remains the human-readable submission worksheet. It is updated to contain final product copy and explicit operator gates rather than stale GitHub URLs or misleading completed checkboxes.

The worksheet covers:

- name, subtitle, description, keywords, categories, locale, price, availability, copyright, privacy URL, support URL, and optional marketing URL;
- App Privacy answer: no data collected and no tracking for this offline build, subject to the signed-build Xcode privacy report;
- export-compliance determination aligned with `ITSAppUsesNonExemptEncryption` and the candidate input;
- age-rating answers based on the actual build, with no Kids Category selection;
- review contact and step-by-step offline setup instructions requiring no remote demo account;
- an honest explanation of the supervised loopback core, imported files, local encryption, clean quit, and preparation-only reporting scope;
- a declaration checklist for advertising, analytics, tracking, in-app purchases, licence keys, downloaded executable code, and background processes; and
- the exact five screenshot filenames and captions in upload order.

Repository checks distinguish three states: repository-complete, candidate-evidenced, and operator-confirmed. Items such as seller identity, agreements, privacy questionnaire submission, age-rating answers, export answers, build selection, and App Review submission remain operator-confirmed even when the repository contains recommended answers.

## Screenshot production

### Source and data safety

Screenshots must come from Tammy's actual packaged renderer, not a recreated mockup. A dedicated Playwright release scenario creates a fresh temporary encrypted workspace and deterministic fictional Australian business named **Wattle & Co Supplies Pty Ltd**. All people, ABNs, bank references, invoices, documents, balances, dates, and identifiers are explicitly fictional and contain no developer or customer data.

The scenario uses production-like packaged application code with test-only orchestration outside the shipped bundle. It must not enable a hidden reviewer mode, bypass product security in the release build, or leave seeded records in a developer's normal workspace.

### Output

The canonical set uses PNG without alpha at one accepted size, initially 1440×900 unless the capture environment can prove a consistent 2880×1800 output. A validator rejects mixed dimensions, alpha, missing files, unexpected filenames, non-PNG output, and a count outside Apple's one-to-ten range.

The five images are:

1. `01-overview.png` — accounting overview with documents, banking, and reporting attention.
2. `02-document-review.png` — reviewed source document and extracted accounting details.
3. `03-journal-trial-balance.png` — balanced journal linked to the resulting trial balance.
4. `04-bank-reconciliation.png` — imported statement lines and a clear reconciliation state.
5. `05-bas-draft.png` — GST/BAS workpaper with an unmistakable **draft — not lodged** boundary.

The capture task writes the images and a small manifest containing locale, dimensions, fictional dataset version, source commit, app version, capture timestamp, filename, and caption. Validation checks only non-sensitive release facts. Screenshot generation does not imply that Apple has accepted the assets.

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

The evidence collector writes under a gitignored version/build directory. It records commands and outcomes in structured JSON plus a readable summary. It may record certificate/profile display names, expiry dates, identifiers, and hashes, but never private keys, certificate exports, passwords, tokens, profile contents, or environment values. It fails closed when required evidence is missing, stale, belongs to another commit/build, or contradicts the repository metadata.

The Xcode privacy report remains a human-supplied evidence file because it must describe the exact signed archive. A submission-readiness task validates its presence and release association but does not invent its findings.

## Task scenarios

The top-level Taskfile exposes scenario-oriented commands while keeping individual checks composable:

- `task site:dev`, `task site:test`, and `task site:build` operate the public compliance site locally.
- `task release:check` validates repository-owned Mac App Store inputs without credentials.
- `task release:screenshots` creates the canonical fictional screenshot set.
- `task release:screenshots:check` validates screenshot count, format, size, manifest, and captions without recapturing.
- `task release:development` produces the locally runnable sandboxed development app when development signing inputs exist.
- `task release:candidate` produces and locally validates the signed distribution package but never uploads it.
- `task release:evidence` collects or refreshes non-secret evidence for an existing exact candidate.
- `task release:submission-check` gives a single redacted repository/candidate/operator-gate report and exits non-zero until every required local artifact is coherent.

Task summaries explicitly state whether they build, validate, capture, publish, upload, or submit. No task named `deploy` or `release` may silently perform an App Store Connect upload or public Sites deployment.

## App Store Connect handoff

Once the public URLs, screenshots, metadata, signed candidate, and evidence pass, the operator follows one ordered checklist:

1. Verify the Apple Developer/App Store Connect seller and agreements for Gamma Systems Pty Ltd.
2. Upload the signed `.pkg` with Apple's supported uploader and wait for processing.
3. Select the processed build for version `0.1.0` using a unique build number.
4. Enter the canonical privacy/support URLs and metadata.
5. Upload the five screenshots in manifest order.
6. Complete App Privacy, export-compliance, content-rights, age-rating, price, availability, and release-method declarations from the worksheet and signed-build evidence.
7. Enter the offline reviewer setup instructions and support contact.
8. Run the final submission check, inspect App Store Connect for omissions or warnings, and submit for review only after an accountable operator confirms every declaration.

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
7. Distribution candidate inspection with `codesign`, `security`, `pkgutil`, and `spctl`, plus an exact-build Xcode privacy report.
8. Manual App Store Connect review of seller identity, declarations, screenshots, processed build, and reviewer notes before submission.

No layer may substitute for a later Apple-controlled layer. The final handoff reports observed evidence and remaining external gates separately.

## Rollback and release records

The public site and candidate are versioned independently but linked in the release record. The store build embeds the exact public URLs that were verified before packaging. Site changes that alter privacy meaning require corresponding app/privacy/store review before deployment. Copy-only support corrections may be deployed independently if they do not contradict the submitted build.

Every uploaded build keeps its source commit, build number, package digest, screenshots, metadata snapshot, privacy report, public-site deployment URL, validation results, App Store Connect build identifier, and review outcome. Build numbers are never reused. Rollback means restoring a previously valid public site version where appropriate or shipping a higher corrective app version/build under Apple's rules.

## Acceptance criteria

The work is complete when:

- the public Sites URL serves unauthenticated product, privacy, and support pages over HTTPS;
- all public and in-app legal identity/support references say Gamma Systems Pty Ltd and `ben.ebsworth@gmail.com`;
- the candidate embeds the exact public privacy/support URLs and denies all other new-window URLs;
- the App Store worksheet contains final truthful copy and no unresolved repository-owned placeholders;
- five validated real-UI screenshots with fictional data exist in one accepted Mac size;
- repository and candidate evidence tasks pass for the exact source revision and build;
- the signed package, privacy report, and release record are ready for operator inspection;
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
