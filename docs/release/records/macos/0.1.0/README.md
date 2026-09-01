# Tammy 0.1.0 macOS release records

This directory owns redacted, version-and-build-bound App Store evidence. Files in `attestation-templates/` end in `.example.json`, contain `OPERATOR_REQUIRED`, and never satisfy a readiness check. Copy the relevant template into `build-<number>/attestations/` only after the accountable person has checked the named Apple screen or document and supplied the non-secret evidence reference.

Agents cannot self-attest company control, seller eligibility, legal declarations, App Store Connect state, or review outcomes. Do not add passwords, API tokens, signing credentials, private keys, provisioning profiles, or screenshots containing account secrets. Lifecycle events belong under `build-<number>/events/`; add a new immutable event rather than editing an earlier event.

Evidence sources:

- Company controller: the repository authority record confirmed by Gamma Systems Pty Ltd.
- Seller eligibility and active agreements: Apple Developer membership and App Store Connect Agreements screens. The active-developer-account branch also requires the authorised publisher's explicit launch decision.
- Content rights, export compliance, pricing/availability, privacy, and age rating: the corresponding App Store Connect declaration screen or retained legal document.
- Processed build, metadata/assets, and warning review: the exact App Store Connect version/build screen immediately before submission.

The repository checker may recommend an answer, but only the authorised operator records the accountable outcome.
