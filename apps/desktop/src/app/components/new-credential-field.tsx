import { Eye, EyeOff } from "lucide-react";
import { useId, useState } from "react";

import {
  getNewCredentialFeedback,
  newCredentialValuesMatch,
} from "../../shared/new-credential-policy";

function fieldClassName(): string {
  return "focus-ring h-11 w-full rounded-xl border border-border bg-surface px-3 pr-11 text-[14px] text-foreground outline-none placeholder:text-muted-foreground";
}

function InputWithVisibility({
  autoComplete,
  describedBy,
  id,
  invalid,
  label,
  onChange,
  value,
}: {
  readonly autoComplete: string;
  readonly describedBy: string;
  readonly id: string;
  readonly invalid: boolean;
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly value: string;
}) {
  const [visible, setVisible] = useState(false);
  return (
    <div className="grid gap-1.5 text-[12px] font-medium text-foreground">
      <label htmlFor={id}>{label}</label>
      <div className="relative">
        <input
          autoComplete={autoComplete}
          className={fieldClassName()}
          id={id}
          aria-invalid={invalid}
          onChange={(event) => onChange(event.target.value)}
          required
          spellCheck={false}
          type={visible ? "text" : "password"}
          value={value}
          aria-describedby={describedBy}
        />
        <button
          aria-label={`${visible ? "Hide" : "Show"} ${label.toLowerCase()}`}
          aria-pressed={visible}
          className="focus-ring absolute right-2 top-1/2 grid size-8 -translate-y-1/2 place-items-center rounded-lg text-muted-foreground hover:bg-muted hover:text-foreground"
          onClick={() => setVisible((current) => !current)}
          type="button"
        >
          {visible ? (
            <EyeOff aria-hidden="true" className="size-4" />
          ) : (
            <Eye aria-hidden="true" className="size-4" />
          )}
        </button>
      </div>
    </div>
  );
}

export function NewCredentialField({
  confirmation,
  confirmationLabel,
  label,
  onChange,
  onConfirmationChange,
  value,
}: {
  readonly confirmation: string;
  readonly confirmationLabel: string;
  readonly label: string;
  readonly onChange: (value: string) => void;
  readonly onConfirmationChange: (value: string) => void;
  readonly value: string;
}) {
  const id = useId();
  const feedbackId = `${id}-feedback`;
  const confirmationInputId = `${id}-confirmation`;
  const confirmationFeedbackId = `${id}-confirmation-feedback`;
  const feedback = getNewCredentialFeedback(value);
  const confirmationMatches = newCredentialValuesMatch(value, confirmation);
  const summary =
    value.length === 0
      ? "Choose a new password."
      : !feedback.wellFormedUnicode
        ? "Use regular text characters."
        : !feedback.withinCodePointRange
          ? "Use 15–128 characters."
          : !feedback.withinUtf8Limit
            ? "Use fewer or simpler characters."
            : !feedback.printableUnicode
              ? "No tabs, line breaks, or unusual spacing."
              : "Format checks pass. Tammy checks this again when you create the workspace.";

  return (
    <div className="grid gap-3">
      <InputWithVisibility
        autoComplete="new-password"
        describedBy={feedbackId}
        id={`${id}-value`}
        invalid={value.length > 0 && !feedback.valid}
        label={label}
        onChange={onChange}
        value={value}
      />
      <div className="grid gap-1.5">
        <InputWithVisibility
          autoComplete="new-password"
          describedBy={confirmationFeedbackId}
          id={confirmationInputId}
          invalid={confirmation.length > 0 && !confirmationMatches}
          label={confirmationLabel}
          onChange={onConfirmationChange}
          value={confirmation}
        />
        <p
          className="min-h-5 text-[12px] font-normal leading-5 text-muted-foreground"
          id={confirmationFeedbackId}
          role="status"
          aria-live="polite"
        >
          {confirmation.length === 0
            ? "Enter the same password again."
            : confirmationMatches
              ? "Passwords match."
              : "Passwords do not match yet."}
        </p>
      </div>
      <div className="rounded-xl border border-border bg-muted/30 px-3 py-2.5">
        <p
          className="min-h-10 text-[12px] leading-5 text-foreground"
          id={feedbackId}
          role="status"
          aria-live="polite"
          aria-atomic="true"
        >
          {summary}
        </p>
        <p className="mt-2 text-[12px] leading-5 text-muted-foreground">
          Character count: {feedback.codePointCount} / 128 (minimum 15)
        </p>
        <p className="mt-2 text-[12px] leading-5 text-muted-foreground">
          Format checks only. There is no password-strength score.
        </p>
      </div>
    </div>
  );
}
