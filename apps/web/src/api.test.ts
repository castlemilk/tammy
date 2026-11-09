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
});
