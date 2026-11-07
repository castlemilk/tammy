# Tammy website feature review: 7 November 2026

The feature-led design candidate preserves [Soft Precision](../../DESIGN.md), the established typography and palette, all twelve tools, financial calculations, development availability and the explicit BAS/bank-feed/invoicing limits. The currently published Worker still runs `56bd4ef4091a7df58e004b7ddb643e6f4c58cd7b`. This review does not publish the new design or move `tammy.tax`.

## Changes

- Keep “Your books. On your Mac.”; straighten the workspace illustration and remove the decorative tilted stack. Use readable, normal-flow synthetic captions and distinct Draft, Reviewed and Posted states.
- Show four concrete examples: a balanced draft journal, reviewed source evidence that is not posted, imported statement rows awaiting manual matching, and an issued PDF specimen. These are semantic HTML illustrations, explicitly not application screenshots. The invoice tool link is functional.
- Add an original generated receipt/pencil/ledger illustration. [Generation provenance](source-ledger-illustration.json) records the actual prompts, revision and hashes. The 1200px/640px WebP files are 37,898/10,698 bytes; the full PNG remains local. The requested local Imagegen CLI lacked an API key and SDK, so the available image-generation tool was used without installing dependencies or creating credentials. Its model version was not exposed.
- Shorten repeated promotional copy, remove decorative tool numbering, compact the tool introduction and retain the Journal's lead-story/archive hierarchy. Tool search has a Clear action that preserves the selected category and restores keyboard focus. The existing Gamma EmptyState and shared Button/Input primitives are reused through Tammy tokens.
- Fix the catalogue's inherited grid specificity: three ordinary columns at desktop, two at medium width and one on narrow screens. The featured invoice spans the row.
- Correct the inherited claim that issuing an invoice does not post a receivable. The explicit native Issue action calls accounting posting atomically; [existing acceptance evidence](../development/invoice-income-bas-acceptance-2026-10-04.md) confirms receivables, income and basis-aware GST. The homepage and `llms.txt` now distinguish that action from email delivery, payment collection and separately recorded receipts. This changes copy, not accounting behaviour; browser-only invoice/quote tools still do not post desktop entries.

## Review methods and limits

Local Frontend Design, Design Taste, GPT Taste and Deslop guidance informed the bounded pass. Frontend Design Premium 1.4.0 strict source audit completed with zero findings. The installed Impeccable 2.3.2 source detector (`detect --json --gpt`) returned an empty result; its context command could not load context, so the existing `.impeccable.md` and `DESIGN.md` were read directly. The available Impeccable skill is 4.3.1; no upgrade was made. These source checks do not establish rendered acceptance.

[Hallmark 2.0.0 guidance](https://github.com/nutlope/hallmark) was reviewed manually. No installed Hallmark CLI was available on Gamma; no sloplint, stamp or certification is claimed. [Appllama's public design skill 1.3.0](https://github.com/Appllama/appllama-skills/blob/main/skills/appllama-app-design-skill/SKILL.md) was read as relevant guidance: hierarchy, established controls, consistent tokens, tabular numbers, purposeful elevation and real state/flow checks. Its native Expo/navigation assumptions do not replace this web application. No Appllama MCP, reference-screen library, simulator or automated pass was executed. This is the plausible public source for the user's spelling, not an alias for Frontend Design Premium.

An independent reviewer inspected before/after desktop and 320px pixels for home, tools and Journal. Two findings were resolved: a capture made before the lazy-loaded artwork appeared, and the inherited six-column tool grid. The refreshed captures received **“pass for user design review; no material visual fixes remain.”** This is a whole-composition verdict, separate from interaction tests and publication approval.

## Rendered and functional evidence

Local production preview, pinned Node 24.18.0 / pnpm 11.15.0 / Wrangler 4.92.0:

- Home, tools and Journal inspected at 1440px and 320px, with additional overflow checks at 390px, 768px and 1280px. No horizontal overflow; all twelve tool destinations retained. Homepage figure captions are 12px in normal flow; financial example text is at least 13px.
- Keyboard checks: Clear search retains the GST category, Show all tools resets it, both restore focus to the search input with visible outline. Native FAQ summary opens with Enter. Quote line addition/removal works with Enter.
- Four real browser PDF downloads: invoices `DESIGN-QA-001/002` with totals AUD100/200 and quotes `DESIGN-QUOTE-001/002` with totals AUD150/300. All four are one-page A4 PDFs, text/reference/amount checked; the second invoice and quote were visually inspected after rendering. These are nonpersonal QA examples, not issued business documents.
- The first invoice attempt produced no PDF because the automated native date input had not committed its value. A keyboard date change resolved it. A later attempt referenced an old chunk after the preview build changed; reloading the final build resolved that. No blind duplicate download was assumed successful.
- Physical touch, OS print dialog, assistive-technology testing and native simulator checks were not performed in this pass. Existing reduced-motion rules are retained; no new motion system was added.

Screenshots, rendered PDFs and check logs remain local under `tmp/tammy-deslop-review/` in the task workspace. [Evidence hashes and viewport results](2026-11-07-site-feature-evidence.json) identify the retained files. Before captures are the published `56bd4ef4…` site. After captures are this design source; `after-{home,tools,blog}-{1440,320}.jpg` are the final review set. The prepared candidate and CI status belong in PR48's exact-head release record, not in these fixed historical observations. No source bundle or QA evidence was uploaded to Space.

## Validation and release boundaries

Site tests (87), invoice-domain tests (34), release/descriptor/preflight tests (169), CI contract tests (7), both typechecks, root/site lint, nine-post editorial lint, 36-route SEO inventory and direct production build passed. Root lint retains existing warnings/information diagnostics but no errors. Run the normal immediate-child descriptor attestation and export/build verification for the exact final candidate before considering deployment. Root Biome errors in the prior release's two evidence JSON files were repaired by formatting only; semantic equality was checked and their runbook hashes updated. Deployed source, original receipt and release artifact bytes were preserved.

New-design publication remains held pending specific authorization. [Domain migration](../release/cloudflare/tammy-tax-domain-plan.md), persistent CI credentials and Cuttlefish runner registration have separate unresolved access/approval gates. The draft PR does not claim unattended CI deployment or Cuttlefish qualification.
