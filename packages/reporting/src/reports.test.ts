import assert from "node:assert/strict";
import test from "node:test";
import { parse } from "csv-parse/sync";
import { pdfStreams } from "../../invoices/test-support/pdf-text.ts";

const orgId = "018f0000-0000-7000-8000-000000000004";
const id = (index = 5) => `018f0000-0000-7000-8000-${String(index).padStart(12, "0")}`;
const date = { year: 2026, month: 10, day: 1 };
export const organisation = () => ({
  id: orgId,
  version: 2n,
  legalName: 'Gamma, "Systems"\nPty Ltd',
  displayName: "Gamma Systems",
  abn: "51824753556",
});
export const income = () => ({
  organisation: organisation(),
  acquiredAt: "2026-10-05T02:03:04.000Z",
  summary: {
    organisationId: orgId,
    periodStart: { ...date },
    periodEnd: { ...date, day: 31 },
    asOfDate: { ...date, day: 5 },
    businessIncome: { currencyCode: "AUD", minorUnits: 9007199254740993n },
    invoiceIncome: { currencyCode: "AUD", minorUnits: 10001n },
    paymentsReceived: { currencyCode: "AUD", minorUnits: 5500n },
    outstanding: { currencyCode: "AUD", minorUnits: 5501n },
    invoiceGst: { currencyCode: "AUD", minorUnits: 500n },
    unpostedIncome: { currencyCode: "AUD", minorUnits: 0n },
    issuedCount: 1n,
    unpostedIssuedCount: 0n,
    gstBasis: 1,
    financialRevision: 4n,
  },
});
export const bas = (count = 1) => ({
  organisation: organisation(),
  acquiredAt: "2026-10-05T02:03:04.000Z",
  workpaper: {
    id: id(),
    organisationId: orgId,
    version: 1n,
    periodStart: { ...date },
    periodEnd: { ...date, day: 31 },
    status: 1,
    createdAt: { seconds: 1791165784n, nanos: 0 },
    gstBasis: 1,
    salesG1: { currencyCode: "AUD", minorUnits: 5500n },
    gstOnSales1a: { currencyCode: "AUD", minorUnits: 500n },
    gstCredits1b: { currencyCode: "AUD", minorUnits: 1000n },
    netGstPayable: { currencyCode: "AUD", minorUnits: -500n },
    sourceFingerprint: new Uint8Array(32),
    excludedCashPurchaseCount: 0n,
    sourcesChanged: true,
    reviewWarnings: ['Review, "carefully"\nnow'],
    sources: Array.from({ length: count }, (_, index) => ({
      documentId: id(index + 100),
      supplierName: '  \t=HYPERLINK("bad")',
      invoiceNumber: "P-1",
      documentDate: { ...date },
      gross: { currencyCode: "AUD", minorUnits: 11000n },
      gstCredit: { currencyCode: "AUD", minorUnits: 1000n },
    })),
    salesSources: Array.from({ length: count }, (_, index) => ({
      invoiceId: id(index + 2000),
      invoiceNumber: "INV-001",
      customerName: "Δοκιμή",
      taxDate: { ...date },
      gross: { currencyCode: "AUD", minorUnits: 5500n },
      gst: { currencyCode: "AUD", minorUnits: 500n },
      paymentId: id(index + 4000),
      sourceRevision: 2n,
      sourceHash: new Uint8Array(32).fill(7),
    })),
  },
});
function first<T>(values: readonly T[]): T {
  const value = values[0];
  assert.ok(value);
  return value;
}
function csv(bytes: Uint8Array): string[][] {
  return parse(new TextDecoder().decode(bytes), { relax_column_count: false });
}
async function renderer() {
  return import("./reports.ts");
}

