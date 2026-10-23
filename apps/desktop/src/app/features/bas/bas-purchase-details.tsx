import {
  PurchaseBasClassification,
  PurchaseReviewStatus,
  PurchaseUse,
} from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import type { BasPurchaseReview, BasWorkpaper } from "@tammy/connect-client/tammy/v1/tax_pb.js";
import {
  BasPurchaseReviewKind,
  BasPurchaseSourceFormat,
} from "@tammy/connect-client/tammy/v1/tax_pb.js";
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import type { TammyConveyorModule } from "../../../conveyor/client";
import { isPurchaseCorrectionCode } from "../../../shared/purchase-errors";
import { Button } from "../../components/ui/button";
import { SurfaceHeading } from "../../components/workspace-primitives";
import { purchaseCorrectionGuidance } from "../documents/purchase-accounting-panel";
import { authenticatedPurchaseRetry } from "../documents/purchase-attempt-recovery";
import { purchaseCodecs } from "../documents/purchase-codecs";
import {
  aud,
  purchaseAttemptScope,
  purchaseAttemptsFor,
} from "../documents/purchase-command-state";
import { dateLabel } from "../documents/purchase-fields";
import { PurchaseLegacyReviewPanel } from "../documents/purchase-legacy-review-panel";
import type { ProposePurchase } from "../documents/purchase-posting-form";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

