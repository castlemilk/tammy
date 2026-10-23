import { create } from "@bufbuild/protobuf";
import {
  type Account,
  AccountStatus,
  AccountType,
  ListAccountsRequestSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import { type Document, DocumentStatus } from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { GstBasis } from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import * as p from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import {
  isPurchaseCorrectionCode,
  type PurchaseCorrectionCode,
} from "../../../shared/purchase-errors";
import { Button } from "../../components/ui/button";
import { SurfaceHeading } from "../../components/workspace-primitives";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { authenticatedPurchaseRetry } from "./purchase-attempt-recovery";
import { PurchaseClaimForm } from "./purchase-claim-form";
import { accountCodec, purchaseCodecs } from "./purchase-codecs";
import {
  aud,
  decimalAud,
  exactAud,
  type PurchaseMutation,
  purchaseAttemptScope,
  purchaseAttemptsFor,
} from "./purchase-command-state";
import {
  evidenceFieldsFromInput,
  PurchaseEvidenceFields,
  reviewedEvidence,
} from "./purchase-evidence-fields";
import {
  civil,
  commandContext,
  dateLabel,
  iso,
  mayPrepare,
  PurchaseChoice,
  PurchaseField,
} from "./purchase-fields";
import { PurchaseLegacyReviewPanel } from "./purchase-legacy-review-panel";
import { type ProposePurchase, PurchasePostingForm } from "./purchase-posting-form";

export { accountCodec, purchaseCodecs } from "./purchase-codecs";
export type PurchaseAPI = TammyConveyorModule<"purchases">;
export type PurchaseAccountingAPI = Pick<
  TammyConveyorModule<"accounting">,
  "listAccounts" | "listJournals"
>;
export const purchaseCorrectionGuidance: Readonly<Record<PurchaseCorrectionCode, string>> = {
  PURCHASE_STALE:
    "The source or accounting version changed. Reload the authoritative purchase, then review your retained entries before submitting a corrected request.",
  PURCHASE_PAYMENT_INVALID:
    "Check the actual payment date and positive AUD amount. Advance payments and amounts above the current outstanding balance are unsupported.",
  PURCHASE_DUPLICATE:
    "This transaction may already be recorded. Review the existing purchase before confirming a distinct transaction.",
  PURCHASE_PERIOD_CLOSED:
    "The accounting period is closed. Review the date and period before submitting again.",
  PURCHASE_GST_BASIS_CHANGED:
    "GST basis history changed. Review the current basis and dates before continuing.",
  PURCHASE_EVIDENCE_UNSUPPORTED:
    "The retained evidence or exception is unsupported. Review its ownership, held date and supporting facts.",
  PURCHASE_CLAIM_TIMING_INVALID:
    "The claim period, actual due-date proof or ordinary time limit is unsupported. Review each timing fact.",
  PURCHASE_CLAIM_BALANCE_UNAVAILABLE:
    "The selected portion has no available unclaimed GST, or an outstanding deferral needs its retained closing link. Reload the purchase and review its source.",
  PURCHASE_ELIGIBILITY_REQUIRED:
    "Review allocation amounts, business/private use and credit eligibility explicitly.",
  PURCHASE_ACCOUNT_INVALID:
    "Choose a current owned account suitable for this allocation or payment.",
  PURCHASE_PRIOR_ACCOUNTING:
    "Existing accounting or opening provenance needs review before this purchase can be posted.",
  PURCHASE_VOLUME_UNSUPPORTED:
    "The supported complete-source limit has been exceeded. This action has no partial result.",
  PURCHASE_AUDIT_COVERAGE_UNAVAILABLE:
    "The retained audit history cannot yet cover this purchase action. Review the audit support before continuing.",
};

export function PurchaseAccountingPanel({
  api,
  accountingApi,
  documentApi,
  document,
  workspace,
  onChanged,
}: {
  readonly api: PurchaseAPI;
  readonly accountingApi: PurchaseAccountingAPI;
  readonly documentApi: Pick<TammyConveyorModule<"documents">, "listDocuments">;
  readonly document: Document;
  readonly workspace: AuthenticatedWorkspace;
  readonly onChanged?: () => Promise<void>;
}) {
  const active = useRef(true);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
  const attempt = useMemo(
    () =>
      purchaseAttemptsFor(
        purchaseAttemptScope(
          workspace.workspaceId,
          workspace.organisationId!,
          workspace.userId,
          "document",
          document.id,
        ),
      ),
    [workspace.workspaceId, workspace.organisationId, workspace.userId, document.id],
  );
  useSyncExternalStore(attempt.subscribe, attempt.getSnapshot);
  const pending = Boolean(attempt.pending);
  const pendingAttempt = attempt.pending;
  const retainedPost =
    pendingAttempt?.method === "postReviewedPurchase"
      ? purchaseCodecs.post.decodeRequest(pendingAttempt.frame)
      : undefined;
  const retainedEvidence =
    pendingAttempt?.method === "recordPurchaseTaxEvidence"
      ? purchaseCodecs.evidence.decodeRequest(pendingAttempt.frame)
      : undefined;
  const retainedClaim =
    pendingAttempt?.method === "attributePurchaseGst"
      ? purchaseCodecs.attribute.decodeRequest(pendingAttempt.frame)
      : pendingAttempt?.method === "deferPurchaseGstClaim"
        ? purchaseCodecs.defer.decodeRequest(pendingAttempt.frame)
        : undefined;
  const retainedLegacy =
    pendingAttempt?.method === "resolveLegacyPurchaseReview"
      ? purchaseCodecs.legacy.decodeRequest(pendingAttempt.frame)
      : undefined;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [notice, setNotice] = useState<string>();
  const [confirmation, setConfirmation] = useState<{
    method: PurchaseMutation;
    frame: Uint8Array;
    text: string;
    label: string;
  }>();
  const [authoritative, setAuthoritative] = useState<p.PurchaseAccounting>();
  const [recordedEvidence, setRecordedEvidence] = useState<p.PurchaseTaxEvidence>();
  const [recordedDeferral, setRecordedDeferral] = useState<p.PurchaseGstDeferral>();
  const [paymentCursors, setPaymentCursors] = useState<readonly string[]>([""]);
  const [accountCursors, setAccountCursors] = useState<readonly string[]>([""]);
  const [seenAccounts, setSeenAccounts] = useState<readonly Account[]>([]);
  const authentication = useMemo(
    () => ({ actorUserId: workspace.userId, sessionId: workspace.sessionId }),
    [workspace.userId, workspace.sessionId],
  );
  const getInput = useMemo(
    () =>
      purchaseCodecs.get.encodeRequest(
        create(p.GetPurchaseAccountingRequestSchema, {
          authentication,
          organisationId: workspace.organisationId!,
          documentId: document.id,
        }),
      ),
    [authentication, workspace.organisationId, document.id],
  );
  const get = api.getPurchaseAccounting.useQuery({
    input: getInput,
    enabled: document.status === DocumentStatus.REVIEWED,
    retry: false,
  });
  const getResponse = useMemo(() => {
    try {
      return get.data && purchaseCodecs.get.decodeResponse(get.data);
    } catch {
      return undefined;
    }
  }, [get.data]);
  const accounting = authoritative ?? getResponse?.accounting;
  const evidence = recordedEvidence ?? getResponse?.latestEvidence;
  const deferral = recordedDeferral ?? getResponse?.latestDeferral;
  const listInput = useMemo(
    () =>
      purchaseCodecs.list.encodeRequest(
        create(p.ListPurchasePaymentsRequestSchema, {
          authentication,
          organisationId: workspace.organisationId!,
          documentId: document.id,
          page: { pageSize: 200, cursor: paymentCursors.at(-1) || undefined },
        }),
      ),
    [authentication, workspace.organisationId, document.id, paymentCursors],
  );
  const list = api.listPurchasePayments.useQuery({
    input: listInput,
    enabled: accounting?.posted === true,
    retry: false,
  });
  const payments = useMemo(() => {
    try {
      return list.data && purchaseCodecs.list.decodeResponse(list.data);
    } catch {
      return undefined;
    }
  }, [list.data]);
  const accountsInput = useMemo(
    () =>
      accountCodec.encodeRequest(
        create(ListAccountsRequestSchema, {
          authentication,
          organisationId: workspace.organisationId!,
          status: AccountStatus.ACTIVE,
          page: { pageSize: 200, cursor: accountCursors.at(-1) || undefined },
        }),
      ),
    [authentication, workspace.organisationId, accountCursors],
  );
  const accountsQuery = accountingApi.listAccounts.useQuery({
    input: accountsInput,
    enabled: Boolean(workspace.userId && workspace.sessionId && workspace.organisationId),
    retry: false,
  });
  const accountsResponse = useMemo(() => {
    try {
      return accountsQuery.data && accountCodec.decodeResponse(accountsQuery.data);
    } catch {
      return undefined;
    }
  }, [accountsQuery.data]);
  useEffect(() => {
    if (accountsResponse)
      setSeenAccounts((previous) => [
        ...new Map(
          [...previous, ...accountsResponse.accounts].map((account) => [account.id, account]),
        ).values(),
      ]);
  }, [accountsResponse]);
  const propose: ProposePurchase = (method, frame, text, label) => {
    if (attempt.pending || busy) return;
    setError(undefined);
    setNotice(undefined);
    setConfirmation({ method, frame: frame.slice(), text, label });
  };
  const send = async (method: PurchaseMutation, frame: Uint8Array, retry = false) => {
    if (busy || (!retry && attempt.pending)) return;
    if (!retry) attempt.begin(method, frame);
    setBusy(true);
    setError(undefined);
    setConfirmation(undefined);
    try {
      const result = await api[method](frame);
      switch (method) {
        case "postReviewedPurchase": {
          const response = purchaseCodecs.post.decodeResponse(result);
          if (!response.accounting || !response.posting) throw new Error();
          if (active.current) setAuthoritative(response.accounting);
          break;
        }
        case "recordPurchasePayment": {
          const response = purchaseCodecs.payment.decodeResponse(result);
          if (!response.accounting || !response.payment) throw new Error();
          if (active.current) setAuthoritative(response.accounting);
          break;
        }
        case "recordPurchaseTaxEvidence": {
          const response = purchaseCodecs.evidence.decodeResponse(result);
          if (!response.accounting || !response.evidence) throw new Error();
          if (active.current) setAuthoritative(response.accounting);
          if (active.current) setRecordedEvidence(response.evidence);
          break;
        }
        case "attributePurchaseGst": {
          const response = purchaseCodecs.attribute.decodeResponse(result);
          if (!response.accounting || !response.attributions.length) throw new Error();
          if (active.current) setAuthoritative(response.accounting);
          break;
        }
        case "deferPurchaseGstClaim": {
          const response = purchaseCodecs.defer.decodeResponse(result);
          if (!response.accounting || !response.deferral) throw new Error();
          if (active.current) setAuthoritative(response.accounting);
          if (active.current) setRecordedDeferral(response.deferral);
          break;
        }
        case "resolveLegacyPurchaseReview": {
          const response = purchaseCodecs.legacy.decodeResponse(result);
          if (!response.review) throw new Error();
          if (active.current) setNotice(`Legacy decision retained: ${response.review.id}`);
          break;
        }
      }
      attempt.confirmed();
      if (!active.current) return;
      setNotice((current) => current ?? "Purchase action confirmed and retained locally.");
      // The committed result is confirmed even if a subsequent projection refresh fails.
      void Promise.all([
        api.getPurchaseAccounting.invalidate(getInput),
        api.listPurchasePayments.invalidate(),
        onChanged?.(),
      ]).catch(() => {
        if (active.current)
          setNotice("Action confirmed. Reload the purchase to refresh its projection.");
      });
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
            "The result has not been confirmed. Retry the exact retained request before editing or starting another action.",
          );
      }
    } finally {
      if (active.current) setBusy(false);
    }
  };
  const reload = async () => {
    setAuthoritative(undefined);
    setRecordedEvidence(undefined);
    setRecordedDeferral(undefined);
    await api.getPurchaseAccounting.invalidate(getInput);
  };
  return (
    <section
      className="work-surface grid gap-4 overflow-hidden pb-5"
      aria-label="Purchase accounting"
    >
      <SurfaceHeading
        title="Purchase accounting"
        description="Authoritative posting, settlement and GST evidence for this reviewed purchase."
        action={
          accounting ? (
            <Button
              type="button"
              className="h-11 text-[13px]"
              disabled={busy}
              onClick={() => void reload()}
            >
              Reload purchase
            </Button>
          ) : undefined
        }
      />
      {document.status !== DocumentStatus.REVIEWED ? (
        <p className="px-5 text-[13px] leading-5">
          Save the extraction review before adding this purchase to accounts.
        </p>
      ) : !accounting ? (
        <p className="px-5 text-[13px] leading-5" role={get.isError ? "alert" : "status"}>
          {get.isError
            ? "Purchase accounting could not be loaded. Reload after checking the active workspace."
            : "Loading authoritative purchase…"}
        </p>
      ) : (
        <>
          {accounting.posted ? (
            <PurchaseProjection value={accounting} evidence={evidence} accounts={seenAccounts} />
          ) : (
            <p className="px-5 text-[13px]">Reviewed source · not added to accounts</p>
          )}
          {!mayPrepare(workspace) ? (
            <p className="px-5 text-[13px] leading-5 text-muted-foreground">
              Read-only role. Purchase and GST changes require an administrator or preparer.
            </p>
          ) : (
            <fieldset
              disabled={busy || pending || Boolean(confirmation)}
              className="mx-5 grid min-w-0 gap-5 disabled:opacity-70"
            >
              {accounting.posted ? (
                <>
                  {(accounting.outstanding?.minorUnits ?? 0n) > 0n ? (
                    <PaymentForm
                      document={document}
                      workspace={workspace}
                      accounting={accounting}
                      accounts={seenAccounts}
                      propose={propose}
                      onError={setError}
                      retainedRequest={
                        attempt.pending?.method === "recordPurchasePayment"
                          ? purchaseCodecs.payment.decodeRequest(attempt.pending.frame)
                          : undefined
                      }
                    />
                  ) : (
                    <p className="text-[10px]">This payable is fully settled.</p>
                  )}
                  <EvidenceForm
                    document={document}
                    workspace={workspace}
                    documentApi={documentApi}
                    accounting={accounting}
                    propose={propose}
                    onError={setError}
                    retainedRequest={retainedEvidence}
                  />
                  {(accounting.suspenseGst?.minorUnits ?? 0n) > 0n || retainedClaim ? (
                    <PurchaseClaimForm
                      document={document}
                      workspace={workspace}
                      documentApi={documentApi}
                      accounting={accounting}
                      evidence={evidence}
                      payments={payments?.payments ?? []}
                      deferral={deferral}
                      propose={propose}
                      onError={setError}
                      retainedRequest={retainedClaim}
                    />
                  ) : null}
                </>
              ) : (
                <>
                  <PurchasePostingForm
                    document={document}
                    workspace={workspace}
                    accounts={seenAccounts}
                    documentApi={documentApi}
                    duplicateCandidates={getResponse?.duplicateCandidates ?? []}
                    propose={propose}
                    onError={setError}
                    retainedRequest={retainedPost}
                  />
                  <PurchaseLegacyReviewPanel
                    document={document}
                    workspace={workspace}
                    documentApi={documentApi}
                    purchaseApi={api}
                    accountingApi={accountingApi}
                    propose={propose}
                    onError={setError}
                    retainedRequest={retainedLegacy}
                  />
                </>
              )}
              {accountsQuery.isError ? (
                <p className="text-[10px]" role="alert">
                  Accounts could not be loaded.
                </p>
              ) : null}
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  className="h-6 text-[9px]"
                  disabled={accountCursors.length < 2 || accountsQuery.isFetching}
                  onClick={() => setAccountCursors((current) => current.slice(0, -1))}
                >
                  Previous account page
                </Button>
                <Button
                  type="button"
                  className="h-6 text-[9px]"
                  disabled={!accountsResponse?.page?.nextCursor || accountsQuery.isFetching}
                  onClick={() =>
                    setAccountCursors((current) => [
                      ...current,
                      accountsResponse!.page!.nextCursor ?? "",
                    ])
                  }
                >
                  Next account page
                </Button>
              </div>
            </fieldset>
          )}
          {accounting.posted ? (
            <div className="mt-4 border-t border-border pt-3">
              <h3 className="text-[11px] font-semibold">
                Retained payment history · {accounting.paymentCount} total
              </h3>
              <div className="mt-2 overflow-x-auto">
                <table
                  className="w-full text-left text-[9px] [&_td]:align-top"
                  aria-label="Purchase payments"
                >
                  <thead>
                    <tr>
                      <th className="py-2 pr-3">Actual date</th>
                      <th className="pr-3">Amount</th>
                      <th className="pr-3">Reference</th>
                      <th>Retained source</th>
                    </tr>
                  </thead>
                  <tbody>
                    {payments?.payments.map((payment) => (
                      <tr className="border-t border-border" key={payment.id}>
                        <td className="py-2 pr-3 whitespace-nowrap">
                          {dateLabel(payment.actualPaymentDate)}
                        </td>
                        <td className="pr-3 whitespace-nowrap">
                          {aud(payment.amount?.minorUnits)}
                        </td>
                        <td className="pr-3">{payment.reference || "—"}</td>
                        <td className="break-all">
                          <div>
                            {payment.id} · version {payment.accountingVersion}
                          </div>
                          <details className="mt-1">
                            <summary className="cursor-pointer text-muted-foreground">
                              Retained payment details
                            </summary>
                            <dl className="mt-2 grid gap-1">
                              <dt>Bank account</dt>
                              <dd>{payment.bankAccountId || "—"}</dd>
                              <dt>Payment journal</dt>
                              <dd>{payment.journalId || "—"}</dd>
                              <dt>Payment source hash</dt>
                              <dd>{sourceHash(payment.sourceHash)}</dd>
                              <dt>Financial revision</dt>
                              <dd>{payment.financialRevision}</dd>
                              <dt>Recorded by</dt>
                              <dd>{payment.actorUserId || "—"}</dd>
                              <dt>Recorded at</dt>
                              <dd>{timestampLabel(payment.recordedAt)}</dd>
                              <dt>Ordered allocations</dt>
                              <dd>
                                {payment.allocations.length > 0 ? (
                                  <ol className="grid gap-1">
                                    {[...payment.allocations]
                                      .sort((left, right) => left.sequence - right.sequence)
                                      .map((allocation) => (
                                        <li
                                          className="border-l border-border pl-2"
                                          key={allocation.sequence}
                                        >
                                          Allocation {allocation.sequence} · account{" "}
                                          {allocation.accountId} · version{" "}
                                          {allocation.accountVersion} ·{" "}
                                          {purchaseUseLabel(allocation.use)} ·{" "}
                                          {purchaseBasClassificationLabel(
                                            allocation.basClassification,
                                          )}{" "}
                                          ·{" "}
                                          {purchaseCashFlowLabel(allocation.cashFlowClassification)}{" "}
                                          · {aud(allocation.gross?.minorUnits)} gross ·{" "}
                                          {aud(allocation.supplierGst?.minorUnits)} supplier GST ·{" "}
                                          {aud(allocation.eligibleGst?.minorUnits)} eligible GST
                                        </li>
                                      ))}
                                  </ol>
                                ) : (
                                  "No allocation detail returned."
                                )}
                              </dd>
                            </dl>
                          </details>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {list.isError ? (
                <p role="alert" className="text-[10px]">
                  Payment history could not be loaded.
                </p>
              ) : null}
              <div className="mt-2 flex gap-2">
                <Button
                  type="button"
                  className="h-7 text-[9px]"
                  disabled={paymentCursors.length < 2 || list.isFetching}
                  onClick={() => setPaymentCursors((current) => current.slice(0, -1))}
                >
                  Previous payment page
                </Button>
                <Button
                  type="button"
                  className="h-7 text-[9px]"
                  disabled={!payments?.page?.nextCursor || list.isFetching}
                  onClick={() =>
                    setPaymentCursors((current) => [...current, payments!.page!.nextCursor ?? ""])
                  }
                >
                  Next payment page
                </Button>
              </div>
            </div>
          ) : null}
        </>
      )}
      {error ? (
        <p className="mt-3 text-[10px] leading-5 text-warning" role="alert">
          {error}
        </p>
      ) : null}
      {notice ? (
        <p className="mt-3 text-[10px] text-forest" role="status">
          {notice}
        </p>
      ) : null}
      {pending && !error ? (
        <p className="mt-3 text-[10px]" role="status">
          A purchase request is retained from an earlier view. Its business facts and key are
          unchanged; retry requires an explicit action.
        </p>
      ) : null}
      {pending ? (
        <div className="mt-2 flex flex-wrap gap-2">
          {attempt.pending?.status === "uncertain" ? (
            <Button
              disabled={busy}
              type="button"
              onClick={() => {
                const retry = attempt.retry();
                void send(retry.method, authenticatedPurchaseRetry(retry, workspace), true);
              }}
            >
              Retry exact request
            </Button>
          ) : attempt.pending?.status === "rejected" ? (
            <Button
              type="button"
              onClick={() => {
                attempt.resolveRejected();
              }}
            >
              Review and correct retained entries
            </Button>
          ) : null}
        </div>
      ) : null}
      {confirmation ? (
        <div
          role="alertdialog"
          aria-label="Confirm purchase action"
          className="mt-3 grid gap-3 rounded-[5px] border border-warning-border bg-warning-soft p-3"
        >
          <p className="text-[10px] leading-5">{confirmation.text}</p>
          <div className="flex flex-wrap gap-2">
            <Button
              autoFocus
              type="button"
              onClick={() => void send(confirmation.method, confirmation.frame)}
            >
              {confirmation.label}
            </Button>
            <Button type="button" onClick={() => setConfirmation(undefined)}>
              Keep reviewing
            </Button>
          </div>
        </div>
      ) : null}
    </section>
  );
}

function PurchaseProjection({
  value,
  evidence,
  accounts,
}: {
  readonly value: p.PurchaseAccounting;
  readonly evidence?: p.PurchaseTaxEvidence | undefined;
  readonly accounts: readonly Account[];
}) {
  const originalEvidence = value.posting?.evidence;
  const latestEvidenceReview = evidence?.review;
  const summaryEvidence = latestEvidenceReview ?? originalEvidence;
  const rows = [
    ["Gross", value.posting?.gross?.minorUnits],
    ["Supplier GST", value.posting?.supplierGst?.minorUnits],
    ["Eligible GST", value.eligibleGst?.minorUnits],
    ["Paid", value.paid?.minorUnits],
    ["Outstanding", value.outstanding?.minorUnits],
    ["Deferred GST", value.deferredGst?.minorUnits],
    ["Suspense GST", value.suspenseGst?.minorUnits],
    ["Attributed GST", value.attributedGst?.minorUnits],
  ] as const;
  const needsReview =
    value.reviewOutcomes?.evidence !== p.PurchaseReviewStatus.CLEAR ||
    value.reviewOutcomes?.timing !== p.PurchaseReviewStatus.CLEAR;
  return (
    <div className="mt-3 grid gap-3">
      <p className="text-[10px] font-medium">
        Added to accounts ·{" "}
        {value.posting?.gstBasis === GstBasis.CASH
          ? "Cash basis"
          : value.posting?.gstBasis === GstBasis.NON_CASH
            ? "Non-cash basis"
            : "No GST basis"}{" "}
        · accounting version {value.accountingVersion}
      </p>
      {needsReview ? (
        <p className="text-[10px] text-warning">Needs GST evidence/claim-period review</p>
      ) : null}
      <dl className="grid grid-cols-[1fr_auto] gap-x-3 gap-y-1 text-[10px]">
        {rows.map(([label, amount]) => (
          <div className="contents" key={label}>
            <dt>{label}</dt>
            <dd className="text-right font-medium tabular-nums">{aud(amount)}</dd>
          </div>
        ))}
      </dl>
      <div className="overflow-x-auto">
        <table
          className="w-full text-left text-[9px] [&_th]:pr-2 [&_td]:pr-2"
          aria-label="Posted purchase allocations"
        >
          <thead>
            <tr>
              <th>Allocation</th>
              <th>Account</th>
              <th>Use / class</th>
              <th>Cash flow</th>
              <th>Gross</th>
              <th>Supplier GST</th>
              <th>Eligible GST</th>
            </tr>
          </thead>
          <tbody>
            {value.posting?.allocations.map((row) => (
              <tr className="border-t border-border" key={row.sequence}>
                <td className="py-2">{row.sequence}</td>
                <td>
                  {(() => {
                    const account = accounts.find((candidate) => candidate.id === row.accountId);
                    return (
                      <>
                        <div>
                          {account ? `${account.code} · ${account.name}` : "Retained account"}
                        </div>
                        <div className="break-all text-muted-foreground">
                          {row.accountId} · version {row.accountVersion}
                        </div>
                      </>
                    );
                  })()}
                </td>
                <td>
                  {row.use === p.PurchaseUse.PRIVATE ? "Private" : "Business"} /{" "}
                  {row.basClassification === p.PurchaseBasClassification.CAPITAL
                    ? "Capital"
                    : row.basClassification === p.PurchaseBasClassification.NON_CAPITAL
                      ? "Non-capital"
                      : "Excluded"}
                </td>
                <td>
                  {row.cashFlowClassification === p.PurchaseCashFlowClassification.OPERATING
                    ? "Operating"
                    : row.cashFlowClassification === p.PurchaseCashFlowClassification.INVESTING
                      ? "Investing"
                      : row.cashFlowClassification === p.PurchaseCashFlowClassification.FINANCING
                        ? "Financing"
                        : "Unclassified"}
                </td>
                <td>{aud(row.gross?.minorUnits)}</td>
                <td>{aud(row.supplierGst?.minorUnits)}</td>
                <td>{aud(row.eligibleGst?.minorUnits)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="text-[9px] break-all text-muted-foreground">
        Posting: {value.posting?.id} · evidence: {evidence?.id ?? "posting snapshot"} · held{" "}
        {dateLabel(summaryEvidence?.heldDate)}
      </p>
      <details>
        <summary className="cursor-pointer text-[9px] text-muted-foreground">
          Retained posting and evidence details
        </summary>
        <div className="mt-2 grid gap-2 break-all text-[9px]">
          <section>
            <h4 className="font-semibold">Posting provenance</h4>
            <dl className="grid grid-cols-[minmax(120px,1fr)_minmax(0,2fr)] gap-1">
              <dt>Posting source hash</dt>
              <dd>{sourceHash(value.posting?.sourceHash)}</dd>
              <dt>Reviewed source version</dt>
              <dd>{value.posting?.reviewedVersion ?? "—"}</dd>
              <dt>Original document hash</dt>
              <dd>{sourceHash(value.posting?.originalContentHash)}</dd>
              <dt>Reviewed snapshot hash</dt>
              <dd>{sourceHash(value.posting?.reviewedSnapshotHash)}</dd>
              <dt>Posting journal</dt>
              <dd>{value.posting?.journalId || "—"}</dd>
              <dt>Financial revision</dt>
              <dd>{value.posting?.financialRevision ?? "—"}</dd>
              <dt>Posted by</dt>
              <dd>{value.posting?.actorUserId || "—"}</dd>
              <dt>Posted at</dt>
              <dd>{timestampLabel(value.posting?.postedAt)}</dd>
              <dt>GST basis profile</dt>
              <dd>
                {value.posting?.gstBasis === GstBasis.CASH
                  ? "Cash"
                  : value.posting?.gstBasis === GstBasis.NON_CASH
                    ? "Non-cash"
                    : "No GST basis"}{" "}
                · profile {value.posting?.gstProfileVersion ?? "—"} · history watermark{" "}
                {value.posting?.gstHistoryWatermark ?? "—"}
              </dd>
            </dl>
          </section>
          <section>
            <h4 className="font-semibold">Original posting evidence · immutable snapshot</h4>
            <dl className="grid grid-cols-[minmax(120px,1fr)_minmax(0,2fr)] gap-1">
              <EvidenceReviewFacts review={originalEvidence} prefix="Original evidence" />
            </dl>
          </section>
          <section>
            <h4 className="font-semibold">Latest evidence review</h4>
            {evidence ? (
              <>
                <dl className="grid grid-cols-[minmax(120px,1fr)_minmax(0,2fr)] gap-1">
                  <dt>Latest evidence record</dt>
                  <dd>
                    {evidence.id} · accounting version {evidence.accountingVersion}
                  </dd>
                  <dt>Evidence source hash</dt>
                  <dd>{sourceHash(evidence.sourceHash)}</dd>
                  <dt>Evidence recorded by</dt>
                  <dd>{evidence.actorUserId || "—"}</dd>
                  <dt>Evidence recorded at</dt>
                  <dd>{timestampLabel(evidence.recordedAt)}</dd>
                </dl>
                <dl className="mt-1 grid grid-cols-[minmax(120px,1fr)_minmax(0,2fr)] gap-1">
                  <EvidenceReviewFacts review={latestEvidenceReview} prefix="Evidence" />
                </dl>
              </>
            ) : (
              <p>No latest evidence record returned.</p>
            )}
          </section>
        </div>
      </details>
    </div>
  );
}

function EvidenceReviewFacts({
  review,
  prefix,
}: {
  readonly review: p.PurchaseEvidenceInput | undefined;
  readonly prefix: string;
}) {
  const exception = review?.exceptionReview;
  if (!review) {
    return (
      <>
        <dt>{prefix} review</dt>
        <dd>No evidence review returned.</dd>
      </>
    );
  }

  return (
    <>
      <dt>{prefix} review</dt>
      <dd>
        {evidenceClassificationLabel(review.classification)} · held {dateLabel(review.heldDate)}
      </dd>
      <dt>{prefix} document</dt>
      <dd>
        {review.source
          ? `${review.source.sourceId} · version ${review.source.sourceVersion} · ${sourceHash(review.source.sourceHash)}`
          : "—"}
      </dd>
      <dt>{prefix} review confirmations</dt>
      <dd>
        Registration {review.registrationReviewConfirmed ? "confirmed" : "not confirmed"} · business
        use {review.businessUseReviewConfirmed ? "confirmed" : "not confirmed"} · evidence{" "}
        {review.evidenceReviewConfirmed ? "confirmed" : "not confirmed"}
      </dd>
      <dt>{prefix} review reason</dt>
      <dd>{review.reason || "—"}</dd>
      {exception ? (
        <>
          <dt>{prefix} exception kind</dt>
          <dd>{exceptionKindLabel(exception.kind)}</dd>
          <dt>{prefix} retained rule</dt>
          <dd>{retainedSourceLabel(exception.rule)}</dd>
          <dt>{prefix} supporting records</dt>
          <dd>
            {exception.supportingRecords.length ? (
              <ol className="grid gap-1">
                {exception.supportingRecords.map((source, index) => (
                  <li key={`${source.sourceId}:${source.sourceVersion}`}>
                    Record {index + 1} · {retainedSourceLabel(source)}
                  </li>
                ))}
              </ol>
            ) : (
              "No supporting records returned."
            )}
          </dd>
          <dt>{prefix} exception confirmations</dt>
          <dd>
            Tax invoice not required{" "}
            {exception.taxInvoiceNotRequiredReviewConfirmed ? "confirmed" : "not confirmed"} ·
            supporting record requirements{" "}
            {exception.supportingRecordRequirementsReviewConfirmed ? "confirmed" : "not confirmed"}
          </dd>
          <dt>{prefix} exception review reason</dt>
          <dd>{exception.reason || "—"}</dd>
        </>
      ) : null}
    </>
  );
}

function sourceHash(value: Uint8Array | undefined): string {
  return value?.length
    ? [...value].map((byte) => byte.toString(16).padStart(2, "0")).join("")
    : "—";
}

function retainedSourceLabel(value: p.PurchaseRetainedSourceRef | undefined): string {
  return value
    ? `${value.sourceId} · version ${value.sourceVersion} · ${sourceHash(value.sourceHash)}`
    : "—";
}

function timestampLabel(
  value: { readonly seconds: bigint; readonly nanos: number } | undefined,
): string {
  if (!value) return "—";
  const date = new Date(Number(value.seconds) * 1_000);
  if (!Number.isFinite(date.getTime())) return "—";
  const fractional = value.nanos
    ? `.${String(value.nanos).padStart(9, "0").replace(/0+$/u, "")}`
    : "";
  return `${date.toISOString().slice(0, 19)}${fractional}Z`;
}

function evidenceClassificationLabel(value: p.PurchaseEvidenceClassification | undefined): string {
  switch (value) {
    case p.PurchaseEvidenceClassification.TAX_INVOICE:
      return "Tax invoice";
    case p.PurchaseEvidenceClassification.SUPPORTED_EXCEPTION:
      return "Supported exception";
    case p.PurchaseEvidenceClassification.UNKNOWN:
      return "Unknown / missing";
    default:
      return "—";
  }
}

function exceptionKindLabel(value: p.PurchaseTaxInvoiceExceptionKind | undefined): string {
  switch (value) {
    case p.PurchaseTaxInvoiceExceptionKind.LOW_VALUE_ACQUISITION:
      return "Low-value acquisition";
    case p.PurchaseTaxInvoiceExceptionKind.SPECIFIC_RETAINED_WAIVER:
      return "Specific retained waiver";
    default:
      return "Unknown exception";
  }
}

function purchaseUseLabel(value: p.PurchaseUse): string {
  return value === p.PurchaseUse.PRIVATE ? "Private" : "Business";
}

function purchaseBasClassificationLabel(value: p.PurchaseBasClassification): string {
  switch (value) {
    case p.PurchaseBasClassification.CAPITAL:
      return "Capital";
    case p.PurchaseBasClassification.NON_CAPITAL:
      return "Non-capital";
    default:
      return "Excluded";
  }
}

function purchaseCashFlowLabel(value: p.PurchaseCashFlowClassification): string {
  switch (value) {
    case p.PurchaseCashFlowClassification.OPERATING:
      return "Operating";
    case p.PurchaseCashFlowClassification.INVESTING:
      return "Investing";
    case p.PurchaseCashFlowClassification.FINANCING:
      return "Financing";
    default:
      return "Unclassified";
  }
}

function PaymentForm({
  document,
  workspace,
  accounting,
  accounts,
  propose,
  onError,
  retainedRequest,
}: {
  readonly document: Document;
  readonly workspace: AuthenticatedWorkspace;
  readonly accounting: p.PurchaseAccounting;
  readonly accounts: readonly Account[];
  readonly propose: ProposePurchase;
  readonly onError: (message: string) => void;
  readonly retainedRequest?: p.RecordPurchasePaymentRequest | undefined;
}) {
  const [open, setOpen] = useState(Boolean(retainedRequest));
  const [date, setDate] = useState(iso(retainedRequest?.actualPaymentDate));
  const [amount, setAmount] = useState(
    retainedRequest?.amount ? decimalAud(retainedRequest.amount.minorUnits) : "",
  );
  const [bank, setBank] = useState(retainedRequest?.bankAccountId ?? "");
  const [reference, setReference] = useState(retainedRequest?.reference ?? "");
  const submit = () => {
    try {
      const actualDate = civil(date);
      const cents = exactAud(amount);
      if (
        !actualDate ||
        cents === undefined ||
        cents <= 0n ||
        cents > (accounting.outstanding?.minorUnits ?? 0n) ||
        !bank
      )
        throw new Error(
          "Choose an actual payment date and bank account, and a positive AUD amount within the outstanding balance.",
        );
      const invoiceDate = accounting.posting?.invoiceDate;
      if (invoiceDate && dateKey(actualDate) < dateKey(invoiceDate))
        throw new Error("Advance payments before the invoice date are unsupported.");
      propose(
        "recordPurchasePayment",
        purchaseCodecs.payment.encodeRequest(
          create(p.RecordPurchasePaymentRequestSchema, {
            commandContext: commandContext(workspace),
            organisationId: workspace.organisationId!,
            documentId: document.id,
            expectedAccountingVersion: accounting.accountingVersion,
            actualPaymentDate: actualDate,
            amount: { currencyCode: "AUD", minorUnits: cents },
            bankAccountId: bank,
            reference,
          }),
        ),
        "Records an actual payment already made, settles the payable and retains its cash-flow effect. GST follows the actual payment date and evidence on cash basis. This does not send money or reconcile a bank statement.",
        "Confirm recorded payment",
      );
    } catch (error) {
      onError(error instanceof Error ? error.message : "Complete payment details.");
    }
  };
  return (
    <details
      open={open || Boolean(retainedRequest)}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="cursor-pointer text-[11px] font-semibold">Record payment</summary>
      <div className="mt-3 grid gap-3">
        <PurchaseField label="Actual payment date" type="date" value={date} onChange={setDate} />
        <PurchaseField label="Payment amount (AUD)" value={amount} onChange={setAmount} />
        <PurchaseChoice label="Payment bank account" value={bank} onChange={setBank}>
          <option value="">Choose current bank account</option>
          {bank && !accounts.some((account) => account.id === bank) ? (
            <option value={bank}>Previously selected bank account · {bank}</option>
          ) : null}
          {accounts
            .filter(
              (account) =>
                account.type === AccountType.ASSET &&
                (account.reportClassification === "balance_sheet.cash" ||
                  account.reportClassification === "balance_sheet.cash_equivalent"),
            )
            .map((account) => (
              <option key={account.id} value={account.id}>
                {account.code} · {account.name}
              </option>
            ))}
        </PurchaseChoice>
        <PurchaseField label="Payment reference" value={reference} onChange={setReference} />
        <Button type="button" className="justify-self-start" onClick={submit}>
          Review recorded payment
        </Button>
      </div>
    </details>
  );
}
function dateKey(value: {
  readonly year: number;
  readonly month: number;
  readonly day: number;
}): number {
  return value.year * 10_000 + value.month * 100 + value.day;
}
function EvidenceForm({
  document,
  workspace,
  documentApi,
  accounting,
  propose,
  onError,
  retainedRequest,
}: {
  readonly document: Document;
  readonly workspace: AuthenticatedWorkspace;
  readonly documentApi: Pick<TammyConveyorModule<"documents">, "listDocuments">;
  readonly accounting: p.PurchaseAccounting;
  readonly propose: ProposePurchase;
  readonly onError: (message: string) => void;
  readonly retainedRequest?: p.RecordPurchaseTaxEvidenceRequest | undefined;
}) {
  const [open, setOpen] = useState(Boolean(retainedRequest));
  const [fields, setFields] = useState(() =>
    evidenceFieldsFromInput(retainedRequest?.evidence, document),
  );
  return (
    <details
      className="border-t border-border pt-3"
      open={open || Boolean(retainedRequest)}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="cursor-pointer text-[11px] font-semibold">Review GST evidence</summary>
      <div className="mt-3 grid gap-3">
        <PurchaseEvidenceFields
          api={documentApi}
          workspace={workspace}
          acquisition={document}
          value={fields}
          onChange={setFields}
        />
        <Button
          type="button"
          className="justify-self-start"
          onClick={() => {
            try {
              propose(
                "recordPurchaseTaxEvidence",
                purchaseCodecs.evidence.encodeRequest(
                  create(p.RecordPurchaseTaxEvidenceRequestSchema, {
                    commandContext: commandContext(workspace),
                    organisationId: workspace.organisationId!,
                    documentId: document.id,
                    expectedAccountingVersion: accounting.accountingVersion,
                    evidence: reviewedEvidence(fields, document),
                  }),
                ),
                "Retains a new evidence review and its actual held date. Existing purchase and payment amounts are unchanged; GST in suspense still needs explicit claim-period review.",
                "Confirm GST evidence review",
              );
            } catch (error) {
              onError(error instanceof Error ? error.message : "Complete evidence review.");
            }
          }}
        >
          Retain GST evidence review
        </Button>
      </div>
    </details>
  );
}
