import { create } from "@bufbuild/protobuf";
import {
  type Document,
  type DocumentAccountingFilter,
  DocumentStatus,
  ListDocumentsRequestSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { useMemo, useState } from "react";
import type { TammyConveyorModule } from "../../../conveyor/client";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { retainedDocumentCodec } from "./purchase-codecs";
import { PurchaseChoice } from "./purchase-fields";

export function RetainedDocumentPicker({
  api,
  workspace,
  label,
  selected,
  onChange,
  exclude = [],
  accounting,
}: {
  readonly api: Pick<TammyConveyorModule<"documents">, "listDocuments">;
  readonly workspace: AuthenticatedWorkspace;
  readonly label: string;
  readonly selected: Document | undefined;
  readonly onChange: (document: Document | undefined) => void;
  readonly exclude?: readonly string[];
  readonly accounting?: DocumentAccountingFilter | undefined;
}) {
  const [cursors, setCursors] = useState<readonly string[]>([""]);
  const cursor = cursors.at(-1) ?? "";
  const input = useMemo(
    () =>
      retainedDocumentCodec.encodeRequest(
        create(ListDocumentsRequestSchema, {
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
          organisationId: workspace.organisationId!,
          status: DocumentStatus.REVIEWED,
          ...(accounting ? { accounting } : {}),
          page: { pageSize: 200, cursor: cursor || undefined },
        }),
      ),
    [accounting, cursor, workspace.organisationId, workspace.sessionId, workspace.userId],
  );
  const query = api.listDocuments.useQuery({ input, retry: false });
  const response = useMemo(() => {
    try {
      return query.data && retainedDocumentCodec.decodeResponse(query.data);
    } catch {
      return undefined;
    }
  }, [query.data]);
  const options = response?.documents.filter((document) => !exclude.includes(document.id)) ?? [];
  const retained =
    selected && !options.some((document) => document.id === selected.id)
      ? [selected, ...options]
      : options;
  return (
    <div className="grid gap-1">
      <PurchaseChoice
        label={label}
        value={selected?.id ?? ""}
        onChange={(id) => onChange(retained.find((document) => document.id === id))}
      >
        <option value="">Choose a reviewed retained document</option>
        {retained.map((document) => (
          <option key={document.id} value={document.id}>
            {document.sourceDisplayName} · version {document.version}
          </option>
        ))}
      </PurchaseChoice>
      <div className="flex items-center gap-2 text-[9px]">
        <Button
          type="button"
          className="h-6 px-2 text-[9px]"
          disabled={cursors.length < 2 || query.isFetching}
          onClick={() => setCursors((current) => current.slice(0, -1))}
        >
          Previous {label.toLowerCase()} page
        </Button>
        <Button
          type="button"
          className="h-6 px-2 text-[9px]"
          disabled={!response?.page?.nextCursor || query.isFetching}
          onClick={() => setCursors((current) => [...current, response!.page!.nextCursor ?? ""])}
        >
          Next {label.toLowerCase()} page
        </Button>
        {query.isError ? <span role="alert">Reviewed records could not be loaded.</span> : null}
      </div>
    </div>
  );
}
