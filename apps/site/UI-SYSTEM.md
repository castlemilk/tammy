# Tammy UI: shadcn/ui + Gamma + Tailwind v4

**Decision:** Reuse one component language for free utilities and the developing accounting product. The existing Soft Precision forest/ivory palette and tactile editorial serif are the brand, not an optional theme.

## Actual implementation

- Tailwind CSS v4 is already installed and imported in `app/globals.css`, together with `tw-animate-css` and `shadcn/tailwind.css`. The repo already contains a shadcn `components.json` and `lib/utils.ts` combining `clsx` and `tailwind-merge`.
- `components/ui/` adds local shadcn-style native **Button**, **Input**, **Textarea**, **Card** and **Table** primitives. All use Tailwind class utilities, accessible native semantics, focus-visible, disabled and motion-reduction treatment. These are repo-owned components, not another imported UI runtime.
- Gamma catalogue's validated **Empty State** component was adapted into `components/gamma/empty-state.tsx`, with native consumer-owned actions. Its design-system tokens come from Gamma **Atelier / Ledger** but are re-mapped and scoped under Tammy's `.tools-shell` in `app/tools/gamma-theme.css`. No external fonts, RGB palette swap or wholesale Gamma CSS import.
- Pages can combine the shadcn primitives with Gamma compositions without introducing inconsistent focus/typography. The application stays on Tailwind v4 with existing Vinext/Vite build; no Radix dependency or class-variance-authority package is introduced.

### Usage

```tsx
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { GammaEmptyState } from "@/components/gamma/empty-state";

<Card>
  <CardContent>
    <GammaEmptyState title="No payments planned" description="Add the first expected payment.">
      <Button variant="outline">Add payment</Button>
    </GammaEmptyState>
    <Input aria-label="Amount in AUD" inputMode="decimal" />
  </CardContent>
</Card>
```

The forecast tool consumes these components and Tailwind directly. The quote editor consumes shadcn Input, Textarea and Button; its existing document-preview CSS remains because CSS printing has distinct requirements. Other legacy pages can migrate incrementally, but new forms should use these shared components.

## Gamma selection rationale

Catalogue source: Gamma component `empty-state` (reviewed/validated) with native div, h3, optional icon, explicit recovery action and focusable children. Design-system reference: Gamma Atelier (humanist interface + editorial display) and Ledger (quiet table lines and tabular values). The Tammy token adapter maps `--gamma-surface`, `--gamma-ink`, `--gamma-accent`, `--gamma-border` and radii to Soft Precision tokens.

**Do not** copy the default Gamma cobalt theme over the product, invent variant badges, or use animation to imply a financial calculation has run. A reusable component should keep meaning when motion is disabled.

## Financial and privacy requirements

- Integer cents, unambiguous AUD labels, real dated transaction data and accessible tables.
- PDF and CSV are created in the browser without uploading user inputs; JSON drafts are explicitly saved by the user to their device and are **not encrypted**. No automatic browser cache, cloud account, server-side storage or consentless analytics.
- Quote PDF uses a dedicated `QUOTE` renderer, not a re-labelled invoice.
- Cash forecast is a user-entered scenario and **not** automated cash-feed prediction or actual ledger state.
- New designs remain separate from actual G0/G1 accounting release gates and the ChatGPT Sites production publication process.

## Quality gates

All new components and financial actions need typechecking, oxlint, Vitest tests, quote PDF renderer identity checks, existing financial model tests, the site SEO audit and a production build. Browser viewport, keyboard and PDF download inspection remain separate release tests when the hosting preview becomes available.
