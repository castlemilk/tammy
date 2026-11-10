import { afterEach, describe, expect, it, vi } from "vitest";

import { ApiError, api } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("the local gateway client", () => {
  it("loads the actual session response using same-origin credentials", async () => {
    const fetch = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({ state: "authenticated", organisation: { id: "1", name: "Example" } }),
        {
          status: 200,
          headers: { "Content-Type": "application/json" },
        },
      ),
    );
    vi.stubGlobal("fetch", fetch);

    await expect(api.session()).resolves.toEqual({
      state: "authenticated",
      organisation: { id: "1", name: "Example" },
    });
    expect(fetch).toHaveBeenCalledWith(
      "/api/session",
      expect.objectContaining({ credentials: "same-origin" }),
    );
  });

  it("shows a useful gateway error without treating a failed mutation as successful", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: { message: "The journal is not balanced." } }), {
          status: 422,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    );

    await expect(
      api.postJournal({
        idempotencyKey: "test",
        date: "2024-05-12",
        description: "Entry",
        lines: [],
      }),
    ).rejects.toEqual(new ApiError("The journal is not balanced.", 422));
  });

  it("starts a new browser session once when the gateway lost it during unlock", async () => {
    const reply = (status: number, value: unknown) =>
      new Response(JSON.stringify(value), {
        status,
        headers: { "Content-Type": "application/json" },
      });
    const unlocked = { state: "authenticated", organisation: { id: "1", name: "Example" } };
    const fetch = vi
      .fn()
      .mockResolvedValueOnce(
        reply(401, {
          error: { code: "unauthenticated", message: "Start a browser session first." },
        }),
      )
      .mockResolvedValueOnce(reply(200, { state: "locked" }))
      .mockResolvedValueOnce(reply(200, unlocked));
    vi.stubGlobal("fetch", fetch);
    const credentials = {
      username: "alex@example.com",
      workspacePassphrase: "workspace-passphrase-long",
      administratorPassword: "administrator-password-long",
    };

    await expect(api.unlock(credentials)).resolves.toEqual(unlocked);
    expect(fetch.mock.calls.map(([url]) => url)).toEqual([
      "/api/workspace/unlock",
      "/api/session",
      "/api/workspace/unlock",
    ]);
  });

  it("reports wrong credentials without renewing the session or retrying", async () => {
    const message = "The workspace passphrase, username or administrator password is incorrect.";
    const fetch = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "unauthenticated", message } }), {
        status: 401,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetch);

    await expect(
      api.unlock({ username: "a", workspacePassphrase: "b", administratorPassword: "c" }),
    ).rejects.toEqual(new ApiError(message, 401));
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});
