import { execFileSync } from "node:child_process";
import { pathToFileURL } from "node:url";

export function validateToolVersions(outputs) {
  const errors = [];
  const goVersion = outputs.go.trim().split(/\s+/)[2] ?? outputs.go.trim();

  if (outputs.node !== "v24.18.0") {
    errors.push(`Node must be v24.18.0 (received ${outputs.node})`);
  }
  if (outputs.pnpm !== "11.15.0") {
    errors.push(`pnpm must be 11.15.0 (received ${outputs.pnpm})`);
  }
  if (goVersion !== "go1.26.4") {
    errors.push(`Go must be go1.26.4 (received ${goVersion})`);
  }
  if (outputs.buf !== "1.72.0") {
    errors.push(`Buf must be 1.72.0 (received ${outputs.buf})`);
  }

  return errors;
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  const outputs = {
    node: execFileSync("node", ["--version"], { encoding: "utf8" }).trim(),
    pnpm: execFileSync("pnpm", ["--version"], { encoding: "utf8" }).trim(),
    go: execFileSync("go", ["version"], { encoding: "utf8" }).trim(),
    buf: execFileSync("buf", ["--version"], { encoding: "utf8" }).trim(),
  };
  const errors = validateToolVersions(outputs);

  if (errors.length > 0) {
    for (const error of errors) {
      console.error(error);
    }
    process.exitCode = 1;
  } else {
    console.log("toolchain ok");
  }
}
