import { useEffect, useState } from "react";
import type {
  CompanyInvoiceTemplate,
  InvoiceTemplateDefaults,
} from "../../../shared/invoice-template";
import { Button } from "../../components/ui/button";
import { SurfaceHeading } from "../../components/workspace-primitives";
import { invoiceFieldClass } from "./invoice-line-table";
import { InvoiceLogo } from "./invoice-logo";
import type { InvoicesApi } from "./invoices-screen";

type TemplateEditor = Omit<InvoiceTemplateDefaults, "paymentTermsDays"> & {
  readonly paymentTermsDays: string;
};

export function InvoiceTemplatePanel({
  api,
  template,
  loading,
  error,
  disabled,
  canApply,
  onRetry,
  onSaved,
  onApply,
}: {
  readonly api: InvoicesApi;
  readonly template: CompanyInvoiceTemplate | undefined;
  readonly loading: boolean;
  readonly error: string | undefined;
  readonly disabled: boolean;
  readonly canApply: boolean;
  readonly onRetry: () => void;
  readonly onSaved: (value: CompanyInvoiceTemplate) => void;
  readonly onApply: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const [editor, setEditor] = useState<TemplateEditor>();
  const [version, setVersion] = useState<number>();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string>();
  const [notice, setNotice] = useState<string>();

  useEffect(() => {
    if (template && !editor) {
      setEditor({
        ...template.defaults,
        paymentTermsDays: String(template.defaults.paymentTermsDays),
      });
      setVersion(template.version);
    }
  }, [template, editor]);

  function edit(value: Partial<TemplateEditor>) {
    setEditor((previous) => (previous ? { ...previous, ...value } : previous));
    setNotice(undefined);
  }

  function removeLogo() {
    setEditor((previous) => {
      if (!previous) return previous;
      const { logoAssetId: _logoAssetId, ...next } = previous;
      return next;
    });
    setNotice(undefined);
  }

  async function upload(file: File | undefined) {
    if (!file || busy || disabled || !editor) return;
    setFailure(undefined);
    setNotice(undefined);
    if (!["image/png", "image/jpeg"].includes(file.type)) {
      setFailure("Choose a PNG or JPEG logo.");
      return;
    }
    if (file.size < 1 || file.size > 1_048_576) {
      setFailure("The logo must be a non-empty file no larger than 1 MiB.");
      return;
    }
    setBusy(true);
    try {
      edit({ logoAssetId: await api.uploadLogo(file) });
      setNotice("Logo uploaded. Save the template to use it for new invoices.");
    } catch {
      setFailure(
        "The logo could not be uploaded. Choose a valid PNG or JPEG and try again. Your template entries are still here.",
      );
    } finally {
      setBusy(false);
    }
  }

  async function save() {
    if (!editor || version === undefined || busy || disabled) return;
    setFailure(undefined);
    setNotice(undefined);
    if (!/^\d+$/.test(editor.paymentTermsDays) || Number(editor.paymentTermsDays) > 365) {
      setFailure("Payment terms must be a whole number from 0 to 365 days.");
      return;
    }
    setBusy(true);
    try {
      const saved = await api.saveTemplate(
        { ...editor, paymentTermsDays: Number(editor.paymentTermsDays) },
        version,
      );
      setEditor({ ...saved.defaults, paymentTermsDays: String(saved.defaults.paymentTermsDays) });
      setVersion(saved.version);
      onSaved(saved);
      setNotice("Company template saved.");
    } catch {
      setFailure(
        "The template could not be saved. Your entries are still here. Reload the latest template version and try again.",
      );
    } finally {
      setBusy(false);
    }
  }

  async function reloadVersion() {
    if (busy || disabled) return;
    setBusy(true);
    setFailure(undefined);
    try {
      const latest = await api.getTemplate();
      setVersion(latest.version);
      onSaved(latest);
      setNotice(
        "Latest version loaded. Your entries are unchanged; saving will replace those company defaults.",
      );
    } catch {
      setFailure(
        "The latest template version could not be loaded. Your entries are still here. Try again.",
      );
    } finally {
      setBusy(false);
    }
  }

  const labelClass = "grid min-w-0 gap-1.5 text-[12px] font-semibold text-foreground";
  return (
    <section
      aria-busy={loading || busy}
      aria-label="Company invoice template"
      className="work-surface grid gap-4 overflow-hidden pb-5"
    >
      <SurfaceHeading
        title="Company invoice template"
        description="Logo, payment details and terms for new invoices."
        action={
          <Button
            aria-controls="invoice-template-fields"
            aria-expanded={expanded}
            className="h-11 text-[13px]"
            disabled={disabled || busy}
            onClick={() => setExpanded((value) => !value)}
            type="button"
            variant="outline"
          >
            {expanded ? "Close template editor" : "Manage invoice template"}
          </Button>
        }
      />
      {loading ? (
        <p className="px-5 text-[13px] text-muted-foreground">Loading company template…</p>
      ) : null}
      {error ? (
        <div className="flex flex-wrap items-center gap-3">
          <p className="text-[13px] text-destructive" role="alert">
            {error}
          </p>
          <Button
            className="h-11 text-[13px]"
            disabled={loading || busy || disabled}
            onClick={onRetry}
            type="button"
            variant="outline"
          >
            Retry template
          </Button>
        </div>
      ) : null}
      {expanded ? (
        <div className="grid gap-4 px-5" id="invoice-template-fields">
          <fieldset className="grid gap-4" disabled={disabled || busy || !editor || loading}>
            <div className="grid grid-cols-[minmax(0,1fr)_180px] gap-4 max-[620px]:grid-cols-1">
              <label className={labelClass}>
                Template seller address
                <textarea
                  className={`${invoiceFieldClass} h-16 py-2`}
                  maxLength={1000}
                  onChange={(event) => edit({ sellerAddress: event.target.value })}
                  value={editor?.sellerAddress ?? ""}
                />
              </label>
              <div className="grid content-start gap-1">
                <label className={labelClass}>
                  Payment terms (days)
                  <input
                    aria-describedby="invoice-template-terms-help"
                    className={invoiceFieldClass}
                    inputMode="numeric"
                    maxLength={3}
                    onChange={(event) => edit({ paymentTermsDays: event.target.value })}
                    value={editor?.paymentTermsDays ?? ""}
                  />
                </label>
                <span
                  className="text-[12px] font-normal text-muted-foreground"
                  id="invoice-template-terms-help"
                >
                  0–365 days after the issue date
                </span>
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3 max-[620px]:grid-cols-1">
              <label className={labelClass}>
                Default notes
                <textarea
                  className={`${invoiceFieldClass} h-20 py-2`}
                  maxLength={2000}
                  onChange={(event) => edit({ notes: event.target.value })}
                  value={editor?.notes ?? ""}
                />
              </label>
              <label className={labelClass}>
                Default payment instructions
                <textarea
                  className={`${invoiceFieldClass} h-20 py-2`}
                  maxLength={1000}
                  onChange={(event) => edit({ paymentInstructions: event.target.value })}
                  value={editor?.paymentInstructions ?? ""}
                />
              </label>
            </div>
            <div className="grid gap-2">
              <label className={labelClass}>
                Company logo
                <input
                  accept="image/png,image/jpeg"
                  className="focus-ring min-h-11 w-full text-[13px] text-muted-foreground file:mr-3 file:rounded-xl file:border file:border-border file:bg-background file:px-3 file:py-2 file:text-[13px] file:text-foreground"
                  onChange={(event) => {
                    const file = event.target.files?.[0];
                    event.target.value = "";
                    void upload(file);
                  }}
                  type="file"
                />
              </label>
              <p className="text-[12px] leading-5 text-muted-foreground">
                PNG or JPEG · up to 1 MiB · maximum 4096 px per side and 4 million pixels.
              </p>
              {editor?.logoAssetId ? (
                <div className="flex flex-wrap items-center gap-4">
                  <InvoiceLogo api={api} id={editor.logoAssetId} label="Company logo preview" />
                  <Button
                    className="h-11 text-[13px]"
                    onClick={removeLogo}
                    type="button"
                    variant="outline"
                  >
                    Remove logo
                  </Button>
                </div>
              ) : (
                <p className="text-[12px] text-muted-foreground">No company logo selected.</p>
              )}
            </div>
          </fieldset>
          {failure ? (
            <p className="text-[13px] text-destructive" role="alert">
              {failure}
            </p>
          ) : null}
          {notice ? (
            <p className="text-[13px] text-muted-foreground" role="status">
              {notice}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2">
            <Button
              className="h-11 text-[13px]"
              disabled={disabled || busy || !editor || loading}
              onClick={save}
              type="button"
            >
              {busy ? "Working…" : "Save template"}
            </Button>
            <Button
              className="h-11 text-[13px]"
              disabled={disabled || busy || !template || loading || !canApply}
              onClick={onApply}
              type="button"
              variant="outline"
            >
              Apply saved template
            </Button>
            {failure ? (
              <Button
                className="h-11 text-[13px]"
                disabled={disabled || busy}
                onClick={reloadVersion}
                type="button"
                variant="outline"
              >
                Reload latest template version
              </Button>
            ) : null}
          </div>
          <p className="text-[12px] leading-5 text-muted-foreground">
            Saving updates company defaults. Apply replaces this draft’s logo, seller address,
            notes, payment instructions and due date. Saved invoices keep their own details.
          </p>
        </div>
      ) : null}
    </section>
  );
}
