# Offline desktop foundation development

## Evidence boundary

The current foundation is a self-contained offline Electron shell backed by a supervised Go process. Startup, the diagnostics call, packaging, and local packaged E2E require no cloud service. External CI and future SBR services are optional for local development, but external platform and conformance evidence cannot be claimed until those systems have actually run.

The repository does not yet implement accounting, Activity Statements, credential storage, ATO transport, or SBR submission. Packages produced by these commands are unsigned development artifacts and are not production-ready or approval evidence.

All commands below run from the repository root. The `rtk` prefix is required by this workspace.

## Toolchain setup

```sh
rtk mise install
rtk mise exec -- corepack prepare pnpm@11.15.0 --activate
rtk mise exec -- pnpm install --frozen-lockfile
rtk mise exec -- pnpm check:toolchain
```

The pinned versions are declared in `mise.toml` and `package.json`. Do not substitute globally installed Node, Go, Buf, or pnpm versions when producing evidence.

## Generate and validate contracts

```sh
rtk mise exec -- pnpm proto:format:check
rtk mise exec -- pnpm proto:lint
rtk mise exec -- pnpm proto:breaking
rtk mise exec -- pnpm proto:generate
rtk git diff --exit-code -- services/core/internal/gen/tammy/v1 packages/connect-client/src/gen/tammy/v1
```

The final command must be clean. A generated-code diff means the checked-in Connect-Go or Connect-ES v2 output does not match the protobuf source and pinned generators.

## Unit and foundation checks

```sh
rtk mise exec -- pnpm test:toolchain
rtk mise exec -- pnpm test:proto-breaking
rtk mise exec -- node --test scripts/check-foundation-evidence.test.mjs
rtk mise exec -- go test -race ./services/core/...
rtk mise exec -- pnpm desktop:test
rtk mise exec -- pnpm desktop:typecheck
rtk mise exec -- pnpm lint
rtk mise exec -- pnpm compliance:foundation
```

The compliance command validates the fixed traceability schema, required design rows, conservative statuses, and Windows evidence classification. It validates the evidence register's structure; it does not create external evidence or confer approval.

## Run in development

```sh
rtk mise exec -- pnpm desktop:start
```

This builds the local Go core and starts Electron Forge. Close the application window or press `Ctrl-C` in the owning terminal. Electron closes the child input pipe and waits for the Go process to stop; a bounded forced termination is the fallback.

Development mode is useful for iteration, but its local development-server CSP is not packaged-release evidence.

## Build the unsigned package

```sh
rtk mise exec -- pnpm desktop:package
```

The output is an unsigned local development package. Building it does not demonstrate code signing, notarisation, Windows support, SBR conformance, or production readiness.

## Packaged end-to-end tests

Run the supported local packaged flow:

```sh
rtk mise exec -- pnpm desktop:e2e
```

For repeated lifecycle and orphan-process coverage:

```sh
rtk mise exec -- pnpm --dir apps/desktop e2e --repeat-each=3
```

The E2E harness starts the packaged application, exercises the offline diagnostics call under the production custom protocol and CSP, exits the app, and verifies that the exact packaged core process path has not been orphaned.

The local supported evidence target is `darwin-arm64`. Hosted macOS results remain unproduced until hosted CI runs. The Windows Server 2025 job is classified `WINDOWS_SERVER_SMOKE_ONLY`; its `WINDOWS_SERVER_SMOKE_ONLY-squirrel-windows-x64` artifact is not Windows 11 evidence. Windows 11 23H2 x64 evidence must come from the separately gated `windows11-x64-foundation-evidence` workflow artifact and is currently unproduced.

## Clean-shutdown verification

The normal verification command is:

```sh
rtk mise exec -- pnpm desktop:e2e
```

For a focused lifecycle check:

```sh
rtk mise exec -- pnpm --dir apps/desktop test src/main/core-process.test.ts src/main/index.test.ts
rtk mise exec -- go test -race ./services/core/cmd/tammy-core ./services/core/internal/transport
```

Do not use a broad process-name kill as routine cleanup or as evidence. It can hide an orphan bug and can terminate unrelated development processes. If a test fails, preserve the exact packaged-path diagnostics emitted by the E2E harness before manually terminating that exact process.

