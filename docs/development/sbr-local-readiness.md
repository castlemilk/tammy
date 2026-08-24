# Local SBR readiness

Tammy separates local accounting, the deterministic SBR simulator, and externally authorised EVTE work. The simulator is visibly synthetic and network-disabled. It is useful for local machine-credential lifecycle and transport-state tests, but it is not ATO approval or conformance evidence. There is no production SBR path and no BAS submit or lodge action.

All commands run from the repository root. SBR scenarios require macOS arm64.

## Bootstrap once

```sh
mise install
mise exec -- task setup
```

## Scenario: run accounting in a clean local workspace

```sh
mise exec -- task dev:accounting:fresh
```

Complete setup, recovery-code confirmation, unlock, and named-user sign-in in the app. This scenario uses a new isolated Electron user-data root and prints the retained root after exit. It does not delete or alter the persistent development workspace.

## Scenario: exercise synthetic SBR readiness

```sh
mise exec -- task dev:sbr:simulator
mise exec -- task sbr:doctor
mise exec -- task test:sbr
mise exec -- task package:e2e
```

The simulator uses fixed test identity and credential material and cannot select an EVTE or production endpoint. The doctor reports redacted readiness after normal workspace unlock and sign-in. `test:sbr` owns focused protocol, vault, core, desktop, and policy checks. Only a clean packaged E2E result is packaged evidence; an interactive launch is a development smoke.

## Scenario: inspect the external registration handoff

```sh
mise exec -- task sbr:registration:check
```

This read-only check reports the external gaps it can determine from fixed signed-input locations. It accepts no credential, password, TOTP, Product ID, endpoint URL, or arbitrary path and does not launch EVTE.

Before any EVTE launch, complete and retain this external registration checklist:

- DSP registration is approved and has its external reference and decision dates.
- Product registration is approved for the intended product and service scope.
- The OSF assessment is approved for the applicable category.
- The ATO-approved credential component licence is approved, and the component target and version match macOS arm64.
- EVTE access is approved and current.
- The signed endpoint profile is current and bound by hash to the registration and runtime profiles.
- Each required service enrolment is approved for its exact service and release artefacts.
- Service-specific conformance state and retained results are current; passed conformance is distinct from pre-conformance readiness.
- An independent review approves the signed manifest and records the reviewer and approval time.
- Every expiry and revalidation date is current.
- The redacted evidence export is retained for registration or conformance review without credentials, Product IDs, raw tax identifiers, or workspace data.

`mise exec -- task dev:sbr:evte` remains blocked until all externally issued, signed inputs are installed and accepted. Actual component integration is the next separately approved plan after issuance; repository examples and local simulator evidence cannot enable it.

After those prerequisites exist, the intended operator sequence is:

```sh
mise exec -- task sbr:registration:check
mise exec -- task dev:sbr:evte
mise exec -- task evidence:sbr
```

EVTE is non-production. The sequence does not connect the current BAS draft to SBR and cannot lodge or submit it.

## Handle a real RAM machine credential safely

Launch EVTE only after signed readiness passes. Then sign in normally and perform import, inspect, unlock-for-use, replace, and remove in Settings → SBR.

1. Select the credential with Tammy's native file chooser. Do not use a Task argument or a terminal path.
2. Enter the credential password and fresh TOTP only in Tammy. Task accepts no live credential.
3. Verify the returned redacted ABN, expiry, and fingerprint before accepting the binding.
4. Confirm the credential is bound to the intended installation, workspace, organisation, and verified ABN.
5. Remove or replace it only through the authenticated in-app action; reimport is required after moving to another machine or restoring a workspace.

Never copy the credential bytes, credential password, TOTP secret, or Product ID into the repository, terminal, environment variables, command arguments, logs, evidence, backups, or cloud storage. The credential never leaves this Mac, is stored outside the accounting workspace, and is not included in workspace backups. If the secure OS store is unavailable, credential operations fail closed while ordinary accounting remains available.

The developer's own RAM credential proves neither Tammy DSP/product registration nor authority to transact. Do not install a Product ID or real component until the separately reviewed integration explicitly owns that in-app flow.
