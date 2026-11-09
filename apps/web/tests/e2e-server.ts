import { mkdtemp, rm } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { startApp } from "../server/main";

const dataRoot = await mkdtemp(path.join(os.tmpdir(), "tammy-browser-e2e-"));
try {
  const app = await startApp({ dataRoot, port: 4176, origin: "http://127.0.0.1:5174" });
  let stopping = false;
  async function stop() {
    if (stopping) return;
    stopping = true;
    await app.close();
    await rm(dataRoot, { recursive: true, force: true });
    process.exit(0);
  }
  process.on("SIGTERM", () => void stop());
  process.on("SIGINT", () => void stop());
} catch (error) {
  await rm(dataRoot, { recursive: true, force: true });
  throw error;
}
