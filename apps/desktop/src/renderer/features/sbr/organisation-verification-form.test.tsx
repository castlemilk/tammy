import { create } from "@bufbuild/protobuf";
import { TimestampSchema } from "@bufbuild/protobuf/wkt";
import { Role } from "@tammy/connect-client/tammy/v1/identity_pb.js";
import {
  EntityVerificationSchema,
  OrganisationSchema,
  OrganisationVerificationState,
  RecordEntityVerificationRequestSchema,
  RecordEntityVerificationResponseSchema,
  VerificationSourceMethod,
} from "@tammy/connect-client/tammy/v1/organisation_pb.js";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode } from "react";
import { expect, it, vi } from "vitest";
import { createProtoMethodCodec } from "../../../shared/proto-ipc";
import { OrganisationVerificationForm } from "./organisation-verification-form";

const organisationId = "01900f3c-7b2e-7cc4-98c4-dc0c0c073994";
const workspace = {
  workspaceId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073991",
  userId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073992",
  sessionId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073993",
  organisationId,
  organisationDisplayName: "Wattle",
  organisationCanonicalAbn: "11000000560",
  organisationLegalName: "Wattle Pty Ltd",
  organisationEntityType: "company",
  organisationVersion: 1n,
  organisationVerificationState: OrganisationVerificationState.UNVERIFIED,
  roles: [Role.WORKSPACE_ADMIN],
};
const codec = createProtoMethodCodec({
  input: RecordEntityVerificationRequestSchema,
  maximumRequestBytes: Math.floor(1.1 * 1024 * 1024),
  maximumResponseBytes: 32_768,
  output: RecordEntityVerificationResponseSchema,
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve;
  });
  return { promise, resolve };
}

it("records bounded evidence bytes and renders core-authored expiry without displaying content", async () => {
  const recordEntityVerification = vi.fn((frame: Uint8Array) => {
    const request = codec.decodeRequest(frame);
    expect(request.sourceMethod).toBe(VerificationSourceMethod.ABR_EXTRACT_MANUAL);
    expect(request.evidence?.content.length).toBeGreaterThan(0);
    return Promise.resolve(
      codec.encodeResponse(
        create(RecordEntityVerificationResponseSchema, {
          organisation: create(OrganisationSchema, {
            id: organisationId,
            version: 2n,
            abn: workspace.organisationCanonicalAbn,
            legalName: workspace.organisationLegalName,
            displayName: workspace.organisationDisplayName,
            entityType: "company",
            verificationState: OrganisationVerificationState.VERIFIED,
          }),
          verification: create(EntityVerificationSchema, {
            id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
            organisationId,
            state: OrganisationVerificationState.VERIFIED,
            sourceMethod: VerificationSourceMethod.ABR_EXTRACT_MANUAL,
            verifiedLegalName: workspace.organisationLegalName,
            verifiedEntityType: "company",
            evidenceObjectId: "01900f3c-7b2e-7cc4-98c4-dc0c0c073996",
            expiresAt: create(TimestampSchema, { seconds: 1_800_000_000n }),
          }),
        }),
      ),
    );
  });
  const user = userEvent.setup();
  render(
    <StrictMode>
      <OrganisationVerificationForm
        api={{ recordEntityVerification }}
        onChanged={vi.fn()}
        workspace={workspace}
      />
    </StrictMode>,
  );
  const file = new File([new Uint8Array([1, 2, 3])], "evidence.pdf", { type: "application/pdf" });
  await user.upload(screen.getByLabelText("Independent evidence"), file);
  await user.click(screen.getByRole("button", { name: "Record verification" }));
  expect(recordEntityVerification).toHaveBeenCalledOnce();
  expect(await screen.findByText(/evidence expiry/i)).toBeTruthy();
  expect(document.body.textContent).not.toContain("1,2,3");
});

it("rejects oversized or unapproved evidence before invoking core", async () => {
  const recordEntityVerification = vi.fn();
  const user = userEvent.setup();
  render(
    <OrganisationVerificationForm
      api={{ recordEntityVerification }}
      onChanged={vi.fn()}
      workspace={workspace}
    />,
  );
  await user.upload(
    screen.getByLabelText("Independent evidence"),
    new File([new Uint8Array(1024 * 1024 + 1)], "private.pdf", { type: "application/pdf" }),
  );
  expect(await screen.findByText(/PDF, JPEG, or PNG/i)).toBeTruthy();
  expect(recordEntityVerification).not.toHaveBeenCalled();
  expect(document.body.textContent).not.toContain("private.pdf");
});

it("synchronously prevents duplicate verification submits", async () => {
  const recordEntityVerification = vi.fn(() => new Promise<Uint8Array>(() => undefined));
  const user = userEvent.setup();
  render(
    <OrganisationVerificationForm
      api={{ recordEntityVerification }}
      onChanged={vi.fn()}
      workspace={workspace}
    />,
  );
  await user.upload(
    screen.getByLabelText("Independent evidence"),
    new File([new Uint8Array([1])], "evidence.pdf", { type: "application/pdf" }),
  );
  await user.dblClick(screen.getByRole("button", { name: "Record verification" }));
  expect(recordEntityVerification).toHaveBeenCalledOnce();
});

it("fails closed for an unknown verification state returned by core", async () => {
  const recordEntityVerification = vi.fn(() =>
    Promise.resolve(
      codec.encodeResponse(
        create(RecordEntityVerificationResponseSchema, {
          organisation: create(OrganisationSchema, {
            id: organisationId,
            version: 2n,
            verificationState: 999 as OrganisationVerificationState,
          }),
          verification: create(EntityVerificationSchema, {
            id: "01900f3c-7b2e-7cc4-98c4-dc0c0c073995",
            organisationId,
            state: 999 as OrganisationVerificationState,
            expiresAt: create(TimestampSchema, { seconds: 1_800_000_000n }),
          }),
        }),
      ),
    ),
  );
  const user = userEvent.setup();
  render(
    <OrganisationVerificationForm
      api={{ recordEntityVerification }}
      onChanged={vi.fn()}
      workspace={workspace}
    />,
  );
  await user.upload(
    screen.getByLabelText("Independent evidence"),
    new File([new Uint8Array([1])], "evidence.pdf", { type: "application/pdf" }),
  );
  await user.click(screen.getByRole("button", { name: "Record verification" }));
  expect(
    await screen.findByText(/outcome is unknown.*refresh status before trying again/i),
  ).toBeTruthy();
});

it("does not dispatch verification after unmount during evidence read", async () => {
  const bytes = deferred<ArrayBuffer>();
  const recordEntityVerification = vi.fn();
  const file = new File([new Uint8Array([1])], "evidence.pdf", { type: "application/pdf" });
  vi.spyOn(file, "arrayBuffer").mockReturnValue(bytes.promise);
  const user = userEvent.setup();
  const view = render(
    <OrganisationVerificationForm
      api={{ recordEntityVerification }}
      onChanged={vi.fn()}
      workspace={workspace}
    />,
  );
  await user.upload(screen.getByLabelText("Independent evidence"), file);
  await user.click(screen.getByRole("button", { name: "Record verification" }));
  view.unmount();
  await act(async () => {
    bytes.resolve(new Uint8Array([1]).buffer);
    await bytes.promise;
  });
  expect(recordEntityVerification).not.toHaveBeenCalled();
});