export interface BasPurchaseControls {
  readonly purchaseApi?: TammyConveyorModule<"purchases"> | undefined;
  readonly documentApi?: TammyConveyorModule<"documents"> | undefined;
  readonly accountingApi?: TammyConveyorModule<"accounting"> | undefined;
  readonly workspace?: AuthenticatedWorkspace | undefined;
  readonly onReviewDocument?: ((id: string) => void) | undefined;
  readonly onChanged?: (() => Promise<void>) | undefined;
}
function hex(bytes: Uint8Array | undefined) {
  return bytes ? [...bytes].map((byte) => byte.toString(16).padStart(2, "0")).join("") : "—";
}
function statusLabel(status: PurchaseReviewStatus) {
  switch (status) {
    case PurchaseReviewStatus.CLEAR:
      return "Reviewed";
    case PurchaseReviewStatus.NEEDS_EVIDENCE:
      return "Needs GST evidence";
    case PurchaseReviewStatus.NEEDS_TIMING_REVIEW:
      return "Needs claim-period review";
    case PurchaseReviewStatus.UNRESOLVED_LEGACY:
      return "Unresolved legacy source";
    case PurchaseReviewStatus.UNSUPPORTED:
      return "Unsupported";
    case PurchaseReviewStatus.DEFERRED:
      return "Deferred future claim";
    default:
      return "Review required";
  }
}
export function BasPurchaseDetails({
  value,
  ...controls
}: { readonly value: BasWorkpaper } & BasPurchaseControls) {
  if (value.purchaseSourceFormat !== BasPurchaseSourceFormat.RECONCILED_V1) return null;
  return (
    <div className="grid gap-4">
      <section className="work-surface overflow-hidden">
        <SurfaceHeading
          title="Reconciled purchase sources"
          description="Saved source allocations and claimed GST for this workpaper. Invoice, actual payment and claim-period dates are separate."
        />
        <div className="overflow-x-auto">
          <table
            className="work-table w-full text-left text-[12px]"
            aria-label="Saved BAS purchase sources"
          >
            <thead>
              <tr className="border-b border-border">
                <th className="p-4">Invoice date</th>
                <th className="p-4">Actual payment</th>
                <th className="p-4">Claim period</th>
                <th className="p-4">Allocation</th>
                <th className="p-4 text-right">Gross</th>
                <th className="p-4 text-right">Supplier / eligible GST</th>
                <th className="p-4 text-right">Claimed GST</th>
                <th className="p-4">Review</th>
              </tr>
            </thead>
            <tbody>
              {value.purchaseSources.map((source) => (
                <tr
                  className="border-b border-border last:border-0 align-top"
                  key={`${source.documentId}:${source.postingId}:${source.paymentId ?? ""}:${source.attributionId ?? ""}:${source.allocationSequence}`}
                >
                  <td className="p-4 whitespace-nowrap">{dateLabel(source.invoiceDate)}</td>
                  <td className="p-4 whitespace-nowrap">{dateLabel(source.actualPaymentDate)}</td>
                  <td className="p-4 whitespace-nowrap">
                    {dateLabel(source.claimPeriod?.periodStart)} –{" "}
                    {dateLabel(source.claimPeriod?.periodEnd)}
                  </td>
                  <td className="p-4">
                    {source.allocationSequence} ·{" "}
                    {source.use === PurchaseUse.PRIVATE ? "Private" : "Business"} ·{" "}
                    {source.basClassification === PurchaseBasClassification.CAPITAL
                      ? "Capital"
                      : source.basClassification === PurchaseBasClassification.NON_CAPITAL
                        ? "Non-capital"
                        : "Excluded"}
                  </td>
                  <td className="p-4 text-right whitespace-nowrap tabular-nums">
                    {aud(source.allocatedGross?.minorUnits)}
                  </td>
                  <td className="p-4 text-right whitespace-nowrap tabular-nums">
                    {aud(source.supplierGst?.minorUnits)} / {aud(source.eligibleGst?.minorUnits)}
                  </td>
                  <td className="p-4 text-right font-semibold whitespace-nowrap tabular-nums">
                    {aud(source.claimedGst?.minorUnits)}
                  </td>
                  <td className="p-3">
                    <details>
                      <summary className="cursor-pointer">Retained source details</summary>
                      <dl className="mt-2 grid gap-1 break-all">
                        <dt>Posting</dt>
                        <dd>
                          {source.postingId} · {hex(source.postingHash)}
                        </dd>
                        <dt>Payment</dt>
                        <dd>
                          {source.paymentId ?? "—"} · {hex(source.paymentHash)}
                        </dd>
                        <dt>Attribution</dt>
                        <dd>
                          {source.attributionId ?? "—"} · {hex(source.attributionHash)}
                        </dd>
                        <dt>Normal tax event</dt>
                        <dd>{dateLabel(source.normalTaxEventDate)}</dd>
                        <dt>Evidence</dt>
                        <dd>
                          {source.evidence?.sourceId} · version {source.evidence?.sourceVersion} ·{" "}
                          {hex(source.evidence?.sourceHash)}
                        </dd>
                        <dt>Source revision / hash</dt>
                        <dd>
                          {source.sourceRevision} · {hex(source.sourceHash)}
                        </dd>
                      </dl>
                    </details>
                    {controls.onReviewDocument ? (
                      <Button
                        type="button"
                        className="mt-2 h-11 text-[13px]"
                        onClick={() => controls.onReviewDocument!(source.documentId)}
                      >
                        Open purchase review
                      </Button>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {value.purchaseSources.length === 0 ? (
            <p className="p-5 text-[13px] text-muted-foreground">
              No purchase GST is claimed in this saved period.
            </p>
          ) : null}
        </div>
      </section>
      <section className="work-surface overflow-hidden" aria-label="Purchase GST review outcomes">
        <SurfaceHeading
          title="Purchase reviews and future claims"
          description="Suspense and provisional legacy amounts are separate from GST credits (1B)."
        />
        <div className="grid gap-3 p-5">
          {value.legacyCandidateGst ? (
            <p className="mt-2 text-[10px]">
              Provisional legacy candidate GST: {aud(value.legacyCandidateGst.minorUnits)} ·
              entitlement remains subject to review.
            </p>
          ) : null}
        </div>
        {value.purchaseReviews.map((review) => (
          <ReviewRow
            key={`${review.postingId ?? ""}:${review.paymentId ?? ""}:${review.deferralId ?? ""}:${legacySourceId(review)}:${review.kind}:${review.sourceRevision}`}
            value={review}
            workpaper={value}
            controls={controls}
          />
        ))}
        {value.purchaseReviews.length === 0 ? (
          <p className="p-4 text-[10px] text-muted-foreground">
            No retained purchase review outcomes for this snapshot.
          </p>
        ) : null}
      </section>
    </div>
  );
}
function legacySourceId(review: BasPurchaseReview): string {
  const source = review.legacySource?.source;
  if (source?.case === "document") return source.value.documentId;
  if (source?.case === "openingPayable") return source.value.openingPayableId;
  return "";
}
function ReviewRow({
  value,
  workpaper,
  controls,
}: {
  readonly value: BasPurchaseReview;
  readonly workpaper: BasWorkpaper;
  readonly controls: BasPurchaseControls;
}) {
  const future = value.futureClaimObligation;
  const documentId =
    value.documentId ??
    future?.documentId ??
    workpaper.purchaseSources.find((source) => source.postingId === value.postingId)?.documentId ??
    (value.legacySource?.source.case === "document"
      ? value.legacySource.source.value.documentId
      : undefined);
  return (
    <div className="grid gap-2 border-b border-border p-3 text-[10px] last:border-0">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="font-semibold">{statusLabel(value.status)}</h3>
        <p>
          Eligible {aud(value.eligibleGst?.minorUnits)} · unclaimed{" "}
          {aud(value.unclaimedGst?.minorUnits)}
        </p>
      </div>
      <p className="leading-5">{value.reason}</p>
      <p className="text-[9px] text-muted-foreground">
        Invoice {dateLabel(value.invoiceDate)} · payment {dateLabel(value.actualPaymentDate)} ·
        affected period {dateLabel(value.affectedPeriod?.periodStart)} –{" "}
        {dateLabel(value.affectedPeriod?.periodEnd)}
      </p>
      {future ? (
        <dl className="grid grid-cols-[minmax(130px,1fr)_minmax(0,2fr)] gap-1 text-[9px]">
          <dt>Future claim amount</dt>
          <dd>{aud(future.allocatedDeferredGst?.minorUnits)}</dd>
          <dt>Actual required return due</dt>
          <dd>{dateLabel(future.portion?.requiredReturnDueDate)}</dd>
          <dt>Ordinary last claim date</dt>
          <dd>{dateLabel(future.portion?.ordinaryLastClaimDate)}</dd>
          <dt>Earliest eligible period</dt>
          <dd>
            {dateLabel(future.portion?.earliestOtherwiseEligiblePeriod?.periodStart)} –{" "}
            {dateLabel(future.portion?.earliestOtherwiseEligiblePeriod?.periodEnd)}
          </dd>
          <dt>Evidence held</dt>
          <dd>{dateLabel(future.portion?.evidenceHeldDate)}</dd>
          <dt>Deferral</dt>
          <dd className="break-all">
            {future.deferralId} · {hex(future.deferralHash)}
          </dd>
          <dt>Due-date proof</dt>
          <dd className="break-all">
            {future.portion?.dueDateEvidence?.sourceId} ·{" "}
            {hex(future.portion?.dueDateEvidence?.sourceHash)}
          </dd>
          <dt>Decision reason</dt>
          <dd>{future.reason}</dd>
        </dl>
      ) : null}
      {value.legacyReview ? (
        <details>
          <summary className="cursor-pointer text-[9px]">Retained legacy decision</summary>
          <div className="mt-2 grid gap-1 text-[9px] break-all">
            <p>
              {value.legacyReview.id} · revision {value.legacyReview.sourceRevision} ·{" "}
              {hex(value.legacyReview.sourceHash)}
            </p>
            <p>{value.legacyReview.reason}</p>
            <p>Balance date: {dateLabel(value.legacyReview.reportingBalanceDate)}</p>
            {workpaper.legacyPurchaseSupport
              .filter((support) => support.reviewId === value.legacyReview!.id)
              .map((support) => (
                <p key={`${support.kind}:${support.sequence}`}>
                  Support {support.sequence}: {support.source?.sourceId} · version{" "}
                  {support.source?.sourceVersion} · {hex(support.source?.sourceHash)}
                </p>
              ))}
          </div>
        </details>
      ) : null}
      {documentId && controls.onReviewDocument ? (
        <Button
          type="button"
          className="justify-self-start h-7 text-[9px]"
          onClick={() => controls.onReviewDocument!(documentId)}
        >
          Review purchase source
        </Button>
      ) : null}
      {value.legacySource &&
      controls.workspace &&
      controls.purchaseApi &&
      controls.documentApi &&
      controls.accountingApi ? (
        <BasLegacyDecision
          review={value}
          controls={
            controls as Required<
              Pick<
                BasPurchaseControls,
                "workspace" | "purchaseApi" | "documentApi" | "accountingApi"
              >
            > &
              BasPurchaseControls
          }
          balanceDate={workpaper.periodEnd}
        />
      ) : value.kind === BasPurchaseReviewKind.OPENING_PAYABLE ? (
        <p className="text-warning">
          Opening settlements and GST credit allocation remain unsupported. Review existing opening
          provenance; a document link is not required.
        </p>
      ) : null}
    </div>
  );
}
function BasLegacyDecision({
  review,
  controls,
  balanceDate,
}: {
  readonly review: BasPurchaseReview;
  readonly controls: Required<
    Pick<BasPurchaseControls, "workspace" | "purchaseApi" | "documentApi" | "accountingApi">
  > &
    BasPurchaseControls;
  readonly balanceDate: BasWorkpaper["periodEnd"];
}) {
  const source = review.legacySource?.source;
  const sourceId =
    source?.case === "document"
      ? source.value.documentId
      : source?.case === "openingPayable"
        ? source.value.openingPayableId
        : "";
  const sourceKind = source?.case === "document" ? "document" : "legacy";
  const attempt = useMemo(
    () =>
      purchaseAttemptsFor(
        purchaseAttemptScope(
          controls.workspace!.workspaceId,
          controls.workspace!.organisationId!,
          controls.workspace!.userId,
          sourceKind,
          sourceId,
        ),
      ),
    [controls.workspace, sourceKind, sourceId],
  );
  useSyncExternalStore(attempt.subscribe, attempt.getSnapshot);
  const pendingAttempt = attempt.pending;
  const pending = Boolean(pendingAttempt);
  const retainedRequest =
    pendingAttempt?.method === "resolveLegacyPurchaseReview"
      ? purchaseCodecs.legacy.decodeRequest(pendingAttempt.frame)
      : undefined;
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const [confirmation, setConfirmation] = useState<{
    frame: Uint8Array;
    text: string;
    label: string;
  }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const propose: ProposePurchase = (method, frame, text, label) => {
    if (method !== "resolveLegacyPurchaseReview" || attempt.pending) return;
    setConfirmation({ frame, text, label });
    setError(undefined);
  };
  const send = async (frame: Uint8Array, retry = false) => {
    if (busy || (!retry && attempt.pending)) return;
    if (!retry) attempt.begin("resolveLegacyPurchaseReview", frame);
    setBusy(true);
    setConfirmation(undefined);
    try {
      const response = purchaseCodecs.legacy.decodeResponse(
        await controls.purchaseApi!.resolveLegacyPurchaseReview(frame),
      );
      if (!response.review) throw new Error();
      attempt.confirmed();
      if (!active.current) return;
      setNotice(
        `Decision retained: ${response.review.id}. Saved workpaper figures remain unchanged until explicit regeneration.`,
      );
      void controls.onChanged?.();
    } catch (failure) {
      const code =
        failure && typeof failure === "object" && "code" in failure ? String(failure.code) : "";
      if (isPurchaseCorrectionCode(code)) {
        attempt.rejected();
        if (active.current) setError(purchaseCorrectionGuidance[code]);
      } else {
        attempt.uncertain();
        if (active.current)
          setError(
            "The result is unconfirmed. Retry the exact retained request before changing this decision.",
          );
      }
    } finally {
      if (active.current) setBusy(false);
    }
  };
  return (
    <div>
      <p className="text-[9px] text-muted-foreground">
        These are saved snapshot facts. Confirm the current source and supporting records before
        retaining a new decision.
      </p>
      <fieldset disabled={pending || busy || Boolean(confirmation)}>
        <PurchaseLegacyReviewPanel
          source={review.legacySource}
          affectedPeriod={review.affectedPeriod}
          balanceDate={balanceDate}
          workspace={controls.workspace!}
          documentApi={controls.documentApi!}
          purchaseApi={controls.purchaseApi!}
          accountingApi={controls.accountingApi!}
          propose={propose}
          onError={setError}
          retainedRequest={retainedRequest}
        />
      </fieldset>
      {error ? (
        <p className="mt-2 text-warning" role="alert">
          {error}
        </p>
      ) : null}
      {notice ? (
        <p className="mt-2 text-forest" role="status">
          {notice}
        </p>
      ) : null}
      {pending && !busy ? (
        pendingAttempt?.method !== "resolveLegacyPurchaseReview" ? (
          <div className="mt-2 grid justify-items-start gap-2">
            <p className="text-warning" role="alert">
              A different purchase action is pending for this document. Resolve it from the source
              document before continuing this legacy review.
            </p>
            {source?.case === "document" && controls.onReviewDocument ? (
              <Button
                type="button"
                className="h-7 text-[9px]"
                onClick={() => controls.onReviewDocument!(source.value.documentId)}
              >
                Open source document
              </Button>
            ) : null}
          </div>
        ) : pendingAttempt.status === "uncertain" ? (
          <Button
            type="button"
            onClick={() => {
              const retry = attempt.retry();
              void send(authenticatedPurchaseRetry(retry, controls.workspace!), true);
            }}
          >
            Retry exact legacy request
          </Button>
        ) : pendingAttempt.status === "rejected" ? (
          <Button
            type="button"
            onClick={() => {
              attempt.resolveRejected();
            }}
          >
            Review and correct legacy entries
          </Button>
        ) : (
          <p className="text-muted-foreground" role="status">
            This legacy decision is still being processed.
          </p>
        )
      ) : null}
      {confirmation ? (
        <div
          role="alertdialog"
          aria-label="Confirm legacy review"
          className="mt-3 grid gap-2 border border-warning-border bg-warning-soft p-3"
        >
          <p>{confirmation.text}</p>
          <div className="flex gap-2">
            <Button type="button" autoFocus onClick={() => void send(confirmation.frame)}>
              {confirmation.label}
            </Button>
            <Button type="button" onClick={() => setConfirmation(undefined)}>
              Keep reviewing
            </Button>
          </div>
        </div>
      ) : null}
    </div>
  );
}
