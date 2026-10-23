import { create } from "@bufbuild/protobuf";
import type { Document } from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import * as p from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { useEffect, useState } from "react";
import type { TammyConveyorModule } from "../../../conveyor/client";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { purchaseCodecs } from "./purchase-codecs";
import { retainedDocumentFromRef, retainedRef } from "./purchase-evidence-fields";
import {
  civil,
  commandContext,
  dateLabel,
  iso,
  PurchaseCheck,
  PurchaseChoice,
  PurchaseField,
} from "./purchase-fields";
import type { ProposePurchase } from "./purchase-posting-form";
import { RetainedDocumentPicker } from "./retained-document-picker";

export function PurchaseClaimForm({
  document,
  workspace,
  documentApi,
  accounting,
  evidence,
  payments,
  deferral,
  propose,
  onError,
  retainedRequest,
}: {
  readonly document: Document;
  readonly workspace: AuthenticatedWorkspace;
  readonly documentApi: Pick<TammyConveyorModule<"documents">, "listDocuments">;
  readonly accounting: p.PurchaseAccounting;
  readonly evidence: p.PurchaseTaxEvidence | undefined;
  readonly payments: readonly p.PurchasePayment[];
  readonly deferral?: p.PurchaseGstDeferral | undefined;
  readonly propose: ProposePurchase;
  readonly onError: (message: string) => void;
  readonly retainedRequest?:
    | p.AttributePurchaseGstRequest
    | p.DeferPurchaseGstClaimRequest
    | undefined;
}) {
  const posting = accounting.posting!;
  const [open, setOpen] = useState(Boolean(retainedRequest));
  const retainedPortion = retainedRequest?.portions[0];
  const retainedSource = retainedPortion?.source;
  const retainedAttribute =
    retainedRequest && "claimPeriod" in retainedRequest ? retainedRequest : undefined;
  const retainedDeferralRequest =
    retainedRequest && "affectedPeriod" in retainedRequest ? retainedRequest : undefined;
  const retainedPeriod = retainedAttribute?.claimPeriod ?? retainedDeferralRequest?.affectedPeriod;
  const [sourceId, setSourceId] = useState(
    retainedSource?.case === "payment" ? retainedSource.value.paymentId : "",
  );
  const [sequence, setSequence] = useState(
    String(
      retainedSource?.case === "payment"
        ? retainedSource.value.allocationSequence
        : retainedSource?.case === "posting"
          ? retainedSource.value.allocationSequence
          : 1,
    ),
  );
  const [earliestStart, setEarliestStart] = useState(
    iso(retainedPortion?.earliestOtherwiseEligiblePeriod?.periodStart),
  );
  const [earliestEnd, setEarliestEnd] = useState(
    iso(retainedPortion?.earliestOtherwiseEligiblePeriod?.periodEnd),
  );
  const [due, setDue] = useState(iso(retainedPortion?.requiredReturnDueDate));
  const [deadline, setDeadline] = useState(iso(retainedPortion?.ordinaryLastClaimDate));
  const [proof, setProof] = useState<Document | undefined>(() =>
    retainedDocumentFromRef(retainedPortion?.dueDateEvidence, workspace.organisationId!),
  );
  const [periodStart, setPeriodStart] = useState(iso(retainedPeriod?.periodStart));
  const [periodEnd, setPeriodEnd] = useState(iso(retainedPeriod?.periodEnd));
  const [release, setRelease] = useState(iso(retainedAttribute?.releasePostingDate));
  const [asOf, setAsOf] = useState(iso(retainedRequest?.timingReview?.reviewedAsOf));
  const [reason, setReason] = useState(
    retainedDeferralRequest?.reason ?? retainedRequest?.timingReview?.reason ?? "",
  );
  const [confirmed, setConfirmed] = useState(
    Boolean(
      retainedRequest?.timingReview?.allowablePeriodConfirmed &&
        retainedRequest.timingReview.ordinaryTimeLimitConfirmed &&
        retainedRequest.timingReview.evidenceHeldConfirmed,
    ),
  );
  const [retainedIndex, setRetainedIndex] = useState("");
  const retained = retainedIndex === "" ? undefined : deferral?.portions[Number(retainedIndex)];
  useEffect(() => {
    if (!retainedAttribute?.deferralId || retainedIndex || !deferral) return;
    const key = sourceKey(retainedSource);
    const index = deferral.portions.findIndex((portion) => sourceKey(portion.source) === key);
    if (index >= 0) setRetainedIndex(String(index));
  }, [retainedAttribute?.deferralId, retainedIndex, deferral, retainedSource]);
  const payment = payments.find((item) => item.id === sourceId);
  const allocations =
    posting.gstBasis === GstBasis.CASH ? (payment?.allocations ?? []) : posting.allocations;
  const selectedAllocation = allocations.find((item) => item.sequence === Number(sequence));
  const submit = (mode: "claim" | "defer") => {
    try {
      const periodStartDate = civil(periodStart);
      const periodEndDate = civil(periodEnd);
      const reviewDate = civil(asOf);
      if (
        !confirmed ||
        !reason.trim() ||
        !periodStartDate ||
        !periodEndDate ||
        periodStart > periodEnd ||
        !reviewDate
      )
        throw new Error(
          "Review the allowable period, ordinary deadline and evidence, then enter the dates and reason.",
        );
      if (
        !evidence?.review?.heldDate ||
        evidence.review.classification === p.PurchaseEvidenceClassification.UNKNOWN
      )
        throw new Error(
          "Retain reviewed GST evidence with its actual held date before claiming or deferring.",
        );
      const earliestStartDate =
        retained?.earliestOtherwiseEligiblePeriod?.periodStart ?? civil(earliestStart);
      const earliestEndDate =
        retained?.earliestOtherwiseEligiblePeriod?.periodEnd ?? civil(earliestEnd);
      const dueDate = retained?.requiredReturnDueDate ?? civil(due);
      const lastDate = retained?.ordinaryLastClaimDate ?? civil(deadline);
      if (!earliestStartDate || !earliestEndDate || !dueDate || !lastDate || (!retained && !proof))
        throw new Error(
          "Select a distinct retained due-date proof and enter the actual required return due date, earliest eligible period and ordinary last claim date. These dates cannot be inferred from the invoice.",
        );
      if (
        !retained &&
        (!selectedAllocation ||
          selectedAllocation.eligibleGst!.minorUnits <= 0n ||
          (posting.gstBasis === GstBasis.CASH && !payment))
      )
        throw new Error(
          "Select an eligible owned allocation and its actual payment source for cash basis.",
        );
      const source =
        retained?.source ??
        (posting.gstBasis === GstBasis.CASH
          ? {
              case: "payment" as const,
              value: {
                postingId: posting.id,
                postingHash: posting.sourceHash,
                paymentId: payment!.id,
                paymentHash: payment!.sourceHash,
                allocationSequence: Number(sequence),
              },
            }
          : {
              case: "posting" as const,
              value: {
                postingId: posting.id,
                postingHash: posting.sourceHash,
                allocationSequence: Number(sequence),
              },
            });
      const portion = create(p.PurchaseGstSourcePortionSchema, {
        source,
        normalEventDate:
          retained?.normalEventDate ?? payment?.actualPaymentDate ?? posting.invoiceDate,
        earliestOtherwiseEligiblePeriod: {
          periodStart: earliestStartDate,
          periodEnd: earliestEndDate,
        },
        requiredReturnDueDate: dueDate,
        dueDateEvidence: retained?.dueDateEvidence ?? retainedRef(proof!),
        ordinaryLastClaimDate: lastDate,
        evidenceId: evidence.id,
        evidenceHeldDate: evidence.review.heldDate,
      });
      const common = {
        commandContext: commandContext(workspace),
        organisationId: workspace.organisationId!,
        documentId: document.id,
        expectedAccountingVersion: accounting.accountingVersion,
        portions: [portion],
        timingReview: {
          reviewedAsOf: reviewDate,
          allowablePeriodConfirmed: true,
          ordinaryTimeLimitConfirmed: true,
          evidenceHeldConfirmed: true,
          reason: reason.trim(),
        },
      };
      if (mode === "claim") {
        const date = civil(release);
        if (!date) throw new Error("Enter the actual GST release posting date.");
        propose(
          "attributePurchaseGst",
          purchaseCodecs.attribute.encodeRequest(
            create(p.AttributePurchaseGstRequestSchema, {
              ...common,
              claimPeriod: { periodStart: periodStartDate, periodEnd: periodEndDate },
              releasePostingDate: date,
              ...(retained ? { deferralId: deferral!.id } : {}),
            }),
          ),
          "Moves only the core-verified unclaimed GST for the selected portion from suspense into this claim period. The expense and payment are retained once.",
          "Confirm GST attribution",
        );
      } else
        propose(
          "deferPurchaseGstClaim",
          purchaseCodecs.defer.encodeRequest(
            create(p.DeferPurchaseGstClaimRequestSchema, {
              ...common,
              affectedPeriod: { periodStart: periodStartDate, periodEnd: periodEndDate },
              reason: reason.trim(),
            }),
          ),
          "Retains a future claim obligation and its deadline. This does not add a GST credit or change the purchase or payment amounts.",
          "Confirm GST deferral",
        );
    } catch (error) {
      onError(error instanceof Error ? error.message : "Complete the claim-period review.");
    }
  };
  return (
    <details
      className="work-surface overflow-hidden"
      open={open || Boolean(retainedRequest)}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="cursor-pointer border-b border-border px-5 py-4 text-[14px] font-semibold">
        Review claim period or defer GST
      </summary>
      <div className="grid gap-4 p-5">
        <p className="text-[13px] leading-6 text-muted-foreground">
          Delayed claims require actual due-date proof and review of the ordinary time limit. The
          core verifies ownership, remaining credit, closed periods and expiry.
        </p>
        {deferral ? (
          <>
            <p className="text-[12px] leading-5">
              Retained deferral {deferral.id}. Original decision amount is not a current remaining
              balance.
            </p>
            <PurchaseChoice
              label="Retained deferral portion"
              value={retainedIndex}
              onChange={setRetainedIndex}
            >
              <option value="">Review another source portion</option>
              {deferral.portions.map((portion, i) => (
                <option key={sourceKey(portion.source)} value={i}>
                  Allocation{" "}
                  {portion.source.case === "payment"
                    ? portion.source.value.allocationSequence
                    : portion.source.case === "posting"
                      ? portion.source.value.allocationSequence
                      : ""}{" "}
                  · {dateLabel(portion.normalEventDate)} · deadline{" "}
                  {dateLabel(portion.ordinaryLastClaimDate)}
                </option>
              ))}
            </PurchaseChoice>
          </>
        ) : null}
        {retained ? (
          <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-[12px] max-[560px]:grid-cols-1">
            <dt>Normal event</dt>
            <dd>{dateLabel(retained.normalEventDate)}</dd>
            <dt>Earliest eligible period</dt>
            <dd>
              {dateLabel(retained.earliestOtherwiseEligiblePeriod?.periodStart)} –{" "}
              {dateLabel(retained.earliestOtherwiseEligiblePeriod?.periodEnd)}
            </dd>
            <dt>Actual required due date</dt>
            <dd>{dateLabel(retained.requiredReturnDueDate)}</dd>
            <dt>Ordinary last claim date</dt>
            <dd>{dateLabel(retained.ordinaryLastClaimDate)}</dd>
            <dt>Due-date proof</dt>
            <dd className="break-all">{retained.dueDateEvidence?.sourceId}</dd>
          </dl>
        ) : (
          <>
            {posting.gstBasis === GstBasis.CASH ? (
              <PurchaseChoice
                label="Owned payment for claim review"
                value={sourceId}
                onChange={setSourceId}
              >
                <option value="">Choose an actual payment from the history page</option>
                {sourceId && !payments.some((item) => item.id === sourceId) ? (
                  <option value={sourceId}>Previously selected payment · {sourceId}</option>
                ) : null}
                {payments.map((item) => (
                  <option key={item.id} value={item.id}>
                    {dateLabel(item.actualPaymentDate)} · {item.id}
                  </option>
                ))}
              </PurchaseChoice>
            ) : (
              <p className="text-[12px] leading-5">
                Normal event: invoice {dateLabel(posting.invoiceDate)} · posting {posting.id}
              </p>
            )}
            <PurchaseChoice
              label="Owned allocation for claim review"
              value={sequence}
              onChange={setSequence}
            >
              {allocations
                .filter((item) => (item.eligibleGst?.minorUnits ?? 0n) > 0n)
                .map((item) => (
                  <option key={item.sequence} value={item.sequence}>
                    Allocation {item.sequence}
                  </option>
                ))}
            </PurchaseChoice>
            <div className="grid grid-cols-2 gap-2">
              <PurchaseField
                label="Earliest eligible period start"
                type="date"
                value={earliestStart}
                onChange={setEarliestStart}
              />
              <PurchaseField
                label="Earliest eligible period end"
                type="date"
                value={earliestEnd}
                onChange={setEarliestEnd}
              />
            </div>
            <PurchaseField
              label="Actual required return due date"
              type="date"
              value={due}
              onChange={setDue}
            />
            <RetainedDocumentPicker
              api={documentApi}
              workspace={workspace}
              label="Due-date proof"
              selected={proof}
              onChange={setProof}
              exclude={[document.id, evidence?.review?.source?.sourceId ?? ""]}
            />
            <PurchaseField
              label="Ordinary last claim date"
              type="date"
              value={deadline}
              onChange={setDeadline}
            />
          </>
        )}
        <p className="text-[12px]">
          Current evidence: {evidence?.id ?? "Missing"} · held{" "}
          {dateLabel(evidence?.review?.heldDate)}
        </p>
        <div className="grid grid-cols-2 gap-3 max-[560px]:grid-cols-1">
          <PurchaseField
            label="Claim / affected period start"
            type="date"
            value={periodStart}
            onChange={setPeriodStart}
          />
          <PurchaseField
            label="Claim / affected period end"
            type="date"
            value={periodEnd}
            onChange={setPeriodEnd}
          />
        </div>
        <PurchaseField
          label="GST release posting date (for claim)"
          type="date"
          value={release}
          onChange={setRelease}
        />
        <PurchaseField
          label="Timing review as-of date"
          type="date"
          value={asOf}
          onChange={setAsOf}
        />
        <PurchaseField label="Claim / deferral review reason" value={reason} onChange={setReason} />
        <PurchaseCheck
          label="Reviewed allowable period, ordinary time limit and evidence held"
          checked={confirmed}
          onChange={setConfirmed}
        />
        <div className="flex flex-wrap gap-3">
          <Button type="button" onClick={() => submit("claim")}>
            Attribute reviewed GST
          </Button>
          <Button type="button" onClick={() => submit("defer")}>
            Defer reviewed GST
          </Button>
        </div>
      </div>
    </details>
  );
}

function sourceKey(source: p.PurchaseGstSourcePortion["source"] | undefined): string {
  if (source?.case === "payment")
    return `payment:${source.value.postingId}:${source.value.paymentId}:${source.value.allocationSequence}`;
  if (source?.case === "posting")
    return `posting:${source.value.postingId}:${source.value.allocationSequence}`;
  return "";
}
