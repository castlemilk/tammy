import { create } from "@bufbuild/protobuf";
import {
  GetReportingCapabilityRequestSchema,
  GetReportingCapabilityResponseSchema,
  ReportingCapabilityStatus,
  type ReportingEntityType,
  type ReportKind,
} from "@tammy/connect-client/tammy/v1/reporting_capability_pb.js";
import { useEffect, useMemo } from "react";

import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";

const codec = createProtoMethodCodec({
  input: GetReportingCapabilityRequestSchema,
  maximumRequestBytes: 8_192,
  maximumResponseBytes: 32_768,
  output: GetReportingCapabilityResponseSchema,
});

const unavailableCopy = "Reporting support is unavailable in this build.";

interface ReportingCapabilityNoticeProps {
  readonly api: Pick<TammyConveyorModule<"reporting">, "getReportingCapability">;
  readonly entityType: ReportingEntityType;
  readonly fallbackCopy?: string;
  readonly onStatusChange?: (
    taxYear: number,
    status: ReportingCapabilityStatus | undefined,
  ) => void;
  readonly report: ReportKind;
  readonly taxYear: number;
}

interface CapabilityCopy {
  readonly status: string;
  readonly summary: string;
}

export function ReportingCapabilityNotice({
  api,
  entityType,
  fallbackCopy,
  onStatusChange,
  report,
  taxYear,
}: ReportingCapabilityNoticeProps) {
  const input = useMemo(
    () =>
      codec.encodeRequest(
        create(GetReportingCapabilityRequestSchema, { entityType, report, taxYear }),
      ),
    [entityType, report, taxYear],
  );
  const capabilityQuery = api.getReportingCapability.useQuery({ input, retry: false });
  const result = useMemo<
    | { readonly copy: CapabilityCopy; readonly status: ReportingCapabilityStatus }
    | { readonly failed: true }
    | undefined
  >(() => {
    if (capabilityQuery.isError) return { failed: true };
    if (!capabilityQuery.data) return undefined;
    try {
      const capability = codec.decodeResponse(capabilityQuery.data).capability;
      if (
        !capability ||
        capability.report !== report ||
        capability.entityType !== entityType ||
        capability.taxYear !== taxYear ||
        capability.status === ReportingCapabilityStatus.UNSPECIFIED
      ) {
        return { failed: true };
      }
      return {
        copy: { status: statusCopy(capability.status), summary: capability.summary },
        status: capability.status,
      };
    } catch {
      return { failed: true };
    }
  }, [capabilityQuery.data, capabilityQuery.isError, entityType, report, taxYear]);
  const failed = result !== undefined && "failed" in result;
  const copy = result !== undefined && "copy" in result ? result.copy : undefined;

  useEffect(() => {
    onStatusChange?.(
      taxYear,
      result !== undefined && "status" in result ? result.status : undefined,
    );
  }, [onStatusChange, result, taxYear]);

  return (
    <section className="rounded-[6px] border border-border bg-background px-3 py-3 text-[10px]">
      <div aria-live="polite" role="status">
        {failed ? (
          <p className="font-semibold text-warning">{unavailableCopy}</p>
        ) : copy ? (
          <>
            <p className="font-semibold text-foreground">{copy.status}</p>
            <p className="mt-1 leading-5 text-muted-foreground">{copy.summary}</p>
          </>
        ) : (
          <p className="text-muted-foreground">Checking reporting support…</p>
        )}
        {fallbackCopy ? (
          <p className="mt-1 leading-5 text-muted-foreground">{fallbackCopy}</p>
        ) : null}
      </div>
    </section>
  );
}

function statusCopy(status: ReportingCapabilityStatus): string {
  switch (status) {
    case ReportingCapabilityStatus.AVAILABLE:
      return "Available in this build";
    case ReportingCapabilityStatus.PREPARATION_ONLY:
      return "Preparation only in this build";
    case ReportingCapabilityStatus.HANDOFF_ONLY:
      return "Handoff only in this build";
    case ReportingCapabilityStatus.DIRECT_LODGEMENT:
      return "Direct lodgement available in this build";
    case ReportingCapabilityStatus.UNSUPPORTED:
      return "Unsupported in this build";
    default:
      return unavailableCopy;
  }
}
