import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { App } from "./app";
import { deadlineLabel } from "./utils";

const json = (value: unknown, status = 200) =>
  new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
const expiresAt = new Date(Date.now() + 15 * 60_000).toISOString();
// Testing Library collapses whitespace, and some ICU versions put U+202F before AM or PM.
const deadline = deadlineLabel(expiresAt).replace(/\s+/g, " ");
const discarded = "This setup expired before it was confirmed. Start setup again.";

afterEach(() => vi.unstubAllGlobals());

it("shows the recovery code and waits for confirmation before entering the workspace", async () => {
  const calls: string[] = [];
  let confirmations = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      calls.push(url);
      if (url === "/api/session") return json({ state: "setup" });
      if (url === "/api/workspace/create")
        return json({ setupId: "setup-1", recoveryCode: "ABCD-EFGH-IJKL-MNOP", expiresAt });
      if (url === "/api/workspace/confirm") {
        confirmations += 1;
        if (confirmations === 1)
          return new Response(JSON.stringify({ error: { message: "Check the ABN." } }), {
            status: 400,
            headers: { "Content-Type": "application/json" },
          });
        return json({ state: "authenticated", organisation: { id: "1", name: "Example Books" } });
      }
      if (url === "/api/overview")
        return json({
          documentsNeedingReview: 0,
          documentsReviewedInPeriod: 0,
          bankingLinesNeedingReconciliation: 0,
          bankingLinesUnreconciledInPeriod: 0,
          basStatus: "not_created",
          currentDraftBasWorkpapers: 0,
          attentionItems: [],
        });
      if (url === "/api/journals") return json({ journals: [] });
      if (url === "/api/accounts") return json({ accounts: [] });
      throw new Error(`Unexpected request ${url}`);
    }),
  );
  const user = userEvent.setup();
  render(<App />);

  await screen.findByRole("heading", { name: "Create your local workspace" });
  await user.type(screen.getByLabelText("Your name"), "Alex Example");
  await user.type(screen.getByLabelText("Email or username"), "alex@example.com");
  await user.type(screen.getByLabelText("Business legal name"), "Example Books Pty Ltd");
  await user.type(screen.getByLabelText("Business display name"), "Example Books");
  await user.type(screen.getByLabelText("ABN"), "11111111111");
  await user.type(screen.getByLabelText("Workspace passphrase"), "workspace-passphrase-long");
  await user.type(screen.getByLabelText("Administrator password"), "administrator-password-long");
  await user.click(screen.getByRole("button", { name: "Create local workspace" }));

  expect(await screen.findByText("ABCD-EFGH-IJKL-MNOP")).toBeTruthy();
  expect(
    screen.getByText(
      `Confirm by ${deadline}. After that, or after 5 incorrect codes, this setup is discarded and you need to start again.`,
    ),
  ).toBeTruthy();
  expect(calls).not.toContain("/api/workspace/confirm");
  await user.type(screen.getByLabelText("Enter your saved recovery code"), "ABCD-EFGH-IJKL-MNOP");
  await user.click(screen.getByRole("button", { name: "Confirm recovery code" }));
  await screen.findByRole("alert");
  await user.clear(screen.getByLabelText("ABN"));
  await user.type(screen.getByLabelText("ABN"), "51824753556");
  await user.click(screen.getByRole("button", { name: "Confirm recovery code" }));
  await waitFor(() => expect(confirmations).toBe(2));
  expect(await screen.findByRole("heading", { name: "Overview" })).toBeTruthy();
});

it("returns to setup with the reason when a pending setup is discarded", async () => {
  let sessions = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url === "/api/session") {
        sessions += 1;
        return json(
          sessions === 1
            ? { state: "recovery", setupId: "setup-1", expiresAt }
            : { state: "setup" },
        );
      }
      if (url === "/api/workspace/confirm")
        return json({ error: { code: "conflict", message: discarded } }, 409);
      throw new Error(`Unexpected request ${url}`);
    }),
  );
  const user = userEvent.setup();
  render(<App />);

  await screen.findByRole("heading", { name: "Save your recovery code" });
  expect(screen.getByText(new RegExp(`Confirm by ${deadline}\\.`))).toBeTruthy();
  await user.type(screen.getByLabelText("Enter your saved recovery code"), "ABCD-EFGH-IJKL-MNOP");
  await user.type(screen.getByLabelText("Email or username"), "alex@example.com");
  await user.type(screen.getByLabelText("Administrator password"), "administrator-password-long");
  await user.type(screen.getByLabelText("Business legal name"), "Example Books Pty Ltd");
  await user.type(screen.getByLabelText("Business display name"), "Example Books");
  await user.type(screen.getByLabelText("ABN"), "51824753556");
  await user.click(screen.getByRole("button", { name: "Confirm recovery code" }));

  expect(await screen.findByRole("heading", { name: "Create your local workspace" })).toBeTruthy();
  expect(screen.getByRole("alert").textContent).toBe(discarded);
  expect(sessions).toBe(2);
});

it("returns to unlock when an authenticated session expires", async () => {
  let sessions = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url === "/api/session") {
        sessions += 1;
        return json(
          sessions === 1
            ? { state: "authenticated", organisation: { id: "1", name: "Example Books" } }
            : { state: "locked" },
        );
      }
      return new Response(JSON.stringify({ error: { message: "Unlock the workspace again." } }), {
        status: 401,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
  render(<App />);
  expect(await screen.findByRole("heading", { name: "Unlock your workspace" })).toBeTruthy();
});
