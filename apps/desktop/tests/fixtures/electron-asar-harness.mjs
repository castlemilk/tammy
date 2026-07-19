import { pathToFileURL } from "node:url";

import { app, net, protocol } from "electron";

process.stderr.write("TAMMY_ASAR_HARNESS_START\n");

const rendererRoot = process.env.TAMMY_TEST_RENDERER_ROOT;
const securitySource = process.env.TAMMY_TEST_SECURITY_SOURCE;

async function run() {
  if (!rendererRoot || !securitySource) {
    throw new Error("HARNESS_ARGUMENTS_MISSING");
  }

  process.stderr.write("TAMMY_ASAR_IMPORT_START\n");
  const security = await import(pathToFileURL(securitySource).href);
  process.stderr.write("TAMMY_ASAR_IMPORT_DONE\n");
  security.installApplicationScheme({ app, protocol });
  process.stderr.write("TAMMY_ASAR_READY_WAIT\n");
  await app.whenReady();
  process.stderr.write("TAMMY_ASAR_READY\n");
  await security.installApplicationProtocol({
    app,
    fetchFile: (url) => net.fetch(url),
    protocol,
    rendererRoot,
  });

  const response = await net.fetch("tammy://app/");
  const result = {
    body: await response.text(),
    contentType: response.headers.get("Content-Type"),
    nosniff: response.headers.get("X-Content-Type-Options"),
    status: response.status,
  };
  process.stdout.write(`TAMMY_ASAR_RESULT ${JSON.stringify(result)}\n`);
}

void run().then(
  () => app.exit(0),
  (error) => {
    const code = error instanceof Error ? error.message : "UNKNOWN";
    process.stderr.write(`TAMMY_ASAR_FAILURE ${code}\n`);
    app.exit(1);
  },
);