### Recover an exact packaged orphan on macOS

Run recovery only after retaining the E2E failure diagnostics. The block below derives the two expected executable paths from the repository root, selects only exact `comm` path matches, and aborts if either path has more than one match. If both processes still exist, it also requires the core to be a child of that exact application PID. It revalidates the executable path immediately before each signal and confirms that neither exact executable remains.

This is a native `zsh` recovery block rather than test evidence, so its OS commands do not use the `rtk` development wrapper:

```zsh
set -eu
APP_EXEC="$PWD/apps/desktop/out/Tammy-darwin-arm64/Tammy.app/Contents/MacOS/Tammy"
CORE_EXEC="$PWD/apps/desktop/out/Tammy-darwin-arm64/Tammy.app/Contents/Resources/core/darwin-arm64/tammy-core"

typeset -a APP_PIDS CORE_PIDS
typeset -A PARENT_BY_PID
while read -r pid ppid executable; do
  [[ "$pid" == <-> && "$ppid" == <-> ]] || continue
  if [[ "$executable" == "$APP_EXEC" ]]; then
    APP_PIDS+=("$pid")
    PARENT_BY_PID[$pid]="$ppid"
  elif [[ "$executable" == "$CORE_EXEC" ]]; then
    CORE_PIDS+=("$pid")
    PARENT_BY_PID[$pid]="$ppid"
  fi
done < <(/bin/ps -axo pid=,ppid=,comm=)

(( ${#APP_PIDS[@]} <= 1 && ${#CORE_PIDS[@]} <= 1 )) || {
  print -u2 -- "ORPHAN_RECOVERY_AMBIGUOUS"
  exit 1
}

APP_PID="${APP_PIDS[1]:-}"
CORE_PID="${CORE_PIDS[1]:-}"
if [[ -n "$APP_PID" && -n "$CORE_PID" && "${PARENT_BY_PID[$CORE_PID]}" != "$APP_PID" ]]; then
  print -u2 -- "ORPHAN_RECOVERY_PARENT_MISMATCH"
  exit 1
fi

process_path() {
  /bin/ps -p "$1" -o comm= | /usr/bin/sed -e 's/^[[:space:]]*//'
}

terminate_exact() {
  local expected="$1"
  local pid="$2"
  [[ -n "$pid" ]] || return 0
  local current
  current="$(process_path "$pid")"
  [[ -z "$current" ]] && return 0
  [[ "$current" == "$expected" ]] || {
    print -u2 -- "ORPHAN_RECOVERY_PATH_MISMATCH:$pid"
    return 1
  }
  /bin/kill -TERM "$pid"
  for _ in {1..50}; do
    [[ "$(process_path "$pid")" != "$expected" ]] && return 0
    /bin/sleep 0.1
  done
  [[ "$(process_path "$pid")" == "$expected" ]] && /bin/kill -KILL "$pid"
}

terminate_exact "$APP_EXEC" "$APP_PID"
terminate_exact "$CORE_EXEC" "$CORE_PID"

for expected in "$APP_EXEC" "$CORE_EXEC"; do
  while read -r pid _ executable; do
    if [[ "$executable" == "$expected" ]]; then
      print -u2 -- "ORPHAN_RECOVERY_FAILED:$pid"
      exit 1
    fi
  done < <(/bin/ps -axo pid=,ppid=,comm=)
done
print -r -- "ORPHAN_RECOVERY_CONFIRMED"
```

If the block reports ambiguity or a parent/path mismatch, stop and use the retained E2E process IDs to identify the owned instance. Do not weaken the exact-path checks.

### Recover an exact packaged orphan on Windows 11

Run this block in Windows PowerShell from the repository root. It uses `Win32_Process.ExecutablePath`, requires at most one exact case-insensitive match per packaged path, validates the live core's `ParentProcessId` when the application still exists, and calls `taskkill` only with the validated numeric application/core PID. `/T` is therefore scoped to that owned PID tree; `/IM` is never used.

