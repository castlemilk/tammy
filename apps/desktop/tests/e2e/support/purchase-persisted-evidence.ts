import { execFile, spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";
import {
  type AuthenticatedAccountingWorkspace,
  CURRENT_WORKFLOW_PASSPHRASE,
} from "./current-accounting-workflow";

const root = fileURLToPath(new URL("../../../../../", import.meta.url));
export async function buildPurchaseEvidenceInspector() {
  await promisify(execFile)(
    "rtk",
    [
      "proxy",
      "mise",
      "exec",
      "--",
      "node",
      path.join(root, "scripts/build-purchase-e2e-inspector.mjs"),
    ],
    { cwd: root, maxBuffer: 1024 * 1024 },
  );
}
export interface PurchasePersistedEvidence {
  readonly workspaceId: string;
  readonly organisationId: string;
  readonly sbrSimulatorTransportCount: number;
  readonly facts: readonly {
    readonly id: string;
    readonly journalId: string;
    readonly journalLineId: string;
    readonly paymentId: string;
    readonly paymentDate: string;
    readonly category: string;
    readonly sequence: number;
    readonly amountMinor: string;
    readonly sourceRevision: string;
  }[];
  readonly projection: Readonly<Record<string, string>>;
}
export async function inspectPurchasePersistedEvidence(
  userDataPath: string,
  workspace: AuthenticatedAccountingWorkspace,
): Promise<PurchasePersistedEvidence> {
  const executable = path.join(
    root,
    ".tmp/purchase-e2e-inspector",
    process.platform === "win32" ? "inspect.exe" : "inspect",
  );
  return new Promise((resolve, reject) => {
    const child = spawn(executable, [], { cwd: root, stdio: ["pipe", "pipe", "pipe"] });
    let stdout = Buffer.alloc(0);
    let stderr = Buffer.alloc(0);
    let failed = false;
    const timer = setTimeout(() => {
      failed = true;
      child.kill();
    }, 35_000);
    child.stdout.on("data", (bytes: Buffer) => {
      if (stdout.length + bytes.length > 1024 * 1024) {
        failed = true;
        child.kill();
        return;
      }
      stdout = Buffer.concat([stdout, bytes]);
    });
    child.stderr.on("data", (bytes: Buffer) => {
      if (stderr.length + bytes.length > 512) {
        failed = true;
        child.kill();
        return;
      }
      stderr = Buffer.concat([stderr, bytes]);
    });
    child.once("error", () => {
      clearTimeout(timer);
      reject(new Error("PURCHASE_PACKAGED_INSPECTOR_UNAVAILABLE"));
    });
    child.once("close", (code) => {
      clearTimeout(timer);
      if (failed || code !== 0) {
        const diagnostic = stderr.toString("utf8").trim();
        reject(
          new Error(
            /^PURCHASE_PACKAGED_INSPECTION_FAILED_(INPUT|INSTALLATION_KEY|HEADER|UNWRAP|DATABASE|ORGANISATION|BANK_FACTS|PROJECTION|SBR_TRANSPORTS|OUTPUT)$/.test(
              diagnostic,
            )
              ? diagnostic
              : "PURCHASE_PACKAGED_INSPECTION_FAILED",
          ),
        );
        return;
      }
      try {
        const result = JSON.parse(stdout.toString("utf8")) as PurchasePersistedEvidence;
        if (
          result.workspaceId !== workspace.workspaceId ||
          result.organisationId !== workspace.organisationId ||
          !Number.isSafeInteger(result.sbrSimulatorTransportCount) ||
          result.sbrSimulatorTransportCount < 0 ||
          result.sbrSimulatorTransportCount > 10_000 ||
          !Array.isArray(result.facts) ||
          !result.projection
        )
          throw new Error();
        resolve(result);
      } catch {
        reject(new Error("PURCHASE_PACKAGED_INSPECTION_INVALID"));
      }
    });
    child.stdin.on("error", () => {
      failed = true;
      child.kill();
    });
    child.stdin.end(
      JSON.stringify({
        userDataPath,
        workspaceId: workspace.workspaceId,
        organisationId: workspace.organisationId,
        passphrase: CURRENT_WORKFLOW_PASSPHRASE,
      }),
    );
  });
}
