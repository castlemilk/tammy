import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { readFileSync } from "node:fs";
import { afterEach, describe, expect, it } from "vitest";
import { aud } from "../lib/tools/calculations";
import {
  createExpenseRecord, exportExpenseCSV, exportExpenseDraft,
  importExpenseDraft, summariseExpenseRecords, type ExpenseInput,
} from "../lib/tools/expense-records";
import ExpensePage, { metadata } from "../app/tools/expense-organiser/page";
import { matchTools, publicTools } from "../lib/tool-library";

afterEach(cleanup);
function input(patch: Partial<ExpenseInput> = {}): ExpenseInput {
  return {
    id: "expense-001",
    date: "2026-11-05",
    supplier: "Sample Hardware",
    description: "Office supplies",
    category: "Supplies",
    amount: "220.00",
    businessUsePercent: 75,
    gstEvidence: "Standard GST shown",
    sourceFileName: "receipt-001.pdf",
    ...patch,
  };
}

describe("Local-only expense records, not taxable deductions", () => {
  it("uses exact AUD cents and preserves user classification explicitly", () => {
    const record = createExpenseRecord(input());
    expect(record.grossCents).toBe(22000);
    const summary = summariseExpenseRecords([record]);
    expect(summary).toEqual({
      count: 1, grossCents: 22000, businessPortionCents: 16500,
      standardGstIndicationCents: 2000, referenceCount: 1,
    });
    expect(aud(summary.standardGstIndicationCents)).toBe("$20.00");
  });

  it("does not calculate GST for GST-free or unreviewed evidence", () => {
    const a = createExpenseRecord(input({
      id:"1", amount:"15.01", businessUsePercent: 33,
      gstEvidence:"GST-free", sourceFileName:"",
    }));
    const b = createExpenseRecord(input({
      id:"2", amount:"35.02", businessUsePercent: 0,
      gstEvidence:"Review treatment", sourceFileName:"",
    }));
    const summary = summariseExpenseRecords([a,b]);
    expect(summary.grossCents).toBe(5003);
    expect(summary.businessPortionCents).toBe(495);
    expect(summary.standardGstIndicationCents).toBe(0);
    expect(summary.referenceCount).toBe(0);
  });

  it("rejects invalid dates, negative amounts, bad percentages and duplicate IDs", () => {
    expect(() => createExpenseRecord(input({date:"2026-02-30"}))).toThrow(/date/);
    expect(() => createExpenseRecord(input({amount:"-10.00"}))).toThrow(/amount/);
    expect(() => createExpenseRecord(input({amount:"12.345"}))).toThrow(/amount/);
    expect(() => createExpenseRecord(input({businessUsePercent: 101}))).toThrow(/percentage/);
    expect(() => createExpenseRecord(input({supplier:""}))).toThrow(/Supplier/);
    const a=createExpenseRecord(input());
    expect(() => summariseExpenseRecords([a,a])).toThrow(/Duplicate/);
    expect(() => summariseExpenseRecords(Array.from({length:101},(_,i)=>createExpenseRecord(input({id:"e-"+i}))))).toThrow(/100/);
  });

  it("neutralises spreadsheet formulas in every text field, including filenames", () => {
    const row=createExpenseRecord(input({
      supplier:'=HYPERLINK("https://evil.example","Click")',
      description:'+CMD|\' /C calc',
      sourceFileName:"@SUM(A1:A3).pdf",
    }));
    const csv=exportExpenseCSV([row]);
    expect(csv).toContain('"\'=HYPERLINK(""https://evil.example"",""Click"")"');
    expect(csv).toContain('"\'+CMD');
    expect(csv).toContain('"\'@SUM(A1:A3).pdf"');
    expect(csv).toContain('"220.00"');
  });

  it("exports, reimports, and strictly validates only the record schema", () => {
    const r=createExpenseRecord(input());
    const json=exportExpenseDraft([r]);
    expect(importExpenseDraft(json)).toEqual([r]);
    expect(json).toContain("Receipt files are not bundled");
    const broken=JSON.parse(json);
    broken.records[0].bankAccount="sensitive";
    expect(()=>importExpenseDraft(JSON.stringify(broken))).toThrow(/missing or unexpected/);
    const duplicate=JSON.parse(json);
    duplicate.records.push(duplicate.records[0]);
    expect(()=>importExpenseDraft(JSON.stringify(duplicate))).toThrow(/Duplicate/);
    expect(()=>importExpenseDraft('{"schemaVersion":2,"records":[]}')).toThrow(/Unsupported/);
    expect(()=>importExpenseDraft("{oops")).toThrow(/JSON/);
    expect(()=>importExpenseDraft("a".repeat(2_000_001))).toThrow(/2 MB/);
  });
});