```powershell
$ErrorActionPreference = "Stop"
$AppPath = [System.IO.Path]::GetFullPath(
  (Join-Path $PWD "apps\desktop\out\Tammy-win32-x64\Tammy.exe")
)
$CorePath = [System.IO.Path]::GetFullPath(
  (Join-Path $PWD "apps\desktop\out\Tammy-win32-x64\resources\core\win32-x64\tammy-core.exe")
)
if ($env:SystemRoot -notmatch "^[A-Za-z]:\\Windows$") {
  throw "ORPHAN_RECOVERY_SYSTEM_ROOT_INVALID"
}
$Taskkill = Join-Path $env:SystemRoot "System32\taskkill.exe"

function Get-ExactProcess([string] $ExpectedPath) {
  @(
    Get-CimInstance Win32_Process |
      Where-Object {
        $_.ExecutablePath -and
        [System.StringComparer]::OrdinalIgnoreCase.Equals(
          [System.IO.Path]::GetFullPath($_.ExecutablePath),
          $ExpectedPath
        )
      }
  )
}

$AppMatches = @(Get-ExactProcess $AppPath)
$CoreMatches = @(Get-ExactProcess $CorePath)
if ($AppMatches.Count -gt 1 -or $CoreMatches.Count -gt 1) {
  throw "ORPHAN_RECOVERY_AMBIGUOUS"
}
$AppProcess = $AppMatches | Select-Object -First 1
$CoreProcess = $CoreMatches | Select-Object -First 1
if (
  $AppProcess -and
  $CoreProcess -and
  [int] $CoreProcess.ParentProcessId -ne [int] $AppProcess.ProcessId
) {
  throw "ORPHAN_RECOVERY_PARENT_MISMATCH"
}

function Stop-ValidatedTree($process, [string] $ExpectedPath, [switch] $Force) {
  if (-not $process) {
    return
  }
  $Current = @(
    Get-ExactProcess $ExpectedPath |
      Where-Object { [int] $_.ProcessId -eq [int] $process.ProcessId }
  )
  if ($Current.Count -eq 0) {
    return
  }
  if ($Current.Count -ne 1) {
    throw "ORPHAN_RECOVERY_PID_REVALIDATION_FAILED"
  }
  if ($Force) {
    & $Taskkill /PID $process.ProcessId /T /F
  } else {
    & $Taskkill /PID $process.ProcessId /T
  }
}

Stop-ValidatedTree $AppProcess $AppPath
$Deadline = [DateTime]::UtcNow.AddSeconds(5)
while (
  [DateTime]::UtcNow -lt $Deadline -and
  (@(Get-ExactProcess $AppPath).Count -gt 0 -or @(Get-ExactProcess $CorePath).Count -gt 0)
) {
  Start-Sleep -Milliseconds 100
}

foreach ($process in @(Get-ExactProcess $AppPath)) {
  Stop-ValidatedTree $process $AppPath -Force
}
foreach ($process in @(Get-ExactProcess $CorePath)) {
  Stop-ValidatedTree $process $CorePath -Force
}

if (@(Get-ExactProcess $AppPath).Count -ne 0 -or @(Get-ExactProcess $CorePath).Count -ne 0) {
  throw "ORPHAN_RECOVERY_FAILED"
}
Write-Output "ORPHAN_RECOVERY_CONFIRMED"
```

An already-orphaned core may have lost its original parent. The block will still terminate it only when its current PID resolves to the exact packaged core path. If multiple exact Tammy instances are present, it aborts so that the retained E2E PID can be selected manually and revalidated instead of terminating every instance.

## Troubleshooting

If the toolchain check fails, reinstall the pinned tools and dependencies:

```sh
rtk mise install
rtk mise exec -- pnpm install --frozen-lockfile
rtk mise exec -- pnpm check:toolchain
```

If Electron reports that the core binary is missing or stale:

```sh
rtk mise exec -- pnpm core:build
rtk mise exec -- pnpm --dir apps/desktop test src/main/core-process.test.ts
```

If the generated client and server disagree:

```sh
rtk mise exec -- pnpm proto:generate
rtk mise exec -- pnpm test:proto-breaking
rtk mise exec -- go test -race ./services/core/...
rtk mise exec -- pnpm desktop:test
```

If packaged E2E fails, rerun it once with the exact local target and retain the emitted failure diagnostics:

```sh
rtk mise exec -- pnpm desktop:e2e
```

Do not reclassify a Windows Server smoke result as Windows 11 validation, and do not describe an absent hosted result as passing.
