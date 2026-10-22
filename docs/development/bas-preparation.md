# Local BAS preparation rules

The phase-three implementation is in `services/core/internal/baspreparation` and the mounted local-product service. Tasks 1–10 of the [execution plan](../superpowers/plans/2026-10-05-bas-preparation-lifecycle.md) are implemented with focused native, TLS, and desktop verification. The integrated packaged journey and independent PDF/CSV artifact inspection remain pending under Task 11; phase three is not yet accepted as a whole.

The supported local workflow covers Simpler BAS GST, small/medium PAYGW, and notified-amount or notified-rate PAYGI without variation. It captures bounded source coverage and exact retained workpapers, validations, approvals, exports, and user-recorded external observations. It does not lodge with the ATO or claim official acceptance. Unknown or unsupported obligations and unclassified manual tax facts stay visible as blockers. Business income is shown for comparison and is never used to fill PAYGI T1.

## Pinned authority

Official references were reviewed on **2026-10-10**. Some ATO pages reject direct requests with HTTP 403; indexed content from the same official URL supplied the relevant instructions and example. These references inform the supported subset specified in the [approved design](../superpowers/specs/2026-10-04-bas-preparation-lifecycle-design.md); they do not establish the actual obligations, dates, or operative notice of a particular business.

| Reference | Rule used |
| --- | --- |
| [ATO software developer Simpler BAS guidance](https://softwaredevelopers.ato.gov.au/SimplerBAS) | The GST section reports G1, 1A and 1B. Other applicable statement sections still require classification. |
| [ATO BAS preparation tips](https://www.ato.gov.au/api/public/content/0-30bd4b2d-40c7-42bb-984a-adf4ee2fa327) | Report whole dollars, omitting cents rather than rounding up. |
| [ATO PAYG withholding instructions](https://www.ato.gov.au/businesses-and-organisations/preparing-lodging-and-paying/business-activity-statements-bas/pay-as-you-go-payg-withholding) | W1 is the reviewed payment total, W2 withholding from those payments, W3 other withholding and W4 withholding where an ABN was not quoted. Add the reported W2, W3 and W4 values for W5, then copy W5 to label 4. Withholding is an evidence input, not calculated from W1. |
| [ATO PAYG instalment instructions](https://www.ato.gov.au/api/public/content/0-c9dc388e-c500-43a2-863b-bbdc01b6ff3a) | The amount method uses the notified T7 amount at 5A. The rate method multiplies reviewed T1 by notified T2, reporting T11 at 5A. The selected method continues within the income year. The published example uses T1 $20,100 and T2 1.7%, producing $341.70 and reported T11/5A 341. |
| [ATO BAS due-date guidance](https://www.ato.gov.au/businesses-and-organisations/preparing-lodging-and-paying/business-activity-statements-bas/due-dates-for-lodging-and-paying-your-bas) | Nominal monthly dates use the next month's 21st; quarters ending March, June, September and December use 28 April, 28 July, 28 October and the following 28 February respectively. Actual dates depend on the statement and reporting arrangements. |

The supported form equations below are pinned by the approved design. A complete statement requires explicit decisions excluding every unsupported debit or credit section before applying this subset.

| Version | Value |
| --- | --- |
| `RuleVersion` | `ato-simpler-bas-2026-10-10` |
| `CalculatorVersion` | `tammy-bas-calculator-v1` |
| `FormVersion` | `ato-activity-statement-simpler-bas-v1` |
| `DueDateRuleVersion` | `au-bas-normal-dates-2026-10-10-v1` |

These are local implementation identifiers. They do not claim an official ATO schema or receipt version. Historical calculations must retain their versions; updating a rule creates another calculation.

## Amounts and traces

`AmountInput` distinguishes `UNSET`, `UNKNOWN`, `NOT_APPLICABLE`, `KNOWN_ZERO` and `KNOWN`. An active zero must be reviewed as zero. Inactive sections require explicit not-applicable labels; an active section uses known values for every atomic label, including reviewed zeros. Missing or unknown values fail calculation. Known amounts are nonnegative AUD cents; contradictory states and partial section applicability fail closed.

Atomic G1, 1A, 1B, W1, W2, W3 and W4 values truncate **after** their exact-cent aggregation. Derived labels use those reported whole-dollar values:

```text
W5 = reported W2 + reported W3 + reported W4
4  = reported W5
8A = reported 1A + reported 4 + reported 5A
8B = reported 1B
9  = absolute(reported 8A - reported 8B)
```

Direction is separately `PAYABLE`, `REFUNDABLE` or `NIL`, determined from reported 8A and 8B. Label 9 never contains a negative amount. An explicit reviewed decision is required for other obligations; unknown or active unsupported sections block the calculation.

Each trace retains a reduced rational exact-cent value, reported whole dollars, evidence references and the difference between exact cents and reported dollars converted to cents. Derived exact totals are independently retained. For example W2 $100.75, W3 $20.50 and W4 $30.99 report 100, 20 and 30: W5 is **150**, with an independent exact total of $152.24 and a difference of 224 cents. Three 99-cent atomic withholding labels report W5 zero. A label-9 difference can be signed: exact 8A $100 and 8B $99.99 yield reported label 9 of 1, exact magnitude 1 cent and a difference of −99 cents. Independent component truncation can also yield exact label 9 of zero and reported label 9 of 1: 1A and label 4 each have 99 cents, while 1B has 198 cents. The exact state remains `KNOWN_ZERO`, with a −100-cent difference and a refundable direction. This difference is provenance, not an adjustment to the form equation.

Integer sums, differences and exact rational results reject overflow beyond the supported signed 64-bit cent range. Percentage input never uses floating point or the money helper's nearest-cent rounding. `ParseDecimal` accepts a nonnegative base-10 decimal with optional percent sign, a signed-64-bit coefficient and up to 18 fractional digits. This is an implementation bound, not an ATO restriction. Exponents, signs, malformed decimals and overflow are rejected.

## PAYG instalment evidence

The selected method requires a retained notice reference and SHA256, a confirmed operative value, and an explicit prior-variation disposition. A current notice already reflecting a confirmed prior variation is historical provenance. An uncertain or newly initiated variation, required variation fields, or a 5B credit is unsupported.

For the rate method, T1 must be explicitly reviewed. Under the pinned design interpretation, its aggregated cents are truncated to reportable whole-dollar T1 before exact multiplication by T2. T11 retains sub-cent rational precision and truncates to reported whole dollars at 5A. Business income remains a separate comparison and cannot automatically supply T1. Fields belonging to the unused method must be empty, so contradictory data cannot disappear during calculation. The not-applicable method requires no active notice or amount payload.

The transactional capture and review path binds each reference to retained evidence for the exact organisation, profile, period and input revision. It populates the complete other-obligation inventory from the typed profile before setting `OtherObligationsReviewed`; the flag alone is not treated as proof of coverage. The pure calculator verifies states and arithmetic, while source readers and transaction-level checks establish retained provenance and currentness. They do not establish a notice's authenticity beyond the recorded evidence reference.

## Calendar and income-year identity

Candidate periods use civil dates and fixed Gregorian monthly or quarterly windows, independent of the organisation's financial-year end. The calendar stores nominal suggestions separately from a confirmed due date. It deliberately leaves weekend/holiday shifts and any concessions to independently evidenced confirmation; a nominal suggestion cannot confer authority. An external confirmed date may differ from the suggestion, without changing it.

Confirmation requires the exact canonical period, valid retained `SourceRef`, a due-date kind, a valid confirmed date, complete registration coverage and matching obligation cadences. Part-period registration, cadence changes, mixed windows and annual methods return unsupported outcomes. Invalid dates or forged guidance are rejected. Accepted dates and evidence hashes are copied so caller mutation cannot change the result.

`CheckIncomeYearMethod` uses a confirmed income-year identity and dates, including substituted accounting periods. A later GST quarter cannot reset the method. Renaming an overlapping income year cannot reset it either. The same method remains valid in a later period of that year; a separately confirmed, non-overlapping next income year can select another method.

## Preparation contracts

`proto/tammy/v1/bas_preparation.proto` defines the mounted local preparation service: 15 bounded queries and 14 commands, organisation-scoped through the shared authentication, command and pagination messages. The commands run through the ordinary transaction coordinator, durable audit, expected-version checks, and SQLCipher repositories. The desktop exposes them through the `basPreparation` Conveyor client. The packaged journey remains pending, so its result must not be described as complete phase-three acceptance.

History responses contain at most 200 shallow retained identities; exact calculation, frozen, external-observation and dispute-resolution reads carry their saved snapshots. Calendar requests span at most ten calendar years, keeping the complete monthly and quarterly suggestions within the bounded response. A calculation retains its workpaper, complete bounded coverage, profile, period, organisation, PAYG inputs and source revision vector alongside the hashes and rule versions. Optional input evidence permits incomplete facts to be retained while collecting. Successful readiness and freezing must still require the applicable evidence and supported obligation inventory in the transaction service; contract admission is not approval.

Aggregate `content_hash` and `version` support command comparison and update. Separate `input_hash` and `input_revision` bind calculations and validations to reviewed facts, so adding validation, acknowledgement or export history does not itself invalidate those facts. Preparation reads include saved PAYG inputs for that retained input identity and bounded warning acknowledgements, including inputs saved before regeneration. Chain commands pin the family, root, head, branch and active attempt, using explicit absence instead of invented identifiers. The initial regeneration can establish the family from explicit absence. Preparation reads expose the current chain separately from retained snapshots.

Five approval commands use the existing purpose-bound fresh-factor context and enforce business-lodger authority, actor/session, five-minute age, single-use consumption, and transaction rollback. Workspace administrators alone can prepare but cannot approve. Account TOTP enrollment is available through the existing identity flow even when SBR readiness or machine credentials are unavailable; setup does not grant approval or SBR machine authority. Corrections remain restricted to business lodgers without inventing a sixth fresh purpose. External observations always retain local `USER_RECORDED_UNVERIFIED` trust. Each retained record carries either the original event or a complete typed correction. A retraction preserves its kind and exact original reference; it cannot become an invented unknown outcome or acceptance. Summary records preserve the same distinction. The existing tax workpaper service remains compatible, and no BAS submission endpoint is declared.

PAYG input saves use the exact verified profile snapshot bound to the current calculation for the PAYGW category/prefill and PAYGI confirmed income-year identity, including the retained confirmation evidence. Current payroll and notice evidence remain separate inputs. The save path fails closed if that snapshot is absent, its retained profile identity does not match the calculation, or editable income-year fields differ from the pinned identity. `NOT_APPLICABLE` skips only the PAYGI income-year pin requirement.

New workspaces use the exact checksum of the bundled GST rule source. For existing workspaces carrying the known earlier setup reference, an administrator can explicitly repair only that exact old source tuple using expected-version checks, a reason, a future effective date and fresh TOTP. The repair changes the active profile reference; it does not rewrite prior calculations or frozen history. Current bundled checksum: `1da4e89d4fa785da7591070e8a6054f91df5ed02bb4ec10a0c894a15141506de`.

Capture reservations are bounded before body hydration: 1,000 candidate roots, 1,000 receipts, 16,384 physical keys, and 8 MiB of retained source payload. Calculations and frozen snapshots have separate 4 MiB and 8 MiB read budgets; local IPC permits up to 16 MiB to carry those accepted responses and their wrappers. These transport limits do not increase source or storage budgets.

## Verification

The pure foundation checks can be run without SQLCipher or a packaged application:

```sh
rtk proxy mise exec -- go test ./services/core/internal/baspreparation -count=1
rtk proxy mise exec -- go test -race ./services/core/internal/baspreparation -count=1
```

Golden and boundary tests cover exact/reportable values, signed trace differences, nil/unknown/zero/applicability, atomic-cent conservation, PAYGI amount/rate/sub-cent arithmetic, notice and variation rejection, unsupported obligations, overflow, leap dates, calendar authority and income-year method consistency. Native SQLCipher/TLS tests additionally cover source capture, currentness, role/factor authority, history, workpaper persistence, exact exports, rollback and replay. Desktop component checks and the packaged E2E provide separate UI and artifact evidence; the packaged integrated journey and independent artifact inspection are still pending.
