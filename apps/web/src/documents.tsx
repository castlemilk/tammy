import { FileText, Upload } from "lucide-react";
import { type ChangeEvent, type FormEvent, useEffect, useRef, useState } from "react";
import { PageLimit } from "./accounting";
import { api } from "./api";
import { amountInput, formatAmount, parseAmount } from "./money";
import { extractNativePdfText } from "./pdf-text";
import type { DocumentRecord } from "./types";
import { useCommandKey } from "./use-command-key";
import { useRemote } from "./use-remote";
import { displayDate, errorMessage } from "./utils";

const MAX_DOCUMENT_BYTES = 512 * 1024;

function base64(bytes: Uint8Array): string {
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 0x8000)
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  return btoa(binary);
}

export function DocumentsScreen() {
  const documents = useRemote("documents", api.documents);
  const [selectedId, setSelectedId] = useState<string>();
  const [localText, setLocalText] = useState<{ id: string; text: string }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const input = useRef<HTMLInputElement>(null);
  const command = useCommandKey();
  const records = documents.state.status === "ready" ? documents.state.value.documents : [];
  useEffect(() => {
    if (documents.state.status !== "ready") return;
    setSelectedId((current) =>
      current &&
      documents.state.status === "ready" &&
      documents.state.value.documents.some((document) => document.id === current)
        ? current
        : documents.state.status === "ready"
          ? documents.state.value.documents[0]?.id
          : undefined,
    );
  }, [documents.state]);
  const selected = records.find((document) => document.id === selectedId);

  async function upload(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    if (
      !file.size ||
      file.size > MAX_DOCUMENT_BYTES ||
      !["application/pdf", "image/png", "image/jpeg"].includes(file.type)
    ) {
      setError("Choose a PDF, PNG or JPEG no larger than 512 KiB.");
      return;
    }
    setBusy(true);
    setError(undefined);
    try {
      const bytes = new Uint8Array(await file.arrayBuffer());
      // 8,192 UTF-16 units fit under the gateway's 32 KiB UTF-8 text cap.
      const extractedText =
        file.type === "application/pdf" ? extractNativePdfText(bytes).slice(0, 8192) : "";
      const payload = {
        sourceDisplayName: file.name,
        mimeType: file.type,
        originalBase64: base64(bytes),
        extractedText,
        candidate: {
          supplierName: "",
          invoiceNumber: "",
          date: "",
          subtotalMinor: 0,
          gstMinor: 0,
          totalMinor: 0,
        },
      };
      const result = await api.uploadDocument({
        ...payload,
        idempotencyKey: command.keyFor(payload),
      });
      command.clear();
      setSelectedId(result.document.id);
      setLocalText({ id: result.document.id, text: extractedText });
      documents.reload();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="screen">
      <div className="page-heading">
        <div>
          <h1>Documents</h1>
          <p>Retain source files and review their details locally</p>
        </div>
        <input
          accept="application/pdf,image/png,image/jpeg"
          aria-label="Upload document"
          className="visually-hidden"
          onChange={upload}
          ref={input}
          type="file"
        />
        <button
          className="button primary"
          disabled={busy}
          onClick={() => input.current?.click()}
          type="button"
        >
          <Upload aria-hidden="true" />
          {busy ? "Uploading…" : "Upload document"}
        </button>
      </div>
      <p className="supporting-copy">
        PDF, PNG or JPEG up to 512 KiB. Native PDF text is extracted locally when available; images
        need manual review.
      </p>
      {error ? (
        <p className="notice error" role="alert">
          {error}
        </p>
      ) : null}
      {documents.state.status === "error" ? (
        <p className="notice error" role="alert">
          {documents.state.message}{" "}
          <button className="text-button" onClick={documents.reload} type="button">
            Retry
          </button>
        </p>
      ) : documents.state.status === "loading" ? (
        <p className="loading" role="status">
          Loading documents…
        </p>
      ) : (
        <>
          <PageLimit page={documents.state.value.page} />
          <div className="document-grid">
            <section className="panel document-list">
              <h2>Documents</h2>
              {records.length ? (
                <ul>
                  {records.map((document) => (
                    <li key={document.id}>
                      <button
                        aria-pressed={selectedId === document.id}
                        className={selectedId === document.id ? "selected" : ""}
                        onClick={() => setSelectedId(document.id)}
                        type="button"
                      >
                        <FileText aria-hidden="true" />
                        <span>
                          <strong>{document.supplierName || document.sourceDisplayName}</strong>
                          <small>
                            {document.sourceDisplayName} · {displayDate(document.date)}
                          </small>
                        </span>
                        <span className="document-list-end">
                          <strong>{formatAmount(document.totalMinor)}</strong>
                          <small>
                            {document.status === "reviewed" ? "Reviewed" : "Needs review"}
                          </small>
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
              ) : (
                <div className="empty-content">
                  <h3>No documents yet</h3>
                  <p>Upload an invoice or receipt to begin</p>
                </div>
              )}
            </section>
            {selected ? (
              <DocumentReview
                document={selected}
                extractedText={localText?.id === selected.id ? localText.text : ""}
                key={`${selected.id}-${selected.version}`}
                onSaved={documents.reload}
              />
            ) : (
              <section className="panel review-empty">
                <div className="empty-content">
                  <h3>Select a document</h3>
                  <p>Its details will appear here for review</p>
                </div>
              </section>
            )}
          </div>
        </>
      )}
    </div>
  );
}

function DocumentReview({
  document,
  extractedText,
  onSaved,
}: {
  document: DocumentRecord;
  extractedText: string;
  onSaved: () => void;
}) {
  const [supplier, setSupplier] = useState(document.supplierName);
  const [invoice, setInvoice] = useState(document.invoiceNumber);
  const [date, setDate] = useState(document.date ?? "");
  const [subtotal, setSubtotal] = useState(amountInput(document.subtotalMinor));
  const [gst, setGst] = useState(amountInput(document.gstMinor));
  const [total, setTotal] = useState(amountInput(document.totalMinor));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const command = useCommandKey();
  const reviewed = document.status === "reviewed";
  async function save(event: FormEvent) {
    event.preventDefault();
    const subtotalMinor = parseAmount(subtotal);
    const gstMinor = parseAmount(gst);
    const totalMinor = parseAmount(total);
    if (
      subtotalMinor === undefined ||
      gstMinor === undefined ||
      totalMinor === undefined ||
      subtotalMinor < 0 ||
      gstMinor < 0 ||
      totalMinor < 0
    ) {
      setError("Enter non-negative AUD amounts with no more than two decimal places.");
      return;
    }
    const payload = {
      version: document.version,
      supplierName: supplier.trim(),
      invoiceNumber: invoice.trim(),
      date,
      subtotalMinor,
      gstMinor,
      totalMinor,
    };
    setBusy(true);
    setError(undefined);
    try {
      await api.reviewDocument(document.id, {
        ...payload,
        idempotencyKey: command.keyFor(payload),
      });
      command.clear();
      onSaved();
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="panel review-panel">
      <div className="panel-heading">
        <div>
          <h2>Review document</h2>
          <p>{document.sourceDisplayName}</p>
        </div>
        <span className={`status ${reviewed ? "ready" : "pending"}`}>
          {reviewed ? "Reviewed" : "Needs review"}
        </span>
      </div>
      <p className="notice">
        Check every field against your source file. A file preview is not available in this view.
      </p>
      {extractedText ? (
        <details className="extracted-text">
          <summary>Locally extracted PDF text</summary>
          <pre>{extractedText}</pre>
        </details>
      ) : null}
      <form className="form-grid review-form" onSubmit={save}>
        <label>
          Supplier
          <input
            disabled={reviewed}
            onChange={(event) => setSupplier(event.target.value)}
            required
            value={supplier}
          />
        </label>
        <label>
          Invoice number
          <input
            disabled={reviewed}
            onChange={(event) => setInvoice(event.target.value)}
            value={invoice}
          />
        </label>
        <label>
          Document date
          <input
            disabled={reviewed}
            onChange={(event) => setDate(event.target.value)}
            required
            type="date"
            value={date}
          />
        </label>
        <label>
          Subtotal (AUD)
          <input
            disabled={reviewed}
            inputMode="decimal"
            onChange={(event) => setSubtotal(event.target.value)}
            required
            value={subtotal}
          />
        </label>
        <label>
          GST (AUD)
          <input
            disabled={reviewed}
            inputMode="decimal"
            onChange={(event) => setGst(event.target.value)}
            required
            value={gst}
          />
        </label>
        <label>
          Total (AUD)
          <input
            disabled={reviewed}
            inputMode="decimal"
            onChange={(event) => setTotal(event.target.value)}
            required
            value={total}
          />
        </label>
        <div className="form-actions">
          <button className="button primary" disabled={busy || reviewed} type="submit">
            {busy ? "Saving…" : reviewed ? "Review saved" : "Save review"}
          </button>
        </div>
      </form>
      {error ? (
        <p className="notice error form-error" role="alert">
          {error}
        </p>
      ) : null}
    </section>
  );
}
