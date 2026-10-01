import assert from "node:assert/strict";
import test from "node:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { startLocalApiBroker } from "./broker.ts";
import { createLocalApiMcpServer } from "./mcp.ts";
import { validateLocalApiRequest } from "./request.ts";

const draft = {
  number: "INV-001",
  issueDate: "2026-09-30",
  dueDate: "2026-10-14",
  customerName: "Buyer Pty Ltd",
  customerAddress: "1 High Street, Melbourne VIC 3000",
  sellerAddress: "27 Cambridge Road, Mooroolbark VIC 3138",
  gstRegistered: true,
  notes: "",
  paymentInstructions: "Bank transfer",
  lines: [
    {
      description: "Consulting",
      quantityMilli: 1500,
      unitPriceMinor: 10000,
      unitCostMinor: 5000,
      taxTreatment: "taxable",
    },
  ],
};

test("invoice proposal validates bounded drafts but rejects authority and calculated totals", () => {
  const envelope = {
    version: "v1",
    operation: "tool.call",
    name: "tammy.invoices.propose_draft",
    arguments: draft,
  };
  const valid = validateLocalApiRequest(envelope);
  assert.equal(valid.ok, true);
  for (const arguments_ of [
    { ...draft, organisationId: "other" },
    { ...draft, totalMinor: 1 },
    { ...draft, lines: [{ ...draft.lines[0], unitPriceMinor: 1.5 }] },
    { ...draft, dueDate: "2026-02-30" },
    { ...draft, gstRegistered: false },
  ]) {
    assert.deepEqual(validateLocalApiRequest({ ...envelope, arguments: arguments_ }), {
      ok: false,
      error: { code: "INVALID_ARGUMENTS" },
    });
  }
});

test("MCP proposes an invoice over the real broker with no save/issue authority", async (t) => {
  let received: unknown;
  const broker = await startLocalApiBroker({
    execute: async (input) => {
      received = input;
      return { staged: true, reviewRequired: true };
    },
  });
  t.after(() => broker.close());
  const server = createLocalApiMcpServer(broker.connection);
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  const client = new Client({ name: "invoice-e2e", version: "1" });
  await server.connect(serverTransport);
  await client.connect(clientTransport);
  t.after(async () => {
    await client.close();
    await server.close();
  });
  const { tools } = await client.listTools();
  const tool = tools.find(({ name }) => name === "tammy.invoices.propose_draft");
  assert.ok(tool);
  assert.equal(tool.annotations?.readOnlyHint, false);
  assert.equal(tool.annotations?.destructiveHint, false);
  assert.equal(tool.inputSchema.additionalProperties, false);
  const result = await client.callTool({ name: tool.name, arguments: draft });
  assert.equal(result.isError, false);
  assert.equal((received as { operation: string }).operation, "invoice.propose");
  assert.equal(
    tools.some(({ name }) => /save_invoice|issue_invoice/.test(name)),
    false,
  );
});
