import { InvoiceValidationError } from "../model.ts";

/** Intersection of Regular/Bold cmap coverage from Forme v0.25.0's packaged
 * Noto Sans fonts (a16ca5b54f6854d4f89b8e96377f64cc1b8b3b52, engine/fonts).
 * Forme's glyph metrics cover BMP codepoints only, so supplementary ranges
 * are deliberately excluded. Font upgrades must revalidate this allow-list.
 */
const NOTO_SANS_RANGES: ReadonlyArray<readonly [number, number]> = [
  [32, 126],
  [160, 887],
  [890, 895],
  [900, 906],
  [908, 908],
  [910, 929],
  [931, 993],
  [1008, 1327],
  [4347, 4347],
  [6832, 6848],
  [6853, 6853],
  [6855, 6862],
  [7296, 7304],
  [7424, 7673],
  [7675, 7957],
  [7960, 7965],
  [7968, 8005],
  [8008, 8013],
  [8016, 8023],
  [8025, 8025],
  [8027, 8027],
  [8029, 8029],
  [8031, 8061],
  [8064, 8116],
  [8118, 8132],
  [8134, 8147],
  [8150, 8155],
  [8157, 8175],
  [8178, 8180],
  [8182, 8190],
  [8192, 8292],
  [8294, 8305],
  [8308, 8334],
  [8336, 8348],
  [8352, 8384],
  [8432, 8432],
  [8448, 8543],
  [8579, 8580],
  [8585, 8585],
  [8722, 8722],
  [9676, 9676],
  [11360, 11391],
  [11744, 11869],
  [42560, 42655],
  [42752, 42954],
  [42960, 42961],
  [42963, 42963],
  [42965, 42969],
  [42994, 43007],
  [43310, 43310],
  [43824, 43883],
  [64256, 64262],
  [65024, 65024],
  [65056, 65071],
  [65279, 65279],
  [65532, 65533],
];

export class InvoicePdfUnsupportedTextError extends InvoiceValidationError {
  readonly field: string;
  readonly codePoint: number;

  constructor(field: string, codePoint: number) {
    super(
      `${field}: the PDF font does not support U+${codePoint.toString(16).toUpperCase().padStart(4, "0")}. Use supported Latin, Greek or Cyrillic text for export; the saved invoice has not been changed.`,
    );
    this.name = "InvoicePdfUnsupportedTextError";
    this.field = field;
    this.codePoint = codePoint;
  }
}

export function assertInvoicePdfTextSupported(value: string, field: string): void {
  for (const character of value) {
    const codePoint = character.codePointAt(0);
    if (codePoint === undefined || codePoint === 9 || codePoint === 10 || codePoint === 13)
      continue;
    if (!NOTO_SANS_RANGES.some(([start, end]) => codePoint >= start && codePoint <= end)) {
      throw new InvoicePdfUnsupportedTextError(field, codePoint);
    }
  }
}
