# Tammy public design system

Tammy’s [Soft Precision identity](../../DESIGN.md) is a warm accounting desk with precise records and explicit actions. Preserve the two-leaf wordmark, humanist sans controls, restrained serif headings, readable tabular figures and calm light surfaces. The public site introduces the Mac application; its tools are independent browser utilities. The Journal is a reading surface. [PRODUCT.md](../../PRODUCT.md) indexes current behaviour and boundaries: native Issue posts receivables, income and basis-aware GST; a browser PDF export does not issue a native invoice or post anything.

## Ownership and reuse

[`app/tokens.css`](app/tokens.css) is the canonical **public-site runtime** token owner. It implements the accepted palette, type families, radii, spacing rhythm and short-motion tokens. [`globals.css`](app/globals.css) maps those tokens to Tailwind v4; [`design-system.css`](app/design-system.css) adapts Gamma, with the existing [`tools/gamma-theme.css`](app/tools/gamma-theme.css) scoped to the tool shell. The [`landing.css`](app/landing.css) and [`tools/tools.css`](app/tools/tools.css) `--l-*` / `--t-*` names are compatibility aliases, not separate palettes. Desktop’s `apps/desktop/src/app/styles.css` remains its own implementation. No browser control migration adds a desktop capability.

The root [DESIGN.md frontmatter](../../DESIGN.md) records durable primitive values; [`.impeccable/design.json`](../../.impeccable/design.json) adds display metadata, motion, responsive rules and standalone component specimens. The sidecar is documentation, not an alternative runtime theme. Its generated tonal ramps are inspection aids, not approved UI colours.

| Role | Runtime token | Use |
| --- | --- | --- |
| Oat canvas | `--tammy-canvas` · #F7F3EA | Page background |
| Ivory paper | `--tammy-paper` · #FFFCF6 | Inputs and working surfaces |
| Cocoa ink | `--tammy-ink` · #302A25 | Readable content and headings |
| Forest | `--tammy-forest` · #235C4B | Actions, focus, selected states |
| Deep forest | `--tammy-forest-deep` · #102F27 | Existing landing-action hover |
| Secondary text | `--tammy-muted` · #6C6258 | Descriptions and metadata |
| Rule | `--tammy-rule` · #DCD3C5 | Non-interactive separation |
| Field border | `--tammy-field-border` · #8D8275 | Discoverable input boundaries |
| Sage | `--tammy-sage` · #E7EDE2 | Supporting surfaces and posted states |
| Oat | `--tammy-oat` · #EEE8DC | Muted material and supporting surfaces |
| Warning | `--tammy-warning` · #845020 | Draft/unmatched labels with text |
| Warning surface | `--tammy-warning-soft` · #FBECD6 | Background for warning record labels |
| Danger | `--tammy-danger` · #A1352B | Validation/destructive intent |
| Apricot / butter | `--tammy-apricot` · #F2BFA5 / `--tammy-butter` · #F4DC91 | Supporting art and arrival material, not body text |

Control/surface/arrival radii are 12/20/28px, following the existing identity. Compact financial status labels may use a pill-like shape. Type uses Avenir Next / Segoe UI Variable / Helvetica Neue for controls and records; Iowan Old Style / Palatino / Georgia for large headings. Financial data uses tabular numerals. Never animate amounts. Space follows the existing 4/8/12/16/24/32/48/64px rhythm.

The public hierarchy is surface-specific: the shared sans wordmark is 28px/1 at weight 770; tools body copy is 16px/1.5; shared fields/actions are 14px, and static record labels are 12px/1.5. Tools-directory headings use `clamp(42px, 5vw, 68px)` and the landing heading uses `clamp(62px, 6.7vw, 113px)` before its responsive overrides. These editorial sizes do not replace the native app's compact type scale.

