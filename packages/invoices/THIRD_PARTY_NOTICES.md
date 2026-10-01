# pdfcn attribution

The invoice PDF template and the simplified Forme primitives in `src/pdf.ts` and
`src/pdfcn/components.ts` are adapted from [pdfcn](https://github.com/shadcn-labs/pdfcn),
commit `39c75c1abbbad7b89ad1d8d3ea740ef635818a4b` (retrieved 30 September 2026):

- `apps/web/registry/bases/forme/blocks/invoice-modern/invoice-modern.tsx`
- `apps/web/registry/bases/forme/components/text/text.tsx`
- `apps/web/registry/bases/forme/components/page-header/page-header.tsx`
- `apps/web/registry/bases/forme/components/page-footer/page-footer.tsx`
- `apps/web/registry/bases/forme/components/table/table.tsx`
- `apps/web/registry/bases/forme/components/key-value/key-value.tsx`

Tammy replaces the sample data and floating-point calculations with validated AUD
minor units, Australian GST treatments and private cost calculations. The copied
primitive subset uses a fixed offline theme with Forme's bundled Noto Sans regular
and bold fonts. Native Forme tables repeat
headers on page breaks; fixed footers use actual dynamic page counts rather than
the upstream sample's fixed “Page 1 of 1”. Drafts are explicitly marked. Internal
costs, margins and source document IDs are not included in the customer PDF.

The font coverage guard is derived from the intersection of the regular and bold
fonts in Forme v0.25.0, commit `a16ca5b54f6854d4f89b8e96377f64cc1b8b3b52`,
`engine/fonts/NotoSans-Regular.ttf` and `engine/fonts/NotoSans-Bold.ttf`. It supports
Latin accents, Greek and Cyrillic plus the font's symbols. Unsupported scripts
(including CJK) and emoji cause a clear export error instead of silently replacing
invoice identities. Saved invoice text is never altered. No font or image network
requests occur while exporting. Noto Sans 2.015 is supplied by the Forme WASM
dependency; see `OFL-NOTO-SANS.txt` for its licence.

The renderer-safe `pdf-policy` entry point exposes the same font-coverage preflight
as PDF rendering, so issue confirmation can prevent an immutable invoice whose
text cannot be exported. General invoice validation remains separate.

## MIT License

Copyright (c) 2026 Shadcn Labs

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:
The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.
THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
