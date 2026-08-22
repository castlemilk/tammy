const modes = new Set([
  "accounting-fresh",
  "simulator",
  "evte",
  "doctor",
  "registration",
  "test",
  "evidence",
]);
const [mode, ...extraArguments] = process.argv.slice(2);

if (!modes.has(mode) || extraArguments.length !== 0) {
  console.error("SBR_INCOMPLETE_MODE_INVALID");
  process.exitCode = 2;
} else {
  console.error(`SBR_IMPLEMENTATION_INCOMPLETE:${mode}`);
  process.exitCode = 1;
}
