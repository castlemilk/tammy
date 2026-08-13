import assert from "node:assert/strict";
import { access, readFile } from "node:fs/promises";
import test from "node:test";

async function readJson(path) {
  return JSON.parse(await readFile(path, "utf8"));
}

test("connect-client typechecking includes its source tests", async () => {
  const [rootPackage, packageManifest, tsconfig] = await Promise.all([
    readJson("package.json"),
    readJson("packages/connect-client/package.json"),
    readJson("packages/connect-client/tsconfig.json"),
  ]);

  assert.equal(tsconfig.exclude, undefined);
  assert.equal(tsconfig.compilerOptions.composite, undefined);
  assert.equal(tsconfig.compilerOptions.tsBuildInfoFile, undefined);
  assert.deepEqual(tsconfig.include, ["src/**/*.ts"]);
  assert.equal(packageManifest.scripts.typecheck, "tsc --noEmit");
  assert.match(rootPackage.scripts.typecheck, /@tammy\/connect-client typecheck/);
  await assert.rejects(access("packages/connect-client/tsconfig.tsbuildinfo"));
});
