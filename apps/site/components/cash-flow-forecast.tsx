"use client";
import { useEffect, useRef, useState, useSyncExternalStore, type KeyboardEvent, type ReactNode } from "react";
import { addCalendarDays, aud } from "../lib/tools/calculations";
import {
  computeCashForecast, exampleForecast, forecastToCsv, parseForecastMoney,
  type ForecastMovement,
} from "../lib/tools/cash-forecast";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "./ui/card";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell, TableCaption } from "./ui/table";
import { GammaEmptyState } from "./gamma/empty-state";

interface EditableMovement {
  id: string;
  date: string;
  description: string;
  amount: string;
  direction: "receipt" | "payment";
}

const parseLines = (lines: readonly EditableMovement[]): ForecastMovement[] =>
  lines.map((line) => ({
    id:line.id,date:line.date,description:line.description.trim(),
    amountCents:parseForecastMoney(line.amount),direction:line.direction,
  }));
function downloadCsv(content: string, name: string) {
  const url = URL.createObjectURL(new Blob([content], {type:"text/csv;charset=utf-8"}));
  const link = document.createElement("a");
  link.href=url;link.download=name;document.body.appendChild(link);link.click();link.remove();
  window.setTimeout(()=>URL.revokeObjectURL(url),1000);
}

function ForecastTableViewport({children}:{children:ReactNode}) {
  const regionRef=useRef<HTMLElement>(null);
  const [overflow,setOverflow]=useState(false);
  useEffect(()=>{
    const region=regionRef.current;
    if(!region) return;
    const measure=()=>setOverflow(region.scrollWidth>region.clientWidth+1);
    measure();
    const observer=typeof ResizeObserver==="undefined"?null:new ResizeObserver(measure);
    observer?.observe(region);
    if(region.firstElementChild) observer?.observe(region.firstElementChild);
    window.addEventListener("resize",measure);
    return ()=>{observer?.disconnect();window.removeEventListener("resize",measure);};
  },[children]);
  return <div className="tools-forecast-weekly">
    <p className="tools-forecast-scroll-hint" id="forecast-scroll-hint" hidden={!overflow}>Scroll sideways to see every cash column.</p>
    {/* oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex -- The named overflow region needs focus for keyboard scrolling. */}
    <section tabIndex={0} ref={regionRef} className="tools-forecast-table-scroll" aria-label="Weekly cash forecast"
      aria-describedby={overflow?"forecast-scroll-hint":undefined}>
      {children}
    </section>
    <p id="forecast-table-note" className="pt-3 text-xs leading-5 text-muted-foreground">Illustrative twelve-week cash forecast. Amounts in AUD, based on the entered expected dates.</p>
  </div>;
}
const subscribeLocalDate = () => () => {};
const serverLocalDate = () => "";
const browserLocalDate = () => {
  const now = new Date();
  return new Date(now.getTime() - now.getTimezoneOffset() * 60000).toISOString().slice(0, 10);
};