test("income summary CSV preserves exact integer cents, dates, counts and authoritative context", async () => {
  const { renderIncomeCsv } = await renderer();
  const rows = csv(renderIncomeCsv(income()));
  assert.deepEqual(rows[0], ["metric", "value", "currency", "context"]);
  assert.deepEqual(
    rows.find((row) => row[0] === "business_income"),
    ["business_income", "90071992547409.93", "AUD", "Revenue excluding GST"],
  );
  assert.equal(rows.find((row) => row[0] === "financial_revision")?.[1], "4");
  assert.equal(rows.find((row) => row[0] === "as_of_date")?.[1], "2026-10-05");
  assert.equal(
    rows.find((row) => row[0] === "organisation_legal_name")?.[1],
    income().organisation.legalName,
  );
  assert.ok(rows.every((row) => row.length === 4));
});
test("BAS CSV is rectangular, retains every source, negative GST, formulas and warnings", async () => {
  const { renderBasCsv } = await renderer();
  const rows = csv(renderBasCsv(bas(1000)));
  const header = rows[0];
  assert.ok(header);
  assert.ok(rows.every((row) => row.length === header.length));
  assert.equal(rows.filter((row) => row[0] === "sales").length, 1000);
  assert.equal(rows.filter((row) => row[0] === "purchases").length, 1000);
  assert.equal(rows.find((row) => row[1] === "net_gst_payable")?.[2], "-5.00");
  assert.equal(
    rows.find((row) => row[0] === "purchases")?.[header.indexOf("party")],
    '\'  \t=HYPERLINK("bad")',
  );
  assert.equal(
    rows.find((row) => row[0] === "sales")?.[header.indexOf("source_hash")],
    "07".repeat(32),
  );
  assert.ok(rows.some((row) => row[1] === "sources_changed" && row[2] === "true"));
  assert.ok(rows.some((row) => row[0] === "warnings" && row[2] === 'Review, "carefully"\nnow'));
});
test("empty sources and zero totals remain explicit, unsupported PDF text keeps Unicode CSV available", async () => {
  const { renderBasCsv, renderBasPdf } = await renderer();
  const value = bas(0);
  value.organisation.legalName = "中文 😀";
  value.workpaper.netGstPayable.minorUnits = 0n;
  assert.equal(csv(renderBasCsv(value)).find((row) => row[1] === "net_gst_payable")?.[2], "0.00");
  assert.ok(csv(renderBasCsv(value)).some((row) => row[2] === "中文 😀"));
  await assert.rejects(renderBasPdf(value), /REPORT_PDF_UNSUPPORTED_TEXT/);
});
test("malformed dates, currencies, identity, money and source overflow fail visibly", async () => {
  const { renderIncomeCsv, renderBasCsv } = await renderer();
  for (const change of [
    (v: ReturnType<typeof income>) => {
      v.summary.periodStart.day = 32;
    },
    (v: ReturnType<typeof income>) => {
      v.summary.businessIncome.currencyCode = "USD";
    },
    (v: ReturnType<typeof income>) => {
      v.summary.organisationId = id(77);
    },
    (v: ReturnType<typeof income>) => {
      v.summary.businessIncome.minorUnits = 9223372036854775808n;
    },
  ]) {
    const value = income();
    change(value);
    assert.throws(() => renderIncomeCsv(value), /INVALID_REPORT_SNAPSHOT/);
  }
  assert.throws(() => renderBasCsv(bas(1001)), /REPORT_SOURCE_LIMIT/);
});
test("both actual PDFs retain report totals and BAS source headers repeat across pages", async () => {
  const { renderIncomePdf, renderBasPdf } = await renderer();
  const i = income();
  i.organisation.legalName = "Gamma Systems Pty Ltd";
  const incomeText = pdfStreams(await renderIncomePdf(i));
  assert.match(incomeText, /90071992547409\.93/);
  assert.match(incomeText, /Financial revision 4/);
  const b = bas(35);
  b.organisation.legalName = "Gamma Systems Pty Ltd";
  b.workpaper.sources.forEach((source) => {
    source.supplierName = "Supplier";
  });
  const bytes = await renderBasPdf(b),
    text = pdfStreams(bytes);
  assert.match(text, /Draft.*not lodged/);
  assert.match(text, /-5\.00/);
  assert.ok((text.match(/Tax date/g) ?? []).length >= 2);
  assert.ok((text.match(/Document/g) ?? []).length >= 2);
  assert.match(text, /INV-001/);
});

