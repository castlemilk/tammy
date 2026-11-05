import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { computeCashForecast, exampleForecast, forecastToCsv } from "../lib/tools/cash-forecast";
import { parseQuoteDraft, serializeQuoteDraft } from "../lib/tools/local-quote-draft";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { GammaEmptyState } from "../components/gamma/empty-state";
import type { QuoteInput } from "../lib/tools/quote";
import ForecastPage, { metadata } from "../app/tools/cash-flow-forecast/page";

const START="2026-11-05";
afterEach(cleanup);
function draft(): QuoteInput {
  return {
    sellerName:"Sample Business",sellerAbn:"51824753556",sellerAddress:"Melbourne VIC",
    customerName:"Sample Customer",customerAddress:"Victoria",reference:"QUO-001",
    quoteDate:START,validUntil:"2026-12-05",gstRegistered:true,notes:"Draft for review",
    lines:[{description:"Consulting",quantity:"1.5",unitPrice:"120.00",treatment:"taxable"}],
  };
}
describe("Forecast and user-owned local quote drafts",()=>{
  it("calculates twelve contiguous weeks with exact cents and a correctly carried balance",()=>{
    const result=computeCashForecast(START,1200000,exampleForecast(START));
    expect(result.weeks).toHaveLength(12);
    expect(result.weeks[0]).toMatchObject({
      openingCents:1200000,receiptsCents:450000,
      paymentsCents:220000,closingCents:1430000,
    });
    expect(result.weeks[1]).toMatchObject({openingCents:1430000,paymentsCents:620000,closingCents:810000});
    expect(result.weeks[2]).toMatchObject({openingCents:810000,receiptsCents:800000,closingCents:1610000});
    expect(result.endingCents).toBe(1610000);
    expect(result.totalReceiptsCents).toBe(1250000);
    expect(result.totalPaymentsCents).toBe(840000);
    expect(result.lowestClosingCents).toBe(810000);
  });
  it("shows a projected cash shortage instead of hiding it",()=>{
    const result=computeCashForecast(START,2000,[{
      id:"a",date:START,description:"Rent",direction:"payment",amountCents:4500,
    }]);
    expect(result.weeks[0].closingCents).toBe(-2500);
    expect(result.lowestClosingCents).toBe(-2500);
  });
  it("rejects invalid dates, duplicate rows, zero amounts and out-of-window movements",()=>{
    const sample=exampleForecast(START)[0]!;
    expect(()=>computeCashForecast("2026-02-30",100,[])).toThrow();
    expect(()=>computeCashForecast(START,-1,[])).toThrow();
    expect(()=>computeCashForecast(START,100,[{...sample,amountCents:0}])).toThrow();
    expect(()=>computeCashForecast(START,100,[sample,{...sample}])).toThrow();
    expect(()=>computeCashForecast(START,100,[{...sample,date:"2028-11-05"}])).toThrow();
  });
  it("CSV includes weekly row totals and neutralises formula-leading descriptions",()=>{
    const csv=forecastToCsv(START,10000,[{
      id:"one",date:START,direction:"receipt",
      description:'=HYPERLINK("https://example.org","Open")',amountCents:900,
    }]);
    expect(csv).toContain('"Week","Start","End"');
    expect(csv).toContain('"1","2026-11-05","2026-11-11"');
    expect(csv).toContain('"\'=HYPERLINK(');
    expect(csv).toContain('"$109.00"');
    expect(csv.split("\r\n").length).toBeGreaterThan(14);
  });
  it("serializes incomplete drafts locally with strict version, shape and size validation",()=>{
    const json=serializeQuoteDraft(draft());
    expect(parseQuoteDraft(json)).toEqual(draft());
    expect(parseQuoteDraft(serializeQuoteDraft({...draft(),customerName:""})).customerName).toBe("");
    expect(()=>parseQuoteDraft(JSON.stringify({...JSON.parse(json),extra:"bank-secret"}))).toThrow();
    expect(()=>parseQuoteDraft(JSON.stringify({...JSON.parse(json),version:99}))).toThrow();
    expect(()=>parseQuoteDraft(JSON.stringify({...JSON.parse(json),draft:{...draft(),lineItemCount:22}}))).toThrow();
    expect(()=>parseQuoteDraft("{nope")).toThrow();
  });
  it("renders a genuine Gamma pattern with shadcn-style native controls",()=>{
    const {container}=render(<GammaEmptyState title="Nothing yet" description="Create a real forecast."><Button>Start</Button></GammaEmptyState>);
    expect(screen.getByRole("heading",{level:3,name:"Nothing yet"})).toBeTruthy();
    expect(screen.getByRole("button",{name:"Start"}).getAttribute("type")).toBe("button");
    cleanup();
    render(<Input aria-label="Test amount" value="1" readOnly />);
    expect(screen.getByRole("textbox",{name:"Test amount"})).toBeTruthy();
    expect(container.querySelectorAll("button").length).toBe(0);
  });
  it("renders a twelve-week table and a real scenario-loading action",()=>{
    const {container}=render(<ForecastPage />);
    expect(metadata.alternates).toEqual({canonical:"/tools/cash-flow-forecast"});
    expect(container.querySelectorAll("tbody tr")).toHaveLength(12);
    expect(screen.getByRole("heading",{name:/No receipts or payments yet/i})).toBeTruthy();
    fireEvent.click(screen.getByRole("button",{name:"Use sample figures"}));
    expect(screen.getByDisplayValue("Customer payment")).toBeTruthy();
    expect(container.querySelectorAll("tbody tr")).toHaveLength(12);
    expect(screen.getByText(/Illustrative scenario loaded/)).toBeTruthy();
  });
});
