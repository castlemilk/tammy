import { create } from "@bufbuild/protobuf";
import {
  CivilDateSchema,
  CommandContextSchema,
  MoneySchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  type Document,
  DocumentAccountingFilter,
  DocumentCandidateSchema,
  DocumentStatus,
  GetDocumentRequestSchema,
  GetDocumentResponseSchema,
  IngestDocumentRequestSchema,
  IngestDocumentResponseSchema,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
  SaveDocumentReviewRequestSchema,
  SaveDocumentReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { Check, FileText, LoaderCircle, Upload } from "lucide-react";
import { type ChangeEvent, type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { extractNativePdfText } from "./pdf-text";
import {
  type PurchaseAccountingAPI,
  PurchaseAccountingPanel,
  type PurchaseAPI,
} from "./purchase-accounting-panel";
import { aud } from "./purchase-command-state";

const ingestCodec = createProtoMethodCodec({
  input: IngestDocumentRequestSchema,
  maximumRequestBytes: 11 * 1024 * 1024,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: IngestDocumentResponseSchema,
});
const listCodec = createProtoMethodCodec({
  input: ListDocumentsRequestSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 4 * 1024 * 1024,
  output: ListDocumentsResponseSchema,
});
const reviewCodec = createProtoMethodCodec({
  input: SaveDocumentReviewRequestSchema,
  maximumRequestBytes: 32_768,
  maximumResponseBytes: 2 * 1024 * 1024,
  output: SaveDocumentReviewResponseSchema,
});
const getDocumentCodec = createProtoMethodCodec({
  input: GetDocumentRequestSchema,
  output: GetDocumentResponseSchema,
  maximumRequestBytes: 16_384,
  maximumResponseBytes: 2_097_152,
});

interface DocumentsScreenProps {
  readonly api: Pick<
    TammyConveyorModule<"documents">,
    "ingestDocument" | "listDocuments" | "getDocument" | "saveDocumentReview"
  >;
  readonly onDocumentsChanged?: () => Promise<void>;
  readonly purchaseApi?: PurchaseAPI;
  readonly accountingApi?: PurchaseAccountingAPI;
  readonly initialDocumentId?: string | undefined;
  readonly workspace: AuthenticatedWorkspace | undefined;
}

type DocumentState =
  | { readonly status: "loading" }
  | { readonly status: "unavailable" }
  | {
      readonly documents: readonly Document[];
      readonly nextCursor: string;
      readonly status: "ready";
    };

interface ReviewFields {
  readonly supplierName: string;
  readonly invoiceNumber: string;
  readonly date: string;
  readonly subtotal: string;
  readonly gst: string;
  readonly total: string;
}

export function DocumentsScreen({
  api,
  onDocumentsChanged,
  workspace,
  purchaseApi,
  accountingApi,
  initialDocumentId,
}: DocumentsScreenProps) {
  const [selectedID, setSelectedID] = useState<string>(initialDocumentId ?? "");
  const [retainedSelection, setRetainedSelection] = useState<Document>();
  const [cursors, setCursors] = useState<readonly string[]>([""]);
  const [accountingFilter, setAccountingFilter] = useState<DocumentAccountingFilter>();
  const [statusFilter, setStatusFilter] = useState<DocumentStatus>();
  const [locating, setLocating] = useState(Boolean(initialDocumentId));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const input = useRef<HTMLInputElement>(null);
  const organisationId = workspace?.organisationId ?? "";
  const userId = workspace?.userId ?? "";
  const sessionId = workspace?.sessionId ?? "";
  const listInput = useMemo(
    () =>
      listCodec.encodeRequest(
        create(ListDocumentsRequestSchema, {
          authentication: { actorUserId: userId, sessionId },
          organisationId,
          page: create(PageRequestSchema, { pageSize: 200, cursor: cursors.at(-1) || undefined }),
          ...(accountingFilter ? { accounting: accountingFilter } : {}),
          ...(statusFilter ? { status: statusFilter } : {}),
        }),
      ),
    [organisationId, sessionId, userId, cursors, accountingFilter, statusFilter],
  );
  const documentsQuery = api.listDocuments.useQuery({
    enabled: organisationId.length > 0,
    input: listInput,
    retry: false,
  });
  const selectedInput = useMemo(
    () =>
      getDocumentCodec.encodeRequest(
        create(GetDocumentRequestSchema, {
          authentication: { actorUserId: userId, sessionId },
          documentId: initialDocumentId ?? "",
        }),
      ),
    [userId, sessionId, initialDocumentId],
  );
  const selectedQuery = api.getDocument.useQuery({
    enabled: Boolean(initialDocumentId && organisationId),
    input: selectedInput,
    retry: false,
  });
  useEffect(() => {
    if (!initialDocumentId) return;
    try {
      const retained =
        selectedQuery.data && getDocumentCodec.decodeResponse(selectedQuery.data).document;
      if (retained?.id === initialDocumentId && retained.organisationId === organisationId) {
        setSelectedID(retained.id);
        setRetainedSelection(retained);
        setLocating(false);
      } else if (selectedQuery.isError) {
        setLocating(false);
        setError("This document could not be located in the current organisation.");
      }
    } catch {
      setLocating(false);
      setError("This document could not be located in the current organisation.");
    }
  }, [initialDocumentId, organisationId, selectedQuery.data, selectedQuery.isError]);
  const state = useMemo<DocumentState>(() => {
    if (!organisationId || documentsQuery.isError) return { status: "unavailable" };
    if (!documentsQuery.data) return { status: "loading" };
    try {
      return {
        documents: listCodec.decodeResponse(documentsQuery.data).documents,
        nextCursor: listCodec.decodeResponse(documentsQuery.data).page?.nextCursor ?? "",
        status: "ready",
      };
    } catch {
      return { status: "unavailable" };
    }
  }, [documentsQuery.data, documentsQuery.isError, organisationId]);
  useEffect(() => {
    if (state.status !== "ready") return;
    const current = state.documents.find((item) => item.id === selectedID);
    if (current) {
      setRetainedSelection(current);
      setLocating(false);
    } else if (locating) return;
    else if (!selectedID && state.documents[0]) {
      setSelectedID(state.documents[0].id);
      setRetainedSelection(state.documents[0]);
    }
  }, [state, selectedID, locating]);
  const effectiveSelectedID =
    selectedID || (state.status === "ready" ? state.documents[0]?.id : undefined);

  const selected =
    state.status === "ready"
      ? (state.documents.find((item) => item.id === effectiveSelectedID) ??
        (retainedSelection?.organisationId === organisationId ? retainedSelection : undefined))
      : retainedSelection?.organisationId === organisationId
        ? retainedSelection
        : undefined;

  const upload = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file || !workspace?.organisationId) return;
    if (
      file.size < 1 ||
      file.size > 10 * 1024 * 1024 ||
      !["application/pdf", "image/png", "image/jpeg"].includes(file.type)
    ) {
      setError("Choose a PDF, PNG or JPEG no larger than 10 MB.");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      const original = new Uint8Array(await file.arrayBuffer());
      const extractedText = file.type === "application/pdf" ? extractNativePdfText(original) : "";
      const request = create(IngestDocumentRequestSchema, {
        commandContext: create(CommandContextSchema, {
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
          idempotencyKey: uuidV7(),
        }),
        organisationId: workspace.organisationId,
        sourceDisplayName: file.name,
        mimeType: file.type,
        original,
        extractedText,
        candidate: inferCandidate(extractedText, file.name),
      });
      const response = ingestCodec.decodeResponse(
        await api.ingestDocument(ingestCodec.encodeRequest(request)),
      );
      if (!response.document) throw new Error("missing document");
      setSelectedID(response.document.id);
      await Promise.all([api.listDocuments.invalidate(listInput), onDocumentsChanged?.()]);
    } catch {
      setError("The document could not be retained. It has not left this device.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-4">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h1 className="text-[18px] font-semibold tracking-[-0.02em] text-foreground">
            Documents
          </h1>
          <p className="mt-1 text-[11px] leading-5 text-muted-foreground">
            Retain source files, review locally extracted details, then approve them for later
            accounting.
          </p>
        </div>
        <input
          ref={input}
          accept="application/pdf,image/png,image/jpeg"
          className="sr-only"
          onChange={upload}
          type="file"
        />
        <Button disabled={busy} onClick={() => input.current?.click()} type="button">
          {busy ? (
            <LoaderCircle aria-hidden="true" className="size-3 animate-spin" />
          ) : (
            <Upload aria-hidden="true" className="size-3" />
          )}
          {busy ? "Retaining…" : "Upload document"}
        </Button>
      </div>
      {error ? (
        <p
          className="rounded-[6px] border border-warning-border bg-warning-soft px-3 py-2 text-[10px]"
          role="alert"
        >
          {error}
        </p>
      ) : null}

      <div className="grid min-h-[560px] grid-cols-[minmax(300px,0.9fr)_minmax(420px,1.4fr)] gap-4 max-[900px]:grid-cols-1">
        <section className="overflow-hidden rounded-[6px] border border-border bg-surface">
          <div className="grid gap-2 border-b border-border px-3 py-2 text-[9px]">
            <p className="font-semibold uppercase tracking-[0.05em] text-muted-foreground">
              Retained documents
            </p>
            <label className="grid gap-1">
              Accounting filter
              <select
                className="focus-ring rounded border border-border bg-surface p-1"
                value={accountingFilter ?? 0}
                onChange={(event) => {
                  setAccountingFilter(Number(event.target.value) || undefined);
                  setCursors([""]);
                }}
              >
                <option value={0}>All accounting states</option>
                <option value={DocumentAccountingFilter.UNPOSTED}>Not posted</option>
                <option value={DocumentAccountingFilter.POSTED}>Posted purchases</option>
                <option value={DocumentAccountingFilter.NEEDS_GST_REVIEW}>Needs GST review</option>
              </select>
            </label>
            <label className="grid gap-1">
              Review filter
              <select
                className="focus-ring rounded border border-border bg-surface p-1"
                value={statusFilter ?? 0}
                onChange={(event) => {
                  setStatusFilter(Number(event.target.value) || undefined);
                  setCursors([""]);
                }}
              >
                <option value={0}>All review states</option>
                <option value={DocumentStatus.NEEDS_REVIEW}>Needs extraction review</option>
                <option value={DocumentStatus.REVIEWED}>Reviewed</option>
              </select>
            </label>
          </div>
          {state.status === "ready" && state.documents.length > 0 ? (
            <ul className="divide-y divide-border">
              {state.documents.map((document) => (
                <li key={document.id}>
                  <button
                    className={`grid w-full grid-cols-[1fr_auto] gap-3 px-3 py-3 text-left ${effectiveSelectedID === document.id ? "bg-success-soft" : "hover:bg-muted"}`}
                    onClick={() => setSelectedID(document.id)}
                    type="button"
                  >
                    <span className="min-w-0">
                      <span className="block truncate text-[10px] font-semibold text-foreground">
                        {document.candidate?.supplierName || document.sourceDisplayName}
                      </span>
                      <span className="mt-1 block truncate text-[9px] text-muted-foreground">
                        {document.candidate?.invoiceNumber || "Invoice number not detected"} ·{" "}
                        {formatDate(document.candidate?.documentDate)}
                      </span>
                    </span>
                    <span className="text-right">
                      <span className="block text-[10px] font-semibold text-foreground">
                        {formatMoney(document.candidate?.total?.minorUnits)}
                      </span>
                      <span
                        className={`mt-1 inline-block rounded-full px-2 py-0.5 text-[8px] font-semibold ${document.status === DocumentStatus.REVIEWED ? "bg-success-soft text-forest" : "bg-warning-soft text-warning"}`}
                      >
                        {document.status === DocumentStatus.REVIEWED ? "Reviewed" : "Review"}
                      </span>
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <DocumentEmpty status={state.status} />
          )}
          {locating ? (
            <p className="px-3 py-2 text-[10px]" role="status">
              Locating selected document…
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2 border-t border-border p-3">
            <Button
              type="button"
              className="h-7 text-[9px]"
              disabled={cursors.length < 2 || documentsQuery.isFetching}
              onClick={() => setCursors((current) => current.slice(0, -1))}
            >
              Previous document page
            </Button>
            <Button
              type="button"
              className="h-7 text-[9px]"
              disabled={state.status !== "ready" || !state.nextCursor || documentsQuery.isFetching}
              onClick={() => {
                if (state.status === "ready" && state.nextCursor)
                  setCursors((current) => [...current, state.nextCursor]);
              }}
            >
              Next document page
            </Button>
          </div>
        </section>
        <div className="grid min-w-0 content-start gap-4">
          <ReviewPanel
            api={api}
            document={selected}
            onSaved={() =>
              Promise.all([api.listDocuments.invalidate(listInput), onDocumentsChanged?.()]).then(
                () => undefined,
              )
            }
            workspace={workspace}
          />
          {selected && workspace?.organisationId && purchaseApi && accountingApi ? (
            <PurchaseAccountingPanel
              key={`${workspace.workspaceId}:${workspace.userId}:${workspace.sessionId}:${selected.id}`}
              api={purchaseApi}
              accountingApi={accountingApi}
              documentApi={api}
              document={selected}
              workspace={workspace}
              onChanged={() =>
                Promise.all([api.listDocuments.invalidate(listInput), onDocumentsChanged?.()]).then(
                  () => undefined,
                )
              }
            />
          ) : null}
        </div>
      </div>
    </div>
  );
}

function ReviewPanel({
  api,
  document,
  onSaved,
  workspace,
}: {
  readonly api: Pick<TammyConveyorModule<"documents">, "saveDocumentReview">;
  readonly document: Document | undefined;
  readonly onSaved: () => Promise<void>;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const initial = useMemo(() => fieldsFor(document), [document]);
  const [fields, setFields] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();
  useEffect(() => setFields(initial), [initial]);
  if (!document)
    return (
      <section className="grid place-items-center rounded-[6px] border border-dashed border-border bg-surface p-8 text-center">
        <div>
          <FileText className="mx-auto size-6 text-muted-foreground" />
          <p className="mt-3 text-[11px] font-semibold">Select a document</p>
          <p className="mt-1 text-[10px] text-muted-foreground">
            Extracted details appear here for human review.
          </p>
        </div>
      </section>
    );

  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!workspace) return;
    setSaving(true);
    setError(undefined);
    try {
      const request = create(SaveDocumentReviewRequestSchema, {
        commandContext: create(CommandContextSchema, {
          authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
          idempotencyKey: uuidV7(),
        }),
        documentId: document.id,
        expectedVersion: document.version,
        candidate: candidateFromFields(fields),
      });
      const response = reviewCodec.decodeResponse(
        await api.saveDocumentReview(reviewCodec.encodeRequest(request)),
      );
      if (!response.document || response.document.status !== DocumentStatus.REVIEWED)
        throw new Error("review failed");
      await onSaved();
    } catch {
      setError("The review could not be saved. Reload and try again.");
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="rounded-[6px] border border-border bg-surface p-4">
      <div className="flex items-start justify-between gap-3 border-b border-border pb-3">
        <div>
          <p className="text-[9px] font-semibold uppercase tracking-[0.05em] text-muted-foreground">
            Source document
          </p>
          <h2 className="mt-1 text-[12px] font-semibold">{document.sourceDisplayName}</h2>
          <p className="mt-1 text-[9px] text-muted-foreground">
            {formatBytes(document.byteLength)} · retained locally
          </p>
        </div>
        {document.status === DocumentStatus.REVIEWED ? (
          <span className="flex items-center gap-1 rounded-full bg-success-soft px-2 py-1 text-[8px] font-semibold text-forest">
            <Check className="size-3" /> Reviewed
          </span>
        ) : null}
      </div>
      <div className="mt-4 grid grid-cols-[0.9fr_1.1fr] gap-4 max-[760px]:grid-cols-1">
        <pre className="min-h-72 whitespace-pre-wrap break-words rounded-[5px] border border-border bg-background p-3 font-sans text-[9px] leading-5 text-muted-foreground">
          {document.extractedText ||
            "No native text was found. Enter the details from the source image manually."}
        </pre>
        <form className="grid content-start gap-3" onSubmit={save}>
          <Field
            label="Supplier"
            onChange={(value) => setFields({ ...fields, supplierName: value })}
            value={fields.supplierName}
          />
          <Field
            label="Invoice number"
            onChange={(value) => setFields({ ...fields, invoiceNumber: value })}
            value={fields.invoiceNumber}
          />
          <Field
            label="Date"
            onChange={(value) => setFields({ ...fields, date: value })}
            type="date"
            value={fields.date}
          />
          <div className="grid grid-cols-3 gap-2">
            <Field
              label="Subtotal"
              onChange={(value) => setFields({ ...fields, subtotal: value })}
              type="number"
              value={fields.subtotal}
            />
            <Field
              label="GST"
              onChange={(value) => setFields({ ...fields, gst: value })}
              type="number"
              value={fields.gst}
            />
            <Field
              label="Total"
              onChange={(value) => setFields({ ...fields, total: value })}
              type="number"
              value={fields.total}
            />
          </div>
          {error ? (
            <p className="text-[9px] text-destructive" role="alert">
              {error}
            </p>
          ) : null}
          <Button
            className="mt-1"
            disabled={saving || document.status === DocumentStatus.REVIEWED}
            type="submit"
          >
            {saving
              ? "Saving…"
              : document.status === DocumentStatus.REVIEWED
                ? "Review saved"
                : "Save review"}
          </Button>
        </form>
      </div>
    </section>
  );
}

function Field({
  label,
  onChange,
  type = "text",
  value,
}: {
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly type?: string;
  readonly value: string;
}) {
  return (
    <label className="grid gap-1 text-[9px] font-semibold text-foreground">
      {label}
      <input
        className="focus-ring h-8 rounded-[5px] border border-border bg-surface px-2 text-[10px] font-normal outline-none"
        min={type === "number" ? "0" : undefined}
        onChange={(event) => onChange(event.target.value)}
        step={type === "number" ? "0.01" : undefined}
        type={type}
        value={value}
      />
    </label>
  );
}

function DocumentEmpty({ status }: { readonly status: DocumentState["status"] }) {
  return (
    <div className="grid min-h-72 place-items-center p-6 text-center">
      <div>
        {status === "loading" ? (
          <LoaderCircle className="mx-auto size-4 animate-spin text-forest" />
        ) : (
          <FileText className="mx-auto size-5 text-muted-foreground" />
        )}
        <p className="mt-2 text-[11px] font-semibold">
          {status === "unavailable"
            ? "Documents unavailable"
            : status === "loading"
              ? "Loading documents"
              : "No documents yet"}
        </p>
        <p className="mt-1 text-[9px] text-muted-foreground">
          Upload a local invoice or receipt to begin.
        </p>
      </div>
    </div>
  );
}

function inferCandidate(text: string, fileName: string) {
  const lines = text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
  const supplierName =
    lines.find((line) => !/^(tax\s+)?invoice\b/i.test(line))?.slice(0, 256) ??
    fileName.replace(/\.[^.]+$/, "").slice(0, 256);
  const invoiceNumber =
    text
      .match(/invoice(?:\s+(?:number|no\.?))?\s*[:#-]?\s*([A-Z0-9][A-Z0-9-]{2,})/i)?.[1]
      ?.slice(0, 128) ?? "";
  const dateMatch = text.match(/\b(\d{1,2})[/-](\d{1,2})[/-](\d{4})\b/);
  const subtotal = labelledAmount(text, "subtotal");
  const gst = labelledAmount(text, "gst");
  const total = labelledAmount(text, "total");
  return create(DocumentCandidateSchema, {
    supplierName,
    invoiceNumber,
    ...(dateMatch
      ? {
          documentDate: create(CivilDateSchema, {
            year: Number(dateMatch[3]),
            month: Number(dateMatch[2]),
            day: Number(dateMatch[1]),
          }),
        }
      : {}),
    subtotal: create(MoneySchema, { currencyCode: "AUD", minorUnits: subtotal }),
    gst: create(MoneySchema, { currencyCode: "AUD", minorUnits: gst }),
    total: create(MoneySchema, { currencyCode: "AUD", minorUnits: total }),
  });
}

function labelledAmount(text: string, label: string): bigint {
  const match = text.match(
    new RegExp(`${label}[^$\\d]{0,16}\\$?\\s*([0-9][0-9,]*(?:\\.[0-9]{1,2})?)`, "i"),
  );
  return decimalToMinor(match?.[1] ?? "0");
}

function decimalToMinor(value: string): bigint {
  const normalized = value.replace(/,/g, "").trim();
  if (!/^\d+(?:\.\d{0,2})?$/.test(normalized)) return 0n;
  const [whole = "0", fraction = ""] = normalized.split(".");
  return BigInt(whole) * 100n + BigInt(fraction.padEnd(2, "0"));
}

function candidateFromFields(fields: ReviewFields) {
  const parts = fields.date.match(/^(\d{4})-(\d{2})-(\d{2})$/);
  return create(DocumentCandidateSchema, {
    supplierName: fields.supplierName.trim(),
    invoiceNumber: fields.invoiceNumber.trim(),
    ...(parts
      ? {
          documentDate: create(CivilDateSchema, {
            year: Number(parts[1]),
            month: Number(parts[2]),
            day: Number(parts[3]),
          }),
        }
      : {}),
    subtotal: create(MoneySchema, {
      currencyCode: "AUD",
      minorUnits: decimalToMinor(fields.subtotal),
    }),
    gst: create(MoneySchema, { currencyCode: "AUD", minorUnits: decimalToMinor(fields.gst) }),
    total: create(MoneySchema, { currencyCode: "AUD", minorUnits: decimalToMinor(fields.total) }),
  });
}

function fieldsFor(document: Document | undefined): ReviewFields {
  const candidate = document?.candidate;
  return {
    supplierName: candidate?.supplierName ?? "",
    invoiceNumber: candidate?.invoiceNumber ?? "",
    date: candidate?.documentDate
      ? `${candidate.documentDate.year.toString().padStart(4, "0")}-${candidate.documentDate.month.toString().padStart(2, "0")}-${candidate.documentDate.day.toString().padStart(2, "0")}`
      : "",
    subtotal: minorToDecimal(candidate?.subtotal?.minorUnits),
    gst: minorToDecimal(candidate?.gst?.minorUnits),
    total: minorToDecimal(candidate?.total?.minorUnits),
  };
}

function minorToDecimal(value: bigint | undefined): string {
  return `${((value ?? 0n) / 100n).toString()}.${((value ?? 0n) % 100n).toString().padStart(2, "0")}`;
}
function formatMoney(value: bigint | undefined): string {
  return aud(value);
}
function formatBytes(value: bigint): string {
  return value < 1024n ? `${value} B` : `${(Number(value) / 1024).toFixed(1)} KB`;
}
function formatDate(
  value: { readonly year: number; readonly month: number; readonly day: number } | undefined,
): string {
  return value
    ? `${value.day.toString().padStart(2, "0")}/${value.month.toString().padStart(2, "0")}/${value.year}`
    : "Date not detected";
}

function uuidV7(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  let milliseconds = Date.now();
  for (let index = 5; index >= 0; index -= 1) {
    bytes[index] = milliseconds & 0xff;
    milliseconds = Math.floor(milliseconds / 256);
  }
  bytes[6] = ((bytes[6] ?? 0) & 0x0f) | 0x70;
  bytes[8] = ((bytes[8] ?? 0) & 0x3f) | 0x80;
  const hex = [...bytes].map((value) => value.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
