# Tammy Journal: Accounting scenarios and editorial review

Review date: 5 November 2026. This continuation builds on the real MDX component system and the /tammy-blog skill, keeping the approved Soft Precision typography, forest, sage, ivory and paper-like surfaces. All user-facing financial numbers are explicitly fictional worked examples.

## New editorial instruments

- `ScenarioExplorer` in `components/mdx/scenario-explorer.tsx`: tab-like pressed buttons, a labelled fieldset, live explanatory content and two to four explicitly distinct examples. No server mutation and no record storage.
- `JournalEntry` in `components/mdx/journal-entry.tsx`: semantic accessible accounting table, debit and credit columns with integer-cent validation and a balancing assertion; throws on invalid/unbalanced examples instead of displaying fabricated records.
- `ReviewChecklist` in `components/mdx/review-checklist.tsx`: interactive reader checklist using native checkboxes, a live count and reset. It explicitly disclaims saved accounting state or official validation.
- Registered centrally in `components/mdx/tammy-components.tsx`, with scoped styles in `app/blog/mdx.css`.
- Three original standalone SVG illustrations: `journal-gst-two-periods.svg`, `journal-deposit-pathways.svg`, `journal-month-end-close.svg`. These show concepts, not financial measurements. All figures, labels, totals and dates are semantic HTML/MDX.

## Article inventory and claim ledger

### `which-quarter-gets-the-gst`

**Claim:** With the GST cash method, GST is generally reported when payment is received (to the extent received); with the non-cash method, the earlier of invoice issue or receipt of payment determines the tax period (subject to specific rules).

**Sources verified on 5 November 2026:**
- ATO small-business education: https://smallbusiness.taxsuperandyou.gov.au/goods-and-services-tax/accounting-for-gst-on-a-cash-basis
- ATO GSTR 2004/4 paras 53–57: https://www.ato.gov.au/law/view/document?LocID=%22GST%2FGSTR20044%2FNAT%2FATO%2Ffp24%22&PiT=20240131000001
- ATO choosing an accounting method: https://smallbusiness.taxsuperandyou.gov.au/goods-and-services-tax/choosing-an-accounting-method

**Illustrative scenario:** fully taxable sale $1,100 incl GST = $1,000 ex GST + $100 GST; invoice 25 June, full payment 8 July, quarterly reporter. Cash method attributes $100 to July–September; non-cash attributes $100 to April–June. No earlier deposit, progressive supply or adjustment. Other purchases and BAS labels are excluded.

### `not-every-bank-deposit-is-sales`

**Claim:** A bank deposit records a cash receipt, not necessarily sales income. Bank statements and financing agreements must be retained to explain underlying transactions. Owner contributions and ordinary loan proceeds require different classifications from a sale.

**Sources verified on 5 November 2026:**
- ATO bank-account record keeping: https://smallbusiness.taxsuperandyou.gov.au/record-keeping/bank-accounts
- business.gov.au cash flow and funding: https://business.gov.au/guide/guide-to-managing-cash-flow

**Illustrative scenario:** GST-registered sole trader, new cash sale $2,200 incl $200 GST and $2,000 ex-GST sales, business loan $5,000 and capital introduced $1,500. Total bank receipts $8,700. Teaching journal: bank debit $8,700; credits of $2,000 sales + $200 GST payable + $5,000 loan liability + $1,500 owner's capital = $8,700. Example deliberately excludes previously posted receivables, related-party financing and company shareholder treatment.

### `the-month-end-review-that-found-220`

**Claim:** Regular bank reconciliation can reveal duplicated or omitted accounting entries. Direction matters: where ledger cash is $220 lower than the bank, a duplicate book payment is one plausible cause. Correct the verified posting with an audit trail rather than creating a new bank movement.

**Sources verified on 5 November 2026:**
- ATO record-keeping tasks: https://smallbusiness.taxsuperandyou.gov.au/record-keeping/when-to-do-record-keeping-and-reporting-tasks
- ATO bank-record tips: https://smallbusiness.taxsuperandyou.gov.au/record-keeping/tips-for-banking-records

**Illustrative scenario:** statement closing $15,000, ledger $14,780; second mistaken $220 supplier payment posting is removed/reversed through appropriate accounting procedures. Corrected ledger cash $15,000. GST, expense, payable and audit changes require separate review of the actual original entry. The user checklist is ephemeral and not evidence of completed review.

## Human editorial and visual review

- Opening situations are concrete. No generic promise, invented accountant persona, false social proof, fake customer quotes or unsupported ATO registration.
- Equations, assumptions, dates, source links and worked financial examples are outside the illustrations.
- New components encode a real accounting workflow, not generic CTA card layouts, colored badges, arbitrary progress rings or decorative data visualization.
- Use existing token and font roles and responsive article width. Native controls, keyboard focus, chart data visibility, semantics and reduced-motion styles remain present.
- Tests verify interactive state and reset, journal-entry balancing errors, article accessibility, source/inventory links, image presence and arithmetic.
- This is a manual design review informed by the installed design and editorial skills. No automated browser screenshot inspection has been claimed.

## Release gate

The public canonical remains the pre-existing ChatGPT Sites hostname. Code changes and passing GitHub CI do not imply a live deployment or activation of `tammy.tax`. Invoice-PDF browser E2E and the existing stacked site PR series remain outside this article-focused change.
