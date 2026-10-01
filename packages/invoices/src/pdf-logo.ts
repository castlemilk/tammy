import { InvoiceValidationError } from "./model.ts";

/** Immutable local image bytes supplied by the saved invoice's asset snapshot. */
export interface InvoicePdfLogo {
  mimeType: "image/png" | "image/jpeg";
  dataBase64: string;
  width: number;
  height: number;
}

const MAX_BYTES = 1024 * 1024;
const MAX_DIMENSION = 4096;
const MAX_PIXELS = 4_000_000;
const LOGO_KEYS = new Set(["mimeType", "dataBase64", "width", "height"]);
const BASE64 = /^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/;

function invalid(): never {
  throw new InvoiceValidationError(
    "logo: must be a valid local PNG or JPEG no larger than 1 MiB, 4096 pixels per side and 4 million pixels",
  );
}

/** Browser-safe structural preflight. The core also fully decodes retained assets;
 * PDF rendering verifies the image was embedded because Forme can silently omit
 * invalid raster data. No source URL or filesystem path is accepted here.
 */
export function validateInvoicePdfLogo(value: unknown): InvoicePdfLogo {
  if (!value || typeof value !== "object" || Array.isArray(value)) invalid();
  const input = value as Record<string, unknown>;
  if (Object.keys(input).some((key) => !LOGO_KEYS.has(key))) invalid();
  const { mimeType, dataBase64, width, height } = input;
  if (
    (mimeType !== "image/png" && mimeType !== "image/jpeg") ||
    typeof dataBase64 !== "string" ||
    dataBase64.length === 0 ||
    dataBase64.length > Math.ceil(MAX_BYTES / 3) * 4 ||
    !BASE64.test(dataBase64) ||
    typeof width !== "number" ||
    typeof height !== "number" ||
    !Number.isSafeInteger(width) ||
    !Number.isSafeInteger(height) ||
    width < 1 ||
    height < 1 ||
    width > MAX_DIMENSION ||
    height > MAX_DIMENSION ||
    width * height > MAX_PIXELS
  )
    invalid();
  let binary: string;
  try {
    binary = atob(dataBase64);
  } catch {
    invalid();
  }
  if (binary.length < 1 || binary.length > MAX_BYTES || btoa(binary) !== dataBase64) invalid();
  const bytes = Uint8Array.from(binary, (character) => character.charCodeAt(0));
  const dimensions = mimeType === "image/png" ? pngDimensions(bytes) : jpegDimensions(bytes);
  if (dimensions.width !== width || dimensions.height !== height) invalid();
  return { mimeType, dataBase64, width, height };
}

function crc32(bytes: Uint8Array): number {
  let crc = 0xffffffff;
  for (const byte of bytes) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0);
  }
  return (crc ^ 0xffffffff) >>> 0;
}