describe("Expense tool presentation and discovery", () => {
  it("adds a source-record row and updates totals without signing in", () => {
    const {container}=render(<ExpensePage />);
    expect(screen.getByRole("heading",{level:1}).textContent).toContain("spent");
    expect(screen.getByRole("heading",{name:"No expenses recorded yet"})).toBeTruthy();
    expect(screen.getByRole("button",{name:"Export CSV"})).toHaveProperty("disabled",true);
    fireEvent.change(screen.getByLabelText("Transaction date *"),{target:{value:"2026-11-05"}});
    fireEvent.change(screen.getByLabelText("Paid amount (AUD, gross) *"),{target:{value:"110.00"}});
    fireEvent.change(screen.getByLabelText("Supplier *"),{target:{value:"Sample Provider"}});
    fireEvent.change(screen.getByLabelText("GST evidence on the document"),{target:{value:"Standard GST shown"}});
    fireEvent.click(screen.getByRole("button",{name:"Add to worksheet"}));
    expect(screen.getByText("Expense recorded locally. No accounting entry was posted.")).toBeTruthy();
    expect(screen.getByText("Sample Provider")).toBeTruthy();
    expect(container.textContent).toContain("$110.00");
    expect(container.textContent).toContain("$10.00");
    expect(screen.getByRole("button",{name:"Export CSV"})).toHaveProperty("disabled",false);
    fireEvent.click(screen.getByRole("button",{name:"Remove expense from Sample Provider"}));
    expect(screen.getByRole("heading",{name:"No expenses recorded yet"})).toBeTruthy();
  });

  it("accepts local PDFs only and doesn't assume files are preserved in JSON", () => {
    render(<ExpensePage />);
    const input=screen.getByLabelText("Receipt PDF, JPG or PNG (optional)");
    const valid = new File(["%PDF-1.4 test"],"invoice-proof.pdf",{type:"application/pdf"});
    fireEvent.change(input,{target:{files:[valid]}});
    expect(screen.getByText(/Attached for this session: invoice-proof.pdf/)).toBeTruthy();
    const bad = new File(["bad"],"untrusted.html",{type:"text/html"});
    fireEvent.change(input,{target:{files:[bad]}});
    expect(screen.getByRole("alert").textContent).toContain("PDF, JPEG or PNG");
  });

  it("uses one canonical route, source code meta, and a discoverable category", () => {
    expect(metadata.alternates).toEqual({canonical:"/tools/expense-organiser"});
    expect(publicTools.find(t=>t.href==="/tools/expense-organiser")?.category).toBe("bookkeeping");
    expect(matchTools("receipt","bookkeeping").map(t=>t.href)).toEqual(["/tools/expense-organiser"]);
    const origin="https://tammy-accounting.castlemilk.chatgpt.site";
    expect(readFileSync("public/sitemap.xml","utf8")).toContain(origin+"/tools/expense-organiser</loc>");
    expect(readFileSync("public/llms.txt","utf8")).toContain(origin+"/tools/expense-organiser");
  });
});
