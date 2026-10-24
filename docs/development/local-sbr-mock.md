# Local SBR readiness mock

This development HTTP endpoint is a **Tammy internal JSON mock**, not an official ATO API, AS4 implementation, tax-return submission service or EVTE/conformance evidence. It is not connected to the desktop or packaged native helper. All application responses identify `LOCAL_SIMULATOR_NOT_FOR_ATO_LODGMENT` and `official_ato_response: false`.

## Run and exercise

From the repository root, with the pinned toolchain:

```sh
mise exec -- node scripts/sbr-local-mock.mjs --port 8765
```

It binds IPv4 **127.0.0.1 only**. Omitting `--port` chooses an ephemeral port and prints its local address. There is no host, upstream, environment, credential or arbitrary fixture-path argument. Stop the foreground process with Ctrl+C.

```sh
curl -H 'Content-Type: application/json' \
  --data '{"request_id":"018f0000-0000-7000-8000-000000000088","fixture_id":"SIM-SBR-READINESS-V1","case":"ACCEPTED"}' \
  http://127.0.0.1:8765/v1/sbr/readiness/select
```

`GET /healthz` reports the fixed simulator contract/service/fixture and hash. The [local OpenAPI document](local-sbr-mock.openapi.json) specifies only these development endpoints. It is **not provider OpenAPI** and must not be presented as such.

The six cases come from the implemented `services/sbr-helper/internal/simulator.Adapter`: ACCEPTED, NOT_STARTED, MAYBE_SENT, MALFORMED_RESPONSE, HELPER_DEATH and TIMEOUT. Their checked-in synthetic results are compared field-for-field with the actual network-disabled Go adapter by `TestLocalHTTPFixtureMatchesNetworkDisabledAdapter`. The canonical fixture hash is independently checked at server startup. The production helper sources and dependency closure keep their existing network-forbidden guards unchanged.

This endpoint reports selection metadata. TIMEOUT is returned immediately as the existing simulator outcome; it does not delay HTTP. HELPER_DEATH and MALFORMED_RESPONSE report the selected internal helper case; they do not kill the HTTP process or pretend to be ATO XML responses. HTTP 200 means a **fixture was selected**, never that a tax statement was accepted.

## Validation, uncertainty and replay

Only the fixed synthetic fixture ID, a UUIDv7 request ID and one supported case are accepted. JSON extra fields, including tax payloads, credentials, endpoint URLs and production/EVTE selectors, are rejected. Requests with common credential headers, browser Origin headers or non-loopback Host/peer addresses are rejected. There is no CORS grant, authentication store, OS credential access, outbound client or process launcher. Request bodies and credentials are not logged or echoed. Bodies are bounded to 4KiB, headers to 8KiB, request/header time to five seconds and concurrent connections to sixteen.

The adapter is deliberately **stateless**. Identical request/case selections return the same metadata. The same request ID with a different case selects that case; it is not a resend or durable idempotency/replay guarantee. Core owns the real operation journal, same-identity recovery and retry rules. This mock neither reads nor writes it. MAYBE_SENT remains uncertain; it is not silently retried or relabelled accepted.

Business `PENDING`, `REJECTED` and `UNKNOWN` request cases are explicitly unsupported and rejected, since they are not selectable outcomes in this implemented readiness interface. The helper protocol's pending credential mutations and the planned company-return submission protobufs do not establish an implemented AS provider API. A real tax-provider mock covering business rejection/pending transitions needs its separately verified adapter and message/action mapping. This initial endpoint does not claim that coverage.

## Verification

```sh
mise exec -- node --test scripts/sbr-local-mock.test.mjs
mise exec -- go test ./services/sbr-helper/internal/simulator
mise exec -- go test ./services/sbr-helper/cmd/tammy-sbr-helper \
  -run 'TestFinalHelperAndSimulatorDependencyClosureForbidsNetworkClients|TestAllProductionHelperSourcesForbidNetworkAndDynamicExecutionAPIs'
```

Tests use actual ephemeral loopback HTTP. They cover all existing cases, invalid/extra fields, unsupported pending/rejected states, repeated and changed-case requests, bounded bodies, credential/origin/Host rejection, unsupported live-looking routes, fixture integrity and no outbound-client/process imports. The fixture-conformance test does not create credentials or use a vault. Existing simulator tests continue to enforce deny-only dialer composition, cancellation and no send path.

At the 24 October checkpoint, the initial five HTTP tests passed locally. Simulator/protocol/runner/EVTE race tests, both existing helper network guards, JavaScript static typechecking, full repository lint (135 existing warnings and 12 informational findings), and all 55 archived-output hashes passed. A subsequent local run including additional chunked-body and malformed-JSON assertions was blocked before binding by the workspace sandbox (`listen EPERM`); it was not retried. The existing macOS CI script glob discovers the complete HTTP test file. Hosted results must be read for the exact pushed head; the new chunked assertion is not claimed as locally passed. Linux/Windows canonical jobs do not currently run the whole scripts glob or the new helper conformance test, so their green checks alone do not qualify this mock.

This companion change is stacked on visual branch commit `3ad0f20036da143ed2da07c623a48e04cbc26073`, including the demonstrated multi-record Audit disclosure assertion correction. Its own diff contains the local mock, tests, specification inventory and generated provenance only. Audit UI work and its real screenshots remain owned by the visual draft PR. No production adapter, credentials, endpoint grants, registration, release or gate-policy changes are included here.
