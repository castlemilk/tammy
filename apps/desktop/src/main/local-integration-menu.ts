import type { MessageBoxOptions } from "electron";
import type { DesktopLocalIntegration } from "./local-integration";

interface IntegrationMenuItem {
  readonly id: string;
  readonly label: string;
  readonly click: () => Promise<void>;
}

export function createLocalIntegrationMenuItems(
  integration: DesktopLocalIntegration,
  native: {
    readonly showMessageBox: (options: MessageBoxOptions) => Promise<{ readonly response: number }>;
    readonly showItemInFolder: (filePath: string) => void;
  },
): readonly IntegrationMenuItem[] {
  const failure = async (error: unknown) => {
    const needsLogin = error instanceof Error && error.message === "LOCAL_SESSION_REQUIRED";
    await native.showMessageBox({
      type: "info",
      buttons: ["OK"],
      message: needsLogin ? "Sign in to Tammy first" : "Local integration could not be enabled",
      detail: needsLogin
        ? "Open your workspace, sign in, and complete organisation setup before enabling local API & MCP access."
        : "Try pairing again after signing in to Tammy.",
    });
  };
  return [
    {
      id: "tammy-integrations-pair",
      label: "Enable local API & MCP…",
      click: async () => {
        const { response } = await native.showMessageBox({
          type: "question",
          message: "Enable local API & MCP access?",
          detail:
            "Clients on this computer with the private connection file can read your current organisation's accounting data and stage editable invoice proposals. You must review and save proposals in Tammy. Clients cannot save, issue, post or send invoices. Access ends when you revoke it, sign in again, or close Tammy.",
          buttons: ["Enable access", "Cancel"],
          defaultId: 1,
          cancelId: 1,
        });
        if (response !== 0) return;
        try {
          await integration.enable();
          await native.showMessageBox({
            type: "info",
            buttons: ["OK"],
            message: "Local API & MCP enabled",
            detail: `Use this private connection file in your client:\n${integration.connectionPath}`,
          });
        } catch (error) {
          await failure(error);
        }
      },
    },
    {
      id: "tammy-integrations-revoke",
      label: "Revoke local API & MCP access",
      click: async () => {
        try {
          await integration.revoke();
        } catch (error) {
          await failure(error);
        }
      },
    },
    {
      id: "tammy-integrations-show-connection",
      label: "Show local connection file",
      click: async () => native.showItemInFolder(integration.connectionPath),
    },
  ];
}
