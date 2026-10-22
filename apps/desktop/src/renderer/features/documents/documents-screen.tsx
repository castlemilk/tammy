import { create } from "@bufbuild/protobuf";
import {
  CivilDateSchema,
  CommandContextSchema,
  MoneySchema,
  PageRequestSchema,
} from "@tammy/connect-client/tammy/v1/common_pb.js";
import {
  type Document,
  DocumentCandidateSchema,
  DocumentStatus,
  IngestDocumentRequestSchema,
  IngestDocumentResponseSchema,
  ListDocumentsRequestSchema,
  ListDocumentsResponseSchema,
  SaveDocumentReviewRequestSchema,
  SaveDocumentReviewResponseSchema,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import { Check, FileText, RefreshCw, Upload, X } from "lucide-react";
import { type ChangeEvent, type FormEvent, useEffect, useMemo, useRef, useState } from "react";

import type { TammyDesktopAPI } from "../../../shared/desktop-api";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import {
  WorkFilters,
  WorkHeading,
  WorkNotice,
  WorkSearch,
  WorkState,
} from "../../components/review-workspace";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { extractNativePdfText } from "./pdf-text";

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

interface DocumentsScreenProps {
  readonly api: Pick<TammyDesktopAPI, "ingestDocument" | "listDocuments" | "saveDocumentReview">;
  readonly workspace: AuthenticatedWorkspace | undefined;
}

type DocumentState =
  | { readonly status: "loading" }
  | { readonly status: "unavailable" }
  | { readonly documents: readonly Document[]; readonly status: "ready" };

interface ReviewFields {
  readonly supplierName: string;
  readonly invoiceNumber: string;
  readonly date: string;
  readonly subtotal: string;
  readonly gst: string;
  readonly total: string;
}

export function DocumentsScreen({ api, workspace }: DocumentsScreenProps) {
  const [state, setState] = useState<DocumentState>({ status: "loading" });
  const [selectedID, setSelectedID] = useState<string>();
  const [reload, setReload] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<"all" | "review" | "reviewed">("all");
  const input = useRef<HTMLInputElement>(null);
  const uploadActive = useRef(false);
  const rowButtons = useRef(new Map<string, HTMLButtonElement>());

  useEffect(() => {
    void reload;
    if (!workspace?.organisationId) {
      setState({ status: "unavailable" });
      return;
    }
    let active = true;
    setState({ status: "loading" });
    const request = create(ListDocumentsRequestSchema, {
      authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
      organisationId: workspace.organisationId,
      page: create(PageRequestSchema, { pageSize: 200 }),
    });
    void api
      .listDocuments(listCodec.encodeRequest(request))
      .then((frame) => listCodec.decodeResponse(frame))
      .then((response) => {
        if (!active) return;
        setState({ documents: response.documents, status: "ready" });
        setSelectedID((current) =>
          current && response.documents.some((item) => item.id === current)
            ? current
            : response.documents[0]?.id,
        );
      })
      .catch(() => {
        if (active) setState({ status: "unavailable" });
      });
    return () => {
      active = false;
    };
  }, [api, reload, workspace]);

  const documents = state.status === "ready" ? state.documents : [];
  const selected = documents.find((item) => item.id === selectedID);
  const reviewCount = documents.filter((item) => item.status !== DocumentStatus.REVIEWED).length;
  const visible = documents.filter(
    (item) =>
      (filter === "all" || (filter === "reviewed") === (item.status === DocumentStatus.REVIEWED)) &&
      [item.sourceDisplayName, item.candidate?.supplierName, item.candidate?.invoiceNumber]
        .join(" ")
        .toLowerCase()
        .includes(query.toLowerCase().trim()),
  );

  const upload = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file || !workspace?.organisationId || uploadActive.current) return;
    if (
      file.size < 1 ||
      file.size > 10 * 1024 * 1024 ||
      !["application/pdf", "image/png", "image/jpeg"].includes(file.type)
    ) {
      setError("Choose a PDF, PNG or JPEG no larger than 10 MB.");
      return;
    }
    uploadActive.current = true;
    setBusy(true);
    setError(undefined);
    try {
      const original = new Uint8Array(await file.arrayBuffer());
      const extractedText = file.type === "application/pdf" ? extractNativePdfText(original) : "";
      const response = ingestCodec.decodeResponse(
        await api.ingestDocument(
          ingestCodec.encodeRequest(
            create(IngestDocumentRequestSchema, {
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
            }),
          ),
        ),
      );
      if (!response.document) throw new Error("missing document");
      const retained = response.document;
      setState((current) => ({
        status: "ready",
        documents: [
          retained,
          ...(current.status === "ready"
            ? current.documents.filter((item) => item.id !== retained.id)
            : []),
        ],
      }));
      setSelectedID(retained.id);
      setQuery("");
      setFilter("all");
    } catch {
      setError("The document could not be retained. It has not left this device.");
    } finally {
      uploadActive.current = false;
      setBusy(false);
    }
  };

  return (
    <div className="mx-auto grid w-full max-w-[1120px] gap-6">
      <WorkHeading
        title="Documents"
        description="A clear place for your source documents. Check the details, save your review, and keep the evidence close."
        action={
          <>
            <input
              ref={input}
              accept="application/pdf,image/png,image/jpeg"
              className="sr-only"
              onChange={upload}
              type="file"
            />
            <Button
              disabled={busy || state.status !== "ready" || !workspace?.organisationId}
              onClick={() => input.current?.click()}
              type="button"
            >
              <Upload aria-hidden="true" className="size-4" />
              {busy ? "Retaining…" : "Upload document"}
            </Button>
          </>
        }
      />
      {error ? <WorkNotice>{error}</WorkNotice> : null}
      <section aria-label="Source documents" className="work-surface overflow-hidden">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border p-4">
          <WorkFilters
            label="Document status"
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: "All documents", count: documents.length },
              { value: "review", label: "Needs review", count: reviewCount },
              { value: "reviewed", label: "Reviewed", count: documents.length - reviewCount },
            ]}
          />
          <div className="flex w-full min-w-0 max-w-[330px] items-center gap-2">
            <WorkSearch label="Search documents" value={query} onChange={setQuery} />
            <Button
              aria-label="Refresh documents"
              disabled={busy || state.status === "loading"}
              onClick={() => setReload((value) => value + 1)}
              type="button"
              variant="ghost"
            >
              <RefreshCw aria-hidden="true" className="size-4" />
            </Button>
          </div>
        </div>
        {state.status !== "ready" ? (
          <WorkState
            title={state.status === "loading" ? "Loading documents" : "Documents unavailable"}
            loading={state.status === "loading"}
            description={
              state.status === "loading"
                ? "Opening the source documents retained in this workspace."
                : "Your local documents could not be loaded. Try refreshing this view."
            }
            action={
              state.status === "unavailable" ? (
                <Button onClick={() => setReload((value) => value + 1)} variant="outline">
                  Try again
                </Button>
              ) : undefined
            }
          />
        ) : visible.length === 0 ? (
          <WorkState
            title={documents.length ? "No matching documents" : "Your next clear step"}
            description={
              documents.length
                ? "Try a different name or status. Your retained documents are still here."
                : "Upload a local invoice or receipt. Native PDF text can be suggested; image details need your review."
            }
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="work-table min-w-[650px]" aria-label="Retained documents">
              <thead>
                <tr>
                  <th>Document & supplier</th>
                  <th>Invoice</th>
                  <th>Date</th>
                  <th className="text-right">Total · AUD</th>
                  <th>Status</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((document) => (
                  <tr
                    key={document.id}
                    className={
                      selectedID === document.id ? "bg-forest-soft" : "hover:bg-background"
                    }
                  >
                    <td>
                      <button
                        ref={(node) => {
                          if (node) rowButtons.current.set(document.id, node);
                          else rowButtons.current.delete(document.id);
                        }}
                        aria-pressed={selectedID === document.id}
                        className="focus-ring flex min-h-11 w-full items-center gap-3 rounded-lg text-left"
                        onClick={() => setSelectedID(document.id)}
                        type="button"
                      >
                        <span className="grid size-10 shrink-0 place-items-center rounded-xl border border-border bg-surface text-forest">
                          <FileText aria-hidden="true" className="size-4" />
                        </span>
                        <span className="min-w-0">
                          <span className="block max-w-[260px] truncate font-semibold">
                            {document.sourceDisplayName}
                          </span>
                          <span className="mt-1 block text-[12px] text-muted-foreground">
                            {document.candidate?.supplierName || "Supplier to review"}
                          </span>
                        </span>
                      </button>
                    </td>
                    <td>{document.candidate?.invoiceNumber || "—"}</td>
                    <td className="tabular-nums">{formatDate(document.candidate?.documentDate)}</td>
                    <td className="text-right font-semibold tabular-nums">
                      {formatMoney(document.candidate?.total?.minorUnits)}
                    </td>
                    <td>
                      <DocumentStatusLabel reviewed={document.status === DocumentStatus.REVIEWED} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div className="border-t border-border px-5 py-3 text-[12px] text-muted-foreground">
          {state.status === "ready"
            ? `${visible.length} shown · ${documents.length} loaded (up to 200)`
            : "Local source documents"}{" "}
          · Files stay in this workspace
        </div>
      </section>
      {selected ? (
        <ReviewPanel
          key={selected.id}
          api={api}
          document={selected}
          workspace={workspace}
          onClose={() => {
            setSelectedID(undefined);
            rowButtons.current.get(selected.id)?.focus();
          }}
          onSaved={(saved) =>
            setState((current) =>
              current.status === "ready"
                ? {
                    ...current,
                    documents: current.documents.map((item) =>
                      item.id === saved.id ? saved : item,
                    ),
                  }
                : current,
            )
          }
        />
      ) : null}
    </div>
  );
}

function DocumentStatusLabel({ reviewed }: { readonly reviewed: boolean }) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 py-1 text-[12px] font-semibold ${reviewed ? "bg-success-soft text-forest" : "bg-warning-soft text-warning"}`}
    >
      {reviewed ? (
        <Check aria-hidden="true" className="size-3" />
      ) : (
        <span aria-hidden="true" className="size-1.5 rounded-full bg-warning" />
      )}
      {reviewed ? "Reviewed" : "Needs review"}
    </span>
  );
}

function ReviewPanel({
  api,
  document,
  onSaved,
  onClose,
  workspace,
}: {
  readonly api: Pick<TammyDesktopAPI, "saveDocumentReview">;
  readonly document: Document;
  readonly onSaved: (document: Document) => void;
  readonly onClose: () => void;
  readonly workspace: AuthenticatedWorkspace | undefined;
}) {
  const initial = useMemo(() => fieldsFor(document), [document]);
  const [fields, setFields] = useState(initial);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string>();
  const active = useRef(false);
  const attempt = useRef<{ signature: string; key: string } | undefined>(undefined);
  const reviewed = document.status === DocumentStatus.REVIEWED;
  useEffect(() => {
    setFields(initial);
    setError(undefined);
  }, [initial]);
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!workspace || active.current || reviewed) return;
    active.current = true;
    setSaving(true);
    setError(undefined);
    const signature = JSON.stringify([document.id, document.version.toString(), fields]);
    if (attempt.current?.signature !== signature) attempt.current = { signature, key: uuidV7() };
    try {
      const response = reviewCodec.decodeResponse(
        await api.saveDocumentReview(
          reviewCodec.encodeRequest(
            create(SaveDocumentReviewRequestSchema, {
              commandContext: create(CommandContextSchema, {
                authentication: { actorUserId: workspace.userId, sessionId: workspace.sessionId },
                idempotencyKey: attempt.current.key,
              }),
              documentId: document.id,
              expectedVersion: document.version,
              candidate: candidateFromFields(fields),
            }),
          ),
        ),
      );
      if (
        !response.document ||
        response.document.id !== document.id ||
        response.document.status !== DocumentStatus.REVIEWED
      )
        throw new Error("review failed");
      onSaved(response.document);
    } catch {
      setError(
        "The review could not be saved. Your changes are still here. Try again, or refresh the document list if it changed elsewhere.",
      );
    } finally {
      active.current = false;
      setSaving(false);
    }
  };
  return (
    <section aria-label="Document review" className="work-surface overflow-hidden">
      <div className="flex items-start justify-between gap-3 border-b border-border p-5">
        <div className="min-w-0">
          <p className="work-eyebrow">{reviewed ? "Retained evidence" : "Review document"}</p>
          <h2 className="mt-1 break-words font-serif text-[26px] leading-8">
            {document.sourceDisplayName}
          </h2>
          <p className="mt-1 text-[12px] text-muted-foreground">
            {formatBytes(document.byteLength)} · retained locally
          </p>
        </div>
        <Button
          aria-label="Close review"
          disabled={saving}
          onClick={onClose}
          type="button"
          variant="ghost"
        >
          <X aria-hidden="true" className="size-4" />
        </Button>
      </div>
      <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] max-[1050px]:grid-cols-1">
        <div className="border-r border-border bg-background p-6 max-[1050px]:border-b max-[1050px]:border-r-0 max-[700px]:p-4">
          <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-[14px] font-semibold">Extracted source text</h3>
            <span className="text-[12px] text-muted-foreground">
              {document.mimeType === "application/pdf" ? "Native PDF text" : "Image · manual entry"}
            </span>
          </div>
          <div className="min-h-64 rounded-xl border border-border bg-surface p-6 max-[700px]:p-4">
            <FileText aria-hidden="true" className="mb-5 size-6 text-forest" />
            <pre className="whitespace-pre-wrap break-words font-sans text-[14px] leading-7 text-foreground">
              {document.extractedText ||
                "No native text was found. Enter the details from the source image manually."}
            </pre>
          </div>
          <p className="mt-4 text-[12px] leading-5 text-muted-foreground">
            This is retained text, not an original-file preview. Check the source file when
            confirming details.
          </p>
        </div>
        <form className="grid content-start gap-4 p-6 max-[700px]:p-4" onSubmit={save}>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-[14px] font-semibold">
              {reviewed ? "Reviewed details" : "Check the details"}
            </h3>
            <DocumentStatusLabel reviewed={reviewed} />
          </div>
          <p className="text-[12px] leading-5 text-muted-foreground">
            {reviewed
              ? "Your review is saved. No accounting entries were posted by this action."
              : "Suggestions need your confirmation. Saving a review does not post accounting entries."}
          </p>
          <Field
            label="Supplier"
            onChange={(value) => setFields({ ...fields, supplierName: value })}
            value={fields.supplierName}
            readOnly={reviewed || saving}
          />
          <div className="grid grid-cols-2 gap-3 max-[700px]:grid-cols-1">
            <Field
              label="Invoice number"
              onChange={(value) => setFields({ ...fields, invoiceNumber: value })}
              value={fields.invoiceNumber}
              readOnly={reviewed || saving}
            />
            <Field
              label="Date"
              type="date"
              onChange={(value) => setFields({ ...fields, date: value })}
              value={fields.date}
              readOnly={reviewed || saving}
            />
          </div>
          <div className="grid grid-cols-3 gap-3 max-[700px]:grid-cols-1">
            {(
              [
                ["Subtotal", "subtotal"],
                ["GST", "gst"],
                ["Total", "total"],
              ] as const
            ).map(([label, key]) => (
              <Field
                key={key}
                label={label}
                type="number"
                onChange={(value) => setFields({ ...fields, [key]: value })}
                value={fields[key]}
                readOnly={reviewed || saving}
              />
            ))}
          </div>
          {error ? <WorkNotice>{error}</WorkNotice> : null}
          <div className="mt-2 flex flex-wrap items-center gap-3 border-t border-border pt-4">
            <Button disabled={saving || reviewed} type="submit">
              {saving ? "Saving…" : reviewed ? "Review saved" : "Save review"}
            </Button>
            {!reviewed ? (
              <Button
                disabled={saving}
                onClick={() => {
                  setFields(initial);
                  setError(undefined);
                }}
                type="button"
                variant="ghost"
              >
                Cancel changes
              </Button>
            ) : null}
          </div>
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
  readOnly,
}: {
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly type?: string;
  readonly value: string;
  readonly readOnly: boolean;
}) {
  return (
    <label className="grid gap-1.5 text-[12px] font-semibold">
      {label}
      <input
        className="focus-ring h-11 min-w-0 w-full rounded-xl border border-border bg-surface px-3 text-[14px] font-normal read-only:bg-background"
        min={type === "number" ? "0" : undefined}
        step={type === "number" ? "0.01" : undefined}
        onChange={(event) => onChange(event.target.value)}
        readOnly={readOnly}
        type={type}
        value={value}
      />
    </label>
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
  return new Intl.NumberFormat("en-AU", { style: "currency", currency: "AUD" }).format(
    Number(value ?? 0n) / 100,
  );
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
