# Forecast table discovery and reduced-motion navigation

Local review on 8 November 2026, based on `8bb2c6d56393c55fc3d88fd527709f0f6f2688fa`. This independent branch leaves PR 51's held candidate `f82c4882fe7a2baf969466a2a92683d85d0e4bcb` unchanged. It has not been pushed, merged or deployed.

## What changed

At a 320px viewport, the live forecast put a 640px table inside a 231px region. Dates were visible, but every financial column and most of the explanatory caption required sideways scrolling. Current Chromium already allowed keyboard scrolling through automatic overflow focus; the problem was discoverability and clipped explanation, not broken Chromium keyboard access.

The table now has a named, explicitly focusable scroll region with a forest focus outline. A short instruction appears only when the rendered table overflows. The complete explanatory note stays outside the scroll area and is associated with the table; a concise accessible caption identifies the table and AUD. Native arrow scrolling, semantic column headers, 14px financial text, twelve rows and existing Gamma/Table primitives remain in use.

The first cue implementation used a fixed width threshold. Independent Astra max reflection identified that large valid amounts could widen the table beyond 640px. This was reproduced through ordinary form entry, then corrected by measuring rendered overflow and observing both the region and table. The instruction and its accessible description disappear together when the table fits again. A resize fallback was exercised with `ResizeObserver` unavailable.

The same review exposed an unrelated existing reduced-motion defect: the tools stylesheet reset every link's transform, leaving the unfocused skip link over the page. The corrected selector preserves the skip link's static hide/reveal transform while retaining zero-duration transitions. First Tab reveals it, Enter navigates to main, the following Tab enters main content, and the link hides after focus leaves.

## Before and after

These are actual browser captures, converted losslessly to WebP. The ordinary-value table captures fit inside the review viewport; the large-value table uses a full-element capture. The skip-link pair has Export focused and reduced motion enabled; the before image shows the misplaced link at the top left.

| State | Evidence |
| --- | --- |
| Live baseline, 320px table | [Dates visible; explanation clipped](assets/evidence/forecast-table/before-table-320.webp) |
| Local, 320px table start | [Instruction and complete note](assets/evidence/forecast-table/after-table-left-320.webp) |
| Local, 320px after arrow scrolling | [All twelve Closing amounts accessible](assets/evidence/forecast-table/after-table-closing-320.webp) |
| Local, 390px | [Narrow layout](assets/evidence/forecast-table/after-table-390.webp) |
| Local, 1440px | [Ordinary table fits; no scrolling instruction](assets/evidence/forecast-table/after-table-1440.webp) |
| Live reduced-motion baseline | [Unfocused skip link covers content](assets/evidence/forecast-table/before-skip-320.webp) |
| Local reduced motion | [Unfocused skip link stays off screen](assets/evidence/forecast-table/after-skip-320.webp) |
| Local, 1440px large valid amounts | [715px table triggers the instruction](assets/evidence/forecast-table/large-values-1440.webp) |
| Local forced colours | [Readable rows and native scrollbar; external focus outline is outside this crop](assets/evidence/forecast-table/forced-colours-320.webp) |

## Executed checks

Node 24.18.0 and pnpm 11.15.0 were used with the frozen offline workspace dependencies. Site typecheck, site lint, root lint, 100 site tests, 34 invoice tests, 15 palette checks and the direct-target production build passed. Root lint retains 136 existing warnings and 54 informational findings. No tax or calculation helper changed.

The actual Chromium journey began at the tools directory, entered a synthetic payment and exercised the result at widths 320, 390, 639, 728, 729, 760, 761, 768, 1280 and 1440. It covered Tab entry and exit, native arrow scrolling through Closing, all twelve rows, complete explanatory text, visible focus, forced colours, reduced motion, invalid-input recovery, keeping existing work, loaded-offline scrolling and an actual CSV download. No page errors were recorded in this journey.

Four additional skip-link cases covered 320px and 1440px with and without reduced motion. Twenty-two valid movements produced a 715px table inside 640/641/668px regions; the instruction appeared at all three desktop/intermediate widths, arrow scrolling reached the end, and explicit sample replacement removed the instruction and its description when the table fit again. The one-pixel rounding tolerance was checked at the ordinary-table boundary.

The live baseline and final local export are byte-identical: 1,158 bytes, SHA-256 `df6b3387a7462e91bd6680427c17234d6228edf042aa833c7a764611bba0905e`. The scenario starts with $100.00 and one $250.00 payment; all twelve closing balances remain -$150.00. The [evidence record](2026-11-08-forecast-table-evidence.json) includes measured results, timestamps, source hashes and capture hashes.

## Scope and release boundary

The visual direction remains Soft Precision: readable financial type, restrained secondary instructions and a clear focus rule. This pass used existing primitives and colours, with no added illustration or decorative surfaces. No image generation, Hallmark or Appllama tools ran in this bounded pass. The [independent Astra max review](2026-11-08-forecast-table-independent-review.md) found no remaining material issue in this local scope and distinguishes its source/image inspection from checks executed by the main session.

Coverage is Chromium keyboard plus viewport/media emulation. Physical touch, screen readers, other engines, native desktop and OS print were not qualified. The large-value cases qualify the table and its scrolling cue, not all summary-card layouts at arbitrary amounts. An aborted stale-build run and tall-element captures that painted an offscreen fixed skip link are excluded from the retained evidence.

No remote write or deployment was attempted. PR 51's merge and public description holds, CI credential/runner holds, DNS/canonical migration and Sites publication boundaries remain in place. At the separate 21:46 UTC PR-status read, foundation Ubuntu and Windows jobs had succeeded, macOS was cancelled, and website verification was cancelled with deployment skipped. Those results belong to PR 51, not this local branch; they do not qualify Cuttlefish or CI deployment.
