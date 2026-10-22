# Instructions

- Following Playwright test failed.
- Explain why, be concise, respect Playwright best practices.
- Provide a snippet of code with the fix, if possible.

# Test info

- Name: purchase-payments.spec.ts >> purchases/payments-gst: opening without a document and an earlier-period legacy payment retain distinct review outcomes
- Location: tests/e2e/purchase-payments.spec.ts:65:1

# Error details

```
AssertionError: purchase PDF is missing 01a1229f-b4b8-72c9-bdb3-88a23482a796
```

# Page snapshot

```yaml
- generic [ref=e5]:
  - complementary [ref=e6]:
    - generic [ref=e7]:
      - generic [ref=e8]: Tammy
      - img [ref=e9]
    - navigation "Primary" [ref=e10]:
      - list [ref=e11]:
        - listitem [ref=e12]:
          - link "Overview" [ref=e13] [cursor=pointer]:
            - /url: /overview
            - img [ref=e14]
            - generic [ref=e17]: Overview
        - listitem [ref=e18]:
          - link "Documents" [ref=e19] [cursor=pointer]:
            - /url: /documents
            - img [ref=e20]
            - generic [ref=e23]: Documents
        - listitem [ref=e24]:
          - link "Invoices" [ref=e25] [cursor=pointer]:
            - /url: /invoices
            - img [ref=e26]
            - generic [ref=e29]: Invoices
        - listitem [ref=e30]:
          - link "Banking" [ref=e31] [cursor=pointer]:
            - /url: /banking
            - img [ref=e32]
            - generic [ref=e34]: Banking
        - listitem [ref=e35]:
          - link "Chart of accounts" [ref=e36] [cursor=pointer]:
            - /url: /accounting/chart
            - img [ref=e37]
            - generic [ref=e40]: Chart of accounts
        - listitem [ref=e41]:
          - link "Journals" [ref=e42] [cursor=pointer]:
            - /url: /accounting/journals
            - img [ref=e43]
            - generic [ref=e45]: Journals
        - listitem [ref=e46]:
          - link "Trial balance" [ref=e47] [cursor=pointer]:
            - /url: /accounting/trial-balance
            - img [ref=e48]
            - generic [ref=e52]: Trial balance
        - listitem [ref=e53]:
          - link "GST & BAS" [ref=e54] [cursor=pointer]:
            - /url: /gst-bas
            - img [ref=e55]
            - generic [ref=e57]: GST & BAS
        - listitem [ref=e58]:
          - link "Audit trail" [ref=e59] [cursor=pointer]:
            - /url: /audit
            - img [ref=e60]
            - generic [ref=e63]: Audit trail
      - list [ref=e64]:
        - listitem [ref=e65]:
          - link "Settings" [ref=e66] [cursor=pointer]:
            - /url: /settings
            - img [ref=e67]
            - generic [ref=e70]: Settings
  - generic [ref=e71]:
    - banner [ref=e72]:
      - generic [ref=e75]: Local data
      - button "TB Tammy Business" [ref=e76]:
        - generic [ref=e77]: TB
        - generic [ref=e78]: Tammy Business
        - img [ref=e79]
    - main [ref=e81]:
      - status "Local engine" [ref=e82]: Local engine ready. Offline. No cloud required.
      - generic [ref=e83]:
        - generic [ref=e84]:
          - generic [ref=e85]:
            - heading "GST & BAS" [level=1] [ref=e86]
            - paragraph [ref=e87]: A local workpaper from invoice sales and reviewed expenses. Tammy does not lodge this BAS.
          - generic [ref=e88]:
            - paragraph [ref=e89]: Draft — not lodged
            - paragraph [ref=e90]: Review locally when ready.
        - group "BAS export" [ref=e91]:
          - generic [ref=e92]:
            - button "Export PDF" [ref=e93]
            - button "Export CSV" [ref=e94]
            - button "Refresh export identity" [ref=e95]
            - paragraph [ref=e96]: Synthetic Purchase Acceptance Pty Ltd · ABN 51824753556
          - status [ref=e97]: BAS PDF exported.
        - region "Reporting support" [ref=e98]:
          - generic [ref=e99]:
            - heading "GST workpaper" [level=2] [ref=e100]
            - status [ref=e102]:
              - paragraph [ref=e103]: Available in this build
              - paragraph [ref=e104]: Tammy supports a local invoice and reconciled purchase GST workpaper.
          - generic [ref=e105]:
            - heading "Full BAS return" [level=2] [ref=e106]
            - status [ref=e108]:
              - paragraph [ref=e109]: Unsupported in this build
              - paragraph [ref=e110]: This report is not supported for the selected entity and year.
        - region "BAS reporting period" [ref=e111]:
          - generic [ref=e112]:
            - text: Period start
            - textbox "Period start" [ref=e113]: 2026-10-01
          - generic [ref=e114]:
            - text: Period end
            - textbox "Period end" [ref=e115]: 2026-10-09
          - button "Regenerate local draft" [ref=e116]
          - paragraph [ref=e117]: Saved figures remain visible until the new draft is created.
        - generic [ref=e118]:
          - region "BAS source review" [ref=e119]:
            - paragraph [ref=e120]: Cash basis · sales follow recorded payments
            - paragraph [ref=e121]: "Saved reporting period: 01/10/2026 – 09/10/2026"
            - paragraph [ref=e122]: "Saved workpaper: 01a1229f-d7cd-70ea-bdc9-f95981e4bed9 · version 1"
            - paragraph [ref=e123]: 3 retained purchase review rows include legacy candidates, evidence suspense, exclusions or future claims. Review the complete typed purchase details before relying on this draft.
            - paragraph [ref=e124]: 2 reviewed purchases have no recorded payment evidence and are excluded from GST credits in this cash-basis draft.
          - generic [ref=e125]:
            - generic [ref=e126]:
              - paragraph [ref=e127]: Sales (G1)
              - paragraph [ref=e128]: $0.00
            - generic [ref=e129]:
              - paragraph [ref=e130]: GST on sales (1A)
              - paragraph [ref=e131]: $0.00
            - generic [ref=e132]:
              - paragraph [ref=e133]: GST credits (1B)
              - paragraph [ref=e134]: $0.00
            - generic [ref=e135]:
              - paragraph [ref=e136]: Net GST payable
              - paragraph [ref=e137]: $0.00
          - generic [ref=e138]:
            - generic [ref=e139]:
              - heading "Invoice sales sources" [level=2] [ref=e140]
              - paragraph [ref=e141]: GST-inclusive sales and GST attributed to this period. Out-of-scope lines are excluded.
            - paragraph [ref=e142]: No eligible invoice sales are attributed to this period.
          - generic [ref=e143]:
            - generic [ref=e144]:
              - generic [ref=e145]:
                - heading "Reviewed source documents" [level=2] [ref=e146]
                - paragraph [ref=e147]: 01/10/2026 – 09/10/2026
              - generic [ref=e148]:
                - img [ref=e149]
                - text: 0 sources
            - paragraph [ref=e153]: No reviewed purchase documents are included in GST credits for this period.
          - generic [ref=e154]:
            - generic [ref=e155]:
              - generic [ref=e156]:
                - heading "Reconciled purchase sources" [level=2] [ref=e157]
                - paragraph [ref=e158]: Saved source allocations and claimed GST for this workpaper. Invoice, actual payment and claim-period dates are separate.
              - generic [ref=e159]:
                - table "Saved BAS purchase sources" [ref=e160]:
                  - rowgroup [ref=e161]:
                    - row "Invoice date Actual payment Claim period Allocation Gross Supplier / eligible GST Claimed GST Review" [ref=e162]:
                      - columnheader "Invoice date" [ref=e163]
                      - columnheader "Actual payment" [ref=e164]
                      - columnheader "Claim period" [ref=e165]
                      - columnheader "Allocation" [ref=e166]
                      - columnheader "Gross" [ref=e167]
                      - columnheader "Supplier / eligible GST" [ref=e168]
                      - columnheader "Claimed GST" [ref=e169]
                      - columnheader "Review" [ref=e170]
                  - rowgroup
                - paragraph [ref=e171]: No purchase GST is claimed in this saved period.
            - region "Purchase GST review outcomes" [ref=e172]:
              - generic [ref=e173]:
                - heading "Purchase reviews and future claims" [level=2] [ref=e174]
                - paragraph [ref=e175]: Suspense and provisional legacy amounts are separate from GST credits (1B).
                - paragraph [ref=e176]: "Provisional legacy candidate GST: $10.00 · entitlement remains subject to review."
              - generic [ref=e177]:
                - generic [ref=e178]:
                  - heading "Unresolved legacy source" [level=3] [ref=e179]
                  - paragraph [ref=e180]: Eligible $0.00 · unclaimed $0.00
                - paragraph [ref=e181]: Legacy source needs review of eligibility, settlement and prior GST claims.
                - paragraph [ref=e182]: Invoice 09/10/2026 · payment — · affected period 01/10/2026 – 09/10/2026
                - button "Review purchase source" [ref=e183]
                - generic [ref=e184]:
                  - paragraph [ref=e185]: These are saved snapshot facts. Confirm the current source and supporting records before retaining a new decision.
                  - group [ref=e186]:
                    - group [ref=e187]:
                      - generic "Review legacy purchase / opening provenance" [ref=e188] [cursor=pointer]
                      - option "Reviewed noncreditable" [selected]
                      - option "Confirmed unpaid through balance date"
                      - option "Already claimed in an earlier BAS"
                      - option "Duplicate of an existing transaction"
                      - option "Evidence only, linked to an existing purchase"
                      - option "Existing settlement remains unsupported"
                      - option "Choose a reviewed retained document" [selected]
                      - option "EARLIER-LEGACY-CASH-PAYMENT.pdf · version 2"
                      - option "OPENING-SETTLEMENT-PROOF.pdf · version 2"
              - generic [ref=e189]:
                - generic [ref=e190]:
                  - heading "Unsupported" [level=3] [ref=e191]
                  - paragraph [ref=e192]: Eligible $10.00 · unclaimed $10.00
                - paragraph [ref=e193]: Reviewed actual 2 October cash payment of a prior-quarter legacy invoice; settlement allocation and any GST claim remain unsupported in this workpaper
                - paragraph [ref=e194]: Invoice 10/06/2026 · payment — · affected period 01/10/2026 – 09/10/2026
                - group [ref=e195]:
                  - generic "Retained legacy decision" [ref=e196] [cursor=pointer]
                - button "Review purchase source" [ref=e197]
                - generic [ref=e198]:
                  - paragraph [ref=e199]: These are saved snapshot facts. Confirm the current source and supporting records before retaining a new decision.
                  - group [ref=e200]:
                    - group [ref=e201]:
                      - generic "Review legacy purchase / opening provenance" [ref=e202] [cursor=pointer]
                      - option "Reviewed noncreditable" [selected]
                      - option "Confirmed unpaid through balance date"
                      - option "Already claimed in an earlier BAS"
                      - option "Duplicate of an existing transaction"
                      - option "Evidence only, linked to an existing purchase"
                      - option "Existing settlement remains unsupported"
                      - option "Choose a reviewed retained document" [selected]
                      - option "EARLIER-LEGACY-CASH-PAYMENT.pdf · version 2"
                      - option "OPENING-SETTLEMENT-PROOF.pdf · version 2"
              - generic [ref=e203]:
                - generic [ref=e204]:
                  - heading "Reviewed" [level=3] [ref=e205]
                  - paragraph [ref=e206]: Eligible $10.00 · unclaimed $0.00
                - paragraph [ref=e207]: Retained conversion source has no document and remains unpaid through 9 October; no new expense, settlement or GST credit is posted
                - paragraph [ref=e208]: Invoice — · payment — · affected period 01/10/2026 – 09/10/2026
                - group [ref=e209]:
                  - generic "Retained legacy decision" [ref=e210] [cursor=pointer]
                - generic [ref=e211]:
                  - paragraph [ref=e212]: These are saved snapshot facts. Confirm the current source and supporting records before retaining a new decision.
                  - group [ref=e213]:
                    - group [ref=e214]:
                      - generic "Review legacy purchase / opening provenance" [ref=e215] [cursor=pointer]
                      - option "Reviewed noncreditable" [selected]
                      - option "Confirmed unpaid through balance date"
                      - option "Already claimed in an earlier BAS"
                      - option "Duplicate of an existing transaction"
                      - option "Existing settlement remains unsupported"
                      - option "Choose a reviewed retained document" [selected]
                      - option "EARLIER-LEGACY-CASH-PAYMENT.pdf · version 2"
                      - option "OPENING-SETTLEMENT-PROOF.pdf · version 2"
          - paragraph [ref=e216]: "Status: Draft — not lodged. This screen has no lodge, submit or declaration control."
```

