import { createPrivateKey, sign } from "node:crypto";
import { readFile } from "node:fs/promises";
import path from "node:path";

import { generateSimulatorProfile } from "../../scripts/build-sbr-helper.mjs";

const root = path.resolve(import.meta.dirname, "../..");
const key = createPrivateKey(
  await readFile(path.join(root, "test/fixtures/sbr/simulator-profile-private-key.pem")),
);
await generateSimulatorProfile({
  helper: path.join(root, "apps/desktop/resources/sbr-helper/darwin-arm64/tammy-sbr-helper"),
  profile: path.join(root, "config/sbr/simulator/sbr-profile-v1.json"),
  signature: path.join(root, "config/sbr/simulator/sbr-profile-v1.sig"),
  signer: async (canonical) => sign(null, canonical, key),
});
