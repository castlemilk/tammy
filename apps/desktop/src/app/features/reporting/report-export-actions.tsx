import { create } from "@bufbuild/protobuf";
import {
  GetOrganisationRequestSchema,
  GetOrganisationResponseSchema,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import type { ReportOrganisation } from "@tammy/reporting/model";
import { useEffect, useMemo, useRef, useState } from "react";
import type { TammyConveyorModule } from "../../../conveyor/client";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { type ReportFormat, reportExportMessage } from "../../../shared/report-export";
import { Button } from "../../components/ui/button";
import type { AuthenticatedWorkspace } from "../setup/setup-screen";

const codec = createProtoMethodCodec({
  input: GetOrganisationRequestSchema,
  output: GetOrganisationResponseSchema,
  maximumRequestBytes: 8192,
  maximumResponseBytes: 32768,
});
export type ReportOrganisationApi = Pick<TammyConveyorModule<"organisation">, "getOrganisation">;
export function ReportExportActions({
  organisationApi,
  workspace,
  label,
  disabled,
  execute,
  validateReport,
}: {
  readonly organisationApi: ReportOrganisationApi;
  readonly workspace: AuthenticatedWorkspace | undefined;
  readonly label: "Income" | "BAS";
  readonly disabled: boolean;
  readonly validateReport?: (organisation: ReportOrganisation) => void;
  readonly execute: (
    format: ReportFormat,
    organisationVersion: string,
  ) => Promise<{ readonly saved: boolean }>;
}) {
  const organisationId = workspace?.organisationId ?? "",
    actorUserId = workspace?.userId ?? "",
    sessionId = workspace?.sessionId ?? "";
  const authority = `${organisationId}:${actorUserId}:${sessionId}`;
  const authorityRef = useRef(authority);
  authorityRef.current = authority;
  const input = useMemo(
    () =>
      codec.encodeRequest(
        create(GetOrganisationRequestSchema, {
          authentication: { actorUserId, sessionId },
          organisationId,
        }),
      ),
    [organisationId, actorUserId, sessionId],
  );
  const profile = organisationApi.getOrganisation.useQuery({
    input,
    enabled: Boolean(organisationId),
    retry: false,
    refetchOnMount: "always",
  });
  const organisation = useMemo(() => {
    if (!profile.data || profile.isError) return undefined;
    try {
      const value = codec.decodeResponse(profile.data).organisation;
      return value?.id === organisationId &&
        value.version > 0n &&
        value.legalName.trim() &&
        [...value.legalName].length <= 256 &&
        /^[0-9]{11}$/.test(value.abn)
        ? value
        : undefined;
    } catch {
      return undefined;
    }
  }, [organisationId, profile.data, profile.isError]);
  const validReport = useMemo(() => {
    if (!organisation) return false;
    try {
      validateReport?.(organisation);
      return true;
    } catch {
      return false;
    }
  }, [organisation, validateReport]);
  const [pending, setPending] = useState(false),
    [message, setMessage] = useState<string>(),
    [error, setError] = useState<string>();
  // biome-ignore lint/correctness/useExhaustiveDependencies: A new authority must discard all previous export feedback.
  useEffect(() => {
    setPending(false);
    setMessage(undefined);
    setError(undefined);
  }, [authority]);
  const save = async (format: ReportFormat) => {
    if (!organisation || !validReport || disabled || pending || profile.isFetching) return;
    const captured = authority;
    setPending(true);
    setMessage(undefined);
    setError(undefined);
    try {
      const result = await execute(format, String(organisation.version));
      if (authorityRef.current === captured)
        setMessage(
          result.saved
            ? `${label} ${format.toUpperCase()} exported.`
            : `${label} export cancelled.`,
        );
    } catch (cause) {
      if (authorityRef.current === captured) setError(reportExportMessage(cause));
    } finally {
      if (authorityRef.current === captured) setPending(false);
    }
  };
  return (
    <fieldset
      className="grid min-w-0 gap-2 border-t border-border p-3"
      aria-label={`${label} export`}
    >
      <div className="flex flex-wrap items-center gap-2">
        <Button
          className="h-7 text-[10px]"
          type="button"
          variant="outline"
          disabled={disabled || pending || profile.isFetching || !organisation || !validReport}
          onClick={() => void save("pdf")}
        >
          Export PDF
        </Button>
        <Button
          className="h-7 text-[10px]"
          type="button"
          variant="outline"
          disabled={disabled || pending || profile.isFetching || !organisation || !validReport}
          onClick={() => void save("csv")}
        >
          Export CSV
        </Button>
        <Button
          className="h-7 text-[10px]"
          type="button"
          variant="ghost"
          disabled={pending || profile.isFetching || !organisationId}
          onClick={() => void organisationApi.getOrganisation.invalidate(input)}
        >
          Refresh export identity
        </Button>
        <p className="text-[9px] text-muted-foreground">
          {organisation
            ? `${organisation.legalName} · ABN ${organisation.abn}`
            : profile.isFetching
              ? "Loading organisation details…"
              : "Refresh organisation details to export."}
        </p>
      </div>
      {pending ? (
        <p className="text-[10px]" role="status">
          Preparing {label.toLowerCase()} export…
        </p>
      ) : null}
      {message ? (
        <p className="text-[10px]" role="status">
          {message}
        </p>
      ) : null}
      {error ? (
        <p className="text-[10px] text-destructive" role="alert">
          {error}
        </p>
      ) : null}
    </fieldset>
  );
}
