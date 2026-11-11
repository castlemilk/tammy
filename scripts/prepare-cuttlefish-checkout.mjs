import { execFileSync } from "node:child_process";

function git(...args) {
  return execFileSync("git", args, {
    encoding: "utf8",
    env: { ...process.env, GIT_TERMINAL_PROMPT: "0" },
    stdio: ["ignore", "pipe", "inherit"],
    timeout: 600_000,
  }).trim();
}

try {
  const head = git("rev-parse", "--verify", "HEAD^{commit}");
  const dispatched = process.env.CUTTLE_GITHUB_SHA;
  if (dispatched && (!/^[0-9a-f]{40}$/.test(dispatched) || dispatched !== head)) {
    throw new Error("CUTTLEFISH_CHECKOUT_REVISION_MISMATCH");
  }

  const shallow = git("rev-parse", "--is-shallow-repository");
  if (shallow !== "true" && shallow !== "false") {
    throw new Error("CUTTLEFISH_CHECKOUT_SHALLOW_STATE_INVALID");
  }
  const fetchArgs = ["fetch", "--no-tags"];
  if (shallow === "true") fetchArgs.push("--unshallow");
  fetchArgs.push("origin", "+refs/heads/master:refs/remotes/origin/master");
  git(...fetchArgs);
  git("rev-parse", "--verify", "refs/remotes/origin/master^{commit}");
  if (git("rev-parse", "--verify", "HEAD^{commit}") !== head) {
    throw new Error("CUTTLEFISH_CHECKOUT_HEAD_CHANGED");
  }
  console.log(`Cuttlefish checkout ready: ${head}`);
} catch (error) {
  console.error(error instanceof Error ? error.message : "CUTTLEFISH_CHECKOUT_FAILED");
  process.exitCode = 1;
}
