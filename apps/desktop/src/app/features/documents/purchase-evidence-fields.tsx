import { create } from "@bufbuild/protobuf";
import {
  type Document,
  DocumentSchema,
  DocumentStatus,
} from "@tammy/connect-client/tammy/v1/documents_pb.js";
import {
  PurchaseEvidenceClassification,
  type PurchaseEvidenceInput,
  PurchaseEvidenceInputSchema,
  type PurchaseRetainedSourceRef,
  PurchaseTaxInvoiceExceptionKind,
} from "@tammy/connect-client/tammy/v1/purchases_pb.js";
import type { TammyConveyorModule } from "../../../conveyor/client";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";
import { civil, iso, PurchaseCheck, PurchaseChoice, PurchaseField } from "./purchase-fields";
import { RetainedDocumentPicker } from "./retained-document-picker";

type RetainedSourceRefInput = Pick<
  PurchaseRetainedSourceRef,
  "sourceId" | "sourceVersion" | "sourceHash"
>;
export interface EvidenceFields {
  readonly classification: PurchaseEvidenceClassification;
  readonly held: string;
  readonly reason: string;
  readonly confirmed: boolean;
  readonly exceptionConfirmed: boolean;
  readonly supportConfirmed: boolean;
  readonly differentSource: boolean;
  readonly source?: Document | undefined;
  readonly sourceRef?: RetainedSourceRefInput | undefined;
}
export function initialEvidence(): EvidenceFields {
  return {
    classification: PurchaseEvidenceClassification.UNKNOWN,
    held: "",
    reason: "",
    confirmed: false,
    exceptionConfirmed: false,
    supportConfirmed: false,
    differentSource: false,
  };
}
export function retainedRef(document: Document) {
  return { sourceId: document.id, sourceVersion: document.version, sourceHash: document.sha256 };
}
export function retainedDocumentFromRef(
  ref: RetainedSourceRefInput | undefined,
  organisationId: string,
): Document | undefined {
  return ref
    ? create(DocumentSchema, {
        id: ref.sourceId,
        organisationId,
        version: ref.sourceVersion,
        sha256: ref.sourceHash,
        status: DocumentStatus.REVIEWED,
        sourceDisplayName: `Retained reference ${ref.sourceId}`,
      })
    : undefined;
}
export function evidenceFieldsFromInput(
  input: PurchaseEvidenceInput | undefined,
  acquisition: Document,
): EvidenceFields {
  if (!input) return initialEvidence();
  const differentSource = Boolean(input.source && input.source.sourceId !== acquisition.id);
  const source = differentSource
    ? retainedDocumentFromRef(input.source, acquisition.organisationId)
    : undefined;
  return {
    classification: input.classification,
    held: iso(input.heldDate),
    reason: input.reason,
    confirmed:
      input.registrationReviewConfirmed &&
      input.businessUseReviewConfirmed &&
      input.evidenceReviewConfirmed,
    exceptionConfirmed: input.exceptionReview?.taxInvoiceNotRequiredReviewConfirmed ?? false,
    supportConfirmed: input.exceptionReview?.supportingRecordRequirementsReviewConfirmed ?? false,
    differentSource,
    source,
    sourceRef: input.source,
  };
}
const ruleHash = Uint8Array.from(
  "11cc08f62cb53e5e896e0af18ad2056f1bf0a572a4cc2b98c342de6ba7398157".match(/../g)!,
  (hex) => Number.parseInt(hex, 16),
);
export function reviewedEvidence(fields: EvidenceFields, acquisition: Document) {
  if (!fields.confirmed || !fields.reason.trim())
    throw new Error("Review registration, business use and evidence, and enter the review reason.");
  const known = fields.classification !== PurchaseEvidenceClassification.UNKNOWN;
  const held = civil(fields.held);
  const selectedSource = fields.differentSource ? fields.source : acquisition;
  const sourceRef = fields.differentSource
    ? fields.source
      ? retainedRef(fields.source)
      : fields.sourceRef
    : fields.sourceRef?.sourceId === acquisition.id
      ? fields.sourceRef
      : retainedRef(acquisition);
  if (fields.differentSource && !sourceRef)
    throw new Error("Select the reviewed evidence source before confirming this review.");
  if (
    known &&
    (!held || !sourceRef || (selectedSource && selectedSource.status !== DocumentStatus.REVIEWED))
  )
    throw new Error("Select reviewed evidence and its actual held date.");
  const retainedSource = sourceRef ?? retainedRef(acquisition);
  const exception = fields.classification === PurchaseEvidenceClassification.SUPPORTED_EXCEPTION;
  if (
    exception &&
    (!fields.exceptionConfirmed ||
      !fields.supportConfirmed ||
      (acquisition.candidate?.subtotal?.minorUnits ?? 7501n) > 7500n)
  )
    throw new Error(
      "The supported low-value exception requires review of the whole transaction and supporting record requirements.",
    );
  return create(PurchaseEvidenceInputSchema, {
    classification: fields.classification,
    ...(known ? { source: retainedSource, heldDate: held } : {}),
    registrationReviewConfirmed: true,
    businessUseReviewConfirmed: true,
    evidenceReviewConfirmed: true,
    reason: fields.reason.trim(),
    ...(exception
      ? {
          exceptionReview: {
            kind: PurchaseTaxInvoiceExceptionKind.LOW_VALUE_ACQUISITION,
            rule: {
              sourceId: "019a4210-0000-7000-8000-000000000075",
              sourceVersion: 1n,
              sourceHash: ruleHash,
            },
            supportingRecords:
              retainedSource.sourceId === acquisition.id
                ? [retainedSource]
                : [retainedRef(acquisition), retainedSource],
            taxInvoiceNotRequiredReviewConfirmed: true,
            supportingRecordRequirementsReviewConfirmed: true,
            reason: fields.reason.trim(),
          },
        }
      : {}),
  });
}
export function PurchaseEvidenceFields({
  api,
  workspace,
  acquisition,
  value,
  onChange,
}: {
  readonly api: Pick<TammyConveyorModule<"documents">, "listDocuments">;
  readonly workspace: AuthenticatedWorkspace;
  readonly acquisition: Document;
  readonly value: EvidenceFields;
  readonly onChange: (fields: EvidenceFields) => void;
}) {
  return (
    <div className="grid gap-3">
      <PurchaseChoice
        label="GST evidence classification"
        value={String(value.classification)}
        onChange={(classification) =>
          onChange({ ...value, classification: Number(classification) })
        }
      >
        <option value={PurchaseEvidenceClassification.UNKNOWN}>Unknown / missing evidence</option>
        <option value={PurchaseEvidenceClassification.TAX_INVOICE}>Reviewed tax invoice</option>
        <option value={PurchaseEvidenceClassification.SUPPORTED_EXCEPTION}>
          Supported low-value acquisition exception
        </option>
      </PurchaseChoice>
      {value.classification !== PurchaseEvidenceClassification.UNKNOWN ? (
        <>
          <PurchaseField
            label="Actual evidence held date"
            type="date"
            value={value.held}
            onChange={(held) => onChange({ ...value, held })}
          />
          <PurchaseCheck
            label="Use another reviewed retained document as evidence"
            checked={value.differentSource}
            onChange={(differentSource) =>
              onChange({
                ...value,
                differentSource,
                source: differentSource ? value.source : undefined,
                sourceRef:
                  differentSource && value.sourceRef?.sourceId !== acquisition.id
                    ? value.sourceRef
                    : undefined,
              })
            }
          />
          {value.differentSource ? (
            <RetainedDocumentPicker
              api={api}
              workspace={workspace}
              label="Evidence document"
              selected={value.source}
              onChange={(source) =>
                onChange({ ...value, source, sourceRef: source ? retainedRef(source) : undefined })
              }
            />
          ) : (
            <p className="text-[9px] text-muted-foreground">
              Evidence source: {acquisition.sourceDisplayName} · version {acquisition.version}
            </p>
          )}
        </>
      ) : null}
      {value.classification === PurchaseEvidenceClassification.SUPPORTED_EXCEPTION ? (
        <div className="grid gap-2 border-l-2 border-border pl-3">
          <p className="text-[10px]">
            The retained rule applies to the whole acquisition up to $75 excluding GST. Invoice
            splitting or a small allocation does not qualify by itself.
          </p>
          <PurchaseCheck
            label="Reviewed that a tax invoice is not required for this whole transaction"
            checked={value.exceptionConfirmed}
            onChange={(exceptionConfirmed) => onChange({ ...value, exceptionConfirmed })}
          />
          <PurchaseCheck
            label="Reviewed supplier ABN, items, date, price and GST in the retained supporting records"
            checked={value.supportConfirmed}
            onChange={(supportConfirmed) => onChange({ ...value, supportConfirmed })}
          />
        </div>
      ) : null}
      <PurchaseField
        label="Evidence review reason"
        value={value.reason}
        onChange={(reason) => onChange({ ...value, reason })}
      />
      <PurchaseCheck
        label="Registration, business use and evidence reviewed"
        checked={value.confirmed}
        onChange={(confirmed) => onChange({ ...value, confirmed })}
      />
    </div>
  );
}