# Test source

```ts
  578 |       `Selected claim period ${date(source.claimPeriod?.periodStart)} to ${date(source.claimPeriod?.periodEnd)}`,
  579 |       `release posting date ${date(source.releasePostingDate)}`,
  580 |       `gross allocated $${amount(source.allocatedGross)}`,
  581 |       `supplier GST $${amount(source.supplierGst)}`,
  582 |       `eligible GST $${amount(source.eligibleGst)}`,
  583 |       `claimed GST $${amount(source.claimedGst)}`,
  584 |       `Evidence ${source.evidence?.sourceId ?? "not retained"}`,
  585 |       `version ${source.evidence?.sourceVersion.toString() ?? "not retained"}`,
  586 |       `hash ${hash(source.evidence?.sourceHash) || "not retained"}`,
  587 |       `Source revision ${source.sourceRevision}`,
  588 |       `source hash ${hash(source.sourceHash)}`,
  589 |     ])
  590 |       assert.ok(
  591 |         block.includes(token),
  592 |         `purchase PDF row ${source.postingId}/${source.allocationSequence} is missing ${token}`,
  593 |       );
  594 |     claimedFromPdf += pdfMinorUnits(block, "claimed GST");
  595 |   }
  596 |   const total = normalized.match(/GST credits 1B\s*\$?(-?[0-9,]+\.[0-9]{2})/iu)?.[1];
  597 |   assert.ok(total, "purchase PDF is missing the saved GST credits 1B total");
  598 |   const parsedTotal = pdfMinorUnits(`GST credits 1B ${total}`, "GST credits 1B");
  599 |   assert.equal(claimedFromPdf, parsedTotal, "parsed purchase PDF row amounts must reconcile to 1B");
  600 |   assert.equal(parsedTotal, bas.gstCredits1b?.minorUnits);
  601 | }
  602 | 
  603 | function assertPurchasePdf(pdf: string, bas: BasWorkpaper, pages: readonly string[]) {
  604 |   const normalized = pdf.replace(/\s+/gu, " ");
  605 |   const tokens = pdf.replace(/\s+/gu, "");
  606 |   assert.match(normalized, /Reconciled purchase source allocations/u);
  607 |   assert.match(normalized, /Purchase evidence, timing and legacy review outcomes/u);
  608 |   assertPurchasePdfRows(pages, bas);
  609 |   for (const review of bas.purchaseReviews) {
  610 |     for (const token of [
  611 |       review.reason,
  612 |       hash(review.sourceHash),
  613 |       amount(review.eligibleGst),
  614 |       amount(review.unclaimedGst),
  615 |     ])
  616 |       if (token) assert.ok(normalized.includes(token), `purchase PDF is missing ${token}`);
  617 |     const future = review.futureClaimObligation;
  618 |     const reviewDocumentId = review.documentId ?? future?.documentId;
  619 |     assert.ok(
  620 |       normalized.includes(`Review document ${reviewDocumentId ?? "not retained"}`),
  621 |       `purchase PDF is missing review document ${reviewDocumentId ?? "not retained"}`,
  622 |     );
  623 |     if (future) {
  624 |       for (const token of [
  625 |         future.deferralId,
  626 |         future.organisationId,
  627 |         date(future.affectedPeriod?.periodStart),
  628 |         date(future.affectedPeriod?.periodEnd),
  629 |         hash(future.deferralHash),
  630 |         future.documentId,
  631 |         amount(future.allocatedDeferredGst),
  632 |         date(future.portion?.requiredReturnDueDate),
  633 |         future.portion?.dueDateEvidence?.sourceId ?? "",
  634 |         future.portion?.dueDateEvidence?.sourceVersion.toString() ?? "",
  635 |         hash(future.portion?.dueDateEvidence?.sourceHash),
  636 |         date(future.portion?.normalEventDate),
  637 |         date(future.portion?.earliestOtherwiseEligiblePeriod?.periodStart),
  638 |         date(future.portion?.earliestOtherwiseEligiblePeriod?.periodEnd),
  639 |         date(future.portion?.ordinaryLastClaimDate),
  640 |         date(future.portion?.evidenceHeldDate),
  641 |         future.portion?.source.case ?? "",
  642 |         future.portion?.source.case === "payment" ? future.portion.source.value.paymentId : "",
  643 |         future.portion?.source.case === "payment"
  644 |           ? hash(future.portion.source.value.paymentHash)
  645 |           : "",
  646 |         future.portion?.source.case === "posting" ? future.portion.source.value.postingId : "",
  647 |         future.portion?.source.case === "posting"
  648 |           ? hash(future.portion.source.value.postingHash)
  649 |           : "",
  650 |         future.portion?.source.case ? String(future.portion.source.value.allocationSequence) : "",
  651 |         date(future.timingReview?.reviewedAsOf),
  652 |         future.timingReview?.reason ?? "",
  653 |         String(future.timingReview?.allowablePeriodConfirmed),
  654 |         String(future.timingReview?.ordinaryTimeLimitConfirmed),
  655 |         String(future.timingReview?.evidenceHeldConfirmed),
  656 |         future.actorUserId,
  657 |         timestamp(future.recordedAt),
  658 |       ])
  659 |         if (token) assert.ok(normalized.includes(token), `purchase PDF is missing ${token}`);
  660 |     }
  661 |     const decision = review.legacyReview;
  662 |     if (decision) {
  663 |       for (const token of [
  664 |         decision.id,
  665 |         decision.reason,
  666 |         dispositionNames[decision.disposition],
  667 |         date(decision.reportingBalanceDate),
  668 |         decision.actorUserId,
  669 |         hash(decision.sourceHash),
  670 |         decision.linkedPostingId ?? "",
  671 |         date(decision.affectedPeriod?.periodStart),
  672 |         date(decision.affectedPeriod?.periodEnd),
  673 |         date(decision.previouslyClaimedPeriod?.periodStart),
  674 |         date(decision.previouslyClaimedPeriod?.periodEnd),
  675 |         decision.claimEvidence?.sourceId ?? "",
  676 |         hash(decision.claimEvidence?.sourceHash),
  677 |       ])
> 678 |         if (token) assert.ok(normalized.includes(token), `purchase PDF is missing ${token}`);
      |                           ^ AssertionError: purchase PDF is missing 01a1229f-b4b8-72c9-bdb3-88a23482a796
  679 |     }
  680 |     const legacySource = review.legacySource?.source;
  681 |     if (legacySource?.case === "document") {
  682 |       for (const token of [
  683 |         legacySource.value.documentId,
  684 |         legacySource.value.reviewedVersion.toString(),
  685 |         hash(legacySource.value.originalContentHash),
  686 |         hash(legacySource.value.reviewedSnapshotHash),
  687 |       ])
  688 |         assert.ok(normalized.includes(token), `purchase PDF is missing legacy source ${token}`);
  689 |     } else if (legacySource?.case === "openingPayable") {
  690 |       for (const token of [
  691 |         legacySource.value.openingPayableId,
  692 |         legacySource.value.sourceVersion.toString(),
  693 |         hash(legacySource.value.sourceHash),
  694 |       ])
  695 |         assert.ok(normalized.includes(token), `purchase PDF is missing opening payable ${token}`);
  696 |     }
  697 |   }
  698 |   for (const link of bas.legacyPurchaseSupport) {
  699 |     assert.ok(
  700 |       tokens.includes(link.reviewId),
  701 |       `purchase PDF is missing support review ${link.reviewId}`,
  702 |     );
  703 |     if (link.source) {
  704 |       assert.ok(
  705 |         tokens.includes(link.source.sourceId),
  706 |         `purchase PDF is missing support source ${link.source.sourceId}`,
  707 |       );
  708 |       assert.ok(
  709 |         tokens.includes(hash(link.source.sourceHash)),
  710 |         "purchase PDF is missing a support hash",
  711 |       );
  712 |       assert.ok(
  713 |         normalized.includes(link.source.sourceVersion.toString()),
  714 |         "purchase PDF is missing a support source version",
  715 |       );
  716 |     }
  717 |   }
  718 |   assert.ok(normalized.includes(amount(bas.gstCredits1b)), "purchase PDF is missing 1B");
  719 |   if (bas.legacyCandidateGst)
  720 |     assert.ok(
  721 |       normalized.includes(amount(bas.legacyCandidateGst)),
  722 |       "purchase PDF is missing the separate legacy candidate",
  723 |     );
  724 |   else
  725 |     assert.ok(
  726 |       normalized.includes("not retained"),
  727 |       "purchase PDF must label an absent legacy candidate",
  728 |     );
  729 | }
  730 | 
  731 | /** Parses saved artifacts independently and checks them against the exact retained workpaper. */
  732 | export async function assertPurchaseExportArtifacts(
  733 |   csvFile: string,
  734 |   pdfFile: string,
  735 |   bas: BasWorkpaper,
  736 | ) {
  737 |   const [csv, pages] = await Promise.all([parseReportCsv(csvFile), readPdfPages(pdfFile)]);
  738 |   const pdf = pages.join("\f");
  739 |   assertPurchaseCsv(csv, bas);
  740 |   assertPurchasePdf(pdf, bas, pages);
  741 | }
  742 | 
```