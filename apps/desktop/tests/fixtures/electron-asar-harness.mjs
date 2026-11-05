import { writeSync } from "node:fs";
import { realpath } from "node:fs/promises";
import { performance } from "node:perf_hooks";
import { pathToFileURL } from "node:url";

import { app, net, protocol } from "electron";

const phaseStart = performance.now();
try {
  writeSync(
    2,
    `TAMMY_ASAR_RUNTIME ${process.versions.electron} ${process.versions.node} ${process.platform} ${process.arch}\n`,
  );
} catch {}
function phase(name) {
  // Fixed numeric phase markers contain no paths, environment values or results.
  // Diagnostic delivery must not change the existing exit or timeout behavior.
  try {
    writeSync(2, `TAMMY_ASAR_PHASE ${name} ${(performance.now() - phaseStart).toFixed(3)}\n`);
  } catch {}
}
phase("harness-start");

// This headless protocol fixture must not wait for macOS foreground activation.
if (process.platform === "darwin") {
  app.setActivationPolicy("accessory");
}

process.stderr.write("TAMMY_ASAR_HARNESS_START\n");

const rendererRoot = process.env.TAMMY_TEST_RENDERER_ROOT;
const securitySource = process.env.TAMMY_TEST_SECURITY_SOURCE;

async function run() {
  if (!rendererRoot || !securitySource) {
    throw new Error("HARNESS_ARGUMENTS_MISSING");
  }

  process.stderr.write("TAMMY_ASAR_IMPORT_START\n");
  phase("import-start");
  const security = await import(pathToFileURL(securitySource).href);
  phase("import-done");
  process.stderr.write("TAMMY_ASAR_IMPORT_DONE\n");
  security.installApplicationScheme({ app, protocol });
  process.stderr.write("TAMMY_ASAR_READY_WAIT\n");
  phase("ready-wait");
  await app.whenReady();
  if (
    !process.env.TAMMY_TEST_USER_DATA_ROOT ||
    (await realpath(app.getPath("userData"))) !==
      (await realpath(process.env.TAMMY_TEST_USER_DATA_ROOT))
  ) {
    throw new Error("HARNESS_PROFILE_NOT_ISOLATED");
  }
  process.stderr.write("TAMMY_ASAR_READY\n");
  phase("ready");
  phase("protocol-start");
  await security.installApplicationProtocol({
    app,
    fetchFile: (url) => net.fetch(url),
    protocol,
    rendererRoot,
  });
  phase("protocol-ready");

  phase("fetch-start");
  const response = await net.fetch("tammy://app/");
  phase("fetch-headers");
  const result = {
    body: await response.text(),
    contentType: response.headers.get("Content-Type"),
    dockVisible: process.platform === "darwin" ? app.dock.isVisible() : null,
    isolatedUserData: true,
    nosniff: response.headers.get("X-Content-Type-Options"),
    status: response.status,
  };
  phase("body-read");
  return result;
}

let exited = false;
let failurePending = false;

function exitOnce(code) {
  if (exited) {
    return;
  }
  exited = true;
  phase("exit-requested");
  app.exit(code);
}

function reportFailure(error) {
  if (exited || failurePending) {
    return;
  }
  failurePending = true;
  phase("failure");
  const code = error instanceof Error ? error.message : "UNKNOWN";
  try {
    process.stderr.write(`TAMMY_ASAR_FAILURE ${code}\n`, () => exitOnce(1));
  } catch {
    exitOnce(1);
  }
}

void run().then((result) => {
  try {
    phase("result-write");
    process.stdout.write(`TAMMY_ASAR_RESULT ${JSON.stringify(result)}\n`, (error) => {
      if (error) {
        reportFailure(error);
        return;
      }
      phase("result-flushed");
      exitOnce(0);
    });
  } catch (error) {
    reportFailure(error);
  }
}, reportFailure);