| Capability | Canonical owner | Contract |
| --- | --- | --- |
| Public brand | `components/site-brand.tsx` | One labelled home link and leaf mark across landing, tools, guides and Journal |
| Buttons | `components/ui/button.tsx` | Native button; emphasis variants; busy disables repeat actions and preserves dimensions |
| Fields | `components/ui/input.tsx`, `select.tsx`, `label.tsx` | Native semantics; shared field border, paper background, control radius, forest focus and disabled state; Input also maps `aria-invalid` to danger |
| Single choice | Existing Gamma `SegmentedControl` | Native radio group, controlled state, keyboard arrows, 44px faces, reduced motion |
| Record labels | `components/accounting-status.tsx` | Draft, Reviewed, Posted, Not matched and Issued stay distinct; static labels do not announce a mutation |
| Search recovery | `ToolLibrary`, Gamma `EmptyState` | Immediate local filtering; Clear preserves category and restores input focus; Show all resets both |
| Site navigation | `ToolsShell` | Three stable destinations, current section labelled with `aria-current=location`, 44px links |
| Scrollbars | `globals.css` | Global visible track/thumb tokens, engine fallback, forced-colours override |
| Document preview tables | `tools/tools.css`, invoice/quote generators | 12px tabular figures; labelled keyboard-scroll region; unit ex GST agrees with the PDF; document totals stay outside the region |
| Financial calculations | Existing domain helpers / `@tammy/invoices` | Integer AUD cents and validated tax rules; no logic in the theme |

Native select and date popups remain platform-owned. This pass does not replace existing financial validation, document lifecycle or storage contracts. Search contains no business records, uses no requests and stays local to the current catalogue visit. Existing loading/error/export handling remains authoritative.

Shared `Input` and `Select` use `border-input`, mapped by `globals.css` to `--tammy-field-border`. The legacy `.tools-field > :is(input,select,textarea)` wrapper now uses that same token and `--tammy-radius-control`, while retaining its existing 45px minimum height and 15px field type. Shared primitives retain a 44px minimum target. Their base focus outline is 2px with a 2px offset; the tools shell applies its existing 3px outline/3px offset. The directory search uses a 2px wrapper `:focus-within` outline so focus does not double up. Validation messages remain the responsibility of the tool and its domain model.

`AccountingStatus` uses text as well as colour: Draft and Not matched use warning material, Reviewed stays neutral, and Posted and Issued use sage/forest. Shared colour does not merge these five meanings. It is a static span, with no live-region or mutation semantics. Gamma choice controls retain native radio inputs and their labels; the tools adapter makes their faces 44px and lays a full-width choice group into two columns below 640px. Keep Gamma provenance and behaviour intact.

## Layout and art direction

Invoice and quote previews use a shared 440px minimum line-item table inside a named, focusable horizontal scroll region. The paper's container width controls the scroll cue below 440px, including narrow desktop columns. Keep figures at 12px rather than shrinking them on mobile. The `Unit ex GST` column displays the validated unit price; quantity, line GST and line total remain separate. Subtotal/GST/document total stay outside the scrolling area. These rules affect browser previews only; PDF templates, tax arithmetic and print geometry retain their own contracts.

Document editors are native fieldsets that remain disabled while their browser code loads. `useBrowserReady` preserves the same disabled state during server rendering and the first hydration render, then enables the controls when React can retain input. The small existing locality label reads “Preparing form…” while loading, with `aria-busy` on the fieldset and a no-JavaScript explanation. Keep document data and export handling in the existing generators; the readiness hook stores no business data and changes no calculation.

The cash-flow planner protects entered work before loading sample figures. An edited opening balance or any movement opens a small inline native fieldset beside the sample action; untouched planners load the sample directly. The confirmation names what will be replaced, focuses **Keep my forecast** first, supports Escape on either decision, and returns focus to the sample action when dismissed or accepted. Further edits dismiss the pending confirmation. **Replace with sample** uses the shared destructive Button; it retains the chosen start date and explicitly replaces only the current figures. This is local input protection, not automatic saving, undo, a ledger action or a new forecast model.

Cash-forecast summary tiles wrap according to both their content and available width. Ordinary desktop figures retain three equal tiles; longer values receive wider rows before they can overlap or lose cents. Amounts remain complete AUD text with a sign and two decimal places: 1.5rem display type, or 1.125rem below 375px. Both sizes scale with the user's text size. Native word-break opportunities after grouping commas allow enlarged or substituted fonts to wrap without splitting the sign or decimal group; do not abbreviate, clip or insert hidden characters into amounts. Test the rendered glyph bounds at the supported 150-movement aggregate limit and layout breakpoints, including enlarged text, not only page overflow. Calculation and weekly-table contracts remain unchanged.

