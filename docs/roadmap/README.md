# Tammy product roadmap

- [Xero replacement roadmap](xero-replacement-roadmap.md): market target, release gates, dependencies, migration plan, release-quality acceptance, and first engineering epics.
- [Xero feature-parity inventory](xero-feature-inventory-2026-11-05.csv): **259 decomposed features across 20 domains**, with each item's documented development-state classification, priority, acceptance gate and Xero evidence URL.
- [Current capability source](../development/tech-state.md): what is connected in the real development build.
- [Core accounting programme](../superpowers/plans/2026-08-02-core-accounting-programme.md): the approved vertical-slice implementation plan; reuse this work.
- [Reporting backlog](../development/self-reporting-backlog.md): regulatory/tax scope and approval gates.

The **first switchable customer** is a non-employer Australian sole trader or service business operating one AUD entity. Do not equate initial-market replacement with the entire Xero product and partner ecosystem.

## Reading the inventory

Status `connected_dev` means a workflow is documented as connected in development, **not available in a signed public release**. `partial_dev` means some connected work remains incomplete. `core_only` is Go-core capability without the complete user journey. `not_started` is not connected. `external_gate` requires provider arrangements, ATO/other authorisation or conformance.

Priority `P0` covers durability, core books and migration (G0–G2). `P1` adds optional connectivity (G3), `P2` broader business functions (G4), and `P3` practice/ecosystem expansion (G5). No dates are committed, and gate priority does not override real dependency chains.

The CSV is intentionally granular. Its row count must not be advertised as a literal count of native Xero features; some rows split one competitor feature into independently tested components, or describe Tammy-specific safe migration and security prerequisites.

Run the validation script after changing the inventory:

```bash
node scripts/check-xero-roadmap.mjs
```

The CI workflow runs the same check on roadmap PRs.