test("CSV neutralises every formula prefix through whitespace and controls while keeping numeric negatives exact", async () => {
  const { renderBasCsv, renderIncomeCsv } = await renderer();
  for (const prefix of ["", " ", "\t", "\r\n", "\u0001", "\u200b"]) {
    for (const operator of ["=", "+", "-", "@"]) {
      const value = bas();
      const source = value.workpaper.sources[0];
      assert.ok(source);
      source.supplierName = `${prefix}${operator}SUM(1,2)`;
      const rows = csv(renderBasCsv(value)),
        header = rows[0];
      assert.ok(header);
      assert.equal(
        rows.find((row) => row[0] === "purchases")?.[header.indexOf("party")],
        `'${source.supplierName}`,
      );
    }
  }
  const value = income();
  value.summary.businessIncome.minorUnits = -1n;
  assert.equal(
    csv(renderIncomeCsv(value)).find((row) => row[0] === "business_income")?.[1],
    "-0.01",
  );
});

test("byte bounds reject oversized output without truncation and accept the exact 16 MiB limit", async () => {
  const { boundedReportBytes, MAX_REPORT_BYTES } = await renderer();
  assert.equal(boundedReportBytes(new Uint8Array(MAX_REPORT_BYTES)).length, MAX_REPORT_BYTES);
  assert.throws(
    () => boundedReportBytes(new Uint8Array(MAX_REPORT_BYTES + 1)),
    /REPORT_SIZE_LIMIT/,
  );
  assert.throws(() => boundedReportBytes(new Uint8Array()), /REPORT_RENDER_FAILED/);
});

test("saved workpaper creation time retains full nanosecond precision", async () => {
  const { renderBasCsv } = await renderer();
  const value = bas(0);
  value.workpaper.createdAt.nanos = 123456789;
  assert.match(
    csv(renderBasCsv(value)).find((row) => row[1] === "created_at")?.[2] ?? "",
    /\.123456789Z$/,
  );
});

test("BAS rejects invalid money, currencies, source identity, hash, basis and bounds", async () => {
  const { renderBasCsv } = await renderer();
  for (const change of [
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.sources).gross.minorUnits = -1n;
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.salesSources).gst.minorUnits = -1n;
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.sources).gstCredit.minorUnits = 11001n;
    },
    (v: ReturnType<typeof bas>) => {
      v.workpaper.gstBasis = 0;
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.sources).gross.minorUnits = 9223372036854775808n;
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.salesSources).gst.currencyCode = "USD";
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.salesSources).sourceHash = new Uint8Array(31);
    },
    (v: ReturnType<typeof bas>) => {
      first(v.workpaper.salesSources).invoiceId = "invalid";
    },
    (v: ReturnType<typeof bas>) => {
      v.workpaper.gstBasis = 3;
    },
    (v: ReturnType<typeof bas>) => {
      v.workpaper.createdAt.nanos = 1000000000;
    },
  ]) {
    const value = bas();
    change(value);
    assert.throws(() => renderBasCsv(value), /INVALID_REPORT_SNAPSHOT/);
  }
});