function pngDimensions(bytes: Uint8Array): { width: number; height: number } {
  const signature = [0x89, 0x50, 0x4e, 0x47, 13, 10, 26, 10];
  if (signature.some((byte, index) => bytes[index] !== byte)) invalid();
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let position = 8;
  let dimensions: { width: number; height: number } | undefined;
  let colorType = -1;
  let seenPalette = false;
  let seenData = false;
  let endedData = false;
  let dataLength = 0;
  while (position < bytes.length) {
    if (bytes.length - position < 12) invalid();
    const length = view.getUint32(position);
    if (length > bytes.length - position - 12) invalid();
    const typeStart = position + 4;
    const dataStart = typeStart + 4;
    const dataEnd = dataStart + length;
    const end = dataEnd + 4;
    const type = String.fromCharCode(...bytes.subarray(typeStart, dataStart));
    if (!/^[A-Za-z]{2}[A-Z][A-Za-z]$/.test(type)) invalid();
    if (crc32(bytes.subarray(typeStart, dataEnd)) !== view.getUint32(dataEnd)) invalid();
    if (!dimensions && type !== "IHDR") invalid();
    if (type === "IHDR") {
      if (dimensions || length !== 13 || position !== 8) invalid();
      const bitDepth = bytes[dataStart + 8];
      colorType = bytes[dataStart + 9] ?? -1;
      const depths: Record<number, readonly number[]> = {
        0: [1, 2, 4, 8, 16],
        2: [8, 16],
        3: [1, 2, 4, 8],
        4: [8, 16],
        6: [8, 16],
      };
      if (
        bitDepth === undefined ||
        !depths[colorType]?.includes(bitDepth) ||
        bytes[dataStart + 10] !== 0 ||
        bytes[dataStart + 11] !== 0 ||
        (bytes[dataStart + 12] !== 0 && bytes[dataStart + 12] !== 1)
      )
        invalid();
      dimensions = { width: view.getUint32(dataStart), height: view.getUint32(dataStart + 4) };
    } else if (type === "PLTE") {
      if (
        seenPalette ||
        seenData ||
        colorType === 0 ||
        colorType === 4 ||
        length === 0 ||
        length > 768 ||
        length % 3 !== 0
      )
        invalid();
      seenPalette = true;
    } else if (type === "IDAT") {
      if (endedData || (colorType === 3 && !seenPalette)) invalid();
      seenData = true;
      dataLength += length;
    } else if (type === "IEND") {
      if (!dimensions || !seenData || dataLength === 0 || length !== 0 || end !== bytes.length)
        invalid();
      return dimensions;
    } else {
      if (type[0] === type[0]?.toUpperCase() || ["acTL", "fcTL", "fdAT"].includes(type)) invalid();
      if (seenData) endedData = true;
    }
    position = end;
  }
  invalid();
}

function jpegDimensions(bytes: Uint8Array): { width: number; height: number } {
  if (bytes.length < 4 || bytes[0] !== 0xff || bytes[1] !== 0xd8) invalid();
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let position = 2;
  let inScan = false;
  let seenScan = false;
  let scanDataBytes = 0;
  let dimensions: { width: number; height: number } | undefined;
  while (position < bytes.length) {
    if (inScan && bytes[position] !== 0xff) {
      scanDataBytes++;
      position++;
      continue;
    }
    if (bytes[position] !== 0xff) invalid();
    while (position < bytes.length && bytes[position] === 0xff) position++;
    if (position >= bytes.length) invalid();
    const marker = bytes[position++];
    if (marker === undefined) invalid();
    if (inScan && marker === 0x00) {
      scanDataBytes++;
      continue;
    }
    if (inScan && marker >= 0xd0 && marker <= 0xd7) continue;
    if (inScan && scanDataBytes === 0) invalid();
    inScan = false;
    if (marker === 0xd9) {
      if (!dimensions || !seenScan || position !== bytes.length) invalid();
      return dimensions;
    }
    if (marker === 0x00 || marker === 0xd8 || (marker >= 0xd0 && marker <= 0xd7)) invalid();
    if (marker === 0x01) continue;
    if (marker < 0xc0 || bytes.length - position < 2) invalid();
    const length = view.getUint16(position);
    if (length < 2 || length > bytes.length - position) invalid();
    if ([0xc0, 0xc1, 0xc2].includes(marker)) {
      if (dimensions || length < 8 || bytes[position + 2] !== 8) invalid();
      const components = bytes[position + 7];
      if (!components || ![1, 3, 4].includes(components) || length !== 8 + components * 3)
        invalid();
      dimensions = { width: view.getUint16(position + 5), height: view.getUint16(position + 3) };
    } else if (marker === 0xda) {
      const components = bytes[position + 2];
      if (!dimensions || !components || components > 4 || length !== 6 + components * 2) invalid();
      seenScan = true;
      inScan = true;
      scanDataBytes = 0;
    } else if (![0xc4, 0xdb, 0xdd, 0xfe].includes(marker) && !(marker >= 0xe0 && marker <= 0xef)) {
      invalid();
    }
    position += length;
  }
  invalid();
}
