import { LockKeyhole } from "lucide-react";
import { type FormEvent, type ReactNode, useState } from "react";
import { ApiError, api } from "./api";
import type { Session } from "./types";
import { deadlineLabel, errorMessage } from "./utils";

type SetupFields = {
  displayName: string;
  username: string;
  businessLegalName: string;
  businessDisplayName: string;
  abn: string;
  workspacePassphrase: string;
  administratorPassword: string;
};
const emptyFields: SetupFields = {
  displayName: "",
  username: "",
  businessLegalName: "",
  businessDisplayName: "",
  abn: "",
  workspacePassphrase: "",
  administratorPassword: "",
};

type PendingSetup = { setupId: string; recoveryCode?: string; expiresAt?: string };

export function WorkspaceAccess({
  session,
  onSession,
}: {
  session: Session;
  onSession: (value: Session) => void;
}) {
  const [fields, setFields] = useState<SetupFields>(emptyFields);
  const [pending, setPending] = useState<PendingSetup | undefined>(
    session.state === "recovery" && session.setupId
      ? {
          setupId: session.setupId,
          ...(session.expiresAt ? { expiresAt: session.expiresAt } : {}),
        }
      : undefined,
  );
  const [savedCode, setSavedCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string>();
  const update = (name: keyof SetupFields, value: string) =>
    setFields((current) => ({ ...current, [name]: value }));

  async function createWorkspace(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      const result = await api.createWorkspace({
        displayName: fields.displayName,
        username: fields.username,
        workspacePassphrase: fields.workspacePassphrase,
        administratorPassword: fields.administratorPassword,
      });
      setPending(result);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  async function confirm(event: FormEvent) {
    event.preventDefault();
    if (!pending) return;
    setBusy(true);
    setError(undefined);
    try {
      const result = await api.confirmWorkspace({
        setupId: pending.setupId,
        recoveryCode: savedCode,
        username: fields.username,
        administratorPassword: fields.administratorPassword,
        businessLegalName: fields.businessLegalName,
        businessDisplayName: fields.businessDisplayName,
        abn: fields.abn,
        ...(fields.workspacePassphrase ? { workspacePassphrase: fields.workspacePassphrase } : {}),
      });
      onSession(result);
    } catch (cause) {
      setError(errorMessage(cause));
      // A conflict can mean the setup expired or was discarded after too many incorrect codes.
      // Read the current state so the setup form returns, keeping the message.
      if (cause instanceof ApiError && cause.status === 409) {
        const next = await api.session().catch(() => undefined);
        if (next) {
          if (next.state !== "recovery") {
            setPending(undefined);
            setSavedCode("");
          }
          onSession(next);
        }
      }
    } finally {
      setBusy(false);
    }
  }

  async function unlock(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError(undefined);
    try {
      onSession(
        await api.unlock({
          username: fields.username,
          workspacePassphrase: fields.workspacePassphrase,
          administratorPassword: fields.administratorPassword,
        }),
      );
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(false);
    }
  }

  const recovery = Boolean(pending) || session.state === "recovery";
  const locked = session.state === "locked";
  const deadline = pending?.expiresAt ? deadlineLabel(pending.expiresAt) : "";
  const deadlineNotice = deadline
    ? `Confirm by ${deadline}. After that, or after 5 incorrect codes, this setup is discarded and you need to start again.`
    : "";
  return (
    <main className="auth-page">
      <section className="auth-card">
        <header className="auth-heading">
          <span className="wordmark">Tammy</span>
          <h1>
            {recovery
              ? "Save your recovery code"
              : locked
                ? "Unlock your workspace"
                : "Create your local workspace"}
          </h1>
          <p>
            {recovery
              ? "Keep this code somewhere safe. It is shown only once and can help you recover your workspace."
              : locked
                ? "Your books are encrypted on this device. Enter the workspace passphrase and your administrator credentials to continue."
                : "Bookkeeping for Australian small businesses that stays on this device. Review source documents, match bank lines, post journals and prepare a draft GST workpaper, all encrypted locally."}
          </p>
        </header>
        {error ? (
          <p className="notice error" role="alert">
            {error}
          </p>
        ) : null}
        {recovery ? (
          <form className="form-stack grouped" onSubmit={confirm}>
            {pending?.recoveryCode ? (
              <>
                <div className="recovery-code">
                  <span>One-time recovery code</span>
                  <strong>{pending.recoveryCode}</strong>
                </div>
                {deadlineNotice ? <p className="notice">{deadlineNotice}</p> : null}
              </>
            ) : (
              <p className="notice">
                Enter the recovery code you saved when you created this workspace. {deadlineNotice}
              </p>
            )}
            <FieldGroup title="Recovery">
              <label>
                Enter your saved recovery code
                <input
                  autoComplete="off"
                  onChange={(event) => setSavedCode(event.target.value)}
                  required
                  value={savedCode}
                />
              </label>
              {!pending?.recoveryCode ? (
                <>
                  <label>
                    Email or username
                    <input
                      autoComplete="username"
                      onChange={(event) => update("username", event.target.value)}
                      required
                      value={fields.username}
                    />
                  </label>
                  <label>
                    Administrator password
                    <input
                      autoComplete="current-password"
                      onChange={(event) => update("administratorPassword", event.target.value)}
                      required
                      type="password"
                      value={fields.administratorPassword}
                    />
                  </label>
                  <label>
                    Workspace passphrase
                    <input
                      autoComplete="current-password"
                      onChange={(event) => update("workspacePassphrase", event.target.value)}
                      type="password"
                      value={fields.workspacePassphrase}
                    />
                  </label>
                </>
              ) : null}
            </FieldGroup>
            <FieldGroup title="Business">
              <BusinessFields fields={fields} update={update} />
            </FieldGroup>
            <button className="button primary block" disabled={busy || !pending} type="submit">
              {busy ? "Confirming…" : "Confirm recovery code"}
            </button>
          </form>
        ) : locked ? (
          <form className="form-stack" onSubmit={unlock}>
            <label>
              Workspace passphrase
              <input
                autoComplete="current-password"
                onChange={(event) => update("workspacePassphrase", event.target.value)}
                required
                type="password"
                value={fields.workspacePassphrase}
              />
            </label>
            <label>
              Email or username
              <input
                autoComplete="username"
                onChange={(event) => update("username", event.target.value)}
                required
                value={fields.username}
              />
            </label>
            <label>
              Administrator password
              <input
                autoComplete="current-password"
                onChange={(event) => update("administratorPassword", event.target.value)}
                required
                type="password"
                value={fields.administratorPassword}
              />
            </label>
            <button className="button primary block" disabled={busy} type="submit">
              <LockKeyhole aria-hidden="true" />
              {busy ? "Unlocking…" : "Unlock workspace"}
            </button>
          </form>
        ) : (
          <form className="form-stack grouped" onSubmit={createWorkspace}>
            <FieldGroup title="Your details">
              <label>
                Your name
                <input
                  autoComplete="name"
                  onChange={(event) => update("displayName", event.target.value)}
                  required
                  value={fields.displayName}
                />
              </label>
              <label>
                Email or username
                <input
                  autoComplete="username"
                  onChange={(event) => update("username", event.target.value)}
                  required
                  value={fields.username}
                />
              </label>
            </FieldGroup>
            <FieldGroup title="Business">
              <BusinessFields fields={fields} update={update} />
            </FieldGroup>
            <FieldGroup title="Credentials">
              <label>
                Workspace passphrase
                <input
                  aria-label="Workspace passphrase"
                  autoComplete="new-password"
                  maxLength={128}
                  minLength={16}
                  onChange={(event) => update("workspacePassphrase", event.target.value)}
                  required
                  type="password"
                  value={fields.workspacePassphrase}
                />
                <small>16 to 128 characters</small>
              </label>
              <label>
                Administrator password
                <input
                  aria-label="Administrator password"
                  autoComplete="new-password"
                  maxLength={128}
                  minLength={16}
                  onChange={(event) => update("administratorPassword", event.target.value)}
                  required
                  type="password"
                  value={fields.administratorPassword}
                />
                <small>16 to 128 characters</small>
              </label>
            </FieldGroup>
            <button className="button primary block" disabled={busy} type="submit">
              {busy ? "Creating workspace…" : "Create local workspace"}
            </button>
          </form>
        )}
      </section>
    </main>
  );
}

function FieldGroup({ title, children }: { title: string; children: ReactNode }) {
  return (
    <fieldset className="field-group">
      <legend>{title}</legend>
      <div className="field-group-fields">{children}</div>
    </fieldset>
  );
}

function BusinessFields({
  fields,
  update,
}: {
  fields: SetupFields;
  update: (name: keyof SetupFields, value: string) => void;
}) {
  return (
    <>
      <label>
        Business legal name
        <input
          onChange={(event) => update("businessLegalName", event.target.value)}
          required
          value={fields.businessLegalName}
        />
      </label>
      <label>
        Business display name
        <input
          onChange={(event) => update("businessDisplayName", event.target.value)}
          required
          value={fields.businessDisplayName}
        />
      </label>
      <label>
        ABN
        <input
          aria-label="ABN"
          inputMode="numeric"
          onChange={(event) => update("abn", event.target.value)}
          pattern="[0-9]{11}"
          required
          value={fields.abn}
        />
        <small>11 digits, no spaces</small>
      </label>
    </>
  );
}