test("maximum BAS PDF renders all 1000 sales and 1000 purchases with intact dates and cents", async () => {
  const { renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const value = bas(1000);
  value.organisation.legalName = "Gamma Systems Pty Ltd";
  value.workpaper.salesSources.forEach((source, index) => {
    source.invoiceNumber = `SALE${String(index).padStart(4, "0")}`;
    source.customerName = "Customer";
  });
  value.workpaper.sources.forEach((source, index) => {
    source.invoiceNumber = `PURCHASE${String(index).padStart(4, "0")}`;
    source.supplierName = "Supplier";
  });
  const bytes = await renderBasPdf(value);
  const loading = getDocument({
    data: new Uint8Array(bytes),
    useSystemFonts: false,
    useWorkerFetch: false,
    verbosity: 0,
  });
  const document = await loading.promise;
  try {
    let content = "",
      dateCount = 0,
      salesAmountCount = 0;
    for (let number = 1; number <= document.numPages; number++) {
      const page = await document.getPage(number),
        text = await page.getTextContent();
      for (const item of text.items)
        if ("str" in item) {
          content += `${item.str}\n`;
          if (item.str === "2026-10-01") dateCount++;
          if (item.str === "55.00") salesAmountCount++;
        }
      page.cleanup();
    }
    assert.ok(document.numPages > 2);
    for (let index = 0; index < 1000; index++) {
      assert.ok(content.includes(`SALE${String(index).padStart(4, "0")}`), `missing sale ${index}`);
      assert.ok(
        content.includes(`PURCHASE${String(index).padStart(4, "0")}`),
        `missing purchase ${index}`,
      );
    }
    assert.ok(dateCount >= 2000, `${dateCount} intact source dates`);
    assert.equal(salesAmountCount, 1000);
    assert.ok((content.match(/Tax date/g) ?? []).length > 2);
    assert.ok((content.match(/Document date/g) ?? []).length > 2);
  } finally {
    await loading.destroy();
  }
});

test("BAS source dates and full int64 cents remain intact beside long unbroken party identities", async () => {
  const { renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  for (const [partyLength, gross] of [
    [30, 5500n],
    [200, 9223372036854775807n],
  ] as const) {
    const value = bas();
    value.organisation.legalName = "Gamma Systems Pty Ltd";
    const sale = first(value.workpaper.salesSources),
      purchase = first(value.workpaper.sources);
    sale.customerName = "W".repeat(partyLength);
    sale.taxDate.day = 2;
    sale.gross.minorUnits = gross;
    purchase.supplierName = "W".repeat(256);
    purchase.invoiceNumber = "P".repeat(128);
    purchase.documentDate.day = 3;
    purchase.gross.minorUnits = gross;
    if (gross === 9223372036854775807n) {
      sale.gst.minorUnits = gross;
      purchase.gstCredit.minorUnits = gross;
    }
    const loading = getDocument({
      data: new Uint8Array(await renderBasPdf(value)),
      useSystemFonts: false,
      useWorkerFetch: false,
      verbosity: 0,
    });
    const document = await loading.promise;
    try {
      const items: { text: string; x: number; y: number; width: number; page: number }[] = [];
      for (let number = 1; number <= document.numPages; number++) {
        const page = await document.getPage(number),
          text = await page.getTextContent();
        for (const item of text.items)
          if ("str" in item) {
            items.push({
              text: item.str,
              x: item.transform[4],
              y: item.transform[5],
              width: item.width,
              page: number,
            });
            if (item.str.trim()) {
              assert.ok(
                item.transform[4] >= 39.99,
                `text crosses the left page margin: ${item.str}`,
              );
              assert.ok(
                item.transform[4] + item.width <= 555.29,
                `text crosses the right page margin: ${item.str}`,
              );
            }
          }
        page.cleanup();
      }
      const strings = items.map((item) => item.text);
      assert.ok(
        strings.includes("2026-10-02"),
        `sales date split with ${partyLength} character party`,
      );
      assert.ok(
        strings.includes("2026-10-03"),
        `purchase date split with ${partyLength} character party`,
      );
      const exact = gross === 5500n ? "55.00" : "92233720368547758.07";
      const grossCells = items.filter((item) => item.text === exact);
      assert.equal(
        grossCells.length,
        gross === 5500n ? 2 : 4,
        `${exact} source amount cells split with ${partyLength} character party`,
      );
      for (const date of items.filter((item) => ["2026-10-02", "2026-10-03"].includes(item.text))) {
        const columns = items
          .filter((item) => item.page === date.page && item.y === date.y && item.text.trim())
          .sort((left, right) => left.x - right.x);
        assert.equal(columns.length, 3, "source date, gross and GST share one row");
        for (let column = 1; column < columns.length; column++) {
          const previous = columns[column - 1],
            current = columns[column];
          assert.ok(previous && current);
          assert.ok(
            previous.x + previous.width < current.x,
            "source numeric cells do not overlap adjacent columns",
          );
        }
      }
      if (gross === 5500n) assert.ok(strings.includes("5.00"), "GST must remain intact");
      assert.equal(
        strings
          .map((text) => text.replace(/^(Customer|Supplier): /u, ""))
          .filter((text) => /^W+$/u.test(text))
          .join("").length,
        partyLength + 256,
        "all party glyphs retained",
      );
      assert.equal(
        strings.filter((text) => /^P+$/u.test(text)).join("").length,
        128,
        "all invoice number glyphs retained",
      );
    } finally {
      await loading.destroy();
    }
  }
});

test("long organisation names keep PDF metric labels and exact amounts readable", async () => {
  const { renderIncomePdf, renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const summary = income(),
    workpaper = bas();
  for (const value of [summary, workpaper]) {
    value.organisation.legalName = "W".repeat(256);
    value.organisation.displayName = "M".repeat(256);
  }
  for (const bytes of [await renderIncomePdf(summary), await renderBasPdf(workpaper)]) {
    const loading = getDocument({
      data: new Uint8Array(bytes),
      verbosity: 0,
      useSystemFonts: false,
      useWorkerFetch: false,
    });
    const document = await loading.promise;
    try {
      assert.ok(
        document.numPages <= 3,
        "names must not squeeze metric labels into a vertical column",
      );
      const strings: string[] = [];
      for (let number = 1; number <= document.numPages; number++) {
        const page = await document.getPage(number),
          text = await page.getTextContent();
        for (const item of text.items)
          if ("str" in item) {
            strings.push(item.str);
            if (item.str.trim()) {
              assert.ok(item.transform[4] >= 39.99, "name context stays within the left margin");
              assert.ok(
                item.transform[4] + item.width <= 555.29,
                "name context stays within the right margin",
              );
            }
          }
        page.cleanup();
      }
      assert.ok(strings.includes("organisation abn"), "metric label remains on one line");
      assert.ok(strings.includes("51824753556"), "metric value remains on one line");
      assert.ok(
        strings.includes(strings.includes("Metric") ? "$90071992547409.93" : "$55.00"),
        "exact amount remains on one line",
      );
      assert.ok(strings.join("").includes("M".repeat(256)), "all display name glyphs retained");
    } finally {
      await loading.destroy();
    }
  }
});

test("each BAS source keeps its exact amount and retained identity together across page breaks", async () => {
  const { renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const value = bas(35);
  value.organisation.legalName = "Gamma Systems Pty Ltd";
  value.workpaper.salesSources.forEach((source, index) => {
    source.invoiceNumber = `SALESOURCE${String(index).padStart(4, "0")}`;
    source.gross.minorUnits = 10000n + BigInt(index);
  });
  value.workpaper.sources.forEach((source, index) => {
    source.invoiceNumber = `PURCHASESOURCE${String(index).padStart(4, "0")}`;
    source.gross.minorUnits = 20000n + BigInt(index);
  });
  const loading = getDocument({
    data: new Uint8Array(await renderBasPdf(value)),
    verbosity: 0,
    useSystemFonts: false,
    useWorkerFetch: false,
  });
  const document = await loading.promise;
  try {
    const pages: string[][] = [];
    for (let number = 1; number <= document.numPages; number++) {
      const page = await document.getPage(number),
        text = await page.getTextContent();
      pages.push(text.items.flatMap((item) => ("str" in item ? [item.str] : [])));
      page.cleanup();
    }
    for (const [prefix, major] of [
      ["SALESOURCE", "100"],
      ["PURCHASESOURCE", "200"],
    ] as const) {
      for (let index = 0; index < 35; index++) {
        const page = pages.find((items) =>
          items.includes(`${prefix}${String(index).padStart(4, "0")}`),
        );
        assert.ok(
          page?.includes(`${major}.${String(index).padStart(2, "0")}`),
          `${prefix}${index} identity separated from amount`,
        );
      }
    }
  } finally {
    await loading.destroy();
  }
});

test("PDF text preserves Latin ligature sequences in every retained identity and provenance field", async () => {
  const { renderIncomePdf, renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const identity = "018f0000-0000-7ff1-8000-000000000004";
  const sourceHash = "ff".repeat(32);
  const ordinary = "ff fi fl ffi ffl office affinity coffee";
  const summary = income(),
    workpaper = bas();
  for (const value of [summary, workpaper]) {
    value.organisation.id = identity;
    value.organisation.legalName = ordinary;
    value.organisation.displayName = ordinary;
  }
  summary.summary.organisationId = identity;
  workpaper.workpaper.organisationId = identity;
  workpaper.workpaper.id = "018f0000-0000-7ff1-8000-000000000005";
  workpaper.workpaper.sourceFingerprint = new Uint8Array(32).fill(255);
  const sale = first(workpaper.workpaper.salesSources),
    purchase = first(workpaper.workpaper.sources);
  sale.invoiceId = "018f0000-0000-7ff1-8000-000000000006";
  sale.paymentId = "018f0000-0000-7ff1-8000-000000000007";
  sale.sourceHash = new Uint8Array(32).fill(255);
  sale.customerName = ordinary;
  sale.invoiceNumber = ordinary;
  purchase.documentId = "018f0000-0000-7ff1-8000-000000000008";
  purchase.supplierName = ordinary;
  purchase.invoiceNumber = ordinary;
  workpaper.workpaper.reviewWarnings = [ordinary];
  for (const [kind, bytes] of [
    ["income", await renderIncomePdf(summary)],
    ["bas", await renderBasPdf(workpaper)],
  ] as const) {
    const loading = getDocument({
      data: new Uint8Array(bytes),
      verbosity: 0,
      useSystemFonts: false,
      useWorkerFetch: false,
    });
    const document = await loading.promise;
    try {
      let content = "";
      for (let number = 1; number <= document.numPages; number++) {
        const page = await document.getPage(number),
          text = await page.getTextContent();
        content += text.items.flatMap((item) => ("str" in item ? [item.str] : [])).join("\n");
        page.cleanup();
      }
      assert.ok(content.includes(identity), `${kind} retains the exact organisation identifier`);
      assert.ok(content.includes(ordinary), `${kind} retains every ordinary text glyph`);
      assert.equal(
        content.split(ordinary).length - 1,
        kind === "bas" ? 8 : 3,
        `${kind} retains every ordinary text field`,
      );
      if (kind === "income")
        assert.ok(content.includes("financial revision"), "financial revision label remains exact");
      if (kind === "bas") {
        for (const retained of [
          workpaper.workpaper.id,
          sale.invoiceId,
          sale.paymentId,
          purchase.documentId,
          sourceHash,
          "source fingerprint",
        ]) {
          assert.ok(content.includes(retained), `BAS retains exact provenance ${retained}`);
        }
      }
    } finally {
      await loading.destroy();
    }
  }
});

test("combining marks remain exact in CSV and fail visibly in PDF rather than losing characters", async () => {
  const { renderIncomePdf, renderBasPdf, renderIncomeCsv, renderBasCsv } = await renderer();
  const supported = "Cafe\u0301 Δοκιμη\u0301 Привет A\u0308 Ε\u0301 И\u0306";
  const summary = income(),
    workpaper = bas();
  for (const value of [summary, workpaper]) {
    value.organisation.legalName = supported;
    value.organisation.displayName = supported;
  }
  first(workpaper.workpaper.salesSources).customerName = supported;
  first(workpaper.workpaper.sources).supplierName = supported;
  workpaper.workpaper.reviewWarnings = [supported];
  assert.equal(
    csv(renderIncomeCsv(summary)).find((row) => row[0] === "organisation_legal_name")?.[1],
    supported,
  );
  assert.equal(
    csv(renderBasCsv(workpaper)).find((row) => row[1] === "organisation_legal_name")?.[2],
    supported,
  );
  await assert.rejects(() => renderIncomePdf(summary), /REPORT_PDF_UNSUPPORTED_TEXT/);
  await assert.rejects(() => renderBasPdf(workpaper), /REPORT_PDF_UNSUPPORTED_TEXT/);
  for (const mark of ["\u0301", "\u20dd", "\u034f", "\ufe00"]) {
    const value = income();
    value.organisation.legalName = `Retained ${mark}`;
    assert.equal(
      csv(renderIncomeCsv(value)).find((row) => row[0] === "organisation_legal_name")?.[1],
      `Retained ${mark}`,
    );
    await assert.rejects(() => renderIncomePdf(value), /REPORT_PDF_UNSUPPORTED_TEXT/);
  }
});

test("report fonts preserve precomposed Latin, Greek and Cyrillic text exactly", async () => {
  const { renderIncomePdf, renderBasPdf } = await renderer();
  const { getDocument } = await import("pdfjs-dist/legacy/build/pdf.mjs");
  const supported = "Élodie Δοκιμή Привет Ä Έ Й K K Ω Ω Å Å";
  const summary = income(),
    workpaper = bas();
  for (const value of [summary, workpaper]) {
    value.organisation.legalName = supported;
    value.organisation.displayName = supported;
  }
  first(workpaper.workpaper.salesSources).customerName = supported;
  first(workpaper.workpaper.sources).supplierName = supported;
  workpaper.workpaper.reviewWarnings = [supported];
  for (const bytes of [await renderIncomePdf(summary), await renderBasPdf(workpaper)]) {
    const loading = getDocument({
      data: new Uint8Array(bytes),
      verbosity: 0,
      useSystemFonts: false,
      useWorkerFetch: false,
    });
    const document = await loading.promise;
    try {
      let content = "";
      for (let number = 1; number <= document.numPages; number++) {
        const page = await document.getPage(number),
          text = await page.getTextContent({ disableNormalization: true });
        content += text.items.flatMap((item) => ("str" in item ? [item.str] : [])).join("\n");
        page.cleanup();
      }
      assert.ok(
        content.includes(supported),
        "every supported precomposed code point remains exact",
      );
    } finally {
      await loading.destroy();
    }
  }
});

test("format controls stay exact in CSV and fail visibly in PDF", async () => {
  const { renderIncomePdf, renderBasPdf, renderIncomeCsv, renderBasCsv } = await renderer();
  for (const character of ["\u200b", "\u200c", "\u200d", "\u2060", "\u2066", "\u2069", "\ufeff"]) {
    const retained = `BEFORE${character}AFTER`,
      summary = income(),
      workpaper = bas();
    summary.organisation.legalName = retained;
    const sale = first(workpaper.workpaper.salesSources);
    sale.customerName = retained;
    assert.equal(
      csv(renderIncomeCsv(summary)).find((row) => row[0] === "organisation_legal_name")?.[1],
      retained,
    );
    const records = csv(renderBasCsv(workpaper)),
      header = first(records);
    assert.equal(records.find((row) => row[0] === "sales")?.[header.indexOf("party")], retained);
    await assert.rejects(() => renderIncomePdf(summary), /REPORT_PDF_UNSUPPORTED_TEXT/);
    await assert.rejects(() => renderBasPdf(workpaper), /REPORT_PDF_UNSUPPORTED_TEXT/);
  }
});
