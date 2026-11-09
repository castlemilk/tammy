# Cuttlefish CI

`workflows/tammy-ci.yaml` defines the native Cuttlefish validation workflow for
`castlemilk/tammy`. It runs the existing Linux contract and desktop scenarios,
roadmap checks, and both website preparation builds. The website job retains
separate Sites and direct Cloudflare candidates; it does not publish either.

The GitHub App pins the checkout task to the event's commit on pushes and pull
requests. The preparation step rejects a different checkout and fetches the
complete history and `origin/master` needed by descriptor provenance and the
protobuf breaking-change check. Manual runs use the checkout task's `master`
default. Fork pull requests are subject to Cuttlefish's fork policy.

Cuttlefish stores published workflow versions separately from this file. After
reviewing a change to the definition, update and validate the draft in the
Tammy project, then publish it. Merely changing this file does not update the
published version. The project must remain mapped to `castlemilk/tammy`, and a
rig must be online with sufficient free capacity. A successful local test or a
queued run is not evidence that the Cuttlefish job passed.

The automatic Windows Server smoke job has been removed. The separately
dispatched Windows 11 release gate is retained; it is not satisfied by Linux
or macOS checks. Cuttlefish's current native workflow executor cannot run the
macOS packaged test or Windows jobs. The existing macOS workflow remains in
place until a real Cuttlefish macOS run and failure-evidence retention are
qualified. The website's existing deployment workflow also remains in place:
the native validation artifacts have not yet been qualified as inputs to that
separate release path.
