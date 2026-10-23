import { create } from "@bufbuild/protobuf";
import {
  type Document,
  DocumentAccountingFilter,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import type { PurchasePosting } from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { GetPurchaseAccountingRequestSchema } from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import { useEffect, useMemo } from "react";
import type { TammyConveyorModule } from "../../../conveyor/client";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { purchaseCodecs } from "./purchase-codecs";
import { aud } from "./purchase-command-state";
import { dateLabel } from "./purchase-fields";
import { RetainedDocumentPicker } from "./retained-document-picker";

export function OwnedPostedPurchasePicker({
  documentApi,
  purchaseApi,
  workspace,
  selected,
  excludedDocumentId,
  onChange,
  onPostingChange,
}: {
  readonly documentApi: Pick<TammyConveyorModule<"documents">, "listDocuments">;
  readonly purchaseApi: Pick<TammyConveyorModule<"purchases">, "getPurchaseAccounting">;
  readonly workspace: AuthenticatedWorkspace;
  readonly selected: Document | undefined;
  readonly excludedDocumentId?: string | undefined;
  readonly onChange: (document: Document | undefined) => void;
  readonly onPostingChange: (posting: PurchasePosting | undefined) => void;
}) {
  const input = useMemo(
    () =>
      purchaseCodecs.get.encodeRequest(
        create(GetPurchaseAccountingRequestSchema, {
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
          organisationId: workspace.organisationId!,
          documentId: selected?.id ?? "",
        }),
      ),
    [selected?.id, workspace.organisationId, workspace.sessionId, workspace.userId],
  );
  const query = purchaseApi.getPurchaseAccounting.useQuery({
    input,
    enabled: Boolean(selected?.id),
    retry: false,
  });
  const response = useMemo(() => {
    try {
      return query.data && purchaseCodecs.get.decodeResponse(query.data);
    } catch {
      return undefined;
    }
  }, [query.data]);
  const accounting = response?.accounting;
  const posting =
    selected &&
    accounting?.posted &&
    accounting.organisationId === workspace.organisationId &&
    accounting.documentId === selected.id &&
    accounting.posting?.documentId === selected.id &&
    accounting.posting.organisationId === workspace.organisationId &&
    accounting.posting.id &&
    accounting.posting.sourceHash.length === 32
      ? accounting.posting
      : undefined;
  useEffect(() => {
    onPostingChange(posting);
  }, [onPostingChange, posting]);

  return (
    <div className="grid gap-2">
      <RetainedDocumentPicker
        api={documentApi}
        workspace={workspace}
        label="Existing posted purchase"
        selected={selected}
        exclude={excludedDocumentId ? [excludedDocumentId] : []}
        accounting={DocumentAccountingFilter.POSTED}
        onChange={(document) => {
          onChange(document);
          if (!document) onPostingChange(undefined);
        }}
      />
      {selected && posting ? (
        <div className="grid gap-1 border border-border bg-muted/20 p-3 text-[12px]" role="status">
          <p className="font-semibold">
            Verified posting · {posting.supplierName || "Unknown supplier"} ·{" "}
            {posting.invoiceNumber || "No invoice number"}
          </p>
          <dl className="grid grid-cols-[minmax(120px,1fr)_minmax(0,2fr)] gap-1 break-all">
            <dt>Invoice date / gross</dt>
            <dd>
              {dateLabel(posting.invoiceDate)} · {aud(posting.gross?.minorUnits)}
            </dd>
            <dt>Owned posting source</dt>
            <dd>
              {posting.id} · {posting.documentId}
            </dd>
            <dt>Posting hash</dt>
            <dd>
              {[...posting.sourceHash].map((byte) => byte.toString(16).padStart(2, "0")).join("")}
            </dd>
          </dl>
        </div>
      ) : selected ? (
        <p className="text-[14px] leading-6 text-warning" role={query.isError ? "alert" : "status"}>
          {query.isError
            ? "The selected posted purchase could not be verified. Reload the owned document before linking it."
            : query.isFetching
              ? "Loading the selected document’s current posted purchase…"
              : "The selected reviewed document has no verified current posted purchase."}
        </p>
      ) : null}
      {query.isError ? (
        <Button
          type="button"
          className="justify-self-start text-[12px]"
          onClick={() => void query.refetch()}
        >
          Reload selected purchase
        </Button>
      ) : null}
    </div>
  );
}
