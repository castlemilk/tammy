import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";

import { NewCredentialField } from "./new-credential-field";

function renderField(value = "", confirmation = "") {
  return render(
    <NewCredentialField
      confirmation={confirmation}
      confirmationLabel="Confirm workspace passphrase"
      label="Workspace passphrase"
      onChange={() => undefined}
      onConfirmationChange={() => undefined}
      value={value}
    />,
  );
}

it("starts masked and provides independent keyboard-accessible show controls", async () => {
  renderField("private passphrase value", "private passphrase value");
  const primary = screen.getByLabelText("Workspace passphrase");
  const confirmation = screen.getByLabelText("Confirm workspace passphrase");
  expect(primary.getAttribute("type")).toBe("password");
  expect(confirmation.getAttribute("type")).toBe("password");
  expect(confirmation.getAttribute("aria-describedby")).toBe(
    screen.getByText("Passwords match.").getAttribute("id"),
  );
  expect(document.querySelectorAll(`[id="${confirmation.id}"]`)).toHaveLength(1);

  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Show workspace passphrase" }));
  expect(primary.getAttribute("type")).toBe("text");
  expect(confirmation.getAttribute("type")).toBe("password");
  await user.click(screen.getByRole("button", { name: "Hide workspace passphrase" }));
  expect(primary.getAttribute("type")).toBe("password");

  await user.click(screen.getByRole("button", { name: "Show confirm workspace passphrase" }));
  expect(confirmation.getAttribute("type")).toBe("text");
  expect(primary.getAttribute("type")).toBe("password");
  await user.click(screen.getByRole("button", { name: "Hide confirm workspace passphrase" }));
  expect(confirmation.getAttribute("type")).toBe("password");
});

it("gives format-only feedback and never echoes the entered secret", () => {
  renderField("correct horse battery staple", "different secret");
  expect(screen.getByText(/Format checks pass/)).toBeTruthy();
  expect(screen.getByText("Passwords do not match yet.")).toBeTruthy();
  expect(screen.queryByText("correct horse battery staple")).toBeNull();
  expect(screen.getByText("Character count: 28 / 128 (minimum 15)")).toBeTruthy();
  expect(screen.queryByText(/NFC|Unicode|UTF-8|control|format characters/i)).toBeNull();
});

it("explains rejected spacing without exposing the entered value", () => {
  const value = `${"a".repeat(14)}\u00a0`;
  renderField(value, "different value");
  expect(screen.getByText("No tabs, line breaks, or unusual spacing.")).toBeTruthy();
  expect(screen.queryByText(value)).toBeNull();
});

it("confirms canonically equivalent Unicode secrets without trimming", () => {
  renderField("é".repeat(15), "e\u0301".repeat(15));
  expect(screen.getByText("Passwords match.")).toBeTruthy();
});
