import { createHmac } from "node:crypto";

const BASE32_ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";

function decodeBase32(secret: string): Buffer {
  if (!/^[A-Z2-7]{32}$/.test(secret)) throw new Error("INVALID_TOTP_SECRET");
  let bits = "";
  for (const character of secret) {
    bits += BASE32_ALPHABET.indexOf(character).toString(2).padStart(5, "0");
  }
  return Buffer.from(
    Array.from({ length: Math.floor(bits.length / 8) }, (_, index) =>
      Number.parseInt(bits.slice(index * 8, index * 8 + 8), 2),
    ),
  );
}

export function generateTotp(secret: string, timeMs = Date.now(), digits = 6): string {
  if (!Number.isSafeInteger(timeMs) || timeMs < 0 || (digits !== 6 && digits !== 8)) {
    throw new Error("INVALID_TOTP_INPUT");
  }
  const key = decodeBase32(secret);
  const counter = Buffer.alloc(8);
  counter.writeBigUInt64BE(BigInt(Math.floor(timeMs / 30_000)));
  const digest = createHmac("sha1", key).update(counter).digest();
  key.fill(0);
  counter.fill(0);
  const offset = (digest.at(-1) ?? 0) & 0x0f;
  const binary = (digest.readUInt32BE(offset) & 0x7fffffff) % 10 ** digits;
  digest.fill(0);
  return String(binary).padStart(digits, "0");
}
