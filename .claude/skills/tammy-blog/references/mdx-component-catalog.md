# Tammy MDX component palette

Compiled by `@mdx-js/rollup` in Vinext/Vite. Authors use PascalCase component tags. The registry is `apps/site/components/mdx/tammy-components.tsx`. Do not import components from MDX files: the site chooses the approved list, which keeps code review focused.

## Components

| Name | Props | Use |
| --- | --- | --- |
| `Callout` | `tone="note"` or `caution` or `method`, `title`, children | A specific distinction, genuine caveat or a method worth returning to |
| `WorkedExample` | `title`, children | A clearly hypothetical worked case |
| `Formula` | `label`, `expression`, `result?` | One auditable equation, with defined units |
| `MoneyTable` | `caption`, `columns`, `rows` | Three-column or multi-column financial table, with scoped headers and tabular figures |
| `BarChart` | `title`, `unit`, `items=[{label, value, detail?},...]` | Honest static bar comparison. Numbers and labels rendered as HTML, accessible table supplied |
| `Figure` | `src="/assets/..."`, `alt`, `caption` | Original local illustration with an actual caption and alternative text |
| `ToolLink` | `href="/tools/..."`, `label`, `description?` | Connect the concept to an available calculator |
| `SourceNote` | `url`, `label`, `context?` | Link to authoritative primary guidance close to the supporting claim |
| `ScenarioExplorer` | `title`, `scenarios=[{label, context, rows:[[label,value]], conclusion}]` | Interactive comparison of two to four explicitly hypothetical accounting outcomes |
| `JournalEntry` | `title`, `lines=[{account, debitCents?, creditCents?}]`, `note?` | Balanced double-entry table; values are integer AUD cents and must reconcile |
| `ReviewChecklist` | `title`, `items=[string,...]` | Real reader-operated checkboxes for a limited review routine; state is not saved |

MDX can also use Markdown headings, emphasis, lists, links and GFM tables. Use `## Section` rather than duplicating the page's H1. The layout uses the registered HTML heading style for clear TOC anchors.

## Example

```mdx
## The cash arrives later

An issued invoice and cash in the bank are different events.

<Callout tone="caution" title="A due date is not a receipt">
  A forecast should use plausible payment dates, not automatically the invoice date.
</Callout>

<MoneyTable
  caption="Illustrative amounts, AUD, excluding GST"
  columns={["Item", "Accrual", "Cash"]}
  rows={[
    ["Sales invoiced / collected", "$12,000", "$8,000"],
    ["Costs incurred / paid", "$4,000", "$3,000"],
  ]}
/>

<BarChart
  title="Profit compared with net cash movement"
  unit="AUD"
  items={[
    {label:"Profit",value:8000},
    {label:"Net cash movement",value:5000},
  ]}
/>

<SourceNote
  url="https://business.gov.au/finance/cash-flow/set-up-a-cash-flow-statement"
  label="Australian Government: Set up a cash flow statement"
/>

<ToolLink href="/tools/cash-runway-calculator" label="Try the cash runway calculator" />
```

## Component constraints

- All data examples must reconcile, with units and a clear illustrative caption.
- `MoneyTable` is the canonical data view; `BarChart` is a supporting comparison, not a substitute.
- Do not embed third-party chart iframes or external scripts in posts.
- Authors do not import from `@tammy/invoices` or database packages inside MDX.
- Design tokens come from `app/tools/tools.css` and `app/blog/blog.css`.
- Avoid using four callouts in a row: each block must serve an actual reading purpose.

## New domain components in use

```mdx
<ScenarioExplorer title="The reporting period" scenarios={[
  {label:"Cash",context:"Received in July",rows:[["April–June","$0"],["July–September","$100"]],conclusion:"GST follows the payment in this example."},
  {label:"Non-cash",context:"Invoice issued in June",rows:[["April–June","$100"],["July–September","$0"]],conclusion:"GST follows the earlier invoice event in this example."}
]} />

<JournalEntry title="Teaching example, AUD" lines={[
  {account:"Bank",debitCents:220000},
  {account:"Sales ex GST",creditCents:200000},
  {account:"GST payable",creditCents:20000}
]} />

<ReviewChecklist title="Month-end review" items={[
  "Compare bank and ledger balances.",
  "Verify differences against source transactions.",
  "Retain the correction's audit trail."
]} />
```

Do not render these in the same post unless the subject truly needs all of them. Scenario values should be consistent with the accompanying prose. A JournalEntry with unequal totals must fail loudly rather than display a plausible ledger. ReviewChecklist selections are ephemeral, not persisted to an accounting system.
