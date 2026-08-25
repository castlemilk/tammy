# Local SBR readiness

Tammy separates local accounting, the deterministic SBR simulator, and externally authorised EVTE work. The simulator is visibly synthetic and network-disabled. It is useful for local machine-credential lifecycle and transport-state tests, but it is not ATO approval or conformance evidence. There is no production SBR path and no BAS submit or lodge action.

All commands run from the repository root. Fresh ordinary accounting supports macOS arm64 and
Windows x64. SBR simulator, doctor, registration, and evidence scenarios require macOS arm64.

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

The simulator uses fixed test identity and credential material and cannot select an EVTE or production endpoint. Simulator and doctor launches carry explicit main-process authority and display `SIMULATOR — NOT FOR ATO LODGMENT` for the entire session. Doctor first prints the static EVTE preflight, then launches an isolated simulator; after normal workspace unlock and sign-in it opens `/settings/sbr?doctor=1` to inspect authenticated, redacted readiness.

`test:sbr` first verifies and builds its authenticated SQLCipher resource, so it is runnable after bootstrap from a clean checkout. It then owns the local helper race tests, core SBR and tagged SQLCipher integration graph, schemas, desktop surfaces, policy coverage, process/result checks, and contracts. The signed Keychain integration and native security-bookmark host remain separate opt-in tests: both fail closed until their external signing inputs exist. They are not silently skipped or treated as proof by `test:sbr`.

Only a clean packaged E2E result is packaged evidence; an interactive launch is a development smoke. Generate and then consume evidence in order:

```sh
mise exec -- task package:e2e
mise exec -- task evidence:sbr
```

`evidence:sbr` does not rerun the packaged journey. It accepts no input arguments and consumes only `.tmp/sbr-e2e/latest/result.json` when it is a mode-0600 `PASSED` record for the exact current Git revision. It writes a mode-0600 redacted bundle beside that result and rejects stale, malformed, insecure, or symlinked input.

## Scenario: review the external registration handoff

```sh
mise exec -- task sbr:registration:check
```

This command checks the exact fixed installed signed-input locations below. Missing inputs produce the static repository-owned external handoff checklist. Present inputs are size, type, permission, and symlink checked, then their schemas, hashes, cross-bindings, expiry, and signatures are validated by the registration authenticator. A blocked check emits bounded redacted JSON and exits non-zero; missing inputs report `EVTE_SIGNED_INPUTS_REQUIRED`, so automation cannot mistake the result for approval. It accepts no credential, password, TOTP, Product ID, endpoint URL, environment override, or arbitrary path and does not launch EVTE.

The checker validates installed signed inputs only at these repository-owned locations:

- `config/sbr/evte/sbr-profile-v1.json`
- `config/sbr/evte/sbr-profile-v1.sig`
- `config/sbr/evte/sbr-component-v1.json`
- `config/sbr/evte/sbr-registration-v1.json`
- `config/sbr/evte/sbr-registration-v1.sig`
- `config/sbr/evte/sbr-endpoint-profile-v1.json`

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
3. Continue commits the import; there is no pre-import preview or separate accept step. After commit, confirm the machine-credential state and displayed fingerprint. The redacted status shows safe issuer, serial, creation and expiry dates, component version, and fingerprint. The screen does not expose the credential ABN or credential bytes.
4. The core validates the binding against the authenticated installation, workspace, organisation, and independently verified ABN and rejects a mismatch before credential use.
5. Remove or replace it only through the authenticated in-app action; reimport is required after moving to another machine or restoring a workspace.

Never copy the credential bytes, credential password, TOTP secret, or Product ID into the repository, terminal, environment variables, command arguments, logs, evidence, backups, or cloud storage. The credential never leaves this Mac, is stored outside the accounting workspace, and is not included in workspace backups. If the secure OS store is unavailable, credential operations fail closed while ordinary accounting remains available.

The developer's own RAM credential proves neither Tammy DSP/product registration nor authority to transact. The in-app Product ID controls remain hidden and fail closed until the separately reviewed, signed EVTE registration binds an exact product and service scope; use them only after that gate passes.
