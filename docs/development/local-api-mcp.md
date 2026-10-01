# Local API and MCP

The desktop can grant access to its current signed-in organisation through a loopback HTTP API and a stdio MCP adapter. Access is off until you enable it in Tammy. Clients can read connected workflows and stage an editable invoice proposal. They cannot save or issue invoices, post accounting commands, import files, or submit a BAS.

## Enable access

Start the supported development app from the repository root:

```sh
mise exec -- task dev
```

Create or open and unlock your encrypted workspace, sign in, and complete organisation setup. Choose `Integrations → Enable local API & MCP…` in the native application menu and confirm `Enable access`. Tammy shows the absolute private connection-file path. `Integrations → Show local connection file` reveals it in the file manager.

The file is `local-integrations/connection.json` below Electron's user-data directory. The usual macOS path is `~/Library/Application Support/Tammy/local-integrations/connection.json`; use the path Tammy displays for your running app. It has mode `0600` and contains `version`, a random loopback `origin`, and a private `capability`. A client with this file can read the current organisation's data. Keep it out of Git, logs, screenshots, and shared folders. Do not copy the capability into client configuration or a shell argument.

`Integrations → Revoke local API & MCP access` removes the file and closes the broker. Signing in again, unlocking a workspace, or quitting Tammy also invalidates access. Re-enable access in Tammy after those transitions, then restart your MCP client so it reads the new connection. There is no persistent grant or background broker after Tammy quits.

## Connect an MCP client

Use the pinned Node executable's absolute path:

```sh
mise which node
```

Add a stdio server to your MCP client's configuration. This example uses the usual `mcpServers` format; replace all three absolute paths with the Node path returned above, this checkout's CLI path, and the private file path displayed by Tammy. Paths with spaces stay one JSON string.

```json
{
  "mcpServers": {
    "tammy": {
      "command": "/Users/operator/.local/share/mise/installs/node/24.18.0/bin/node",
      "args": [
        "/Users/operator/projects/tammy/packages/local-api/src/cli.ts",
        "--connection",
        "/Users/operator/Library/Application Support/Tammy/local-integrations/connection.json"
      ]
    }
  }
}
```

Install the workspace dependencies with `mise exec -- task setup` before starting the adapter. The CLI reads an absolute, regular, non-symlink connection file with mode `0600`. A missing or invalid file returns `INVALID_CONNECTION_FILE` without printing its path or contents. The broker must remain enabled in the running desktop.

For a client that launches Task from this checkout, the equivalent command is:

```sh
mise exec -- task integrations:mcp CONNECTION_FILE="/absolute/private/connection.json"
```

This starts a stdio protocol process; it does not produce a human-readable prompt. Task accepts the file path only. Do not pass a workspace passphrase, machine credential, or capability to Task.

## Use the HTTP API

The broker listens only at the file's `http://127.0.0.1:<port>` origin. Each request requires exactly one `Authorization` header whose value is `Bearer ` followed by the file's capability. It rejects browser `Origin` headers, other hosts, and routes outside the two endpoints below. There is no unauthenticated capability listing.

| Endpoint | Request | Response |
| --- | --- | --- |
| `GET /v1/capabilities` | No body | API version, 14 read tools, one invoice proposal tool, and 10 resources. |
| `POST /v1/read` | JSON tool call, invoice proposal, or resource read | `{ "ok": true, "data": ... }`, or a redacted error code. |

This example reads the organisation without displaying the bearer capability. Run it from the repository root after enabling access:

```sh
CONNECTION_FILE="/absolute/private/connection.json" mise exec -- node --input-type=module <<'NODE'
import { readLocalApiConnectionFile } from "./packages/local-api/src/connection.ts";

const connection = await readLocalApiConnectionFile(process.env.CONNECTION_FILE);
const response = await fetch(`${connection.origin}/v1/read`, {
  method: "POST",
  headers: {
    Authorization: `Bearer ${connection.capability}`,
    "Content-Type": "application/json",
  },
  body: JSON.stringify({
    version: "v1",
    operation: "tool.call",
    name: "tammy.organisation.get_current",
    arguments: {},
  }),
});
console.log(JSON.stringify(await response.json(), null, 2));
if (!response.ok) process.exitCode = 1;
NODE
```

A resource read uses `{ "version": "v1", "operation": "resource.read", "uri": "tammy://current/organisation" }` as the body instead. Read-tool arguments are empty; requests cannot supply an organisation ID, authentication context, page cursor, or arbitrary Core procedure. `tammy.invoices.propose_draft` accepts the strict invoice draft fields described in [Invoice builder](invoice-builder.md).

## Read coverage and limits

| Tool | Equivalent resource |
| --- | --- |
| `tammy.accounting.get_trial_balance` | `tammy://current/trial-balance` |
| `tammy.accounting.list_accounts` | `tammy://current/accounts` |
| `tammy.accounting.list_journals` | `tammy://current/journals` |
| `tammy.banking.get_summary` | None |
| `tammy.documents.list_metadata` | None |
| `tammy.identity.get_current_user` | `tammy://current/user` |
| `tammy.invoices.list_cost_sources` | None |
| `tammy.invoices.list_invoices` | `tammy://current/invoices` |
| `tammy.organisation.get_current` | `tammy://current/organisation` |
| `tammy.reporting.get_attention_summary` | `tammy://current/reporting/attention` |
| `tammy.reporting.get_current_bas_draft` | `tammy://current/reporting/bas-draft` |
| `tammy.reporting.get_capability` | None |
| `tammy.sbr.get_readiness` | `tammy://current/sbr/readiness` |
| `tammy.system.get_diagnostics` | `tammy://system/diagnostics` |

List reads return only the first page, with a maximum of 200 entries (20 for saved invoices). The current adapter cannot request the next page even if the response contains a next cursor. Trial balance uses today's Australia/Melbourne date; attention uses the current calendar quarter. This is bounded inspection, not a complete accounting export.

Document metadata excludes `extractedText` and `candidate`; the cost-source tool separately projects reviewed supplier references and AUD amounts, never document bodies. Saved invoice reads include internal costs for the paired accounting client. SBR readiness excludes credential material. BAS remains a draft, with no submit or lodge action. Persistent write commands are not exposed by this interface; invoice proposals exist only in memory until the user explicitly reviews and saves them in Tammy.

The broker limits request bodies to 128 KiB, responses to 1 MiB, and requests to five seconds. Clients should treat revocation, a closed broker, or a rejected session as a reason to stop and ask the operator to re-enable access. Do not log the connection file or authorization header when diagnosing failures.

## Verify the integration

```sh
mise exec -- task integrations:test
mise exec -- task test:desktop:dev
```

The focused task runs the local API package's tests and type-check. The packaged current-workflow acceptance exercises real HTTP and stdio MCP pairing, invoice proposal review, persistence and PDF export. The root gates include both packages. These checks do not establish production release, Apple distribution signing, or ATO submission evidence.
