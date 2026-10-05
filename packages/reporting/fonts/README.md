# Report fonts

The original Noto Sans 2.015 Regular and Bold files are copied from Forme v0.25.0,
commit `a16ca5b54f6854d4f89b8e96377f64cc1b8b3b52`:

- [Regular](https://github.com/formepdf/forme/blob/a16ca5b54f6854d4f89b8e96377f64cc1b8b3b52/engine/fonts/NotoSans-Regular.ttf), SHA-256 `478c558ea716033cd60c03438f628dfa75694dcf6b5f6d505a2f05fd2b4f3823`
- [Bold](https://github.com/formepdf/forme/blob/a16ca5b54f6854d4f89b8e96377f64cc1b8b3b52/engine/fonts/NotoSans-Bold.ttf), SHA-256 `1df075a380fc7cb898acf64c1f7b3b4dd780de3caa860178bf929de35817a913`

The fonts and generated derivatives are distributed under the accompanying
[SIL Open Font License](OFL-NOTO-SANS.txt).

Forme shapes Latin ligatures such as `ff` into one glyph but maps that glyph to
only its first character in the PDF's ToUnicode CMap. Reports must preserve exact
identifiers, hashes and ordinary text when copied or extracted. The generator
removes the GSUB substitution table from these otherwise identical fonts and
rebuilds the TrueType directory and checksum. Character coverage, glyph outlines,
metrics and GPOS positioning are retained. Explicit document font registration
uses the generated data URIs for all report text, including shared page primitives.
No font files or network resources are read during export.

The same Forme mapping defect affects canonical composition of combining marks,
even without GSUB. Unicode format controls can also be dropped or corrupt the
mapping of neighboring glyphs. Reporting PDF preflight therefore rejects Unicode
mark and format characters explicitly with the existing CSV alternative. It does not normalize
or alter saved text. CSV retains every original code point; precomposed Latin,
Greek and Cyrillic remain supported by PDF and have exact extraction regressions.
The shared invoice glyph coverage policy remains unchanged.

From the workspace root, regenerate with
`rtk proxy mise exec -- node packages/reporting/scripts/generate-fonts.mjs`.
Use `--check` to verify the committed modules without writing files. Font tests
verify the pinned original hashes, table preservation, TrueType checksums and
deterministic generation. Changing the original fonts requires revalidating the
shared PDF glyph coverage policy.
