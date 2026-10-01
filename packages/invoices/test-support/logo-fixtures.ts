import { readFileSync } from "node:fs";
import { deflateSync } from "node:zlib";

export function pngChunk(type: string, data: Uint8Array): Buffer {
  const chunk = Buffer.alloc(data.length + 12);
  chunk.writeUInt32BE(data.length, 0);
  chunk.write(type, 4, 4, "ascii");
  chunk.set(data, 8);
  let crc = 0xffffffff;
  for (const byte of chunk.subarray(4, -4)) {
    crc ^= byte;
    for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ (crc & 1 ? 0xedb88320 : 0);
  }
  chunk.writeUInt32BE((crc ^ 0xffffffff) >>> 0, chunk.length - 4);
  return chunk;
}

/** A real transparent company mark, encoded without an image dependency. */
export function pngLogo(width = 240, height = 96) {
  const header = Buffer.alloc(13);
  header.writeUInt32BE(width, 0);
  header.writeUInt32BE(height, 4);
  header[8] = 8;
  header[9] = 6;
  const raster = Buffer.alloc(height * (width * 4 + 1));
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      const offset = y * (width * 4 + 1) + 1 + x * 4;
      raster[offset] = 32;
      raster[offset + 1] = 78;
      raster[offset + 2] = 67;
      raster[offset + 3] =
        (x >= 20 && x < 76 && y >= 20 && y < 32) ||
        (x >= 40 && x < 56 && y >= 20 && y < 76) ||
        (x >= 100 && x < 220 && y >= 28 && y < 42) ||
        (x >= 100 && x < 196 && y >= 56 && y < 68)
          ? 255
          : 0;
    }
  }
  const bytes = Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 13, 10, 26, 10]),
    pngChunk("IHDR", header),
    pngChunk("IDAT", deflateSync(raster)),
    pngChunk("IEND", new Uint8Array()),
  ]);
  return { mimeType: "image/png" as const, dataBase64: bytes.toString("base64"), width, height };
}

/** The same company mark as a real baseline JPEG, with no EXIF or remote source. */
export function jpegLogo() {
  return {
    mimeType: "image/jpeg" as const,
    width: 240,
    height: 96,
    dataBase64: readFileSync(new URL("./company-logo.jpeg", import.meta.url)).toString("base64"),
  };
}
