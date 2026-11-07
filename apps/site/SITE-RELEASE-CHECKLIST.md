# Tammy Sites release checklist

This checklist applies to the **ChatGPT Sites** target and its earlier save-only preparation. For the direct Cloudflare deployment and CI requested on 7 November 2026, use the [direct runbook and record](CLOUDFLARE-RELEASE-RUNBOOK.md#release-checklist-and-record). Copy this Sites checklist into a Sites release record and follow [SITE-RELEASE-RUNBOOK.md](SITE-RELEASE-RUNBOOK.md). Mark a step complete only from its own evidence; build, save, direct Worker publication and Sites publication are separate outcomes.

## Candidate and artifacts

- [ ] GitHub PR URL and exact tested GitHub commit SHA recorded; synthetic merge SHA distinguished from PR head if applicable.
- [ ] Isolated checkout used; concurrent work preserved; intended source and documentation committed before descriptor attestation.
- [ ] `TAMMY_SOURCE_REVISION="$(git rev-parse HEAD)" pnpm proto:descriptors:evidence` run on the clean source commit; expected `descriptors.pb` bytes/SHA-256 unchanged for this site-only release.
- [ ] `pnpm proto:descriptors:verify` passed before and after an immediate evidence-only child commit containing only the two retained files under `compliance/contracts`; final candidate/export SHA is that evidence commit.
- [ ] Existing project ID `appgprj_6a93f33631748191b7c064c033f72b53`, public audience and `d1/r2: null` retained.
- [ ] Source export verified, including `packages/invoices`, root lockfile/workspace/dependency inputs, patches and shared TypeScript config.
- [ ] Exported workspace installed with frozen lockfile and pinned Node/pnpm.
- [ ] Release-script/source/provenance tests, site and invoice tests, typecheck, lint, editorial/SEO/preflight contracts and production build passed on the exact candidate.
- [ ] Worker entrypoint/client assets/hosting metadata/PDF runtime assets verified in the final archive.
- [ ] Artifact path, byte size, SHA-256 and provenance manifest location recorded.
- [ ] CI run/job URLs, actual revision, final result and artifact download location recorded.
- [ ] Cuttlefish runner qualified: `cf-vm-*`, Linux/ARM64, `aarch64`, `tammy-linux-ci`. Queued or skipped means incomplete; local checks do not satisfy this item.

## Source connection and save only

- [ ] Existing valid Sites source access is available without creating credentials or permissions; otherwise record the precise blocker and stop this section.
- [ ] No active publish-on-push window; no automatic publication can result from pushing.
- [ ] Existing Site opened using supported helper; opening result and current Sites source SHA retained.
- [ ] Export reconciled with Sites source; concurrent/Sites-only changes preserved or reviewed into GitHub; obsolete layout deletions explicitly reviewed.
- [ ] Reconciled source validated/built; source helper returned a verified pushed Sites SHA and matching archive.
- [ ] GitHub candidate SHA → source provenance → Sites commit SHA → archive SHA-256 mapping recorded.
- [ ] `save_site_version` succeeded; returned version ID/number, source SHA and archive metadata recorded/read back.
- [ ] No deploy operation called; live version/URL/audience unchanged.

## Separate production authorization and acceptance

- [ ] Publication explicitly authorized for the exact saved version; public audience preserved.
- [ ] Domain/canonical migration decision reviewed separately. Preparation alone leaves canonicals unchanged.
- [ ] Existing deployment state reconciled; one deployment initiated/continued; terminal status, deployment ID and live URL recorded.
- [ ] Live preflight result retained. If canonical migration is deferred, mismatches remain open rather than being relabeled successful.
- [ ] Real-browser invoice and quote PDFs/downloads/print output checked with sample data.
- [ ] Representative Tools/Journal/privacy routes, desktop and narrow/mobile layout/navigation checked; touch coverage recorded.
- [ ] Old-host redirect behavior and, for canonical migration, Search Console/sitemap state recorded.

## Release record

| Evidence | Value |
| --- | --- |
| Date / operator | |
| PR / final GitHub evidence commit | |
| Attested source commit / descriptor SHA-256 / verification results | |
| Source provenance manifest / hash | |
| Local validation commands / results | |
| CI run / job / actual SHA / result | |
| Cuttlefish runner name / OS / arch | |
| Worker archive path / bytes / SHA-256 | |
| Opened Sites source SHA | |
| Final pushed Sites source SHA | |
| Saved version ID / number | |
| Saved archive content hash | |
| Production authorization | Not requested for preparation |
| Deployment ID / terminal status | Not deployed during preparation |
| Live URL / published version | |
| HTTP / canonical preflight | |
| Browser / PDF / mobile evidence | |
| Remaining blockers and exact next action | |