export function CashFlowForecast() {
  // Stable empty SSR snapshot, then read the browser's local date on hydration.
  // An explicit user selection takes precedence and can intentionally be blank.
  const currentLocalDate = useSyncExternalStore(subscribeLocalDate, browserLocalDate, serverLocalDate);
  const [selectedStart,setSelectedStart]=useState<string|null>(null);
  const start = selectedStart ?? currentLocalDate;
  const [opening,setOpening]=useState("12000.00");
  const [lines,setLines]=useState<EditableMovement[]>([]);
  const [notice,setNotice]=useState("");
  const [confirmSample,setConfirmSample]=useState(false);
  const sampleButton=useRef<HTMLButtonElement>(null);
  const keepButton=useRef<HTMLButtonElement>(null);
  const sequence=useRef(0);
  useEffect(()=>{
    if(confirmSample) keepButton.current?.focus();
  },[confirmSample]);
  function keepForecast() {
    setConfirmSample(false);
    sampleButton.current?.focus();
  }
  function cancelOnEscape(event:KeyboardEvent<HTMLButtonElement>) {
    if(event.key==="Escape") {
      event.preventDefault();
      keepForecast();
    }
  }
  function add() {
    setConfirmSample(false);
    sequence.current+=1;
    setLines(prev=>[...prev,{
      id:"row-"+sequence.current,date:start,description:"",
      amount:"0.00",direction:"receipt" as const,
    }]);
    setNotice("");
  }
  function update(id:string,patch:Partial<EditableMovement>) {
    setConfirmSample(false);
    setLines(prev=>prev.map(line=>line.id===id?{...line,...patch}:line));
    setNotice("");
  }
  function fillExample() {
    const items=exampleForecast(start);
    setOpening("12000.00");
    setLines(items.map(item=>({
      ...item,amount:(item.amountCents/100).toFixed(2),
    })));
    sequence.current=items.length;
    setConfirmSample(false);
    setNotice("Illustrative scenario loaded. Replace the example figures before using this as your own forecast.");
    sampleButton.current?.focus();
  }
  function requestExample() {
    if(lines.length>0 || opening!=="12000.00") setConfirmSample(true);
    else fillExample();
  }
  let result:ReturnType<typeof computeCashForecast>|null=null;
  let error="";
  try {result=computeCashForecast(start,parseForecastMoney(opening),parseLines(lines));}
  catch(e){error=e instanceof Error?e.message:"Check your forecast dates and amounts.";}
  function exportForecast() {
    try {
      const csv=forecastToCsv(start,parseForecastMoney(opening),parseLines(lines));
      downloadCsv(csv,"tammy-12-week-forecast-"+start+".csv");
      setNotice("CSV generated in this browser. Nothing was uploaded or posted to Tammy Accounting.");
    }catch(e){setNotice(e instanceof Error?e.message:"Unable to export the forecast.");}
  }
  const lastDay=addCalendarDays(start,83)??undefined;
  return <div className="grid grid-cols-1 gap-6 xl:grid-cols-[minmax(0,0.84fr)_minmax(0,1.16fr)]">
    <Card className="self-start overflow-hidden border-[var(--gamma-border)] shadow-none">
      <CardHeader className="border-b border-border bg-secondary/50">
        <p className="text-xs font-semibold uppercase tracking-wider text-primary">Free planning workbench</p>
        <CardTitle className="font-[family-name:var(--t-display)] text-3xl font-medium">Build the next twelve weeks.</CardTitle>
        <CardDescription>Plan expected cash movements by date. No account, sync or automatic forecasts.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5 pt-5">
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <label className="space-y-2 text-sm font-medium" htmlFor="forecast-start">First week begins
            <Input id="forecast-start" type="date" value={start} onChange={(e)=>{setSelectedStart(e.target.value);setNotice("");setConfirmSample(false);}} />
          </label>
          <label className="space-y-2 text-sm font-medium" htmlFor="forecast-opening">Available opening cash (AUD)
            <Input id="forecast-opening" inputMode="decimal" value={opening} onChange={(e)=>{setOpening(e.target.value);setNotice("");setConfirmSample(false);}} />
          </label>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3">
          <h3 className="font-semibold">Expected movements</h3>
          <div className="flex flex-wrap gap-2">
            <Button ref={sampleButton} variant="outline" size="sm" disabled={!lastDay} onClick={requestExample}
              aria-expanded={confirmSample} aria-controls={confirmSample?"forecast-sample-confirmation":undefined}>Use sample figures</Button>
            <Button variant="secondary" size="sm" onClick={add} disabled={lines.length>=150}>Add movement</Button>
          </div>
        </div>
        {confirmSample && <fieldset id="forecast-sample-confirmation"
          aria-describedby="forecast-sample-description"
          className="m-0 min-w-0 space-y-3 border-0 border-b border-border p-0 pb-5">
          <legend className="tools-forecast-confirmation-title">Replace your forecast with sample figures?</legend>
          <p id="forecast-sample-description" className="text-sm leading-6 text-muted-foreground">
            This replaces your opening cash{lines.length>0?" and "+lines.length+" entered "+(lines.length===1?"movement":"movements"):""}.
            {" "}Your start date stays the same. Export your CSV first if you want to keep a copy.
          </p>
          <div className="flex flex-wrap gap-3">
            <Button ref={keepButton} variant="outline" onClick={keepForecast} onKeyDown={cancelOnEscape}>Keep my forecast</Button>
            <Button variant="destructive" onClick={fillExample} onKeyDown={cancelOnEscape}>Replace with sample</Button>
          </div>
        </fieldset>}
        {lines.length===0
          ? <GammaEmptyState title="No receipts or payments yet" description="Add the cash you expect to receive or pay, or load an editable sample to understand the calculation.">
              <Button variant="outline" onClick={add}>Add first movement</Button>
            </GammaEmptyState>
          : <div className="space-y-3">{lines.map((line,index)=><div className="rounded-lg border border-border bg-background/70 p-3" key={line.id}>
            <div className="mb-3 flex items-center justify-between gap-2">
              <strong className="text-sm">Movement {index+1}</strong>
              <Button variant="ghost" size="sm" onClick={()=>{setLines(prev=>prev.filter(x=>x.id!==line.id));setConfirmSample(false);}}
                aria-label={"Remove movement "+(index+1)}>Remove</Button>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <label className="col-span-2 space-y-1 text-xs font-semibold" htmlFor={"forecast-description-"+line.id}>Description
                <Input id={"forecast-description-"+line.id} maxLength={160} value={line.description} placeholder="Client payment, rent..."
                  onChange={(e)=>update(line.id,{description:e.target.value})}/>
              </label>
              <label className="space-y-1 text-xs font-semibold" htmlFor={"forecast-date-"+line.id}>Expected date
                <Input id={"forecast-date-"+line.id} type="date" min={start} max={lastDay} value={line.date}
                  onChange={(e)=>update(line.id,{date:e.target.value})}/>
              </label>
              <label className="space-y-1 text-xs font-semibold" htmlFor={"forecast-type-"+line.id}>Direction
                <select id={"forecast-type-"+line.id} value={line.direction} onChange={(e)=>update(line.id,{direction:e.target.value as EditableMovement["direction"]})}
                  className="min-h-11 w-full rounded-md border border-input bg-card px-3 text-sm text-foreground focus-visible:outline-none focus-visible:ring-3 focus-visible:ring-ring/35">
                  <option value="receipt">Receipt (+)</option><option value="payment">Payment (−)</option>
                </select>
              </label>
              <label className="col-span-2 space-y-1 text-xs font-semibold" htmlFor={"forecast-amount-"+line.id}>Amount (AUD)
                <Input id={"forecast-amount-"+line.id} inputMode="decimal" value={line.amount}
                  onChange={(e)=>update(line.id,{amount:e.target.value})}/>
              </label>
            </div>
          </div>)}</div>}
        {error && <p className="rounded-md border border-red-300 bg-red-50 p-3 text-sm text-red-800" role="alert">{error}</p>}
        <Button className="w-full" onClick={exportForecast} disabled={!result}>Export 12-week CSV</Button>
        {notice && <output className="block text-sm leading-6 text-muted-foreground">{notice}</output>}
        <p className="text-xs leading-5 text-muted-foreground">
          Estimates only. Dates and figures remain in this tab until you explicitly export. Cash movements do not update the desktop ledger.
        </p>
      </CardContent>
    </Card>
    <Card className="overflow-hidden border-[var(--gamma-border)] shadow-none">
      <CardHeader className="bg-[var(--gamma-surface-2)]">
        <p className="text-xs font-semibold uppercase tracking-wide text-primary">Forecast preview</p>
        <CardTitle className="font-[family-name:var(--t-display)] text-3xl font-medium">The timing of cash.</CardTitle>
        <CardDescription>Closing cash for each week, including any projected shortfall.</CardDescription>
      </CardHeader>
      <CardContent className="space-y-5 pt-5">
        {result
          ? <>
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
              <div className="rounded-lg bg-secondary p-4"><span className="text-xs text-muted-foreground">Week 12 cash</span><strong className="mt-2 block font-[family-name:var(--t-display)] text-2xl tabular-nums">{aud(result.endingCents)}</strong></div>
              <div className="rounded-lg bg-secondary p-4"><span className="text-xs text-muted-foreground">Lowest closing</span><strong className="mt-2 block font-[family-name:var(--t-display)] text-2xl tabular-nums">{aud(result.lowestClosingCents)}</strong></div>
              <div className="rounded-lg bg-secondary p-4"><span className="text-xs text-muted-foreground">Cash out</span><strong className="mt-2 block font-[family-name:var(--t-display)] text-2xl tabular-nums">{aud(result.totalPaymentsCents)}</strong></div>
            </div>
            {result.lowestClosingCents<0 && <output className="block rounded-md border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-950">One or more weeks closes below zero. Check the timing and feasibility of forecast receipts.</output>}
            <ForecastTableViewport>
              <Table className="min-w-[640px]" aria-describedby="forecast-table-note">
                <TableCaption className="sr-only">Weekly cash forecast, AUD</TableCaption>
                <TableHeader><TableRow><TableHead scope="col">Week beginning</TableHead><TableHead scope="col" className="text-right">Opening</TableHead><TableHead scope="col" className="text-right">In</TableHead><TableHead scope="col" className="text-right">Out</TableHead><TableHead scope="col" className="text-right">Closing</TableHead></TableRow></TableHeader>
                <TableBody>{result.weeks.map(week=><TableRow key={week.week} className={week.closingCents<0?"bg-amber-50/75":undefined}>
                  <TableCell>{week.start}</TableCell><TableCell className="text-right">{aud(week.openingCents)}</TableCell>
                  <TableCell className="text-right">{aud(week.receiptsCents)}</TableCell><TableCell className="text-right">{aud(week.paymentsCents)}</TableCell>
                  <TableCell className={"text-right font-bold "+(week.closingCents<0?"text-red-800":"")}>{aud(week.closingCents)}</TableCell>
                </TableRow>)}</TableBody>
              </Table>
            </ForecastTableViewport>
          </>
          : <GammaEmptyState title="Check the forecast inputs" description="A complete weekly calculation appears when the dates, amounts and descriptions are valid."/>
        }
        <p className="text-xs leading-5 text-muted-foreground">No automatic bank feeds, BAS/tax calculation or guarantee of future receipts. Verify actual payment timing and update this estimate regularly.</p>
      </CardContent>
    </Card>
  </div>;
}
