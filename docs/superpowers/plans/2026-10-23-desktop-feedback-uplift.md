# Desktop Feedback and Uplift Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Apply the four feedback items to the real packaged app and leave the validated uplift running.

**Architecture:** Existing generated protobuf ownership and financial commands remain. Pure shared input helpers provide format feedback, the Electron main process owns narrowly approved browser handoffs, and the generated canonical policy is shared between site and desktop. Existing tokens and shared workspace/arrival components provide the approved visual language.

**Tech Stack:** React 19, TypeScript, Tailwind, Conveyor, Electron, Vitest, Playwright, Go/Core unchanged.

## Chunk 1: Input and disclosure behaviour

### Task 1: ABN and new-credential feedback (modern owner)

Files: apps/desktop/src/shared/abn.ts and tests; shared/new-credential-policy.ts and tests; app/components/new-credential-field.tsx; app/components/workspace-entry-layout.tsx; app/features/setup/setup-screen.tsx and tests; app/features/workspace/unlock-screen.tsx and tests; existing setup fixture fills.

- [ ] Add failing helper/component cases for formatted paste without truncation, invalid checksum never reaching createWorkspace, and valid canonical ABN payload.
- [ ] Add failing format/confirmation cases for 14/15/128/129 code points, NFC, invalid UTF-8 surrogate/control input, spaces, masked reveal and no secret in query cache.
- [ ] Implement exact format helpers, explicit confirmations, accessible inline errors and safe lookup link. Update only necessary existing valid setup fixture fills.
- [ ] Run scoped Vitest, desktop typecheck and scoped Biome; retain actual outcomes.

### Task 2: Canonical policy and support (bas owner)

Files: PRIVACY.md; scripts/generate-public-content.mjs if necessary; apps/site/content/public-content.generated.ts; site support source; app/features/privacy/privacy-statement.tsx and tests.

- [ ] Add failing disclosure/navigation/content generation cases for the new policy and compact/full variants.
- [ ] Implement the sourced policy, readable full screen, truthful support and RAM/SBR explanation, using existing publisher/contact facts.
- [ ] Generate shared content and run generator tests plus privacy component tests. Do not publish the site.

### Task 3: Approved external handoffs (root owner)

Files: apps/desktop/src/shared/official-resources.ts and tests; main/security.ts and tests; main/index-production.ts.

- [ ] Add failing guard cases for the canonical valid ABR URL and exact official resources, plus lookalike hosts, credentials, fragments, extra keys, invalid ABNs and non-HTTPS URLs.
- [ ] Add only the explicit public destinations and checksum-valid ABR details route. Preserve deny-navigation, renderer CSP/network isolation and all other guards.
- [ ] Run focused security tests, typecheck and Biome.

## Chunk 2: All connected visual surfaces

### Task 4: Shared working screens (reference owner)

Files: app/components/review-workspace.tsx, ui/card.tsx and connected feature screens/panels under overview, documents, banking, invoices, accounting, ledger, audit, bas, sbr, diagnostics and reporting.

- [ ] Inventory each connected route against the approved boards and preserve the actual functional boundary.
- [ ] Apply coherent page headings, summary/status strips, purposeful sections, inline forms, dense tables and source/detail workspaces through shared primitives.
- [ ] Keep accessible labels and all financial/domain controls and state gates intact. Run existing relevant component tests; use actual runtime screenshots for reversible visual changes.

### Task 5: Shell, Settings and tokens (root owner)

Files: DESIGN.md; app/styles.css; app/app-shell/app-shell.tsx; app/app.tsx Settings presentation.

- [ ] Correct the durable token path and describe approved arrival/workspace patterns.
- [ ] Reconcile shared page width/spacing and Settings groups with the approved connected workspace; no new unsupported settings.
- [ ] Verify sibling routes, narrow scroll ownership, keyboard focus and reduced motion with the real app.

## Chunk 3: Integration and package acceptance

### Task 6: Regression journeys and complete verification (root)

Files: existing packaged E2E specs/support and a focused onboarding feedback spec if needed; approved UI capture tooling.

- [ ] Add meaningful invalid-input/confirmation/browser-handoff coverage before successful isolated setup; retain all current financial assertions and zero retries.
- [ ] Capture and inspect fresh setup/recovery/sign-in/privacy and every connected route at desktop, plus narrow form/table states.
- [ ] Run task verify:quick and task ci:contracts, then task ci:macos on a committed clean source with an immediate descriptor evidence child.
- [ ] Validate exact package/manifest hashes and source binding, reconcile fresh BAS exports and reviewed screens, inspect hosted checks when pushed.
- [ ] Update PR #8 with exact results, restart only the owned running app after safe confirmation of its state, and leave the final app open. Do not merge the PR.
