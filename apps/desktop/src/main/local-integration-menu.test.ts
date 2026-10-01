import { describe, expect, it, vi } from "vitest";
import { createLocalIntegrationMenuItems } from "./local-integration-menu";

function fixture(response = 0) {
  const integration = {
    connectionPath: "/private/app/local-integrations/connection.json",
    enable: vi.fn(async () => undefined),
    revoke: vi.fn(async () => undefined),
  };
  const showMessageBox = vi.fn(async () => ({ response }));
  const showItemInFolder = vi.fn();
  const items = createLocalIntegrationMenuItems(integration, { showMessageBox, showItemInFolder });
  const click = async (id: string) => {
    const action = items.find((item) => item.id === id)?.click;
    if (!action) throw new Error("MENU_ITEM_MISSING");
    await action();
  };
  return { click, integration, showMessageBox, showItemInFolder };
}

describe("native local integration menu", () => {
  it("pairs only after the native read access prompt is accepted", async () => {
    const { click, integration, showMessageBox } = fixture();
    await click("tammy-integrations-pair");
    expect(showMessageBox).toHaveBeenCalledWith(
      expect.objectContaining({
        buttons: ["Enable access", "Cancel"],
        defaultId: 1,
        cancelId: 1,
      }),
    );
    expect(integration.enable).toHaveBeenCalledOnce();
    expect(showMessageBox).toHaveBeenLastCalledWith(
      expect.objectContaining({ message: "Local API & MCP enabled" }),
    );
  });

  it("keeps local access disabled when pairing is cancelled", async () => {
    const { click, integration } = fixture(1);
    await click("tammy-integrations-pair");
    expect(integration.enable).not.toHaveBeenCalled();
  });

  it("revokes access and opens the connection location from explicit menu actions", async () => {
    const { click, integration, showItemInFolder } = fixture();
    await click("tammy-integrations-revoke");
    expect(integration.revoke).toHaveBeenCalledOnce();
    await click("tammy-integrations-show-connection");
    expect(showItemInFolder).toHaveBeenCalledWith(integration.connectionPath);
  });

  it("explains missing app login without reflecting a secret-bearing error", async () => {
    const { click, integration, showMessageBox } = fixture();
    integration.enable.mockRejectedValue(new Error("LOCAL_SESSION_REQUIRED"));
    await click("tammy-integrations-pair");
    expect(showMessageBox).toHaveBeenLastCalledWith(
      expect.objectContaining({ message: "Sign in to Tammy first" }),
    );
    integration.enable.mockRejectedValue(new Error("sensitive password and database path"));
    await click("tammy-integrations-pair");
    expect(JSON.stringify(showMessageBox.mock.calls)).not.toContain("sensitive password");
  });
});