The weekly cash table retains its readable 640px minimum width inside a named, keyboard-focusable scroll region. Show and describe its scrolling cue only when the rendered table actually overflows; observe both the region and table because large valid amounts can widen columns beyond that minimum. Keep the complete explanatory note outside the scrolling area and associate it with the table; the table retains a concise accessible caption. Verify Tab entry, native left/right scrolling to Closing, visible focus, all twelve rows, unchanged values and a cue that disappears when the table fits. Do not shrink financial type to avoid scrolling.

Reduced motion disables decorative control transitions and transforms. Preserve the skip link's static hide/reveal transform: it must remain off-screen until focused, then lead into the main content and hide again when focus leaves. A blanket transform reset must not leave it covering the page.

A visitor to `/tools` should see search, task filters and real choices in the first desktop viewport. The directory’s compound selectors deliberately outrank generic guide-card and hero rules independent of stylesheet load order. Search precedes filters in reading and keyboard order. Keep all twelve tools, their summaries and their specific limitations.

The tools shell is at most 1248px with 32px gutters. The directory uses three columns, moves controls into one column and results into two below 1100px, then one result column below 760px. These are public layout rules, not replacements for the desktop's 1120px workspace and navigation breakpoints. Warm material changes and fine rules provide most depth; the shared Card primitive retains its small inherited shadow, while calculator work surfaces explicitly remove it. Invoice-paper shadows and page geometry remain local.

The landing remains the feature-led introduction. Generated images are supporting editorial art; financial demonstrations remain labelled synthetic HTML. The calculator/paper/pencil still life joins the earlier receipt/ledger illustration. Use small responsive WebP assets and explicit dimensions. Hide the non-essential tools-header art at narrow widths. Do not put illustration behind records, or turn generated UI into a product claim.

[Generated desktop/mobile study](../../docs/design/assets/soft-precision-tools-study.webp) is a critique aid, not an approved pixel comp, screenshot or source of tax rules. The implemented directory retains the real descriptions and complete categories that the study abbreviated. This is a code-led extension of established Soft Precision; no new seed or comp approval is claimed.

## Local component reference

From the repository root, using the pinned toolchain:

```sh
pnpm --dir apps/site build
node apps/site/scripts/design-system-preview.mjs
```

Open [`output/design-system/index.html`](../../output/design-system/index.html). [`design-system/preview.tsx`](design-system/preview.tsx) imports the actual SiteBrand, AccountingStatus, Button, Input, Label, Select, Gamma SegmentedControl and GammaEmptyState components. The generator bundles them with production CSS locally, including native fields, Gamma selection, error, disabled and pending states. It is not an application route, is not linked from the public site and performs no financial action. Its Download PDF control only demonstrates pending state; Reset example restores it. The examples are synthetic. Regenerate after source/CSS changes before treating the local output as current.

```sh
node apps/site/scripts/check-design-system.mjs
pnpm --dir apps/site typecheck
pnpm --dir apps/site lint
pnpm --dir apps/site test
```

The token check compares all fifteen palette roles in the root contract to runtime tokens and verifies landing/tools aliases. It does not cover every field selector, typography, motion, contrast, keyboard operation or layout. Use real desktop/narrow captures, focus/error/recovery checks, reduced motion and repeated PDF downloads for release acceptance. These commands are a verification recipe, not a claim they ran here; no native tests were executed by this design pass.

## Remaining migration boundary

Older invoice/quote/Journal CSS still has local geometry, illustrative colours and some legacy actions. Shared Input/Select and the legacy tools-field token bridge are migrated; that does not mean every workflow now uses shared React primitives. Do not normalize paper exports, table geometry or tax rules merely to obtain a clean grep. Existing eyebrow styles, glyph arrows and isolated decorative tints are not adopted as reusable rules by the sidecar. This pass consolidates the shared palette, chrome and touched controls; it does not claim a native desktop restyle, publication, full palette migration or assistive-technology certification.
