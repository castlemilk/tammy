import { create } from "@bufbuild/protobuf";
import { type Account, AccountDesignation } from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import type { Document } from "@tammy/connect-client/tammy/v1/documents_pb.js";
import * as p from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { useState } from "react";
import type { TammyConveyorModule } from "../../../conveyor/client";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { purchaseCodecs } from "./purchase-codecs";
import { aud, decimalAud, exactAud, type PurchaseMutation, uuidV7 } from "./purchase-command-state";
import {
  evidenceFieldsFromInput,
  initialEvidence,
  PurchaseEvidenceFields,
  reviewedEvidence,
} from "./purchase-evidence-fields";
import {
  commandContext,
  dateLabel,
  PurchaseCheck,
  PurchaseChoice,
  PurchaseField,
} from "./purchase-fields";

interface AllocationFields {
  key: string;
  account: string;
  gross: string;
  supplier: string;
  eligible: string;
  use: p.PurchaseUse;
  bas: p.PurchaseBasClassification;
  flow: p.PurchaseCashFlowClassification;
  confirmed: boolean;
  reason: string;
}
export type ProposePurchase = (
  method: PurchaseMutation,
  frame: Uint8Array,
  confirmation: string,
  label: string,
) => void;
export function PurchasePostingForm({
  document,
  workspace,
  accounts,
  documentApi,
  duplicateCandidates,
  propose,
  onError,
  retainedRequest,
}: {
  readonly document: Document;
  readonly workspace: AuthenticatedWorkspace;
  readonly accounts: readonly Account[];
  readonly documentApi: Pick<TammyConveyorModule<"documents">, "listDocuments">;
  readonly duplicateCandidates: readonly p.PurchaseDuplicateMatch[];
  readonly propose: ProposePurchase;
  readonly onError: (message: string) => void;
  readonly retainedRequest?: p.PostReviewedPurchaseRequest | undefined;
}) {
  const blank = (): AllocationFields => ({
    key: uuidV7(),
    account: "",
    gross: "0.00",
    supplier: "0.00",
    eligible: "",
    use: p.PurchaseUse.BUSINESS,
    bas: p.PurchaseBasClassification.NON_CAPITAL,
    flow: p.PurchaseCashFlowClassification.OPERATING,
    confirmed: false,
    reason: "",
  });
  const [allocations, setAllocations] = useState<readonly AllocationFields[]>(() =>
    retainedRequest?.allocations.length
      ? retainedRequest.allocations.map((row) => ({
          key: uuidV7(),
          account: row.accountId,
          gross: decimalAud(row.gross?.minorUnits),
          supplier: decimalAud(row.supplierGst?.minorUnits),
          eligible: decimalAud(row.eligibleGst?.minorUnits),
          use: row.use,
          bas: row.basClassification,
          flow: row.cashFlowClassification,
          confirmed: row.eligibilityReviewConfirmed && row.evidenceReviewConfirmed,
          reason: row.noncreditableReason,
        }))
      : [
          {
            ...blank(),
            gross: decimalAud(document.candidate?.total?.minorUnits),
            supplier: decimalAud(document.candidate?.gst?.minorUnits),
          },
        ],
  );
  const [evidence, setEvidence] = useState(() =>
    retainedRequest
      ? evidenceFieldsFromInput(retainedRequest.evidence, document)
      : initialEvidence(),
  );
  const candidatePostingId = duplicateCandidates[0]?.posting;
  const verifiedCandidateId =
    duplicateCandidates.length === 1 &&
    candidatePostingId?.type === "purchase_posting" &&
    candidatePostingId.id &&
    candidatePostingId.revision >= 1n &&
    candidatePostingId.contentHash.length === 32
      ? candidatePostingId.id
      : undefined;
  const retainedMatchId =
    verifiedCandidateId &&
    retainedRequest?.duplicateReview?.matchedPostingId === verifiedCandidateId
      ? verifiedCandidateId
      : "";
  const [prior, setPrior] = useState(
    Boolean(retainedRequest?.priorAccountingReview?.reviewedExistingAccountingConfirmed),
  );
  const [priorReason, setPriorReason] = useState(
    retainedRequest?.priorAccountingReview?.reason ?? "",
  );
  const [duplicate, setDuplicate] = useState(
    duplicateCandidates.length === 0 &&
      retainedRequest?.duplicateReview?.decision === p.PurchaseDuplicateDecision.NO_MATCH,
  );
  const [distinct, setDistinct] = useState(
    duplicateCandidates.length === 1 &&
      retainedRequest?.duplicateReview?.decision ===
        p.PurchaseDuplicateDecision.DISTINCT_TRANSACTION &&
      Boolean(retainedMatchId),
  );
  const [matchedId, setMatchedId] = useState(retainedMatchId);
  const [duplicateReason, setDuplicateReason] = useState(
    retainedRequest?.duplicateReview?.reason ?? "",
  );
  const unsupportedCandidates =
    duplicateCandidates.length > 1 || (duplicateCandidates.length === 1 && !verifiedCandidateId);
  const set = (index: number, patch: Partial<AllocationFields>) =>
    setAllocations((current) =>
      current.map((row, i) => (i === index ? { ...row, ...patch } : row)),
    );
  const submit = () => {
    try {
      if (duplicateCandidates.length > 1)
        throw new Error(
          "Multiple possible duplicate purchases were found. Choosing among multiple matches is not supported, so this purchase cannot be posted.",
        );
      if (duplicateCandidates.length === 1 && !verifiedCandidateId)
        throw new Error(
          "The possible duplicate does not include a verified owned posting. Reload the purchase before continuing.",
        );
      if (
        duplicateCandidates.length === 1 &&
        (!distinct || matchedId !== verifiedCandidateId || !duplicateReason.trim())
      )
        throw new Error(
          "Select the core-discovered purchase and confirm with a reason that it is a separate transaction.",
        );
      if (!prior || !priorReason.trim())
        throw new Error("Review existing accounting and GST claims before posting.");
      if (duplicateCandidates.length === 0 && !duplicate)
        throw new Error("Confirm that no duplicate was found before posting.");
      const rows = allocations.map((row, i) => {
        const account = accounts.find((item) => item.id === row.account);
        const gross = exactAud(row.gross);
        const supplier = exactAud(row.supplier);
        const eligible = exactAud(row.eligible);
        if (
          !account ||
          gross === undefined ||
          supplier === undefined ||
          eligible === undefined ||
          supplier > gross ||
          eligible > supplier ||
          !row.confirmed
        )
          throw new Error(
            `Complete account, exact AUD amounts and reviewed eligibility for allocation ${i + 1}.`,
          );
        if (
          (row.use === p.PurchaseUse.PRIVATE &&
            (eligible !== 0n || row.bas !== p.PurchaseBasClassification.EXCLUDED)) ||
          (eligible !== supplier && !row.reason.trim())
        )
          throw new Error(
            `Allocation ${i + 1} needs explicit private/noncreditable treatment and its reason.`,
          );
        return {
          accountId: account.id,
          accountVersion: account.version,
          sequence: i + 1,
          gross: { currencyCode: "AUD", minorUnits: gross },
          supplierGst: { currencyCode: "AUD", minorUnits: supplier },
          eligibleGst: { currencyCode: "AUD", minorUnits: eligible },
          use: row.use,
          basClassification: row.bas,
          cashFlowClassification: row.flow,
          eligibilityReviewConfirmed: true,
          evidenceReviewConfirmed: true,
          noncreditableReason: row.reason.trim(),
        };
      });
      if (
        rows.reduce((sum, row) => sum + row.gross.minorUnits, 0n) !==
          document.candidate?.total?.minorUnits ||
        rows.reduce((sum, row) => sum + row.supplierGst.minorUnits, 0n) !==
          document.candidate?.gst?.minorUnits
      )
        throw new Error(
          "Allocation gross and supplier GST must equal the reviewed invoice totals exactly.",
        );
      const request = create(p.PostReviewedPurchaseRequestSchema, {
        commandContext: commandContext(workspace),
        organisationId: workspace.organisationId!,
        documentId: document.id,
        expectedReviewedVersion: document.version,
        allocations: rows,
        evidence: reviewedEvidence(evidence, document),
        duplicateReview: {
          decision: distinct
            ? p.PurchaseDuplicateDecision.DISTINCT_TRANSACTION
            : p.PurchaseDuplicateDecision.NO_MATCH,
          ...(distinct && verifiedCandidateId ? { matchedPostingId: verifiedCandidateId } : {}),
          reason: duplicateReason.trim(),
        },
        priorAccountingReview: {
          decision: p.PurchasePriorAccountingDecision.NOT_PREVIOUSLY_ACCOUNTED_OR_CLAIMED,
          reviewedExistingAccountingConfirmed: true,
          reason: priorReason.trim(),
        },
      });
      propose(
        "postReviewedPurchase",
        purchaseCodecs.post.encodeRequest(request),
        "Records the expense once and creates the supplier payable. GST follows the reviewed basis, eligibility and evidence; unresolved GST remains deferred or in suspense. No bank payment is made.",
        "Confirm purchase posting",
      );
    } catch (error) {
      onError(error instanceof Error ? error.message : "Complete the purchase review.");
    }
  };
  return (
    <div className="grid gap-4">
      <p className="text-[13px] leading-5 text-muted-foreground">
        Review each allocation. Supplier GST and the amount eligible for credit are separate
        decisions.
      </p>
      {allocations.map((row, index) => (
        <div key={row.key} className="grid gap-3 rounded-xl border border-border bg-background p-4">
          <div className="flex items-center justify-between">
            <h3 className="text-[14px] font-semibold">Allocation {index + 1}</h3>
            {allocations.length > 1 ? (
              <Button
                type="button"
                className="h-11 text-[13px]"
                onClick={() => setAllocations((current) => current.filter((_, i) => i !== index))}
              >
                Remove allocation {index + 1}
              </Button>
            ) : null}
          </div>
          <PurchaseChoice
            label={`Account ${index + 1}`}
            value={row.account}
            onChange={(account) => set(index, { account })}
          >
            <option value="">Choose account</option>
            {row.account && !accounts.some((account) => account.id === row.account) ? (
              <option value={row.account}>Previously selected account · {row.account}</option>
            ) : null}
            {accounts
              .filter((account) => account.designation === AccountDesignation.ORDINARY)
              .map((account) => (
                <option key={account.id} value={account.id}>
                  {account.code} · {account.name}
                </option>
              ))}
          </PurchaseChoice>
          <div className="grid grid-cols-3 gap-3 max-[760px]:grid-cols-1">
            <PurchaseField
              label={`Gross ${index + 1} (AUD)`}
              value={row.gross}
              onChange={(gross) => set(index, { gross })}
            />
            <PurchaseField
              label={`Supplier GST ${index + 1} (AUD)`}
              value={row.supplier}
              onChange={(supplier) => set(index, { supplier })}
            />
            <PurchaseField
              label={`Eligible GST ${index + 1} (AUD)`}
              value={row.eligible}
              onChange={(eligible) => set(index, { eligible })}
            />
          </div>
          <div className="grid grid-cols-3 gap-3 max-[760px]:grid-cols-1">
            <PurchaseChoice
              label={`Use ${index + 1}`}
              value={String(row.use)}
              onChange={(use) => set(index, { use: Number(use) })}
            >
              <option value={p.PurchaseUse.BUSINESS}>Business</option>
              <option value={p.PurchaseUse.PRIVATE}>Private</option>
            </PurchaseChoice>
            <PurchaseChoice
              label={`BAS classification ${index + 1}`}
              value={String(row.bas)}
              onChange={(bas) => set(index, { bas: Number(bas) })}
            >
              <option value={p.PurchaseBasClassification.NON_CAPITAL}>Non-capital</option>
              <option value={p.PurchaseBasClassification.CAPITAL}>Capital</option>
              <option value={p.PurchaseBasClassification.EXCLUDED}>Excluded</option>
            </PurchaseChoice>
            <PurchaseChoice
              label={`Cash flow ${index + 1}`}
              value={String(row.flow)}
              onChange={(flow) => set(index, { flow: Number(flow) })}
            >
              <option value={p.PurchaseCashFlowClassification.OPERATING}>Operating</option>
              <option value={p.PurchaseCashFlowClassification.INVESTING}>Investing</option>
              <option value={p.PurchaseCashFlowClassification.FINANCING}>Financing</option>
            </PurchaseChoice>
          </div>
          <PurchaseField
            label={`Private / noncreditable reason ${index + 1}`}
            value={row.reason}
            onChange={(reason) => set(index, { reason })}
          />
          <PurchaseCheck
            label={`Eligibility and evidence reviewed for allocation ${index + 1}`}
            checked={row.confirmed}
            onChange={(confirmed) => set(index, { confirmed })}
          />
        </div>
      ))}
      <Button
        type="button"
        className="h-11 justify-self-start text-[13px]"
        disabled={allocations.length >= 50}
        onClick={() => setAllocations((current) => [...current, blank()])}
      >
        Add allocation
      </Button>
      <PurchaseEvidenceFields
        api={documentApi}
        workspace={workspace}
        acquisition={document}
        value={evidence}
        onChange={setEvidence}
      />
      <PurchaseCheck
        label="Checked existing accounting and GST claims"
        checked={prior}
        onChange={setPrior}
      />
      <PurchaseField
        label="Prior accounting review reason"
        value={priorReason}
        onChange={setPriorReason}
      />
      {duplicateCandidates.length === 0 ? (
        <PurchaseCheck
          label="No duplicate transaction found"
          checked={duplicate}
          onChange={setDuplicate}
        />
      ) : duplicateCandidates.length === 1 ? (
        <>
          <p
            className="text-[13px] leading-5 text-warning"
            role={verifiedCandidateId ? "note" : "alert"}
          >
            {verifiedCandidateId
              ? "A possible duplicate was found. Select it and confirm only if the retained posting represents a separate transaction. If it is the same transaction, return to the existing purchase review."
              : "The possible duplicate does not include a verified owned posting. Reload the purchase before continuing."}
          </p>
          <PurchaseChoice
            label="Matched purchase candidate"
            value={matchedId}
            onChange={(value) => {
              setMatchedId(value);
              setDistinct(false);
            }}
          >
            <option value="">Choose the core-discovered posting</option>
            {verifiedCandidateId ? (
              <option value={verifiedCandidateId}>
                {duplicateCandidates[0]?.supplierName || "Unknown supplier"} ·{" "}
                {duplicateCandidates[0]?.invoiceNumber || "No invoice number"}
              </option>
            ) : null}
          </PurchaseChoice>
          <dl className="grid grid-cols-[minmax(150px,1fr)_minmax(0,2fr)] gap-x-3 gap-y-2 break-all text-[12px]">
            <dt>Supplier</dt>
            <dd>{duplicateCandidates[0]?.supplierName || "—"}</dd>
            <dt>Invoice</dt>
            <dd>{duplicateCandidates[0]?.invoiceNumber || "—"}</dd>
            <dt>Invoice date / gross</dt>
            <dd>
              {dateLabel(duplicateCandidates[0]?.invoiceDate)} ·{" "}
              {aud(duplicateCandidates[0]?.gross?.minorUnits)}
            </dd>
            <dt>Source document</dt>
            <dd>{duplicateCandidates[0]?.documentId || "—"}</dd>
            <dt>Owned posting source</dt>
            <dd>
              {candidatePostingId?.type} · {candidatePostingId?.id} · revision{" "}
              {candidatePostingId?.revision} ·{" "}
              {[...(candidatePostingId?.contentHash ?? new Uint8Array())]
                .map((byte) => byte.toString(16).padStart(2, "0"))
                .join("") || "—"}
            </dd>
          </dl>
          <PurchaseCheck
            label="Matched purchase reviewed as distinct"
            checked={distinct}
            onChange={(value) => setDistinct(value)}
          />
          <PurchaseField
            label="Distinct transaction reason"
            value={duplicateReason}
            onChange={setDuplicateReason}
          />
        </>
      ) : (
        <div
          className="grid gap-3 rounded-xl border border-warning-border bg-warning-soft p-4"
          role="alert"
        >
          <p className="text-[13px] leading-5">
            Multiple possible duplicate purchases were found. Choosing among multiple matches is not
            supported, so this purchase cannot be posted.
          </p>
          <ul className="grid gap-2 text-[12px] leading-5">
            {duplicateCandidates.map((candidate) => (
              <li key={candidate.posting?.id ?? candidate.documentId} className="break-all">
                {candidate.supplierName || "Unknown supplier"} ·{" "}
                {candidate.invoiceNumber || "No invoice number"} ·{" "}
                {dateLabel(candidate.invoiceDate)} · {aud(candidate.gross?.minorUnits)} ·{" "}
                {candidate.documentId} · {candidate.posting?.type} · {candidate.posting?.id} ·
                revision {candidate.posting?.revision} ·{" "}
                {[...(candidate.posting?.contentHash ?? new Uint8Array())]
                  .map((byte) => byte.toString(16).padStart(2, "0"))
                  .join("") || "—"}
              </li>
            ))}
          </ul>
        </div>
      )}
      <Button
        type="button"
        className="justify-self-start"
        onClick={submit}
        disabled={unsupportedCandidates}
      >
        Add purchase to accounts
      </Button>
    </div>
  );
}
