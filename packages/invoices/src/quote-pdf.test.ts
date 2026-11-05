import assert from "node:assert/strict";
import test from "node:test";
import { extractData } from "@formepdf/core";
import { pdfStreams } from "../test-support/pdf-text.ts";
import { renderQuotePdf } from "./quote-pdf.ts";
import { draftFixture } from "./test-fixtures.ts";

function sample() {
  const draft=draftFixture();
  return {
    sellerName:"Sample Business",
    sellerAbn:"51824753556",
    draft:{...draft,number:"QUO-001",issueDate:"2026-11-05",dueDate:"2026-12-05",notes:"Illustrative quote"},
  };
}
test("creates a real quote PDF without invoice identity or embedded private cost",async()=>{
  const input=sample();
  const line=input.draft.lines[0]!;
  const bytes=await renderQuotePdf({
    ...input,
    draft:{...input.draft,lines:[{...line,unitCostMinor:777700}]},
  });
  assert.equal(String.fromCharCode(...bytes.subarray(0,5)),"%PDF-");
  const content=pdfStreams(bytes);
  for(const expected of ["QUOTE","QUO-001","VALID UNTIL","Quoted total","NOT AN INVOICE"])
    assert.ok(content.includes(expected),"Missing quote content: "+expected);
  assert.equal(content.includes("Tax Invoice"),false);
  assert.equal(content.includes("Total due (AUD)"),false);
  assert.equal(content.includes("7,777.00"),false);
  assert.equal(extractData(bytes),null);
});
test("quote cannot be generated without a named customer, valid date and reference",async()=>{
  const original=sample();
  await assert.rejects(()=>renderQuotePdf({...original,draft:{...original.draft,number:""}}),/Quote needs/);
  await assert.rejects(()=>renderQuotePdf({...original,draft:{...original.draft,customerName:""}}),/Quote needs/);
  await assert.rejects(()=>renderQuotePdf({...original,draft:{...original.draft,dueDate:"2026-10-01"}}),/dueDate/);
});
