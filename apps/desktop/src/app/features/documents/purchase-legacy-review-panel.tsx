import { create, toBinary } from "@bufbuild/protobuf";
import {
  type Journal,
  JournalSchema,
  ListJournalsRequestSchema,
  ListJournalsResponseSchema,
} from "@tammy/connect-client/tammy/v1/accounting_pb.js";
import {
  type Document,
  DocumentCandidateSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import * as p from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { useMemo, useState } from "react";
import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { OwnedPostedPurchasePicker } from "./owned-posted-purchase-picker";
import { purchaseCodecs } from "./purchase-codecs";
import { retainedDocumentFromRef, retainedRef } from "./purchase-evidence-fields";
import {
  civil,
  commandContext,
  dateLabel,
  iso,
  mayPrepare,
  PurchaseCheck,
  PurchaseChoice,
  PurchaseField,
} from "./purchase-fields";
import type { ProposePurchase } from "./purchase-posting-form";
import { RetainedDocumentPicker } from "./retained-document-picker";

const journalsCodec = createProtoMethodCodec({
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4_194_304,
  input: ListJournalsRequestSchema,
  output: ListJournalsResponseSchema,
});
async function hash(content: Uint8Array): Promise<Uint8Array> {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", content.slice().buffer));
}

export function PurchaseLegacyReviewPanel({
  document,
  source,
  affectedPeriod,
  balanceDate,
  workspace,
  documentApi,
  purchaseApi,
  accountingApi,
  propose,
  onError,
  retainedRequest,
}: {
  readonly document?: Document | undefined;
  readonly source?: p.LegacyPurchaseSourceRef | undefined;
  readonly affectedPeriod?: p.PurchaseClaimPeriod | undefined;
  readonly balanceDate?:
    | { readonly year: number; readonly month: number; readonly day: number }
    | undefined;
  readonly workspace: AuthenticatedWorkspace;
  readonly documentApi: Pick<TammyConveyorModule<"documents">, "listDocuments">;
  readonly purchaseApi?:
    | Pick<TammyConveyorModule<"purchases">, "getPurchaseAccounting">
    | undefined;
  readonly accountingApi: Pick<TammyConveyorModule<"accounting">, "listJournals">;
  readonly propose: ProposePurchase;
  readonly onError: (message: string) => void;
  readonly retainedRequest?: p.ResolveLegacyPurchaseReviewRequest | undefined;
}) {
  const retained = retainedRequest?.review;
  const period = retained?.affectedPeriod ?? affectedPeriod;
  const [open, setOpen] = useState(Boolean(retainedRequest));
  const [busy, setBusy] = useState(false);
  const [start, setStart] = useState(iso(period?.periodStart));
  const [end, setEnd] = useState(iso(period?.periodEnd));
  const [balance, setBalance] = useState(iso(retained?.reportingBalanceDate ?? balanceDate));
  const [disposition, setDisposition] = useState(
    retained?.disposition ?? p.LegacyPurchaseDisposition.NONCREDITABLE,
  );
  const [support, setSupport] = useState<Document | undefined>(() =>
    retainedDocumentFromRef(retained?.supportingEvidence[0], workspace.organisationId!),
  );
  const [claimEvidence, setClaimEvidence] = useState<Document | undefined>(() =>
    retainedDocumentFromRef(retained?.claimEvidence, workspace.organisationId!),
  );
  const [claimedStart, setClaimedStart] = useState(
    iso(retained?.previouslyClaimedPeriod?.periodStart),
  );
  const [claimedEnd, setClaimedEnd] = useState(iso(retained?.previouslyClaimedPeriod?.periodEnd));
  const [linkedDocument, setLinkedDocument] = useState<Document>();
  const [linkedPosting, setLinkedPosting] = useState<p.PurchasePosting>();
  const [reason, setReason] = useState(retained?.reason ?? "");
  const [confirmed, setConfirmed] = useState(Boolean(retainedRequest));
  const [payment, setPayment] = useState<Journal>();
  const [retainedPaymentRef, setRetainedPaymentRef] = useState(retained?.supportingPayments[0]);
  const [cursors, setCursors] = useState<readonly string[]>([""]);
  const needsPayment =
    disposition === p.LegacyPurchaseDisposition.ALREADY_CLAIMED ||
    disposition === p.LegacyPurchaseDisposition.UNSUPPORTED_EXISTING_SETTLEMENT;
  const input = useMemo(
    () =>
      journalsCodec.encodeRequest(
        create(ListJournalsRequestSchema, {
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
          organisationId: workspace.organisationId!,
          page: { pageSize: 200, cursor: cursors.at(-1) || undefined },
        }),
      ),
    [workspace.userId, workspace.sessionId, workspace.organisationId, cursors],
  );
  const query = accountingApi.listJournals.useQuery({
    input,
    enabled: open && needsPayment,
    retry: false,
  });
  const journals = useMemo(() => {
    try {
      return query.data && journalsCodec.decodeResponse(query.data);
    } catch {
      return undefined;
    }
  }, [query.data]);
  const options =
    payment && !journals?.journals.some((journal) => journal.id === payment.id)
      ? [payment, ...(journals?.journals ?? [])]
      : (journals?.journals ?? []);
  const submit = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const startDate = civil(start);
      const endDate = civil(end);
      const reportBalance = civil(balance);
      if (
        !confirmed ||
        !reason.trim() ||
        !startDate ||
        !endDate ||
        start > end ||
        !reportBalance ||
        balance < end
      )
        throw new Error(
          "Review the legacy source and enter the affected period, balance date and reason. The balance date must cover the whole period.",
        );
      let retainedSource = retained?.source ?? source;
      if (!retainedSource && document?.candidate)
        retainedSource = create(p.LegacyPurchaseSourceRefSchema, {
          source: {
            case: "document",
            value: {
              documentId: document.id,
              reviewedVersion: document.version,
              originalContentHash: document.sha256,
              reviewedSnapshotHash: await hash(
                toBinary(DocumentCandidateSchema, document.candidate),
              ),
            },
          },
        });
      if (!retainedSource)
        throw new Error("A retained document or opening payable source is required.");
      const requiresLink =
        disposition === p.LegacyPurchaseDisposition.EVIDENCE_ONLY_LINKED_PURCHASE;
      const linkedPostingId = linkedPosting?.id ?? "";
      if (
        (requiresLink || disposition === p.LegacyPurchaseDisposition.DUPLICATE) &&
        !linkedPostingId
      )
        throw new Error("Select a reviewed document and verify its actual owned posted purchase.");
      if (
        !support &&
        (disposition === p.LegacyPurchaseDisposition.CONFIRMED_UNPAID_THROUGH_BALANCE ||
          disposition === p.LegacyPurchaseDisposition.NONCREDITABLE ||
          requiresLink ||
          disposition === p.LegacyPurchaseDisposition.UNSUPPORTED_EXISTING_SETTLEMENT ||
          (disposition === p.LegacyPurchaseDisposition.DUPLICATE && !linkedPostingId))
      )
        throw new Error(
          "Retain and select supporting reviewed evidence. The absence of payment rows does not prove that a legacy purchase was unpaid.",
        );
      const isClaimed = disposition === p.LegacyPurchaseDisposition.ALREADY_CLAIMED;
      const claimedPeriodStart = civil(claimedStart);
      const claimedPeriodEnd = civil(claimedEnd);
      if (
        isClaimed &&
        (!claimEvidence || !claimedPeriodStart || !claimedPeriodEnd || claimedStart > claimedEnd)
      )
        throw new Error(
          "An already-claimed decision needs the previous claim period and a retained claim evidence document.",
        );
      if (
        disposition === p.LegacyPurchaseDisposition.UNSUPPORTED_EXISTING_SETTLEMENT &&
        !payment &&
        !retainedPaymentRef
      )
        throw new Error(
          "Select retained settlement proof. Opening and manual settlements remain unsupported for purchase credit allocation.",
        );
      const paymentRef = payment
        ? {
            sourceId: payment.id,
            sourceVersion: payment.version,
            sourceHash: await hash(toBinary(JournalSchema, payment)),
          }
        : retainedPaymentRef;
      propose(
        "resolveLegacyPurchaseReview",
        purchaseCodecs.legacy.encodeRequest(
          create(p.ResolveLegacyPurchaseReviewRequestSchema, {
            commandContext: commandContext(workspace),
            organisationId: workspace.organisationId!,
            review: {
              source: retainedSource,
              affectedPeriod: { periodStart: startDate, periodEnd: endDate },
              reportingBalanceDate: reportBalance,
              disposition,
              supportingEvidence: support ? [retainedRef(support)] : [],
              supportingPayments: paymentRef ? [paymentRef] : [],
              ...(isClaimed
                ? {
                    previouslyClaimedPeriod: {
                      periodStart: claimedPeriodStart,
                      periodEnd: claimedPeriodEnd,
                    },
                    claimEvidence: retainedRef(claimEvidence!),
                  }
                : {}),
              ...((requiresLink || disposition === p.LegacyPurchaseDisposition.DUPLICATE) &&
              linkedPostingId
                ? { linkedPostingId }
                : {}),
              reason: reason.trim(),
            },
          }),
        ),
        "Retains this period-specific legacy decision and its supporting provenance. It does not post the purchase, record a payment or create a GST credit.",
        "Confirm legacy purchase review",
      );
    } catch (error) {
      onError(error instanceof Error ? error.message : "Complete the legacy review.");
    } finally {
      setBusy(false);
    }
  };
  return (
    <details
      className="work-surface overflow-hidden"
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
    >
      <summary className="cursor-pointer border-b border-border px-5 py-4 text-[14px] font-semibold">
        Review legacy purchase / opening provenance
      </summary>
      <div className="grid gap-4 p-5">
        {source?.source.case === "openingPayable" ? (
          <p className="text-[13px] leading-5 text-warning">
            Opening payable {source.source.value.openingPayableId} has retained provenance without a
            document. Its settlement and GST credit allocation are unsupported; review the existing
            records.
          </p>
        ) : (
          <p className="text-[13px] leading-5 text-muted-foreground">
            Older and undated sources need an explicit review. No recorded payment is not proof of
            an unpaid legacy balance.
          </p>
        )}
        {!mayPrepare(workspace) ? (
          <p className="text-[13px]">
            Read-only role. An administrator or preparer must retain this decision.
          </p>
        ) : (
          <fieldset disabled={busy} className="grid min-w-0 gap-4">
            <div className="grid grid-cols-2 gap-3 max-[560px]:grid-cols-1">
              <PurchaseField
                label="Legacy affected period start"
                type="date"
                value={start}
                onChange={setStart}
              />
              <PurchaseField
                label="Legacy affected period end"
                type="date"
                value={end}
                onChange={setEnd}
              />
            </div>
            <PurchaseField
              label="Legacy reporting balance date"
              type="date"
              value={balance}
              onChange={setBalance}
            />
            <PurchaseChoice
              label="Legacy disposition"
              value={String(disposition)}
              onChange={(value) => setDisposition(Number(value))}
            >
              <option value={p.LegacyPurchaseDisposition.NONCREDITABLE}>
                Reviewed noncreditable
              </option>
              <option value={p.LegacyPurchaseDisposition.CONFIRMED_UNPAID_THROUGH_BALANCE}>
                Confirmed unpaid through balance date
              </option>
              <option value={p.LegacyPurchaseDisposition.ALREADY_CLAIMED}>
                Already claimed in an earlier BAS
              </option>
              <option value={p.LegacyPurchaseDisposition.DUPLICATE}>
                Duplicate of an existing transaction
              </option>
              {source?.source.case !== "openingPayable" ? (
                <option value={p.LegacyPurchaseDisposition.EVIDENCE_ONLY_LINKED_PURCHASE}>
                  Evidence only, linked to an existing purchase
                </option>
              ) : null}
              <option value={p.LegacyPurchaseDisposition.UNSUPPORTED_EXISTING_SETTLEMENT}>
                Existing settlement remains unsupported
              </option>
            </PurchaseChoice>
            <RetainedDocumentPicker
              api={documentApi}
              workspace={workspace}
              label="Legacy supporting evidence"
              selected={support}
              onChange={setSupport}
            />
            {disposition === p.LegacyPurchaseDisposition.EVIDENCE_ONLY_LINKED_PURCHASE ||
            disposition === p.LegacyPurchaseDisposition.DUPLICATE ? (
              purchaseApi ? (
                <OwnedPostedPurchasePicker
                  documentApi={documentApi}
                  purchaseApi={purchaseApi}
                  workspace={workspace}
                  selected={linkedDocument}
                  excludedDocumentId={
                    document?.id ??
                    (source?.source.case === "document"
                      ? source.source.value.documentId
                      : undefined)
                  }
                  onChange={(selected) => {
                    setLinkedDocument(selected);
                    setLinkedPosting(undefined);
                  }}
                  onPostingChange={setLinkedPosting}
                />
              ) : (
                <p className="text-[9px] text-warning" role="alert">
                  Owned posted purchase lookup is unavailable, so this source cannot be linked.
                </p>
              )
            ) : null}
            {retained?.linkedPostingId && !linkedDocument ? (
              <p className="text-[9px] text-muted-foreground">
                Retained linked posting {retained.linkedPostingId}. Select and verify its owned
                posted document before submitting a corrected decision.
              </p>
            ) : null}
            {disposition === p.LegacyPurchaseDisposition.ALREADY_CLAIMED ? (
              <>
                <div className="grid grid-cols-2 gap-2">
                  <PurchaseField
                    label="Previously claimed period start"
                    type="date"
                    value={claimedStart}
                    onChange={setClaimedStart}
                  />
                  <PurchaseField
                    label="Previously claimed period end"
                    type="date"
                    value={claimedEnd}
                    onChange={setClaimedEnd}
                  />
                </div>
                <RetainedDocumentPicker
                  api={documentApi}
                  workspace={workspace}
                  label="Previous claim evidence"
                  selected={claimEvidence}
                  onChange={setClaimEvidence}
                />
              </>
            ) : null}
            {needsPayment ? (
              <div className="grid gap-2">
                <PurchaseChoice
                  label="Retained legacy settlement journal"
                  value={payment?.id ?? retainedPaymentRef?.sourceId ?? ""}
                  onChange={(id) => {
                    const selected = options.find((journal) => journal.id === id);
                    setPayment(selected);
                    setRetainedPaymentRef(
                      id && !selected && retainedPaymentRef?.sourceId === id
                        ? retainedPaymentRef
                        : undefined,
                    );
                  }}
                >
                  <option value="">Select known cash settlement proof, where applicable</option>
                  {retainedPaymentRef &&
                  !options.some((journal) => journal.id === retainedPaymentRef.sourceId) ? (
                    <option value={retainedPaymentRef.sourceId}>
                      Previously selected settlement reference · {retainedPaymentRef.sourceId}
                    </option>
                  ) : null}
                  {options.map((journal) => (
                    <option value={journal.id} key={journal.id}>
                      {dateLabel(journal.postingDate)} · {journal.id}
                    </option>
                  ))}
                </PurchaseChoice>
                <p className="text-[9px] text-muted-foreground">
                  The core verifies that selected journals are owned, linked to this legacy source
                  and prove an actual cash movement.
                </p>
                <div className="flex gap-2">
                  <Button
                    className="h-6 text-[9px]"
                    type="button"
                    disabled={cursors.length < 2 || query.isFetching}
                    onClick={() => setCursors((current) => current.slice(0, -1))}
                  >
                    Previous settlement proof page
                  </Button>
                  <Button
                    className="h-6 text-[9px]"
                    type="button"
                    disabled={!journals?.page?.nextCursor || query.isFetching}
                    onClick={() =>
                      setCursors((current) => [...current, journals!.page!.nextCursor ?? ""])
                    }
                  >
                    Next settlement proof page
                  </Button>
                </div>
                {query.isError ? (
                  <p role="alert" className="text-[10px]">
                    Settlement proof could not be loaded.
                  </p>
                ) : null}
              </div>
            ) : null}
            <PurchaseField label="Legacy review reason" value={reason} onChange={setReason} />
            <PurchaseCheck
              label="Reviewed legacy accounting, settlement and claimed status against retained records"
              checked={confirmed}
              onChange={setConfirmed}
            />
            <Button type="button" className="justify-self-start" onClick={() => void submit()}>
              Review legacy decision
            </Button>
          </fieldset>
        )}
      </div>
    </details>
  );
}
