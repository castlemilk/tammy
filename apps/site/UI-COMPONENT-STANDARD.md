# Tammy UI implementation standard: shadcn + Tailwind + Gamma

**Adoption:** 5 November 2026, [Tammy Soft Precision](../../DESIGN.md) remains the authority for visual identity and financial UI semantics. This is a component implementation standard, not a new colour palette or a new accounting feature claim.

## The stack

- **React 19 + Vinext + Tailwind v4** for public Tammy Tools/Journal. `apps/site/app/globals.css` already imports `tailwindcss`, `tw-animate-css` and `shadcn/tailwind.css`.
- **shadcn/ui** is already configured in `apps/site/components.json` (base-nova, CSS variables, @ alias, Tailwind v4). Locally owned native primitives now live under `components/ui/` and expose `data-slot`, semantic tokens and Tailwind utilities. Keep shared UI source in the repository, per shadcn's copy-and-own model.
- **Gamma** is the owner's maintained component catalogue. The first actual vendored component, `SegmentedControl`, is copied from the user's Shorted/Gamma library; see `components/gamma/README.md` for provenance. It preserves native radio semantics and has the original Gamma CSS block, with documented Tammy-only responsive adjustments.
- **Soft Precision token bridge**: `app/tokens.css` owns the accepted public palette; `app/design-system.css` maps Gamma CSS variables to these semantic colours, fonts, radii and motion, including scoped `.tools-shell` overrides. Do **not** add an independent Gamma theme or mix another brand palette into financial forms.

## Use this hierarchy for new work

1. Existing domain components and accounting/GST calculation rules stay authoritative; UI is **never** the source of financial truth.
2. Use `components/ui/Button|Input|Select|Checkbox|Label|Card` and Tailwind for ordinary controls, forms, surfaces, spacing and state. New controls should have a reusable source file rather than introducing ad hoc `tools-*` class rules for every form.
3. Prefer an actual Gamma component for specialised accessible interactions such as compact single-choice modes. Vendor only the relevant maintained component and its CSS section, with a documented version/source and token bridge; do not copy the entire catalogue or replace native semantics with inert animations.
4. Keep the current `ToolsShell` and `BlogArticle` editorial structure, source-linked MDX blocks and established visual language. Page-specific CSS may still be used for layout/print and specialised charts, not for recreating `button`, `input`, or `checkbox` every time.
5. Desktop Tammy has its own canonical `apps/desktop/src/app/styles.css` palette and shadcn-derived shared controls. Apply the same component/reuse principle there, but do not import a web-only client component into Electron or change tested accounting contracts in a styling PR.

## Current concrete adoption

| Surface | Shared components | Legacy role that remains |
| --- | --- | --- |
| GST calculator | Shared Input/Label and existing Gamma SegmentedControl; associated error/help text | Integer-cent calculation helpers unchanged |
| BAS GST workpaper | shadcn-style Card, Input, Label, Select; genuine Gamma SegmentedControl for Q1–Q4 | Financial calculation and results/list layout from proven tool CSS |
| Sole-trader self-lodgement checklist | shadcn-style Card, Checkbox, Label, Select and Button | Print-oriented page and checklist layout |
| Anonymous quote generator | shadcn-style Input, Select, Checkbox and Button | Quote paper/print CSS; existing invoice-domain calculations |
| Public Tools catalogue | shadcn-style Button and Input | Subject-specific cards and filter layout |
| Journal and earlier tools | Existing accessible MDX palette | Migrate control-by-control when making behavioural changes; do not force a risk-heavy wholesale rewrite |

## Implementation and accessibility rules

- Use 44px minimum actionable targets; visible `:focus-visible` styles, native disabled state, proper label association, screen-reader status text and reduced-motion alternatives.
- Prefer Tailwind's semantic `bg-primary`, `text-primary-foreground`, `border-input`, `ring-ring` and `bg-card` rather than hex values in components. Custom CSS bridge owns actual Soft Precision colours.
- Keep `data-slot` on shared controls for testability and consistent variants; use `cn` from `lib/utils.ts` with `clsx` and `tailwind-merge`.
- Do not invent radio, checkbox, select, menu or tooltip keyboard behaviour. Preserve native semantics or adopt a maintained, audited primitive.
- Money remains integer AUD cents, tax treatment remains in `@tammy/invoices` and model helpers, not the component layer.
- Never create an ATO-lodgement button, cloud-save claim, paid offer or live-bank connection purely for a screenshot.
- All screen previews, Gamma motion and charts must remain subordinate to real tasks and evidence.

## Review and CI gates

```sh
pnpm --dir apps/site typecheck
pnpm --dir apps/site lint
pnpm --dir apps/site test
pnpm --dir apps/site editorial:lint
pnpm --dir apps/site seo:check
pnpm --dir apps/site build
```

`tests/design-system.test.tsx` verifies Gamma selection, shadcn-form primitives, theme bridge import, responsive/reduced-motion CSS, and BAS/tax/quote usage. Existing business tests remain unchanged. Screenshots at 320/390/768/1280px, real browser print/PDF testing and Sites publication are independent release gates, **not** established by source CI.

## New feature PR checklist

- [ ] Existing shared primitive chosen or actual Gamma component vendored, source documented
- [ ] Semantic tokens and Tailwind classes applied without a new competing palette
- [ ] Accounting figures, dates, eligibility and status confirmed by domain tests
- [ ] Keyboard, focus, label, error, disabled, mobile and print states reviewed
- [ ] MDX/SEO metadata and original financial examples reviewed where applicable
- [ ] Exact-head CI green; deployment status reported separately from source completion

The [public design-system contract](DESIGN-SYSTEM.md) documents shared brand, record states, token ownership, navigation and the local interactive component reference.
